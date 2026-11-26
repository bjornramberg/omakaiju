package app

import (
	"fmt"
	"os"
	"path/filepath"
	"strings"

	"github.com/bjornramberg/omakaiju/internal/fs"

	tea "charm.land/bubbletea/v2"
)

// pastePlan is a clipboard paste held at the point where one or more
// destination names are taken. It keeps the selection exactly as staged, so
// cancelling leaves the clipboard armed for another try.
type pastePlan struct {
	clipboard Clipboard
	target    string
	conflicts []string
	skipped   int
}

// handlePaste drops everything into the active pane's current directory — the
// pane the user is looking at is where a paste belongs, wherever the copy was
// started. Tabbing to the other pane and pasting therefore targets that pane,
// and pasting after navigating inside the copying pane gains a fresh copy
// there. Marks outweigh the y-staged file.
//
// A destination name that is already taken is not replaced silently. The user
// is asked whether to overwrite, skip, or keep both, and the clipboard stays
// armed until that question is answered.
//
// Whatever lands is named as the target pane's next cursor position, so the
// reload that follows the operation puts the cursor on the new file rather than
// back at the top of the listing.
func (m Model) handlePaste() (tea.Model, tea.Cmd) {
	if m.copier != nil {
		what := "copy"
		if m.copier.IsMove() {
			what = "move"
		}
		m.opResult = what + " in progress"
		return m, nil
	}
	if len(m.marked) > 0 {
		return m.pasteMarks()
	}
	if !m.clipboardOn {
		return m, nil
	}

	targetPath := m.rawPath(m.activePane)

	// Every paste the interface stages today is a copy. The move form is the
	// marked set above; it refuses an existing destination rather than
	// replacing it.
	if m.clipboard.Action != "copy" {
		return m, nil
	}

	for _, file := range m.clipboard.Files {
		dst := filepath.Join(targetPath, file.Name)
		if filepath.Clean(dst) == filepath.Clean(file.Path) {
			m.opResult = "already in this directory"
			return m, nil
		}
	}

	conflicts := pasteConflicts(m.clipboard.Files, targetPath)
	if len(conflicts) > 0 {
		m.pastePlan = &pastePlan{
			clipboard: m.clipboard,
			target:    targetPath,
			conflicts: conflicts,
		}
		m.confirmPaste = true
		m.opResult = ""
		return m, nil
	}

	return m.startClipboardCopy(m.clipboard.Files, targetPath, 0, 0)
}

// pasteConflictQuestion names the choice without pretending the answer is
// obvious. A single name is worth showing; several are counted.
func pasteConflictQuestion(plan *pastePlan) string {
	if plan == nil || len(plan.conflicts) == 0 {
		return "Replace existing files?"
	}
	if len(plan.conflicts) == 1 {
		return "Replace " + plan.conflicts[0] + "?"
	}
	return fmt.Sprintf("Replace %d existing items?", len(plan.conflicts))
}

// pasteConflicts returns the destination names a paste would land on top of.
// It is deliberately about the top-level entries: choosing overwrite for a
// directory accepts the merge into what is already there, and the question
// names that directory.
func pasteConflicts(files []fs.Entry, target string) []string {
	var conflicts []string
	for _, file := range files {
		dst := filepath.Join(target, file.Name)
		// Two paths for one file are not a replacement question: copying an
		// entry onto itself has to be refused by the copy, not "overwritten".
		if same, err := fs.SameFile(file.Path, dst); err == nil && same {
			continue
		}
		if _, err := os.Lstat(dst); err == nil || !os.IsNotExist(err) {
			conflicts = append(conflicts, file.Name)
		}
	}
	return conflicts
}

// startClipboardCopy dispatches a staged copy after the destination question,
// if any, has been answered. files is the selection that will actually run;
// skipped and replaced describe what the chosen answer left out or replaced.
func (m Model) startClipboardCopy(files []fs.Entry, targetPath string, skipped, replaced int) (tea.Model, tea.Cmd) {
	names := make([]string, 0, len(files))
	for _, f := range files {
		names = append(names, f.Name)
	}

	// Snapshot the selection before disarming it: a failure re-arms this
	// exact paste so the user can retry.
	clipboard := m.clipboard
	clipboard.Files = files
	m.clipboardOn = false
	m.copyClipboard = &clipboard
	m.copyFiles = len(files)
	m.copySkipped = skipped
	m.copyReplaced = replaced
	m.copyGen++
	m.restoreLanded(m.activePane, names)
	m.opResult = fmt.Sprintf("copying %d file(s)...", len(names))
	return m, pasteCopyCmd(files, targetPath, m.copyGen, &clipboard, skipped, replaced)
}

// handleConfirmPaste answers the question about a destination name that is
// already taken. The safe answer is the default: Enter and Esc cancel, leaving
// the clipboard armed.
func (m Model) handleConfirmPaste(msg tea.KeyMsg) (tea.Model, tea.Cmd) {
	plan := m.pastePlan
	if plan == nil {
		m.confirmPaste = false
		return m, nil
	}

	switch keyID(msg) {
	case "o", "O":
		m.closePastePrompt()
		return m.startClipboardCopy(plan.clipboard.Files, plan.target, plan.skipped, len(plan.conflicts))
	case "s", "S":
		files := withoutPasteConflicts(plan.clipboard.Files, plan.conflicts)
		m.closePastePrompt()
		if len(files) == 0 {
			m.opResult = fmt.Sprintf("skipped %d existing file(s)", len(plan.conflicts))
			return m, nil
		}
		return m.startClipboardCopy(files, plan.target, plan.skipped+len(plan.conflicts), 0)
	case "k", "K":
		m.closePastePrompt()
		return m.startKeepBoth(plan)
	case "n", "N", "esc", "enter":
		m.closePastePrompt()
		m.opResult = "paste cancelled"
		return m, nil
	}
	return m, nil
}

