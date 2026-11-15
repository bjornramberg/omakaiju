package app

import (
	"path/filepath"
	"slices"
	"strings"
	"testing"

	tea "charm.land/bubbletea/v2"
)

// rightClickAt builds a right button press, the event a context menu opens on.
func rightClickAt(x, y int) tea.MouseMsg {
	return tea.MouseClickMsg(tea.Mouse{X: x, Y: y, Button: tea.MouseRight})
}

// motionAt builds a pointer motion over a cell.
func motionAt(x, y int) tea.MouseMsg {
	return tea.MouseMotionMsg(tea.Mouse{X: x, Y: y, Button: tea.MouseNone})
}

// menuLabels flattens the menu's rows for comparison. A separator is spelled
// out as its own label so a missing or extra divider fails the comparison
// rather than going unnoticed.
func menuLabels(items []menuItem) []string {
	out := make([]string, len(items))
	for i, it := range items {
		if it.separator {
			out[i] = "──"
			continue
		}
		out[i] = it.label
	}
	return out
}

// openFileMenu right-clicks the given row of the left pane and returns the
// model with the menu open. It fails rather than returning nil so a broken
// open does not turn every later assertion into a panic.
func openFileMenu(t *testing.T, m Model, row int) Model {
	t.Helper()
	got := updateModel(t, m, rightClickAt(3, mouseTopBarRows+mouseFirstFileRow+row))
	if got.menu == nil {
		t.Fatalf("right-click on row %d did not open a menu", row)
	}
	return got
}

// A right-click on a file row opens the entry's actions, focuses the pane it
// landed on, and moves the cursor to the row the actions are about.
func TestContextMenuRightClickOnFileRow(t *testing.T) {
	m, _ := mouseFixture(t, 8)
	m.activePane = 0

	got := openFileMenu(t, m, 2)
	if got.activePane != 0 {
		t.Errorf("activePane = %d, want 0", got.activePane)
	}
	if got.leftCursor != 2 {
		t.Errorf("leftCursor = %d, want 2", got.leftCursor)
	}

	want := []string{"Open", "Rename", "──", "Copy", "Mark", "──", "Delete"}
	if labels := menuLabels(got.menu.items); !slices.Equal(labels, want) {
		t.Errorf("menu items = %v, want %v", labels, want)
	}
	if name := got.visibleFiles(0)[2].Name; got.menu.title != name {
		t.Errorf("menu title = %q, want the entry's name %q", got.menu.title, name)
	}

	// The same gesture on the other pane focuses that pane and uses its rows.
	// The menu is closed first: a right-click while it is open is a dismissal.
	got = updateModel(t, got, arrow(tea.KeyEscape))
	got = updateModel(t, got, rightClickAt(50, mouseTopBarRows+mouseFirstFileRow+3))
	if got.menu == nil {
		t.Fatal("right-click on the right pane did not open a menu")
	}
	if got.activePane != 1 {
		t.Errorf("activePane = %d, want 1", got.activePane)
	}
	if got.rightCursor != 3 {
		t.Errorf("rightCursor = %d, want 3", got.rightCursor)
	}
	if name := got.visibleFiles(1)[3].Name; got.menu.title != name {
		t.Errorf("menu title = %q, want the entry's name %q", got.menu.title, name)
	}
}

