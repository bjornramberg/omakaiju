package app

import (
	"path/filepath"
	"time"

	"github.com/bjornramberg/omakaiju/internal/ui"

	tea "charm.land/bubbletea/v2"
)

// wheelRows is how many listing rows one wheel notch moves the cursor. Three
// is the usual terminal scroll step and keeps a long listing quick to cross
// without skipping over a name.
const wheelRows = 3

// doubleClickWindow is how long two clicks on the same target have to be
// apart to count as one double-click. It is the same window the desktop
// uses, so a double-click feels the way it does everywhere else.
const doubleClickWindow = 500 * time.Millisecond

// modalActive reports whether an overlay or prompt owns the screen. The mouse
// is released in the same frames the keyboard is claimed: the panes under a
// modal are scenery, and a click must not move a cursor the user cannot see
// change behind the panel.
func (m Model) modalActive() bool {
	return m.confirmEmptyTrash || m.confirmDelete || m.fuzzyActive ||
		m.keyListActive || m.mode != inputNone
}

// mouseMode is the reporting mode for the next frame. It is MouseModeNone
// unless the user asked for the mouse and nothing is covering the panes.
// CellMotion is deliberate: AllMotion would also report hover, and nothing here
// reacts to a pointer that is only passing over a row.
func (m Model) mouseMode() tea.MouseMode {
	if !m.cfg.Mouse || m.modalActive() {
		return tea.MouseModeNone
	}
	return tea.MouseModeCellMotion
}

// handleMouse applies the mouse bindings: a left press focuses a pane and
// selects the row under the pointer, dragging that row onto the other pane
// moves or copies it, and the wheel scrolls the pane beneath it. Right and
// middle clicks are ignored; there is no context menu and nothing to paste, so
// they have nowhere to go.
func (m Model) handleMouse(msg tea.MouseMsg) (tea.Model, tea.Cmd) {
	if !m.cfg.Mouse || m.modalActive() {
		// A drag cannot survive the mouse being taken away: with reporting off
		// the terminal sends no release, and a drag left behind would keep the
		// drop indicator lit on a pane nobody is dragging onto — and would
		// then act on a release that arrives after the modal closes.
		m.drag = nil
		return m, nil
	}

	switch msg := msg.(type) {
	case tea.MouseClickMsg:
		if msg.Button != tea.MouseLeft {
			return m, nil
		}
		return m.clickPane(msg.X, msg.Y)
	case tea.MouseMotionMsg:
		// Motion is only reported while a button is down, so this is either
		// the middle of a drag or a pointer the app never pressed.
		return m.dragMotion(msg)
	case tea.MouseReleaseMsg:
		return m.dragRelease(msg)
	case tea.MouseWheelMsg:
		return m.wheelPane(msg.X, msg.Y, msg.Button)
	}
	return m, nil
}

