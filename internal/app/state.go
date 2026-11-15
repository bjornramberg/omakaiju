package app

import (
	"path/filepath"

	"github.com/bjornramberg/omakaiju/internal/fs"
	"github.com/bjornramberg/omakaiju/internal/nav"
	"github.com/bjornramberg/omakaiju/internal/ui"

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

// rawScroll is the index of the first row the pane draws, without keeping the
// cursor inside it. It is the same shape as rawCursor so the reconciled
// accessors have a place to read the unadjusted state from.
func (m Model) rawScroll(pane int) int {
	if pane == 0 {
		return m.leftScroll
	}
	return m.rightScroll
}

func (m *Model) setScroll(pane, scroll int) {
	if pane == 0 {
		m.leftScroll = scroll
	} else {
		m.rightScroll = scroll
	}
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

// reloadPanesCmd refreshes both panes after a file operation. It keeps each
// pane's cursor and filter, so an operation on one entry does not throw the
// user back to the top of the listing. A restore set by the operation — the
// entry it created, renamed, or landed next to — still wins.
func (m *Model) reloadPanesCmd() tea.Cmd {
	return tea.Batch(
		loadDirPreservingCmd(0, m.targetPath(0)),
		loadDirPreservingCmd(1, m.targetPath(1)),
	)
}

// refreshPaneCmd reloads a pane after something outside the app changed its
// directory. Unlike an explicit refresh, it keeps the cursor on the entry the
// user was looking at and leaves an active filter in place: an unrelated file
// appearing on disk must not move the selection or undo a search.
func (m *Model) refreshPaneCmd(pane int) tea.Cmd {
	return loadDirPreservingCmd(pane, m.targetPath(pane))
}

// watchedPath reports the directory a pane has registered with the watcher.
func (m Model) watchedPath(pane int) string {
	if pane == 0 {
		return m.leftWatched
	}
	return m.rightWatched
}

func (m *Model) setWatchedPath(pane int, path string) {
	if pane == 0 {
		m.leftWatched = path
	} else {
		m.rightWatched = path
	}
}

// otherWatchedPath reports the directory the other pane has registered. It is
// how a directory both panes show stays watched when only one of them leaves
// it.
func (m Model) otherWatchedPath(pane int) string {
	if pane == 0 {
		return m.rightWatched
	}
	return m.leftWatched
}

// followPaneWatch points a pane's watcher at the directory it now shows. The
// old directory is dropped once neither pane is using it.
//
// Registration is best effort: a directory can vanish between the listing
// that selected it and the watch that follows it, and that is not a reason to
// turn a successful navigation into an error. The next load will try again.
func (m *Model) followPaneWatch(pane int, path string) {
	if m.dirWatcher == nil || path == "" {
		return
	}
	path = filepath.Clean(path)
	old := m.watchedPath(pane)
	other := m.otherWatchedPath(pane)
	if old == path {
		return
	}

	m.setWatchedPath(pane, path)
	if old != "" && old != other {
		_ = m.dirWatcher.Remove(old)
	}
	if path != other {
		_ = m.dirWatcher.Watch(path)
	}
}

// panesShowing reports which panes display a directory affected by a watcher
// event. fsnotify names the entry that changed, so the directory is its
// parent; an event whose own name is a pane's directory counts as well for
// changes to the directory itself.
func (m Model) panesShowing(eventPath string) []int {
	dir := filepath.Clean(filepath.Dir(eventPath))
	eventPath = filepath.Clean(eventPath)
	panes := make([]int, 0, 2)
	for _, pane := range []int{0, 1} {
		target := filepath.Clean(m.targetPath(pane))
		if dir == target || eventPath == target {
			panes = append(panes, pane)
		}
	}
	return panes
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
	m.reconcileScroll(pane)
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
	// reorder instead of jumping to an unrelated row. The ".." row is not an
	// entry, so there is no name to follow when the cursor is parked on it.
	var selected string
	if files := m.visibleFiles(pane); m.rawCursor(pane) >= 0 && m.rawCursor(pane) < len(files) {
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

// visibleCount is how many rows the pane's listing has after filtering,
// without copying the listing to find out.
//
// visibleFiles copies the file list so a caller cannot reorder the model's
// backing array, and the scroll reconciliation runs on every cursor move. This
// asks the cheap question — how many rows are there? — so that path does not
// allocate a copy per keypress.
func (m Model) visibleCount(pane int) int {
	if m.filterOf(pane) == "" {
		return len(m.rawFiles(pane))
	}
	return len(m.visibleFiles(pane))
}

// parentCursor is the cursor position that means the pane is on its ".." row:
// the way back to the directory above, which is not one of the listing's
// entries and has no index among them.
//
// It is ui.ParentCursor because the pane draws the selection bar from the same
// value; two copies of the number could drift apart. A pane at the filesystem
// root has no parent row, so its cursor is never parentCursor.
const parentCursor = ui.ParentCursor

// parentPath is the directory above the one the pane shows, and the empty
// string when the pane is at the root and there is none.
//
// It is the path the pane is showing, not the one it is heading to: the pane
// draws the parent row for the directory on screen, so the row and the
// directory it leads to have to be the same one.
func (m Model) parentPath(pane int) string {
	path := filepath.Clean(m.rawPath(pane))
	parent := filepath.Dir(path)
	if parent == path {
		return ""
	}
	return parent
}

// listRows is how many file rows the pane's listing has room for. It must agree
// with paneViews exactly: the pane's height comes from the layout, only the
// active pane carries a status row, because the status names the marked set and
// the marks belong to the focused pane, and a pane with a parent gives a row to
// the ".." row above its listing.
func (m Model) listRows(pane int) int {
	return ui.ListRows(m.layout().PaneHeight(pane == 0), m.filterOf(pane),
		m.activePane == pane && len(m.marked) > 0, m.parentPath(pane) != "")
}

// reconcileScroll keeps the cursor inside the window the pane draws.
//
// The window is the model's state, not a function of the cursor: a pane
// scrolled to the middle of a long list must stay there when the cursor moves
// to a row inside it. The cursor rides inside the window, and the window moves
// only when the cursor would leave it.
func (m *Model) reconcileScroll(pane int) {
	visible := m.listRows(pane)
	if visible < 1 {
		visible = 1
	}
	files := m.visibleCount(pane)
	cursor := m.rawCursor(pane)
	scroll := m.rawScroll(pane)

	if scroll < 0 {
		scroll = 0
	}
	// The window cannot reach past the end of the listing.
	if maxStart := files - visible; scroll > maxStart {
		scroll = maxStart
		if scroll < 0 {
			scroll = 0
		}
	}
	// The cursor rides inside the window. A cursor that is not a row of the
	// listing — a load still in flight, or the ".." row above the listing —
	// is left alone: neither can scroll a window it does not sit in.
	if cursor >= 0 && cursor < files {
		if cursor < scroll {
			scroll = cursor
		}
		if cursor >= scroll+visible {
			scroll = cursor - visible + 1
		}
		if scroll < 0 {
			scroll = 0
		}
	}
	m.setScroll(pane, scroll)
}

// reconcileScrolls reconciles both panes' windows.
//
// A pane's row count depends on whether it carries the status row, and the
// status row belongs to the focused pane. So a change to the focus, to the
// marked set, or to the pane heights moves rows between the panes, and a
// window that was reconciled against the old geometry is no longer the window
// the pane draws.
func (m *Model) reconcileScrolls() {
	m.reconcileScroll(0)
	m.reconcileScroll(1)
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
	if cursor == parentCursor {
		// The parent row is above the listing, so it is not clamped to it.
		// A pane without a parent row has no such position at all, and is
		// moved to the first entry instead of keeping a cursor no row draws.
		if m.parentPath(pane) != "" {
			return
		}
		cursor = 0
	}
	if n := len(m.visibleFiles(pane)); cursor > n-1 {
		cursor = n - 1
	}
	if cursor < 0 {
		cursor = 0
	}
	m.setCursor(pane, cursor)
}

// loadActive refreshes the active pane after an operation. It preserves the
// pane's place, and a restore set by the operation lands the cursor on the
// entry it produced.
func (m Model) loadActive() tea.Cmd {
	return loadDirPreservingCmd(m.activePane, m.targetPath(m.activePane))
}
