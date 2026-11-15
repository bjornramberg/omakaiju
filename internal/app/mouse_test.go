package app

import (
	"fmt"
	"os"
	"path/filepath"
	"strings"
	"testing"

	"github.com/bjornramberg/omakaiju/internal/config"
	"github.com/bjornramberg/omakaiju/internal/fs"

	tea "charm.land/bubbletea/v2"
)

// Screen geometry for the fixture below, asserted independently of the code
// that computes it: two bars of three rows, and a pane whose first file row
// comes after its border, padding, header, and the blank line under the header.
const (
	mouseTopBarRows   = 3
	mouseFirstFileRow = 4
)

// mouseFixture builds a model with n files in both panes, the mouse enabled,
// and a terminal large enough for the side-by-side layout with a preview.
func mouseFixture(t *testing.T, n int) (Model, string) {
	t.Helper()
	dir := t.TempDir()
	for i := 0; i < n; i++ {
		name := filepath.Join(dir, fmt.Sprintf("f%02d.txt", i))
		if err := os.WriteFile(name, []byte("x"), 0o644); err != nil {
			t.Fatal(err)
		}
	}

	files, err := fs.ReadDir(dir)
	if err != nil {
		t.Fatal(err)
	}

	m := NewModel(config.Config{ThemePath: filepath.Join(dir, "none.toml")})
	m.cfg.Mouse = true
	m.leftPath, m.leftFiles = dir, files
	m.rightPath, m.rightFiles = dir, files
	m.width, m.height = 100, 30
	m.updatePreview()
	return m, dir
}

func clickAt(x, y int) tea.MouseMsg {
	return tea.MouseClickMsg(tea.Mouse{X: x, Y: y, Button: tea.MouseLeft})
}

func wheelAt(x, y int, button tea.MouseButton) tea.MouseMsg {
	return tea.MouseWheelMsg(tea.Mouse{X: x, Y: y, Button: button})
}

// updateModel feeds one message and returns the model it produced.
func updateModel(t *testing.T, m Model, msg tea.Msg) Model {
	t.Helper()
	updated, _ := m.Update(msg)
	got, ok := updated.(Model)
	if !ok {
		t.Fatalf("Update returned %T, want Model", updated)
	}
	return got
}

// A left click on a file row focuses that pane and moves its cursor to the row
// under the pointer.
func TestMouseLeftClickSelectsRow(t *testing.T) {
	m, _ := mouseFixture(t, 8)
	m.activePane = 1

	// Row 2 of the left pane.
	got := updateModel(t, m, clickAt(3, mouseTopBarRows+mouseFirstFileRow+2))
	if got.activePane != 0 {
		t.Errorf("activePane = %d, want 0", got.activePane)
	}
	if got.leftCursor != 2 {
		t.Errorf("leftCursor = %d, want 2", got.leftCursor)
	}

	// Row 3 of the right pane focuses it and selects there.
	got = updateModel(t, got, clickAt(50, mouseTopBarRows+mouseFirstFileRow+3))
	if got.activePane != 1 {
		t.Errorf("activePane = %d, want 1", got.activePane)
	}
	if got.rightCursor != 3 {
		t.Errorf("rightCursor = %d, want 3", got.rightCursor)
	}
	if got.leftCursor != 2 {
		t.Errorf("clicking the right pane moved the left cursor to %d", got.leftCursor)
	}
}

// Clicking the chrome focuses the pane without selecting a row: a header or a
// border is not a file.
func TestMouseClickChromeFocusesWithoutSelecting(t *testing.T) {
	m, _ := mouseFixture(t, 8)
	m.activePane = 1
	m.leftCursor = 4

	for _, tc := range []struct {
		name string
		y    int
	}{
		{"top border", mouseTopBarRows},
		{"header", mouseTopBarRows + 1},
		{"blank under the header", mouseTopBarRows + 3},
		{"padding below the listing", mouseTopBarRows + mouseFirstFileRow + 8},
	} {
		t.Run(tc.name, func(t *testing.T) {
			got := updateModel(t, m, clickAt(3, tc.y))
			if got.activePane != 0 {
				t.Errorf("activePane = %d, want 0", got.activePane)
			}
			if got.leftCursor != 4 {
				t.Errorf("leftCursor = %d, want 4 (unchanged)", got.leftCursor)
			}
		})
	}
}

