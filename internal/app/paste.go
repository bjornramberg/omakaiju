package app

import (
	"fmt"
	"os"
	"path/filepath"

	"github.com/bjornramberg/omakaiju/internal/fs"

	tea "charm.land/bubbletea/v2"
)

// handlePaste drops everything into the active pane's current directory — the
// pane the user is looking at is where a paste belongs, wherever the copy was
// started. Tabbing to the other pane and pasting therefore targets that pane,
// and pasting after navigating inside the copying pane gains a fresh copy
// there. Marks outweigh the y-staged file, and a file already sitting in the
// target is skipped while the clipboard stays armed, so tabbing somewhere
// else can still complete the copy.
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

	// A copy dispatches first and measures in the command. The size walk
	// decides between the steppable copier — progress visible, cancellable —
	// and the simple path, but it is disk work, so it must not run here: on a
	// large tree it would freeze the UI before the notice below could be
	// drawn. The command replaces the notice with the result when it lands.
	if m.clipboard.Action == "copy" {
		for _, file := range m.clipboard.Files {
			dst := filepath.Join(targetPath, file.Name)
			if filepath.Clean(dst) == filepath.Clean(file.Path) {
				m.opResult = "already in this directory"
				return m, nil
			}
		}
		names := make([]string, 0, len(m.clipboard.Files))
		for _, f := range m.clipboard.Files {
			names = append(names, f.Name)
		}
		// Snapshot the selection before disarming it: a failure re-arms
		// this exact paste so the user can retry.
		clipboard := m.clipboard
		m.clipboardOn = false
		m.copyClipboard = &clipboard
		m.copyFiles = len(clipboard.Files)
		m.copyGen++
		m.restoreLanded(m.activePane, names)
		m.opResult = fmt.Sprintf("copying %d file(s)...", len(names))
		return m, pasteCopyCmd(clipboard.Files, targetPath, m.copyGen, &clipboard)
	}

	verb, gerund := "moved", "moving"
	op := fs.Move
	if m.clipboard.Action == "copy" {
		verb, gerund = "pasted", "copying"
		op = fs.CopyRecursive
	}

	var items []opItem
	var pasted []string
	for _, file := range m.clipboard.Files {
		dst := filepath.Join(targetPath, file.Name)
		if filepath.Clean(dst) == filepath.Clean(file.Path) {
			continue
		}
		items = append(items, opItem{name: file.Name, src: file.Path, dst: dst})
		pasted = append(pasted, file.Name)
	}
	if len(items) == 0 {
		m.opResult = "already in this directory"
		return m, nil
	}

	clipboard := m.clipboard
	m.clipboardOn = false
	m.restoreLanded(m.activePane, pasted)
	// The work has not run yet, so this is a progress notice rather than a
	// result: claiming a paste whose outcome is still unknown is the bug.
	m.opResult = fmt.Sprintf("%s %d file(s)...", gerund, len(items))
	return m, batchOpCmd(verb, items, 0, nil, &clipboard, op)
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
