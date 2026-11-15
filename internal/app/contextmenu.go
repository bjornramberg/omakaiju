package app

import (
	"github.com/bjornramberg/omakaiju/internal/ui"

	tea "charm.land/bubbletea/v2"
)

// menuMaxWidth caps how wide the context menu may grow. The title is the
// entry's name or the directory's path, and an unbounded title would stretch
// the panel across the terminal and cover the listing the menu belongs to. The
// labels themselves are short, so the cap only ever bites on the title.
const menuMaxWidth = 48

// menuAction is what a context menu item does. The zero value is an item that
// does nothing, which is exactly what a separator carries: a separator is a
// divider, not an action, and the dispatch must not mistake it for one.
type menuAction int

const (
	actionNone menuAction = iota
	actionOpen
	actionRename
	actionCopy
	actionMark
	actionDelete
	actionPaste
	actionNewFile
	actionNewDir
	actionSort
	actionRefresh
	actionGoToParent
)

// menuItem is one row of a context menu: what it says and what it does.
type menuItem struct {
	// label is what the menu shows.
	label string
	// action is what choosing the row does.
	action menuAction
	// separator marks a divider: it is drawn, it is not an action, and it
	// cannot be chosen. It stays in the slice so an item's index in the menu
	// is the same index the app uses for dispatch.
	separator bool
	// disabled marks an action that cannot run right now. The keyboard
	// highlight and the pointer both skip it rather than letting the user
	// choose something that would do nothing.
	disabled bool
}

// contextMenu is a right-click menu waiting for a choice.
type contextMenu struct {
	// pane is the pane the menu was opened on. The actions act on that pane's
	// cursor, and the pane is made active when the menu opens, so a menu that
	// outlives a focus change still acts where the user pointed.
	pane int
	// row is the listing row the menu is about, or -1 for a menu opened on
	// the pane's background.
	row int
	// parent says the menu is about the ".." row.
	parent bool
	// title names what the menu is about: the entry's name, or the
	// directory's path for a background menu.
	title string
	// items are the actions, in the order they are shown. The index of an
	// item here is the index the ui's menu shows, so the two agree and the
	// hit-test's answer can be used to dispatch directly.
	items []menuItem
	// selected is the keyboard's highlight. -1 means nothing is selected,
	// which is only possible for a menu with no selectable item at all.
	selected int
	// hovered is the item under the pointer, or -1 when the pointer is not
	// over an item. It is kept apart from selected so the panel can show the
	// pointer's target without moving the keyboard's: the two only meet when
	// the user uses one of them.
	hovered int
	// x and y are where the pointer was when the menu opened. The ui clamps
	// them so the panel stays on screen, and menuPlacement is the single
	// place that clamp is applied. Both the drawing and the hit-test read the
	// clamped result from there, so a click can never land on a different
	// item than the one that was drawn under it.
	x, y int
}

// clone returns a copy of the menu that an update can change freely.
//
// Model is copied by value on every message, but the menu is a pointer, so a
// write through it would be visible to the pre-update state Update hands back
// after a panic in a handler. Replacing the pointer costs one small
// allocation and makes the recovery guarantee hold for the menu too.
func (c *contextMenu) clone() *contextMenu {
	next := *c
	return &next
}

// openContextMenu opens the context menu at the pointer, or closes the one
// already there.
//
// A second right-click is the way out of a menu, so it is handled before
// anything else: reopening at the same cell would leave the user looking at a
// menu that never visibly closed, which reads as a misclick rather than as a
// dismissal.
func (m Model) openContextMenu(x, y int) (tea.Model, tea.Cmd) {
	if m.menu != nil {
		m.menu = nil
		return m, nil
	}

	layout := m.layout()
	hit, ok := layout.HitPane(m.paneViews(layout), x, y)
	if !ok {
		// The pointer is over the bars, the preview or the too-small
		// placeholder. There is no row and no directory there, so a menu
		// would have nothing to act on.
		return m, nil
	}

	// The menu's actions act on the pane's cursor, so the pane under the
	// pointer has to become the active one and the cursor has to land where
	// the user pointed. A right-click on the ".." row puts the cursor on that
	// row, exactly as a left click would, because the row's one action is the
	// row's own action.
	focused := m.activePane != hit.Pane
	m.activePane = hit.Pane
	if hit.Parent {
		m.setCursor(hit.Pane, parentCursor)
	} else if hit.Row >= 0 {
		m.setCursor(hit.Pane, hit.Row)
	}
	m.reconcileScrolls()

	// A press that opens a menu is not the start of a drag. Without this, a
	// drag left over from an earlier press could be revived by the release of
	// a click that was only meant to open a menu.
	m.drag = nil

	menu := &contextMenu{
		pane:     hit.Pane,
		row:      hit.Row,
		parent:   hit.Parent,
		x:        x,
		y:        y,
		selected: -1,
		hovered:  -1,
	}
	menu.title = m.menuTitle(menu)
	menu.items = m.menuItems(menu)
	menu.selected = firstSelectable(menu.items)
	m.menu = menu

	// Nothing visible changed: the pane was already focused and the click
	// missed every row, so there is no preview to rebuild. This mirrors the
	// left click, which is the gesture the right click borrows its selection
	// from.
	if !focused && hit.Row < 0 && !hit.Parent {
		return m, nil
	}
	return m, m.updatePreview()
}