// Clicks in the bars and the preview column belong to no pane, so they must not
// move the focus either.
func TestMouseClickOutsidePanesIgnored(t *testing.T) {
	m, _ := mouseFixture(t, 8)
	m.activePane = 1
	m.leftCursor, m.rightCursor = 1, 2

	for _, tc := range []struct {
		name string
		x, y int
	}{
		{"top bar", 40, 1},
		{"bottom bar", 40, 28},
		{"preview column", 80, 10},
		{"last column", 99, 10},
	} {
		t.Run(tc.name, func(t *testing.T) {
			got := updateModel(t, m, clickAt(tc.x, tc.y))
			if got.activePane != 1 {
				t.Errorf("activePane = %d, want 1", got.activePane)
			}
			if got.leftCursor != 1 || got.rightCursor != 2 {
				t.Errorf("cursors moved to %d/%d, want 1/2", got.leftCursor, got.rightCursor)
			}
		})
	}
}

// A click while the terminal is too small lands on the placeholder and is
// ignored.
func TestMouseClickTooSmallIgnored(t *testing.T) {
	m, _ := mouseFixture(t, 8)
	m.activePane = 1
	m.leftCursor = 2
	m.width, m.height = 40, 9

	got := updateModel(t, m, clickAt(3, 5))
	if got.activePane != 1 || got.leftCursor != 2 {
		t.Errorf("click on the placeholder changed focus/cursor to %d/%d", got.activePane, got.leftCursor)
	}
}

// One wheel notch moves the cursor three rows, clamps at both ends, and leaves
// the focused pane alone even when the pointer is over the other one.
func TestMouseWheelScrollsThreeRows(t *testing.T) {
	m, _ := mouseFixture(t, 8)
	m.activePane = 0

	got := updateModel(t, m, wheelAt(50, mouseTopBarRows+mouseFirstFileRow, tea.MouseWheelDown))
	if got.rightCursor != 3 {
		t.Errorf("one notch down: rightCursor = %d, want 3", got.rightCursor)
	}
	if got.activePane != 0 {
		t.Errorf("the wheel changed the active pane to %d", got.activePane)
	}
	if got.leftCursor != 0 {
		t.Errorf("the wheel scrolled the wrong pane: leftCursor = %d", got.leftCursor)
	}

	got = updateModel(t, got, wheelAt(50, mouseTopBarRows+mouseFirstFileRow, tea.MouseWheelUp))
	if got.rightCursor != 0 {
		t.Errorf("one notch up: rightCursor = %d, want 0", got.rightCursor)
	}

	// Past the top simply stays at the top.
	got = updateModel(t, got, wheelAt(3, mouseTopBarRows+mouseFirstFileRow, tea.MouseWheelUp))
	if got.leftCursor != 0 {
		t.Errorf("wheel up at the top: leftCursor = %d, want 0", got.leftCursor)
	}

	// Past the end clamps to the last entry.
	got.leftCursor = 6
	got = updateModel(t, got, wheelAt(3, mouseTopBarRows+mouseFirstFileRow, tea.MouseWheelDown))
	if got.leftCursor != 7 {
		t.Errorf("wheel down near the end: leftCursor = %d, want 7", got.leftCursor)
	}
}

// The wheel outside both panes does nothing.
func TestMouseWheelOutsidePanesIgnored(t *testing.T) {
	m, _ := mouseFixture(t, 8)
	m.leftCursor = 4

	for _, tc := range []struct {
		name string
		x, y int
	}{
		{"top bar", 40, 1},
		{"bottom bar", 40, 28},
		{"preview column", 80, 10},
	} {
		t.Run(tc.name, func(t *testing.T) {
			got := updateModel(t, m, wheelAt(tc.x, tc.y, tea.MouseWheelDown))
			if got.leftCursor != 4 || got.rightCursor != 0 {
				t.Errorf("wheel at %s moved cursors to %d/%d", tc.name, got.leftCursor, got.rightCursor)
			}
		})
	}
}

