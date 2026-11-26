package app

import (
	"errors"
	"fmt"
	"path/filepath"

	"github.com/bjornramberg/omakaiju/internal/fs"

	tea "charm.land/bubbletea/v2"
)

// moverFailures converts the entries a mover could not move into the
// failures a result reports.
//
// A move that cannot even start an entry — because the destination is the
// file itself, say — is reported the same way as one that failed halfway,
// so the user sees every entry that did not arrive in one place.
func moverFailures(copier *fs.Copier) []opFailure {
	if copier == nil {
		return nil
	}
	failures := copier.Failures()
	out := make([]opFailure, 0, len(failures))
	for _, f := range failures {
		out = append(out, opFailure{name: f.Name, path: f.Path, err: f.Err})
	}
	return out
}

// attachMetaWarnings reports the metadata a finished command could not
// carry. The command already ran to completion inside cmd; this only adds
// the warning count to its result and the details to the debug log, so a
// transfer that landed on a filesystem without metadata support says so
// instead of pretending it is exact.
func attachMetaWarnings(cmd tea.Cmd, warns *[]fs.MetaWarning) tea.Cmd {
	if cmd == nil || warns == nil {
		return cmd
	}
	return safeCmd("paste-warnings", func() tea.Msg {
		msg := cmd()
		m, ok := msg.(fileOpMsg)
		if !ok {
			return msg
		}
		m.unkeptMeta = countUnkept(*warns)
		debugLogWarnings("paste", warnErrors(*warns))
		return m
	})
}

// countUnkept counts the distinct destinations that landed without their
// metadata. One entry can warn twice — once for the mode, once for the
// times — but it is still one entry the result reports on.
func countUnkept(warns []fs.MetaWarning) int {
	seen := make(map[string]bool, len(warns))
	for _, w := range warns {
		seen[w.Path] = true
	}
	return len(seen)
}

// warnErrors renders warnings for the debug log, where the full chain is
// kept. The bottom bar only carries the count; this names each entry.
func warnErrors(warns []fs.MetaWarning) []error {
	out := make([]error, 0, len(warns))
	for _, w := range warns {
		out = append(out, fmt.Errorf("%s: %s not kept (%v)", w.Path, w.What, w.Err))
	}
	return out
}

// recordOpResult stores the outcome of a file operation. A failure goes into
// opErr, which the bottom bar shows and only a keypress clears; a success goes
// into opResult. Both live apart from loadErr so neither the pane reload that
// follows an operation nor a later listing failure can erase the other.
func (m *Model) recordOpResult(msg fileOpMsg) {
	m.opResult = ""
	// Any result ends the copy lifecycle, including a copier that never
	// started, so the retry snapshot must not outlive it. The file count was
	// set when the paste was dispatched; a simple copy that took the
	// blocking path reports through here without finishCopy having run.
	m.copyClipboard = nil
	m.copyFiles = 0
	m.copyReplaced = 0
	if len(msg.failures) > 0 {
		m.restoreSelection(msg)
		m.opErr = errors.New(msg.result())
		return
	}
	m.landAfterRemoval(msg)
	m.opErr = nil
	m.opResult = msg.result()
}

// landAfterRemoval keeps the cursor on the row a deleted or trashed entry
// occupied, so the listing does not jump back to the top. The entry that takes
// its place is selected; at the end of the listing the one before it is.
func (m *Model) landAfterRemoval(msg fileOpMsg) {
	if msg.succeeded != 1 || (msg.verb != "deleted" && msg.verb != "trashed") {
		return
	}
	pane := m.activePane
	files := m.visibleFiles(pane)
	cursor := m.rawCursor(pane)
	if cursor < 0 || cursor >= len(files) {
		// The cursor is not on an entry — it is on the ".." row, say — so
		// there is no row to keep. A panic here would discard the result of
		// an operation that has already happened on disk.
		return
	}
	// The operation's entry is the one that was under the cursor when it
	// started. If the user moved since, find it by name instead.
	if msg.name != "" && files[cursor].Name != msg.name {
		found := -1
		for i, f := range files {
			if f.Name == msg.name {
				found = i
				break
			}
		}
		if found < 0 {
			return
		}
		cursor = found
	}
	if cursor+1 < len(files) {
		m.setRestore(pane, files[cursor+1].Name)
	} else if cursor > 0 {
		m.setRestore(pane, files[cursor-1].Name)
	}
}

// restoreSelection puts back what a failed operation consumed: the marked
// entries it could not move, and the paste selection it used. The work that
// did land stays done; only what failed is offered for another try.
func (m *Model) restoreSelection(msg fileOpMsg) {
	if len(msg.marks) > 0 {
		paths := make([]string, 0, len(msg.failures))
		for _, f := range msg.failures {
			paths = append(paths, f.path)
		}
		m.restoreMarks(msg.marks, paths)
	}
	m.restoreClipboard(msg.clipboard)
}