// menuTitle names the thing the menu is about, so the panel says which entry
// the actions belong to while it floats over the listing. A background menu
// names the directory, and the ".." row names the directory it leads to: that
// is what its one action changes.
func (m Model) menuTitle(c *contextMenu) string {
	switch {
	case c.parent:
		return m.parentPath(c.pane)
	case c.row >= 0:
		if files := m.visibleFiles(c.pane); c.row < len(files) {
			return files[c.row].Name
		}
	}
	return m.rawPath(c.pane)
}

// menuItems builds the actions that apply to what was clicked.
//
// The ".." row is deliberately the sparsest menu in the app. The row answers
// to enter and to a double-click for going up a level, and to nothing else, so
// its menu offers that and nothing else. Copying, marking, renaming or
// deleting it would act on the directory the user is standing in, which is a
// different directory from the one the pane is showing — and deleting it
// would remove the pane's own location out from under it.
func (m Model) menuItems(c *contextMenu) []menuItem {
	if c.parent {
		return []menuItem{
			{label: "Go to parent directory", action: actionGoToParent},
		}
	}

	if c.row >= 0 {
		files := m.visibleFiles(c.pane)
		if c.row < len(files) {
			entry := files[c.row]
			// The label has to say what the item will do, not what the entry
			// is: pressing "Mark" on a marked entry would unmark it, and a
			// menu that promises the opposite of its effect is worse than one
			// that says nothing.
			markLabel := "Mark"
			if _, marked := m.marked[entry.Path]; marked {
				markLabel = "Unmark"
			}
			return []menuItem{
				{label: "Open", action: actionOpen},
				{label: "Rename", action: actionRename},
				{separator: true},
				{label: "Copy", action: actionCopy},
				{label: markLabel, action: actionMark},
				{separator: true},
				{label: "Delete", action: actionDelete},
			}
		}
		// The listing shrank between the hit-test and the menu being built.
		// The directory's actions are still meaningful, and they are the only
		// ones that do not name a row that no longer exists.
	}

	return []menuItem{
		{label: "Paste", action: actionPaste},
		{label: "New file", action: actionNewFile},
		{label: "New directory", action: actionNewDir},
		{separator: true},
		{label: "Sort", action: actionSort},
		{label: "Refresh", action: actionRefresh},
	}
}

// firstSelectable is the index of the first item the keyboard can land on, or
// -1 for a menu that has nothing to choose. A menu opens with its first real
// action highlighted so enter does something sensible before any arrow key.
func firstSelectable(items []menuItem) int {
	for i, it := range items {
		if it.separator || it.disabled {
			continue
		}
		return i
	}
	return -1
}

// moveMenuSelection moves the keyboard highlight by step, skipping separators
// and items that cannot run. It stops at the ends rather than wrapping: a
// highlight that wraps makes a held arrow key run off the menu and back onto
// an item the user has already passed.
func (m *Model) moveMenuSelection(step int) {
	if m.menu == nil {
		return
	}
	items := m.menu.items

	next := m.menu.selected
	if next < 0 {
		// With nothing selected, the first step should land on the nearest
		// end: down on the first item, up on the last.
		if step > 0 {
			next = -1
		} else {
			next = len(items)
		}
	}

	for {
		next += step
		if next < 0 || next >= len(items) {
			// The highlight stays where it is; moving it off the menu would
			// leave the user without a visible target.
			return
		}
		if items[next].separator || items[next].disabled {
			continue
		}
		m.menu = m.menu.clone()
		m.menu.selected = next
		return
	}
}

// handleMenuKeys drives the context menu. The menu owns the keyboard while it
// is open, so every key not listed here is swallowed: q must not quit the app
// out from under a menu the user is aiming at, and a stray letter must not
// start a search or a filter behind the panel.
func (m Model) handleMenuKeys(msg tea.KeyMsg) (tea.Model, tea.Cmd) {
	switch keyID(msg) {
	case "up":
		m.moveMenuSelection(-1)
		return m, nil
	case "down":
		m.moveMenuSelection(1)
		return m, nil
	case "enter":
		return m.chooseMenuSelection()
	case "esc":
		m.menu = nil
		return m, nil
	}
	return m, nil
}

// chooseMenuSelection runs the item the keyboard highlight is on. The bounds
// check is what keeps a menu with no selectable item from indexing past the
// slice: with nothing highlighted, enter has nothing to run.
func (m Model) chooseMenuSelection() (tea.Model, tea.Cmd) {
	if m.menu == nil || m.menu.selected < 0 || m.menu.selected >= len(m.menu.items) {
		return m, nil
	}
	return m.runMenuAction(m.menu.items[m.menu.selected].action)
}