// The ".." row answers to enter and to a double-click for going up a level and
// to nothing else, so its menu offers that and nothing else. It must not become
// a way to copy, mark, rename or delete the directory the pane is standing in.
func TestContextMenuParentRowOffersOnlyGoingUp(t *testing.T) {
	m, dir := mouseFixture(t, 8)

	got := updateModel(t, m, rightClickAt(3, mouseTopBarRows+mouseParentRow))
	if got.menu == nil {
		t.Fatal("right-click on the \"..\" row did not open a menu")
	}
	if !got.menu.parent {
		t.Error("the menu is not marked as being about the parent row")
	}
	if got.leftCursor != parentCursor {
		t.Errorf("leftCursor = %d, want the parent row %d", got.leftCursor, parentCursor)
	}

	if len(got.menu.items) != 1 {
		t.Fatalf("the parent row's menu has %d rows, want exactly 1: %v",
			len(got.menu.items), menuLabels(got.menu.items))
	}
	only := got.menu.items[0]
	if only.separator || only.label != "Go to parent directory" || only.action != actionGoToParent {
		t.Errorf("the parent row's only item = %+v, want the go-to-parent action", only)
	}

	// Spelled out as well as compared: a future item list could keep the one
	// row and quietly add a destructive one, and this is the test that says
	// which actions are not allowed here at all.
	for _, it := range got.menu.items {
		switch it.action {
		case actionCopy, actionMark, actionRename, actionDelete:
			t.Errorf("the parent row's menu offers %q, which would act on the parent directory", it.label)
		}
	}

	// The one action really goes up a level.
	got, cmd := updateModelCmd(t, got, arrow(tea.KeyEnter))
	if got.menu != nil {
		t.Error("enter left the menu open")
	}
	if cmd == nil {
		t.Fatal("enter on the go-to-parent item ran nothing")
	}
	if want := filepath.Dir(dir); got.leftPending != want {
		t.Errorf("pending path = %q, want the parent %q", got.leftPending, want)
	}
}

// A right-click off the rows belongs to the directory, so its menu offers the
// actions that make and reorder entries rather than the ones about one entry.
func TestContextMenuBackgroundOffersDirectoryActions(t *testing.T) {
	m, dir := mouseFixture(t, 8)

	// The pane's blank rows under a short listing: inside the pane, on no row.
	got := updateModel(t, m, rightClickAt(3, mouseTopBarRows+mouseFirstFileRow+12))
	if got.menu == nil {
		t.Fatal("right-click on the background did not open a menu")
	}
	if got.menu.row != -1 {
		t.Errorf("menu row = %d, want -1 for the background", got.menu.row)
	}

	want := []string{"Paste", "New file", "New directory", "──", "Sort", "Refresh"}
	if labels := menuLabels(got.menu.items); !slices.Equal(labels, want) {
		t.Errorf("background menu items = %v, want %v", labels, want)
	}
	if got.menu.title != dir {
		t.Errorf("menu title = %q, want the directory %q", got.menu.title, dir)
	}

	for _, it := range got.menu.items {
		switch it.action {
		case actionOpen, actionRename, actionCopy, actionMark, actionDelete:
			t.Errorf("the background menu offers %q, which needs a row", it.label)
		}
	}
}

// A right-click is also the way out: a second one dismisses the menu instead
// of re-opening it on the same cell.
func TestContextMenuRightClickWhileOpenClosesIt(t *testing.T) {
	m, _ := mouseFixture(t, 8)
	got := openFileMenu(t, m, 1)

	got = updateModel(t, got, rightClickAt(3, mouseTopBarRows+mouseFirstFileRow+1))
	if got.menu != nil {
		t.Errorf("a right-click while the menu was open left it open: %v", menuLabels(got.menu.items))
	}
}

// A right-click outside both panes has no row and no directory to act on, so
// it changes nothing at all.
func TestContextMenuRightClickOutsidePanesDoesNothing(t *testing.T) {
	m, _ := mouseFixture(t, 8)
	m.leftCursor = 2

	// Row 29 is the bottom bar, below every pane.
	got := updateModel(t, m, rightClickAt(50, 29))
	if got.menu != nil {
		t.Error("a right-click on the bottom bar opened a menu")
	}
	if got.leftCursor != 2 {
		t.Errorf("leftCursor = %d, want 2", got.leftCursor)
	}
}

