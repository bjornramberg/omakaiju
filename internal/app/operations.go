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
	if len(msg.failures) > 0 {
		m.restoreSelection(msg)
		m.opErr = errors.New(msg.result())
		return
	}
	m.opErr = nil
	m.opResult = msg.result()
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

func (m Model) handleYank() (tea.Model, tea.Cmd) {
	files := m.visibleFiles(m.activePane)
	cursor := m.rawCursor(m.activePane)
	if cursor >= len(files) {
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
	files := m.visibleFiles(m.activePane)
	cursor := m.rawCursor(m.activePane)
	if cursor >= len(files) {
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
		return m, nil
	}

	m.marked[entry.Path] = entry
	m.opResult = fmt.Sprintf("marked %s (%d)", entry.Name, len(m.marked))
	return m, nil
}

func (m Model) handleDelete() (tea.Model, tea.Cmd) {
	files := m.visibleFiles(m.activePane)
	cursor := m.rawCursor(m.activePane)
	if cursor >= len(files) {
		return m, nil
	}
	m.confirmDelete = true
	m.deleteTarget = files[cursor].Path
	return m, nil
}

func (m Model) handleConfirmDelete(msg tea.KeyMsg) (tea.Model, tea.Cmd) {
	switch msg.String() {
	// Trash is the safe choice, so it is the one that also answers to the
	// keys people reach for out of habit.
	case "t", "T", "y", "Y", "enter":
		m.confirmDelete = false
		target := m.deleteTarget
		m.deleteTarget = ""
		m.opResult = "trashing " + filepath.Base(target) + "..."
		return m, trashCmd(target)
	case "f", "F":
		m.confirmDelete = false
		target := m.deleteTarget
		m.deleteTarget = ""
		m.opResult = "deleting " + filepath.Base(target) + "..."
		return m, deleteCmd(target)
	case "n", "N", "esc":
		m.confirmDelete = false
		m.deleteTarget = ""
		m.opResult = "cancelled"
		return m, nil
	}
	return m, nil
}