// menuMouse applies the pointer to the open context menu: motion moves the
// pointer's highlight, a left click runs the item under it, and a click that
// misses the items dismisses the menu. The wheel is ignored because the menu
// is shorter than the screen and scrolls nothing.
func (m Model) menuMouse(msg tea.MouseMsg) (tea.Model, tea.Cmd) {
	switch msg := msg.(type) {
	case tea.MouseClickMsg:
		switch msg.Button {
		case tea.MouseRight:
			// A right-click on the menu does what a right-click anywhere
			// else does: it puts the menu away.
			m.menu = nil
			return m, nil
		case tea.MouseLeft:
			item := m.menuItemAt(msg.X, msg.Y)
			if item < 0 || m.menu.items[item].disabled {
				// A click on the border, a separator, or anywhere off the
				// menu dismisses it and does nothing else. Acting on whatever
				// is underneath would turn a dismissal into an unintended
				// second action.
				m.menu = nil
				return m, nil
			}
			return m.runMenuAction(m.menu.items[item].action)
		}
		return m, nil
	case tea.MouseMotionMsg:
		m.setMenuHover(msg.X, msg.Y)
		return m, nil
	}
	return m, nil
}

// menuItemAt reports the item under a screen position, or -1 when the position
// is not over a selectable item.
//
// The hit-test is done against the same placed menu the frame drew, so the
// answer is about the pixels the user is pointing at rather than about the
// position the menu was asked for before clamping.
func (m Model) menuItemAt(x, y int) int {
	if m.menu == nil {
		return -1
	}
	place := m.menuPlacement()
	return place.panel.ItemAt(x, y, place.x, place.y)
}

// setMenuHover moves the pointer's highlight to the item under the position.
//
// The menu is replaced rather than written through its pointer for the same
// reason every other menu change is: the pre-update state must stay untouched
// if this handler panics.
func (m *Model) setMenuHover(x, y int) {
	if m.menu == nil {
		return
	}
	item := m.menuItemAt(x, y)
	if item == m.menu.hovered {
		return
	}
	m.menu = m.menu.clone()
	m.menu.hovered = item
}

// runMenuAction does what the menu item says.
//
// The action bodies are the ones the key handler uses, not copies of them: a
// file manager with two versions of "delete the entry under the cursor" has
// two places to fix the day one of them is wrong. The extraction is why the
// menu and the keys can never disagree about what an action does.
func (m Model) runMenuAction(action menuAction) (tea.Model, tea.Cmd) {
	if m.menu != nil {
		// The menu acts on the pane it was opened on. The open already made
		// that pane active, and setting it again keeps a focus change made
		// while the menu was up from moving the action somewhere else.
		m.activePane = m.menu.pane
		// The menu closes before the action runs, so the prompt an action
		// opens — rename, delete confirmation, a new name — is not drawn
		// underneath a menu that is still on screen.
		m.menu = nil
	}

	switch action {
	case actionNone:
		// A separator carries no action. It is never selectable, but a zero
		// value that silently fell through to the default would be one bug
		// away from running the wrong thing.
		return m, nil
	case actionOpen:
		return m.openCursorEntry()
	case actionRename:
		return m.startRename()
	case actionCopy:
		return m.handleYank()
	case actionMark:
		return m.handleMove()
	case actionDelete:
		return m.handleDelete()
	case actionPaste:
		return m.handlePaste()
	case actionNewFile:
		return m.startAddFile()
	case actionNewDir:
		return m.startAddDir()
	case actionSort:
		return m.cycleSortAction()
	case actionRefresh:
		return m, m.reloadPaneCmd(m.activePane)
	case actionGoToParent:
		return m.goToParent(m.activePane)
	}
	return m, nil
}

// placedMenu is the open menu together with the position it actually occupies.
// The panel and its origin have to travel together: passing them separately
// invites drawing one position and hit-testing another.
type placedMenu struct {
	panel ui.Menu
	x, y  int
	// width and height are the panel's own size, which is what the overlay
	// has to be told: it splices the panel over the frame, and a panel sized
	// by the screen would paint over everything to its right.
	width, height int
}

// menuPlacement builds the menu and clamps its position to the screen.
//
// This is the one place the pointer position is turned into the drawn
// position. The frame draws at these coordinates and the mouse resolves clicks
// against them, so the clamp can never make a click land on a different item
// than the one that was drawn under it.
func (m Model) menuPlacement() placedMenu {
	items := make([]ui.MenuItem, len(m.menu.items))
	for i, it := range m.menu.items {
		items[i] = ui.MenuItem{
			Label:     it.label,
			Disabled:  it.disabled,
			Separator: it.separator,
		}
	}
	panel := ui.Menu{
		Items:    items,
		Title:    m.menu.title,
		Selected: m.menu.selected,
		Hovered:  m.menu.hovered,
		Theme:    m.theme,
		Width:    menuMaxWidth,
	}
	width, height := panel.Size()
	x, y := ui.OverlayPosition(m.menu.x, m.menu.y, width, height, m.width, m.height)
	return placedMenu{panel: panel, x: x, y: y, width: width, height: height}
}