// The menu owns the keyboard while it is open: q does not quit, the arrows move
// the menu's highlight, and everything else is swallowed.
func TestContextMenuOwnsTheKeyboard(t *testing.T) {
	m, _ := mouseFixture(t, 8)
	got := openFileMenu(t, m, 2)

	if got.menu.selected != 0 {
		t.Fatalf("menu opened with item %d selected, want the first action", got.menu.selected)
	}

	got, cmd := updateModelCmd(t, got, key('q'))
	if cmd != nil {
		t.Error("q asked the runtime to quit while a menu was open")
	}
	if got.menu == nil {
		t.Fatal("q closed the menu")
	}

	// A letter that means something in normal mode is swallowed too: it must
	// not start jump mode behind the panel.
	got = updateModel(t, got, key('j'))
	if got.jumpActive {
		t.Error("j started jump mode behind the menu")
	}

	// Down moves the menu's highlight, not the file cursor.
	got = updateModel(t, got, arrow(tea.KeyDown))
	if got.menu.selected != 1 {
		t.Errorf("down selected item %d, want 1", got.menu.selected)
	}
	if got.leftCursor != 2 {
		t.Errorf("down moved the file cursor to %d, want 2", got.leftCursor)
	}

	// The separator between Rename and Copy is skipped, so the next selectable
	// item after 1 is 3.
	got = updateModel(t, got, arrow(tea.KeyDown))
	if got.menu.selected != 3 {
		t.Errorf("down from 1 selected item %d, want 3 (the separator is skipped)", got.menu.selected)
	}

	// Up skips it again, and the highlight stops at the first item.
	got = updateModel(t, got, arrow(tea.KeyUp))
	if got.menu.selected != 1 {
		t.Errorf("up selected item %d, want 1", got.menu.selected)
	}
	got = updateModel(t, got, arrow(tea.KeyUp))
	got = updateModel(t, got, arrow(tea.KeyUp))
	if got.menu.selected != 0 {
		t.Errorf("up past the first item selected %d, want it to stop at 0", got.menu.selected)
	}
	if got.leftCursor != 2 {
		t.Errorf("arrow keys moved the file cursor to %d, want 2", got.leftCursor)
	}
}

// Enter runs the highlighted item and closes the menu before the item's own
// prompt is drawn.
func TestContextMenuEnterRunsTheSelectedItem(t *testing.T) {
	m, _ := mouseFixture(t, 8)
	got := openFileMenu(t, m, 2)

	// Down once: Rename.
	got = updateModel(t, got, arrow(tea.KeyDown))
	got, _ = updateModelCmd(t, got, arrow(tea.KeyEnter))

	if got.menu != nil {
		t.Error("enter left the menu open")
	}
	if got.mode != inputRename {
		t.Errorf("mode = %d, want the rename prompt", got.mode)
	}
	entry := got.visibleFiles(0)[2]
	if got.inputBuf != entry.Name {
		t.Errorf("rename buffer = %q, want %q", got.inputBuf, entry.Name)
	}
	if got.renameTarget != entry.Path {
		t.Errorf("rename target = %q, want %q", got.renameTarget, entry.Path)
	}
}

// Esc puts the menu away and runs nothing: the status the user was looking at
// survives, and no prompt opens.
func TestContextMenuEscapeClosesWithoutRunning(t *testing.T) {
	m, _ := mouseFixture(t, 8)
	m.opResult = "previous status"
	got := openFileMenu(t, m, 2)

	got, cmd := updateModelCmd(t, got, arrow(tea.KeyEscape))
	if cmd != nil {
		t.Error("esc asked the runtime to do something")
	}
	if got.menu != nil {
		t.Error("esc left the menu open")
	}
	if got.mode != inputNone {
		t.Errorf("esc opened prompt mode %d", got.mode)
	}
	if got.opResult != "previous status" {
		t.Errorf("esc changed the status to %q, want it untouched", got.opResult)
	}
}

// A menu the pointer cannot reach is not a menu, so the frame keeps reporting
// the mouse while one is open. The prompts still release it.
func TestContextMenuKeepsTheMouse(t *testing.T) {
	m, _ := mouseFixture(t, 8)
	got := openFileMenu(t, m, 2)

	if mode := got.View().MouseMode; mode == tea.MouseModeNone {
		t.Error("the mouse was released while a menu was open")
	}
	if mode := got.mouseMode(); mode != tea.MouseModeCellMotion {
		t.Errorf("mouseMode() = %v, want cell motion", mode)
	}
}