// Right and middle clicks, releases, and motion are tier-two or tier-three
// material: they must be inert.
func TestMouseOtherEventsIgnored(t *testing.T) {
	m, _ := mouseFixture(t, 8)
	m.activePane = 1
	m.leftCursor = 1

	events := []struct {
		name string
		msg  tea.Msg
	}{
		{"right click", tea.MouseClickMsg(tea.Mouse{X: 3, Y: 8, Button: tea.MouseRight})},
		{"middle click", tea.MouseClickMsg(tea.Mouse{X: 3, Y: 8, Button: tea.MouseMiddle})},
		{"release", tea.MouseReleaseMsg(tea.Mouse{X: 3, Y: 8, Button: tea.MouseLeft})},
		{"motion", tea.MouseMotionMsg(tea.Mouse{X: 3, Y: 8, Button: tea.MouseNone})},
	}
	for _, tc := range events {
		t.Run(tc.name, func(t *testing.T) {
			got := updateModel(t, m, tc.msg)
			if got.activePane != 1 || got.leftCursor != 1 {
				t.Errorf("%s changed focus/cursor to %d/%d", tc.name, got.activePane, got.leftCursor)
			}
		})
	}
}

// A modal owns the screen, so the mouse is inert while one is open. The panes
// are visible behind the panel and must not move under it.
func TestMouseIgnoredWhileModalActive(t *testing.T) {
	cases := []struct {
		name  string
		setup func(*Model)
	}{
		{"fuzzy finder", func(m *Model) { m.fuzzyActive = true }},
		{"key list", func(m *Model) { m.keyListActive = true }},
		{"text prompt", func(m *Model) { m.mode = inputFilter }},
		{"delete confirmation", func(m *Model) { m.confirmDelete = true }},
		{"empty trash confirmation", func(m *Model) { m.confirmEmptyTrash = true }},
	}

	for _, tc := range cases {
		t.Run(tc.name, func(t *testing.T) {
			m, _ := mouseFixture(t, 8)
			m.activePane = 1
			m.leftCursor = 1
			tc.setup(&m)

			got := updateModel(t, m, clickAt(3, mouseTopBarRows+mouseFirstFileRow+2))
			if got.activePane != 1 || got.leftCursor != 1 {
				t.Errorf("click changed focus/cursor to %d/%d", got.activePane, got.leftCursor)
			}

			got = updateModel(t, got, wheelAt(3, mouseTopBarRows+mouseFirstFileRow, tea.MouseWheelDown))
			if got.leftCursor != 1 {
				t.Errorf("wheel moved leftCursor to %d, want 1", got.leftCursor)
			}
		})
	}
}

// Without the feature the messages are ignored as well, so a terminal that
// reports anyway cannot move the cursor behind the user's back.
func TestMouseIgnoredWhenDisabled(t *testing.T) {
	m, _ := mouseFixture(t, 8)
	m.cfg.Mouse = false
	m.activePane = 1
	m.leftCursor = 1

	got := updateModel(t, m, clickAt(3, mouseTopBarRows+mouseFirstFileRow+2))
	if got.activePane != 1 || got.leftCursor != 1 {
		t.Errorf("click with the feature off changed focus/cursor to %d/%d", got.activePane, got.leftCursor)
	}
	got = updateModel(t, got, wheelAt(3, mouseTopBarRows+mouseFirstFileRow, tea.MouseWheelDown))
	if got.leftCursor != 1 {
		t.Errorf("wheel with the feature off moved leftCursor to %d", got.leftCursor)
	}
}