// restoreMarks puts the marked entries named by paths back into the marked
// set. marks is the snapshot the transfer took, keyed by source path.
//
// The set is copied before it is extended. Model is copied by value on every
// update, but a map is a reference: writing into the model's map in place
// would survive a panic and be visible in the pre-update state Update
// restores.
func (m *Model) restoreMarks(marks map[string]fs.Entry, paths []string) {
	var next map[string]fs.Entry
	for _, path := range paths {
		entry, ok := marks[path]
		if !ok {
			continue
		}
		if next == nil {
			next = cloneMarked(m.marked)
		}
		next[entry.Path] = entry
	}
	if next != nil {
		m.marked = next
	}
}

// cloneMarked returns a marked set that can be changed without touching the
// caller's map.
func cloneMarked(marked map[string]fs.Entry) map[string]fs.Entry {
	out := make(map[string]fs.Entry, len(marked)+1)
	for path, entry := range marked {
		out[path] = entry
	}
	return out
}

// restoreClipboard re-arms a paste selection a transfer consumed. A selection
// the user staged after the transfer was dispatched is left alone.
func (m *Model) restoreClipboard(clipboard *Clipboard) {
	if clipboard != nil && !m.clipboardOn {
		m.clipboard = *clipboard
		m.clipboardOn = true
	}
}

// onParentRow reports whether the pane's cursor is on its ".." row.
//
// The operations that act on a file refuse it: the parent row is the way up,
// not an entry, and it has no index in the listing. Copying it would copy the
// directory the user is already in; deleting it would delete the very
// directory the pane is showing.
func (m Model) onParentRow(pane int) bool {
	return m.rawCursor(pane) == parentCursor
}

func (m Model) handleYank() (tea.Model, tea.Cmd) {
	if m.onParentRow(m.activePane) {
		m.opResult = "the parent directory cannot be copied"
		return m, nil
	}
	files := m.visibleFiles(m.activePane)
	cursor := m.rawCursor(m.activePane)
	if cursor < 0 || cursor >= len(files) {
		return m, nil
	}
	m.clipboard = Clipboard{
		Action: "copy",
		Files:  []fs.Entry{files[cursor]},
		Source: m.rawPath(m.activePane),
	}
	m.clipboardOn = true
	m.opResult = "yanked " + files[cursor].Name
	return m, nil
}

// handleMove toggles the cursor entry in the marked set. Pressing m again on
// an already-marked entry unmarks it, so several files or folders can be staged
// before moving them into the pane under the cursor with Shift+V.
func (m Model) handleMove() (tea.Model, tea.Cmd) {
	if m.onParentRow(m.activePane) {
		m.opResult = "the parent directory cannot be marked"
		return m, nil
	}
	files := m.visibleFiles(m.activePane)
	cursor := m.rawCursor(m.activePane)
	if cursor < 0 || cursor >= len(files) {
		return m, nil
	}

	entry := files[cursor]
	// Copy the set before changing it. Model is copied by value, but the map
	// is shared with the state Update would restore after a panic, so an
	// in-place mark or unmark would leak out of a failed update.
	m.marked = cloneMarked(m.marked)

	if _, exists := m.marked[entry.Path]; exists {
		delete(m.marked, entry.Path)
		m.opResult = fmt.Sprintf("unmarked %s (%d)", entry.Name, len(m.marked))
		// The status row names the marked set, so marking and unmarking
		// change how many rows the pane has for its listing.
		m.reconcileScroll(m.activePane)
		return m, nil
	}

	m.marked[entry.Path] = entry
	m.opResult = fmt.Sprintf("marked %s (%d)", entry.Name, len(m.marked))
	m.reconcileScroll(m.activePane)
	return m, nil
}

func (m Model) handleDelete() (tea.Model, tea.Cmd) {
	if m.onParentRow(m.activePane) {
		m.opResult = "the parent directory cannot be deleted"
		return m, nil
	}
	files := m.visibleFiles(m.activePane)
	cursor := m.rawCursor(m.activePane)
	if cursor < 0 || cursor >= len(files) {
		return m, nil
	}
	m.confirmDelete = true
	m.deleteTarget = files[cursor].Path
	m.deleteIsDir = files[cursor].IsDir
	return m, nil
}