// Moving the pointer over an item highlights that item without moving the
// keyboard's selection, and moving it off the menu clears the pointer highlight
// again.
func TestContextMenuPointerMovesTheHover(t *testing.T) {
	m, _ := mouseFixture(t, 8)
	got := openFileMenu(t, m, 2)

	place := got.menuPlacement()
	// The second item sits below the border, the top padding, the title and
	// the rule that follows it.
	insideX := place.x + 3
	insideY := place.y + 4 + 1

	got = updateModel(t, got, motionAt(insideX, insideY))
	if got.menu.hovered != 1 {
		t.Errorf("hovered = %d, want 1", got.menu.hovered)
	}
	if got.menu.selected != 0 {
		t.Errorf("pointer motion moved the keyboard selection to %d", got.menu.selected)
	}

	got = updateModel(t, got, motionAt(2, 1))
	if got.menu.hovered != -1 {
		t.Errorf("hovered = %d after leaving the menu, want -1", got.menu.hovered)
	}
}

// A left click on an item runs it and closes the menu.
func TestContextMenuClickRunsTheItem(t *testing.T) {
	m, _ := mouseFixture(t, 8)
	got := openFileMenu(t, m, 2)

	place := got.menuPlacement()
	// Item 1 of the entry menu is Rename.
	got = updateModel(t, got, clickAt(place.x+3, place.y+4+1))

	if got.menu != nil {
		t.Error("clicking an item left the menu open")
	}
	if got.mode != inputRename {
		t.Errorf("mode = %d, want the rename prompt", got.mode)
	}
}

// A click that misses the items dismisses the menu and does nothing else: the
// row under the pointer must not be selected on the way out.
func TestContextMenuClickOutsideDismisses(t *testing.T) {
	m, _ := mouseFixture(t, 8)
	got := openFileMenu(t, m, 2)

	// The right pane's row 5 is a real row, and well away from the menu.
	got = updateModel(t, got, clickAt(50, mouseTopBarRows+mouseFirstFileRow+5))
	if got.menu != nil {
		t.Error("a click outside the menu left it open")
	}
	if got.leftCursor != 2 {
		t.Errorf("leftCursor = %d, want 2: dismissing must not click the pane", got.leftCursor)
	}
	if got.rightCursor == 5 {
		t.Error("the dismissing click also selected a row")
	}
}

// The menu opens near the bottom of the screen, where the ui clamps it to keep
// it visible. The click is resolved against the clamped position: a hit-test
// against the raw pointer position would miss an item that is plainly drawn.
func TestContextMenuHitTestUsesClampedPosition(t *testing.T) {
	m, _ := mouseFixture(t, 8)

	// Row 25 is inside the right pane and near the bottom of the 30-row
	// screen, so the panel has to move up to fit.
	got := updateModel(t, m, rightClickAt(50, 25))
	if got.menu == nil {
		t.Fatal("right-click near the bottom did not open a menu")
	}
	place := got.menuPlacement()
	if place.y >= got.menu.y {
		t.Fatalf("setup: menu was not clamped (drawn at %d, pointer row %d)", place.y, got.menu.y)
	}

	// Item 1 of a background menu is New file.
	got = updateModel(t, got, clickAt(place.x+3, place.y+4+1))
	if got.menu != nil {
		t.Error("clicking a clamped item dismissed the menu instead of running it")
	}
	if got.mode != inputAdd {
		t.Errorf("mode = %d, want the new-file prompt: the at-test used the wrong origin", got.mode)
	}
}