// The frame reports the mouse mode Bubble Tea acts on. It is off by default, on
// with the feature, and off again for every modal frame so the terminal hands
// back its own text selection while a panel is open.
func TestViewMouseMode(t *testing.T) {
	m, _ := mouseFixture(t, 8)

	cases := []struct {
		name  string
		setup func(*Model)
		want  tea.MouseMode
	}{
		{"default off", func(m *Model) { m.cfg.Mouse = false }, tea.MouseModeNone},
		{"enabled", func(m *Model) {}, tea.MouseModeCellMotion},
		{"fuzzy finder", func(m *Model) { m.fuzzyActive = true }, tea.MouseModeNone},
		{"key list", func(m *Model) { m.keyListActive = true }, tea.MouseModeNone},
		{"text prompt", func(m *Model) { m.mode = inputGoto }, tea.MouseModeNone},
		{"delete confirmation", func(m *Model) { m.confirmDelete = true }, tea.MouseModeNone},
		{"empty trash confirmation", func(m *Model) { m.confirmEmptyTrash = true }, tea.MouseModeNone},
		{"too small", func(m *Model) { m.width, m.height = 40, 9 }, tea.MouseModeCellMotion},
	}

	for _, tc := range cases {
		t.Run(tc.name, func(t *testing.T) {
			model := m
			tc.setup(&model)
			if got := model.View().MouseMode; got != tc.want {
				t.Errorf("View().MouseMode = %v, want %v", got, tc.want)
			}
		})
	}
}

// Stacked panes put the second pane below the first, and a click there must
// resolve against that pane's own rows.
func TestMouseClickStackedPane(t *testing.T) {
	m, _ := mouseFixture(t, 8)
	m.panesStacked = true
	m.activePane = 0

	// Height 30, main area 24, so the top pane is 12 rows and the bottom pane
	// starts at row 3+12.
	y := mouseTopBarRows + 12 + mouseFirstFileRow + 1
	got := updateModel(t, m, clickAt(3, y))
	if got.activePane != 1 {
		t.Fatalf("activePane = %d, want 1", got.activePane)
	}
	if got.rightCursor != 1 {
		t.Errorf("rightCursor = %d, want 1", got.rightCursor)
	}
	if got.leftCursor != 0 {
		t.Errorf("leftCursor = %d, want 0", got.leftCursor)
	}
}

// A filtered pane has one fewer row of listing than the same pane without a
// filter, and its last body row is followed by the count. The click must read
// the window the renderer drew, not the generic height.
func TestMouseClickFilteredPane(t *testing.T) {
	m, _ := mouseFixture(t, 20)
	m.leftFilter = "f"

	// Pane height 24: 24 - 6 chrome - 2 filter rows leaves 16 rows.
	lastRow := 15
	got := updateModel(t, m, clickAt(3, mouseTopBarRows+mouseFirstFileRow+lastRow))
	if got.leftCursor != lastRow {
		t.Errorf("leftCursor = %d, want %d", got.leftCursor, lastRow)
	}

	// The row below is the filter's blank line, not an entry.
	got = updateModel(t, got, clickAt(3, mouseTopBarRows+mouseFirstFileRow+lastRow+1))
	if got.leftCursor != lastRow {
		t.Errorf("click on the filter status moved the cursor to %d", got.leftCursor)
	}
}

// A pane with marked entries carries a status row, so its listing is one row
// shorter and the row past it is chrome. This is the case where the two panes
// show different numbers of rows.
func TestMouseClickMarkedPane(t *testing.T) {
	m, _ := mouseFixture(t, 20)
	m.marked = map[string]fs.Entry{m.leftFiles[0].Path: m.leftFiles[0]}

	// 24 - 6 chrome - 1 status = 17 rows.
	lastRow := 16
	got := updateModel(t, m, clickAt(3, mouseTopBarRows+mouseFirstFileRow+lastRow))
	if got.leftCursor != lastRow {
		t.Errorf("leftCursor = %d, want %d", got.leftCursor, lastRow)
	}

	// The status row is chrome: it focuses the pane it is already in and
	// leaves the cursor where it was.
	got = updateModel(t, got, clickAt(3, mouseTopBarRows+mouseFirstFileRow+lastRow+1))
	if got.leftCursor != lastRow {
		t.Errorf("click on the status row moved the cursor to %d, want %d", got.leftCursor, lastRow)
	}
}