// clickPane focuses the pane under the pointer and moves its cursor to the row
// under it: an entry of the listing, or the pane's ".." row, which the cursor
// can sit on. A click on the rest of the chrome focuses without selecting, and
// a click outside both panes changes nothing.
//
// A second click on the same target soon after the first one is a
// double-click, which opens what it landed on: the ".." row goes up a level, a
// directory is entered, and a file is handed to the desktop. The first click
// does its usual work first, so a double-click is a click plus an action and
// not a separate gesture with no selection behind it.
func (m Model) clickPane(x, y int) (tea.Model, tea.Cmd) {
	layout := m.layout()
	hit, ok := layout.HitPane(m.paneViews(layout), x, y)
	if !ok {
		return m, nil
	}

	// The clock is read here rather than carried in the message so the window
	// measures what the user did, not when the terminal got round to
	// reporting it.
	//
	// A click that has no predecessor is not a double-click, however long the
	// clock says it has been. The zero value of the record is a click on the
	// first row of the first pane, so without this check the first click of a
	// session would open the first file on any machine whose clock read the
	// zero time.
	target := clickTarget{pane: hit.Pane, row: hit.Row, parent: hit.Parent}
	now := time.Now()
	double := !m.lastClickAt.IsZero() &&
		now.Sub(m.lastClickAt) <= doubleClickWindow &&
		target == m.lastClick
	m.lastClick, m.lastClickAt = target, now

	focused := m.activePane != hit.Pane
	m.activePane = hit.Pane
	if hit.Parent {
		// The ".." row is a row the cursor can land on, so a click selects
		// it the way a click on an entry does. It is not an index of the
		// listing, which is why it needs its own position.
		m.setCursor(hit.Pane, parentCursor)
	} else if hit.Row >= 0 {
		m.setCursor(hit.Pane, hit.Row)
	}
	// The status row belongs to the focused pane, so focusing a pane takes a
	// row from it and gives one to the other. Both windows are reconciled
	// against the geometry they now have.
	m.reconcileScrolls()

	// A press on an entry starts a drag. The press has already done its usual
	// work — the pane is focused and the row is selected — so a drag is what
	// happens on top of a click rather than instead of one: a press and
	// release that never leaves the pane is still an ordinary click.
	//
	// It is cleared first so a drag nobody released cannot be revived by a
	// later press. The ".." row has no index in the listing and is not an
	// entry, so a press on it starts nothing.
	m.drag = nil
	if hit.Row >= 0 {
		files := m.visibleFiles(hit.Pane)
		if hit.Row < len(files) {
			m.drag = &dragState{pane: hit.Pane, row: hit.Row, entry: files[hit.Row], target: -1}
		}
	}

	if double {
		return m.openTarget(hit)
	}

	// Nothing visible changed: the pane was already focused and the click
	// missed every row, so there is no preview to rebuild.
	if !focused && hit.Row < 0 && !hit.Parent {
		return m, nil
	}
	return m, m.updatePreview()
}

// goToParent takes the pane to the directory above the one it shows.
//
// It is the same way out as the keyboard's left arrow: the pane comes back to
// the directory it came from, so the cursor lands on it. It works from the path
// on screen — unlike the left arrow, which works from where the pane is heading
// — because the ".." row was drawn for the directory the user is looking at.
func (m Model) goToParent(pane int) (tea.Model, tea.Cmd) {
	parent := m.parentPath(pane)
	if parent == "" {
		return m, nil
	}
	m.setRestore(pane, filepath.Base(m.rawPath(pane)))
	return m, m.beginLoad(pane, parent)
}

// openTarget acts on a double-click: the ".." row goes to the directory
// above, a directory is entered, and a file is handed to the desktop.
func (m Model) openTarget(hit ui.Hit) (tea.Model, tea.Cmd) {
	if hit.Parent {
		return m.goToParent(hit.Pane)
	}
	if hit.Row < 0 {
		return m, nil
	}
	files := m.visibleFiles(hit.Pane)
	if hit.Row >= len(files) {
		return m, nil
	}
	entry := files[hit.Row]
	if entry.IsDir {
		return m, m.beginLoad(hit.Pane, entry.Path)
	}
	return m, openCmd(entry.Path, entry.Name)
}

// wheelPane scrolls the listing under the pointer by one notch. It never
// changes the focused pane: the wheel is for reading ahead in the other pane
// without leaving the one being worked in.
func (m Model) wheelPane(x, y int, button tea.MouseButton) (tea.Model, tea.Cmd) {
	delta := 0
	switch button {
	case tea.MouseWheelUp:
		delta = -wheelRows
	case tea.MouseWheelDown:
		delta = wheelRows
	default:
		// Horizontal wheel events have nowhere to go in a single-column list.
		return m, nil
	}

	layout := m.layout()
	hit, ok := layout.HitPane(m.paneViews(layout), x, y)
	if !ok {
		return m, nil
	}
	if len(m.visibleFiles(hit.Pane)) == 0 {
		return m, nil
	}

	// The wheel moves the listing and the selection together, so the row
	// under the pointer stays under the pointer and the preview follows the
	// wheel the way it always has. Without carrying the cursor along the
	// selection would be left behind whenever the wheel scrolled past it.
	m.setScroll(hit.Pane, m.rawScroll(hit.Pane)+delta)
	m.setCursor(hit.Pane, m.rawCursor(hit.Pane)+delta)
	m.clampCursor(hit.Pane)

	// The preview follows the active pane, so scrolling any other pane
	// cannot change it.
	if hit.Pane != m.activePane {
		return m, nil
	}
	return m, m.updatePreview()
}