// closePastePrompt drops the pending question without touching the clipboard.
func (m *Model) closePastePrompt() {
	m.confirmPaste = false
	m.pastePlan = nil
}

// withoutPasteConflicts removes the entries the user chose to skip.
func withoutPasteConflicts(files []fs.Entry, conflicts []string) []fs.Entry {
	if len(conflicts) == 0 {
		return files
	}
	skip := make(map[string]bool, len(conflicts))
	for _, name := range conflicts {
		skip[name] = true
	}
	kept := make([]fs.Entry, 0, len(files))
	for _, file := range files {
		if !skip[file.Name] {
			kept = append(kept, file)
		}
	}
	return kept
}

// startKeepBoth copies each entry under a new name instead of replacing what
// is already there. The new names are chosen before the command runs so the
// cursor can land on them, and the copy itself checks the disk again as it
// writes.
func (m Model) startKeepBoth(plan *pastePlan) (tea.Model, tea.Cmd) {
	files := plan.clipboard.Files
	items := make([]opItem, 0, len(files))
	landed := make([]string, 0, len(files))
	for _, file := range files {
		dst := uniqueDestination(filepath.Join(plan.target, file.Name), file.IsDir)
		name := filepath.Base(dst)
		items = append(items, opItem{name: name, src: file.Path, dst: dst})
		landed = append(landed, name)
	}

	clipboard := plan.clipboard
	m.clipboardOn = false
	m.copyClipboard = &clipboard
	m.copyFiles = len(files)
	m.copySkipped = plan.skipped
	m.copyReplaced = 0
	m.copyGen++
	m.restoreLanded(m.activePane, landed)
	m.opResult = fmt.Sprintf("copying %d file(s)...", len(files))

	var warns []fs.MetaWarning
	op := func(src, dst string) error { return fs.CopyRecursive(src, dst, &warns) }
	return m, attachMetaWarnings(batchOpCmd("pasted", items, plan.skipped, nil, &clipboard, op), &warns)
}

// uniqueDestination returns path when it is free, or the first free
// "name (1).ext" variant beside it. Keep both is meant to preserve the entry
// that is already there, so it can never hand back an occupied name.
func uniqueDestination(path string, isDir bool) string {
	if _, err := os.Lstat(path); err != nil {
		return path
	}
	dir := filepath.Dir(path)
	base := filepath.Base(path)
	stem, ext := base, ""
	if !isDir {
		ext = filepath.Ext(base)
		stem = strings.TrimSuffix(base, ext)
	}
	for i := 1; ; i++ {
		candidate := filepath.Join(dir, fmt.Sprintf("%s (%d)%s", stem, i, ext))
		if _, err := os.Lstat(candidate); err != nil {
			return candidate
		}
	}
}

// pasteMarks moves the marked set into the active pane's current directory.
// Entries that vanished from disk, or that already sit in the destination,
// are skipped so a stale mark cannot fail the whole batch. The pane is left
// pointing at one of the entries that arrived, not at the top of the listing.
//
// A paste with nothing to move keeps the marks. They are still staged, so
// tabbing to the destination and pasting there has to work; clearing them
// made the two-step move the clipboard also supports impossible for marks.
func (m Model) pasteMarks() (tea.Model, tea.Cmd) {
	targetPath := m.rawPath(m.activePane)

	var items []opItem
	var landed []string
	var srcs []string
	marks := make(map[string]fs.Entry, len(m.marked))
	skipped := 0
	alreadyHere := 0
	missing := 0
	for _, entry := range m.marked {
		if _, err := os.Stat(entry.Path); err != nil {
			skipped++
			missing++
			continue
		}
		dst := filepath.Join(targetPath, entry.Name)
		if filepath.Clean(dst) == filepath.Clean(entry.Path) {
			skipped++
			alreadyHere++
			continue
		}
		items = append(items, opItem{name: entry.Name, src: entry.Path, dst: dst})
		srcs = append(srcs, entry.Path)
		landed = append(landed, entry.Name)
		marks[entry.Path] = entry
	}
	dstDir := targetPath

	if len(items) == 0 {
		// Nothing was consumed, so nothing is cleared. Saying the entries
		// are already here matches the clipboard path, which keeps its
		// selection in the same case.
		if alreadyHere > 0 && missing == 0 {
			m.opResult = "already in this directory"
		} else {
			m.opResult = "nothing to move"
		}
		return m, nil
	}

	m.marked = nil
	m.restoreLanded(m.activePane, landed)

	// The moves have not run yet, so report that they are starting rather
	// than claiming they are done. The final count comes back in the batch
	// result once every move has actually been attempted. The marks ride
	// along so a move that fails can be put back for another try.
	m.opResult = fmt.Sprintf("moving %d file(s)...", len(items))
	m.copyMarks = marks
	m.copySkipped = skipped
	m.copyGen++
	return m, startMoveCmd(srcs, dstDir, m.copyGen, marks, skipped)
}