// Clicking a row in the middle of a scrolled listing selects that row without
// moving the listing. This is the regression: the window used to be derived as
// "cursor - rows + 1", so the click dragged the window down and left the
// clicked file on the last row of the pane. A click is a selection, not a
// scroll, and the row the user pointed at has to stay under the pointer.
func TestMouseClickInScrolledPaneDoesNotScroll(t *testing.T) {
	m, _ := mouseFixture(t, 60)
	m.activePane = 0

	// Scroll down first, so the click lands on a row the window did not start
	// on. Three notches put the window at row 9.
	for i := 0; i < 3; i++ {
		m = updateModel(t, m, wheelAt(3, mouseTopBarRows+mouseFirstFileRow, tea.MouseWheelDown))
	}
	if m.leftScroll != 9 {
		t.Fatalf("setup: leftScroll = %d, want 9", m.leftScroll)
	}

	// A row in the middle of the list area, five rows below the top one.
	const offset = 5
	y := mouseTopBarRows + mouseFirstFileRow + offset
	clicked := m.leftScroll + offset
	before := strings.Split(stripTestANSI(m.View().Content), "\n")
	if !strings.Contains(before[y], m.leftFiles[clicked].Name) {
		t.Fatalf("setup: screen row %d = %q does not hold %q", y, before[y], m.leftFiles[clicked].Name)
	}

	got := updateModel(t, m, clickAt(3, y))
	if got.leftCursor != clicked {
		t.Errorf("leftCursor = %d, want the clicked row %d", got.leftCursor, clicked)
	}
	if got.leftScroll != m.leftScroll {
		t.Errorf("the click scrolled the listing: leftScroll = %d, want %d", got.leftScroll, m.leftScroll)
	}

	after := strings.Split(stripTestANSI(got.View().Content), "\n")
	if !strings.Contains(after[y], m.leftFiles[clicked].Name) {
		t.Errorf("the clicked file left screen row %d: %q", y, after[y])
	}
}

// The wheel scrolls the listing and carries the selection with it, so the row
// under the pointer stays under the pointer and the preview follows the wheel
// the way it always has. The cursor has to stay inside the window the pane now
// draws, or the next frame would scroll to reach it.
func TestMouseWheelScrollsListingAndCursor(t *testing.T) {
	m, _ := mouseFixture(t, 60)
	m.activePane = 0

	got := updateModel(t, m, wheelAt(50, mouseTopBarRows+mouseFirstFileRow, tea.MouseWheelDown))
	if got.rightScroll != 3 || got.rightCursor != 3 {
		t.Errorf("wheel down: scroll/cursor = %d/%d, want 3/3", got.rightScroll, got.rightCursor)
	}
	window := got.paneViews(got.layout())[1].Rows()
	if window.Start != got.rightScroll {
		t.Errorf("window starts at %d, want the wheel scroll %d", window.Start, got.rightScroll)
	}
	if got.rightCursor < window.Start || got.rightCursor >= window.Start+window.Count {
		t.Errorf("cursor %d is outside window %+v", got.rightCursor, window)
	}

	got = updateModel(t, got, wheelAt(50, mouseTopBarRows+mouseFirstFileRow, tea.MouseWheelUp))
	if got.rightScroll != 0 || got.rightCursor != 0 {
		t.Errorf("wheel up: scroll/cursor = %d/%d, want 0/0", got.rightScroll, got.rightCursor)
	}
}

// The strongest check: every row the hit-test names must be the row the
// renderer drew there. The frame is rendered once, then each clickable row is
// resolved and looked up in the frame.
func TestMouseHitTestMatchesRenderedFrame(t *testing.T) {
	m, _ := mouseFixture(t, 8)
	m.activePane = 0

	frame := strings.Split(stripTestANSI(m.View().Content), "\n")
	for row := 0; row < len(m.leftFiles); row++ {
		y := mouseTopBarRows + mouseFirstFileRow + row
		if y >= len(frame) {
			t.Fatalf("row %d has no frame line", row)
		}
		if !strings.Contains(frame[y], m.leftFiles[row].Name) {
			t.Fatalf("frame line %d = %q does not hold %q", y, frame[y], m.leftFiles[row].Name)
		}

		got := updateModel(t, m, clickAt(3, y))
		if got.leftCursor != row {
			t.Errorf("click on the line holding %q selected row %d", m.leftFiles[row].Name, got.leftCursor)
		}
	}
}