// The menu is drawn over the finished frame at the clamped position, so what
// the user sees is what the hit-test resolves. The frame keeps its line count:
// an overlay replaces cells, it does not push the layout around.
func TestContextMenuIsDrawnAtTheClampedPosition(t *testing.T) {
	m, _ := mouseFixture(t, 8)
	plain := stripTestANSI(m.View().Content)
	lines := strings.Split(plain, "\n")

	// Open near the bottom so the drawn position is the clamped one rather
	// than the pointer position.
	got := updateModel(t, m, rightClickAt(50, 25))
	if got.menu == nil {
		t.Fatal("right-click near the bottom did not open a menu")
	}
	place := got.menuPlacement()

	drawn := strings.Split(stripTestANSI(got.View().Content), "\n")
	if len(drawn) != len(lines) {
		t.Errorf("the menu changed the frame's line count from %d to %d", len(lines), len(drawn))
	}

	// The first item sits below the border, the padding, the title and the
	// rule under it.
	row := place.y + 4
	if row >= len(drawn) {
		t.Fatalf("item row %d is outside the %d-line frame", row, len(drawn))
	}
	if !strings.Contains(drawn[row], "Paste") {
		t.Errorf("row %d = %q, want the menu's first item at the drawn origin", row, drawn[row])
	}
}

// The menu's actions are the app's actions. These are the ones whose effect is
// visible in the model without touching the disk: copy arms the clipboard,
// mark marks, and delete opens the confirmation instead of deleting outright.
func TestContextMenuActionsDoWhatTheySay(t *testing.T) {
	m, _ := mouseFixture(t, 8)

	cases := []struct {
		name string
		// downs is how many down presses reach the item from the first one.
		downs int
		check func(*testing.T, Model)
	}{
		{
			name:  "copy",
			downs: 2, // Open, Rename, [separator], Copy
			check: func(t *testing.T, m Model) {
				if !m.clipboardOn {
					t.Fatal("copy did not arm the clipboard")
				}
				if len(m.clipboard.Files) != 1 || m.clipboard.Files[0].Name != "f02.txt" {
					t.Errorf("clipboard = %+v, want f02.txt", m.clipboard.Files)
				}
			},
		},
		{
			name:  "mark",
			downs: 3, // ... Mark
			check: func(t *testing.T, m Model) {
				entry := m.visibleFiles(0)[2]
				if _, marked := m.marked[entry.Path]; !marked {
					t.Errorf("%s was not marked", entry.Name)
				}
			},
		},
		{
			name:  "delete",
			downs: 4, // ... [separator], Delete
			check: func(t *testing.T, m Model) {
				if !m.confirmDelete {
					t.Fatal("delete did not open the confirmation")
				}
				if want := m.visibleFiles(0)[2].Path; m.deleteTarget != want {
					t.Errorf("delete target = %q, want %q", m.deleteTarget, want)
				}
			},
		},
	}

	for _, tc := range cases {
		t.Run(tc.name, func(t *testing.T) {
			got := openFileMenu(t, m, 2)
			for i := 0; i < tc.downs; i++ {
				got = updateModel(t, got, arrow(tea.KeyDown))
			}
			got, _ = updateModelCmd(t, got, arrow(tea.KeyEnter))
			if got.menu != nil {
				t.Error("the menu stayed open after its action ran")
			}
			tc.check(t, got)
		})
	}
}

// The mark item says what it will do: "Mark" on an unmarked entry and "Unmark"
// on a marked one.
func TestContextMenuMarkLabelFollowsTheMarkedSet(t *testing.T) {
	m, _ := mouseFixture(t, 8)
	m = updateModel(t, m, shiftKey('m')) // mark the entry under the cursor

	got := openFileMenu(t, m, 0)
	if label := got.menu.items[4].label; label != "Unmark" {
		t.Errorf("item label = %q, want %q for a marked entry", label, "Unmark")
	}

	// Unmarking it puts the old label back.
	got = updateModel(t, got, arrow(tea.KeyEscape))
	got = updateModel(t, got, shiftKey('m'))
	got = openFileMenu(t, got, 0)
	if label := got.menu.items[4].label; label != "Mark" {
		t.Errorf("item label = %q, want %q for an unmarked entry", label, "Mark")
	}
}