// openCursorEntry opens what the active pane's cursor is on: the ".." row goes
// up a level, a directory is entered, and a file is handed to the desktop.
//
// It is the body the enter key has always run, lifted out so the context
// menu's Open item runs the same thing. Two implementations of "open the
// entry under the cursor" is how a menu and a key come to disagree about
// whether the parent row is an entry, or about what a file is opened with.
func (m Model) openCursorEntry() (tea.Model, tea.Cmd) {
	if m.onParentRow(m.activePane) {
		return m.goToParent(m.activePane)
	}
	files := m.visibleFiles(m.activePane)
	cursor := m.rawCursor(m.activePane)
	if cursor >= 0 && cursor < len(files) && files[cursor].IsDir {
		return m, m.beginLoad(m.activePane, files[cursor].Path)
	}
	if cursor >= 0 && cursor < len(files) {
		return m, openCmd(files[cursor].Path, files[cursor].Name)
	}
	return m, nil
}

// startRename opens the rename prompt on the entry under the cursor.
//
// The ".." row is refused here rather than in the caller: it is the way up,
// not a directory on disk, and renaming it would mean renaming the parent —
// a different operation on a directory the pane is not showing. Keeping the
// refusal beside the prompt is what makes both the rename key and the menu's
// Rename item safe without either of them repeating the rule.
func (m Model) startRename() (tea.Model, tea.Cmd) {
	files := m.visibleFiles(m.activePane)
	cursor := m.rawCursor(m.activePane)
	if cursor < 0 || cursor >= len(files) {
		if cursor == parentCursor {
			m.opResult = "the parent directory cannot be renamed"
		}
		return m, nil
	}
	m.mode = inputRename
	m.inputBuf = files[cursor].Name
	m.renameTarget = files[cursor].Path
	return m, nil
}

// startAddFile opens the empty-name prompt that creates a file in the active
// pane's directory. It is the body the new-file key runs, shared with the
// menu so the prompt is reached the same way from either.
func (m Model) startAddFile() (tea.Model, tea.Cmd) {
	m.mode = inputAdd
	m.inputBuf = ""
	return m, nil
}

// startAddDir opens the prompt that creates a directory in the active pane's
// directory, shared with the menu's New directory item.
func (m Model) startAddDir() (tea.Model, tea.Cmd) {
	m.mode = inputAddDir
	m.inputBuf = ""
	return m, nil
}

// cycleSortAction advances the active pane to its next sort mode and reports
// it. The reorder and the status message are one action, so the sort key and
// the menu's Sort item cannot sort one way and report another.
func (m Model) cycleSortAction() (tea.Model, tea.Cmd) {
	mode := m.cycleSort(m.activePane)
	m.opResult = "sort: " + mode.Label()
	return m, m.updatePreview()
}

func (m Model) handleConfirmDelete(msg tea.KeyMsg) (tea.Model, tea.Cmd) {
	switch keyID(msg) {
	// Trash is the safe choice, so it is the one that also answers to the
	// keys people reach for out of habit. Enter is deliberately not one of
	// them: a key pressed to dismiss an unread question must not delete.
	case "t", "T", "y", "Y":
		m.confirmDelete = false
		target := m.deleteTarget
		m.deleteTarget = ""
		m.deleteIsDir = false
		m.opResult = "trashing " + filepath.Base(target) + "..."
		return m, trashCmd(target)
	case "p", "P", "f", "F", "d", "D":
		// Permanent deletion is never one keypress. This opens the question
		// that says exactly what will go; the next answer decides.
		m.confirmDelete = false
		m.confirmPermanentDelete = true
		m.permanentTarget = m.deleteTarget
		m.permanentIsDir = m.deleteIsDir
		m.deleteTarget = ""
		m.deleteIsDir = false
		m.opResult = ""
		return m, nil
	case "n", "N", "esc", "enter":
		m.confirmDelete = false
		m.deleteTarget = ""
		m.deleteIsDir = false
		m.opResult = "cancelled"
		return m, nil
	}
	return m, nil
}

// handleConfirmPermanentDelete answers the second, explicit question about a
// permanent deletion. Only an affirmative answer deletes; Enter is the safe
// answer, not a shortcut to the destructive one.
func (m Model) handleConfirmPermanentDelete(msg tea.KeyMsg) (tea.Model, tea.Cmd) {
	switch keyID(msg) {
	case "y", "Y", "p", "P", "f", "F", "d", "D":
		m.confirmPermanentDelete = false
		target := m.permanentTarget
		m.permanentTarget = ""
		m.permanentIsDir = false
		m.opResult = "deleting " + filepath.Base(target) + "..."
		return m, deleteCmd(target)
	case "n", "N", "esc", "enter":
		m.confirmPermanentDelete = false
		m.permanentTarget = ""
		m.permanentIsDir = false
		m.opResult = "cancelled"
		return m, nil
	}
	return m, nil
}
