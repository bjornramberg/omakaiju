package app

import (
	tea "charm.land/bubbletea/v2"
)

// wheelRows is how many listing rows one wheel notch moves the cursor. Three
// is the usual terminal scroll step and keeps a long listing quick to cross
// without skipping over a name.
const wheelRows = 3

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

// handleMouse applies the tier-one mouse bindings: a left click focuses a pane
// and selects the row under the pointer, and the wheel scrolls the pane
// beneath it. Everything else is ignored — right and middle clicks, releases,
// and motion belong to tiers this build does not have.
func (m Model) handleMouse(msg tea.MouseMsg) (tea.Model, tea.Cmd) {
	if !m.cfg.Mouse || m.modalActive() {
		return m, nil
	}

	switch msg := msg.(type) {
	case tea.MouseClickMsg:
		if msg.Button != tea.MouseLeft {
			return m, nil
		}
		return m.clickPane(msg.X, msg.Y)
	case tea.MouseWheelMsg:
		return m.wheelPane(msg.X, msg.Y, msg.Button)
	}
	return m, nil
}

// clickPane focuses the pane under the pointer and, when it landed on a file
// row, moves that pane's cursor there. A click on the chrome focuses without
// selecting, and a click outside both panes changes nothing.
func (m Model) clickPane(x, y int) (tea.Model, tea.Cmd) {
	layout := m.layout()
	hit, ok := layout.HitPane(m.paneViews(layout), x, y)
	if !ok {
		return m, nil
	}

	focused := m.activePane != hit.Pane
	m.activePane = hit.Pane
	if hit.Row >= 0 {
		m.setCursor(hit.Pane, hit.Row)
	}

	// Nothing visible changed: the pane was already focused and the click
	// missed every row, so there is no preview to rebuild.
	if !focused && hit.Row < 0 {
		return m, nil
	}
	return m, m.updatePreview()
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

	m.setCursor(hit.Pane, m.rawCursor(hit.Pane)+delta)
	m.clampCursor(hit.Pane)

	// The preview follows the active pane, so scrolling any other pane
	// cannot change it.
	if hit.Pane != m.activePane {
		return m, nil
	}
	return m, m.updatePreview()
}
