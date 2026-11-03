package app

import (
	"errors"
	"fmt"
	"os"

	"github.com/bjornramberg/omakaiju/internal/fs"

	tea "charm.land/bubbletea/v2"
)

// openTrash navigates the active pane to the home trash so the user can look
// at what is in it and put entries back by hand.
//
// Only the home trash is reachable: the fs package does not walk the mount
// table to find per-filesystem trashes, so there is no one directory that
// could stand in for all of them. A trash that does not exist yet is said
// plainly rather than presented as a directory that is not there.
func (m Model) openTrash() (tea.Model, tea.Cmd) {
	root, err := fs.TrashRoot()
	if err != nil {
		m.opErr = fmt.Errorf("trash: %w", err)
		return m, nil
	}

	info, err := os.Stat(root)
	if errors.Is(err, os.ErrNotExist) {
		m.opResult = "no trash yet at " + root
		return m, nil
	}
	if err != nil {
		m.opErr = fmt.Errorf("trash %s: %w", root, err)
		return m, nil
	}
	if !info.IsDir() {
		m.opErr = fmt.Errorf("trash %s is not a directory", root)
		return m, nil
	}

	return m, m.beginLoad(m.activePane, root)
}

// requestQuit is the quit key's way out. When the home trash holds something,
// the session does not end straight away: the user is asked whether it should
// be emptied first, and the answer decides what happens.
//
// A trash that cannot be listed is reported and does not hold the quit up:
// the user asked to leave, and a listing failure is no reason to trap them.
func (m Model) requestQuit() (tea.Model, tea.Cmd) {
	entries, err := fs.ListTrash()
	if err != nil {
		m.opErr = fmt.Errorf("trash: %w", err)
		return m.quit()
	}
	if len(entries) == 0 {
		return m.quit()
	}

	m.confirmEmptyTrash = true
	return m, nil
}

// handleConfirmEmptyTrash answers the question asked on the way out.
//
// Enter counts as yes because that is the safe answer the delete prompt also
// gives it. No and Esc quit without touching the trash: Esc means "no" here,
// not "stay", because the user has already asked to quit.
func (m Model) handleConfirmEmptyTrash(msg tea.KeyMsg) (tea.Model, tea.Cmd) {
	switch msg.String() {
	case "y", "Y", "enter":
		m.confirmEmptyTrash = false
		if err := fs.EmptyTrash(); err != nil {
			// The user asked to quit; a trash that will not empty is
			// reported and does not keep the session alive.
			m.opErr = fmt.Errorf("empty trash: %w", err)
		} else {
			m.opResult = "trash emptied"
		}
		return m.quit()
	case "n", "N", "esc":
		m.confirmEmptyTrash = false
		return m.quit()
	}
	return m, nil
}

// quit ends the session, closing the watchers the model owns.
func (m Model) quit() (tea.Model, tea.Cmd) {
	if m.themeWatcher != nil {
		m.themeWatcher.Close()
	}
	if m.dirWatcher != nil {
		m.dirWatcher.Close()
	}
	return m, tea.Quit
}
