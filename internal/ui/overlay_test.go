package ui

import (
	"strings"
	"testing"
)

// makeGrid builds a background string with the given dimensions, with no
// trailing newline so it matches the layout's output shape.
func makeGrid(w, h int) string {
	row := strings.Repeat(".", w)
	return strings.Repeat(row+"\n", h)[:len(row)*h+h-1]
}

// boxOf returns a solid block of '#' characters with the given dimensions.
func boxOf(w, h int) string {
	row := strings.Repeat("#", w)
	return strings.Repeat(row+"\n", h)[:len(row)*h+h-1]
}

func TestOverlayAtPlacesBox(t *testing.T) {
	bg := makeGrid(80, 24)
	box := boxOf(10, 4)
	got := OverlayAt(bg, box, 5, 3, 10, 4)

	lines := strings.Split(got, "\n")
	if len(lines) != 24 {
		t.Fatalf("line count changed: %d, want 24", len(lines))
	}
	for i := 0; i < 24; i++ {
		if gotW := DisplayWidth(lines[i]); gotW != 80 {
			t.Fatalf("line %d width %d, want 80", i, gotW)
		}
	}

	for row := 3; row < 7; row++ {
		line := lines[row]
		want := strings.Repeat(".", 5) + strings.Repeat("#", 10) + strings.Repeat(".", 65)
		if stripANSI(line) != want {
			t.Errorf("row %d = %q, want %q", row, stripANSI(line), want)
		}
	}
}

// OverlayAt must keep the background's width even when the box starts near
// the right edge and the original row is shorter than the splice point.
func TestOverlayAtNearRightEdge(t *testing.T) {
	bg := makeGrid(40, 10)
	box := boxOf(10, 2)
	got := OverlayAt(bg, box, 35, 2, 10, 2)

	lines := strings.Split(got, "\n")
	for i, line := range lines {
		if gotW := DisplayWidth(line); gotW != 40 {
			t.Errorf("row %d width %d, want 40", i, gotW)
		}
	}
	wantRow2 := strings.Repeat(".", 35) + strings.Repeat("#", 5)
	if stripANSI(lines[2]) != wantRow2 {
		t.Errorf("row 2 = %q, want %q", stripANSI(lines[2]), wantRow2)
	}
}

func TestOverlayPositionClampsRightAndBottom(t *testing.T) {
	x, y := OverlayPosition(70, 20, 20, 10, 80, 24)
	if x != 60 {
		t.Errorf("right clamp: x = %d, want 60", x)
	}
	if y != 14 {
		t.Errorf("bottom clamp: y = %d, want 14", y)
	}
}

func TestOverlayPositionClampsLeftAndTop(t *testing.T) {
	x, y := OverlayPosition(-5, -3, 10, 4, 80, 24)
	if x != 0 {
		t.Errorf("left clamp: x = %d, want 0", x)
	}
	if y != 0 {
		t.Errorf("top clamp: y = %d, want 0", y)
	}
}

func TestOverlayPositionKeepsBoxInsideWhenItFits(t *testing.T) {
	x, y := OverlayPosition(10, 5, 8, 3, 80, 24)
	if x != 10 || y != 5 {
		t.Errorf("position changed unexpectedly: got %d,%d want 10,5", x, y)
	}
}

// The box must never hang off the screen, even when it is larger than the
// screen; in that case it is pinned to the top-left and OverlayAt clips it.
func TestOverlayPositionClampsWhenBoxExceedsScreen(t *testing.T) {
	x, y := OverlayPosition(10, 10, 100, 50, 80, 24)
	if x != 0 || y != 0 {
		t.Errorf("oversized box: got %d,%d want 0,0", x, y)
	}
}

// OverlayAt must never add or remove lines, and must never change the
// background width, because the layout's row geometry is load-bearing for
// mouse hit-testing.
func TestOverlayAtPreservesBackgroundGeometry(t *testing.T) {
	bg := makeGrid(80, 24)
	box := boxOf(20, 6)

	for _, tc := range []struct{ x, y int }{
		{0, 0},
		{30, 5},
		{60, 18},
		{-5, -2},
		{70, 20},
	} {
		got := OverlayAt(bg, box, tc.x, tc.y, 20, 6)
		lines := strings.Split(got, "\n")
		if len(lines) != 24 {
			t.Errorf("%d,%d: line count %d, want 24", tc.x, tc.y, len(lines))
		}
		for i, line := range lines {
			if w := DisplayWidth(line); w != 80 {
				t.Errorf("%d,%d: row %d width %d, want 80", tc.x, tc.y, i, w)
			}
		}
	}
}

// A box covers its own cells and no more, whatever width the caller says.
// The caller knows the screen's dimensions, and a box that took the width
// from the caller would wipe out the frame beside it and leave a hole the
// width of the terminal — the menu is a dozen cells wide, and the hole
// beside it looked like a black slab.
func TestOverlayAtLeavesTheBackgroundBesideTheBox(t *testing.T) {
	bg := makeGrid(80, 24)
	box := boxOf(10, 4)

	for _, tc := range []struct {
		name          string
		width, height int
	}{
		{"the box's own size", 10, 4},
		{"the screen's size", 80, 24},
	} {
		t.Run(tc.name, func(t *testing.T) {
			got := OverlayAt(bg, box, 5, 3, tc.width, tc.height)
			lines := strings.Split(got, "\n")

			want := strings.Repeat(".", 5) + strings.Repeat("#", 10) + strings.Repeat(".", 65)
			for row := 3; row < 7; row++ {
				if stripANSI(lines[row]) != want {
					t.Errorf("row %d = %q, want %q", row, stripANSI(lines[row]), want)
				}
			}
		})
	}
}
