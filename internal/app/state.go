package app

import (
	"github.com/bjornramberg/omakaiju/internal/fs"
	"github.com/bjornramberg/omakaiju/internal/nav"

	tea "charm.land/bubbletea/v2"
)

func (m Model) rawFiles(pane int) []fs.Entry {
	if pane == 0 {
		return m.leftFiles
	}
	return m.rightFiles
}

func (m Model) rawCursor(pane int) int {
	if pane == 0 {
		return m.leftCursor
	}
	return m.rightCursor
}

func (m Model) rawPath(pane int) string {
	if pane == 0 {
		return m.leftPath
	}
	return m.rightPath
}

func (m *Model) setPath(pane int, path string) {
	if pane == 0 {
		m.leftPath = path
	} else {
		m.rightPath = path
	}
}

// targetPath is where the pane is heading: the path of a load already in
// flight, or the committed path when nothing is pending. Navigation works
// from it so a second keypress applies to the directory the first one asked
// for, even before its listing has arrived.
func (m Model) targetPath(pane int) string {
	if pane == 0 {
		if m.leftPending != "" {
			return m.leftPending
		}
		return m.leftPath
	}
	if m.rightPending != "" {
		return m.rightPending
	}
	return m.rightPath
}

func (m *Model) setPending(pane int, path string) {
	if pane == 0 {
		m.leftPending = path
	} else {
		m.rightPending = path
	}
}

// beginLoad records the path a pane is heading to and dispatches the load.
// The path is not committed here: the result commits it, so a failed read
// leaves the header naming the directory whose rows are still on screen.
func (m *Model) beginLoad(pane int, path string) tea.Cmd {
	m.setPending(pane, path)
	return loadDirCmd(pane, path)
}

// reloadPaneCmd refreshes a pane without abandoning a navigation already in
// flight for it.
func (m *Model) reloadPaneCmd(pane int) tea.Cmd {
	return m.beginLoad(pane, m.targetPath(pane))
}

// reloadPanesCmd refreshes both panes. It is the reload every file operation
// ends with.
func (m *Model) reloadPanesCmd() tea.Cmd {
	return tea.Batch(m.reloadPaneCmd(0), m.reloadPaneCmd(1))
}

func (m Model) filterOf(pane int) string {
	if pane == 0 {
		return m.leftFilter
	}
	return m.rightFilter
}

func (m *Model) setFilter(pane int, q string) {
	if pane == 0 {
		m.leftFilter = q
	} else {
		m.rightFilter = q
	}
}

func (m *Model) setCursor(pane, cursor int) {
	if pane == 0 {
		m.leftCursor = cursor
	} else {
		m.rightCursor = cursor
	}
}

func (m Model) sortOf(pane int) fs.SortMode {
	if pane == 0 {
		return m.leftSort
	}
	return m.rightSort
}

func (m *Model) setSort(pane int, mode fs.SortMode) {
	if pane == 0 {
		m.leftSort = mode
	} else {
		m.rightSort = mode
	}
}

// applySort re-orders a pane's raw listing by its current sort mode.
//
// The sort happens on a copy. Model is copied by value on every update, but
// a slice is a reference: sorting the model's own backing array in place
// would leak out of a failed update and corrupt the state crash recovery
// hands back.
func (m *Model) applySort(pane int) {
	if pane == 0 {
		m.leftFiles = sortEntriesCopy(m.leftFiles, m.leftSort)
	} else {
		m.rightFiles = sortEntriesCopy(m.rightFiles, m.rightSort)
	}
}

// sortEntriesCopy returns entries sorted by mode, leaving the caller's slice
// and its backing array untouched.
func sortEntriesCopy(entries []fs.Entry, mode fs.SortMode) []fs.Entry {
	out := make([]fs.Entry, len(entries))
	copy(out, entries)
	fs.SortEntriesBy(out, mode)
	return out
}

// cycleSort advances the active pane through name -> size↓ -> size↑ -> name,
// keeps the cursor on the same entry when it survives the reorder, and
// reports the new mode.
func (m *Model) cycleSort(pane int) fs.SortMode {
	// Remember the selected entry so the cursor can follow it across the
	// reorder instead of jumping to an unrelated row.
	var selected string
	if files := m.visibleFiles(pane); m.rawCursor(pane) < len(files) {
		selected = files[m.rawCursor(pane)].Path
	}
	next := m.sortOf(pane).Next()
	m.setSort(pane, next)
	m.applySort(pane)
	if selected != "" {
		for i, f := range m.visibleFiles(pane) {
			if f.Path == selected {
				m.setCursor(pane, i)
				break
			}
		}
	}
	m.clampCursor(pane)
	return next
}

// visibleFiles returns the pane's listing after filtering.
//
// The result is never the model's own backing array: with no filter active
// the caller gets a copy, and filtering already allocates. A caller that
// reorders the slice it is handed therefore cannot change the model — and a
// panic after such a change cannot leave it behind in the restored state.
func (m Model) visibleFiles(pane int) []fs.Entry {
	files := m.rawFiles(pane)
	if filter := m.filterOf(pane); filter != "" {
		return nav.FilterEntries(files, filter)
	}
	out := make([]fs.Entry, len(files))
	copy(out, files)
	return out
}

func (m *Model) setRestore(pane int, name string) {
	if pane == 0 {
		m.leftRestore = name
	} else {
		m.rightRestore = name
	}
}

// restoreOf reports the name the pane's cursor should land on after its next
// load.
//
// The name outlives the loads that apply it rather than being consumed by the
// first one. A paste of several entries dispatches one operation per entry and
// each of them reloads the panes, so a restore that was taken on the first
// landing would leave the cursor back at the top once the rest arrive.
func (m Model) restoreOf(pane int) string {
	if pane == 0 {
		return m.leftRestore
	}
	return m.rightRestore
}

// clearRestores drops both panes' pending restores. A key press means the user
// has moved on, so a selection left over from an earlier operation must not
// survive to drag the cursor back the next time a listing loads.
func (m *Model) clearRestores() {
	m.leftRestore = ""
	m.rightRestore = ""
}

// restoreLanded points a pane at the entry an operation just put on disk, so a
// copy, move, or paste ends with the cursor on what it created.
//
// The entry is the first in name order rather than whichever operation message
// arrives last: the moves of a marked set are dispatched from a map, so their
// messages come back in no particular order, and every reload they trigger has
// to agree on the same row.
func (m *Model) restoreLanded(pane int, names []string) {
	first := ""
	for _, name := range names {
		if first == "" || name < first {
			first = name
		}
	}
	if first == "" {
		return
	}
	m.setRestore(pane, first)
}

func (m *Model) clampCursor(pane int) {
	cursor := m.rawCursor(pane)
	if n := len(m.visibleFiles(pane)); cursor > n-1 {
		cursor = n - 1
	}
	if cursor < 0 {
		cursor = 0
	}
	m.setCursor(pane, cursor)
}

func (m Model) loadActive() tea.Cmd {
	return m.reloadPaneCmd(m.activePane)
}
