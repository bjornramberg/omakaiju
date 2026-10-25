package ui

import (
	"strings"
	"testing"

	"github.com/bjornramberg/omakaiju/internal/config"
)

// paneChrome is the border and vertical padding a pane frame spends before the
// path header, its blank line and any filename row is drawn.
const paneChrome = 2 /*border*/ + 2 /*padding*/ + 2 /*path and blank line*/

// rowContains reports whether any line of out holds both markers. Side-by-side
// panes share a line; stacked ones never do.
func rowContains(out, a, b string) bool {
	for _, line := range strings.Split(out, "\n") {
		if strings.Contains(line, a) && strings.Contains(line, b) {
			return true
		}
	}
	return false
}

// A stacked layout whose terminal shrinks under the stacking minimum must draw
// the panes side by side, with rows left for filenames.
func TestLayoutStackedFallsBackWhenTerminalShrinks(t *testing.T) {
	theme := config.DefaultTheme()
	l := NewLayout(120, 40, theme)
	l.PanesStacked = true

	if !l.CanStack() {
		t.Fatal("120x40 should be tall enough to stack")
	}
	if got, want := l.PaneWidth(), 120-l.PreviewWidth(); got != want {
		t.Fatalf("stacked width %d, want %d", got, want)
	}

	l.Height = 15 // main area 9: below the stacking minimum, one row above nothing

	if l.CanStack() {
		t.Fatal("120x15 must be too short to stack")
	}
	if l.PaneWidth() != (120-l.PreviewWidth())/2 {
		t.Errorf("width %d, want a side-by-side pane", l.PaneWidth())
	}
	if h := l.PaneHeight(true); h-paneChrome < 1 {
		t.Errorf("top pane is %d rows: no row is left for a filename", h)
	}
	if h := l.PaneHeight(false); h-paneChrome < 1 {
		t.Errorf("bottom pane is %d rows: no row is left for a filename", h)
	}
	if !l.PanesStacked {
		t.Error("falling back must not change the user's choice")
	}

	out := l.Render("top", "LEFT", "RIGHT", "", "bottom")
	if !rowContains(out, "LEFT", "RIGHT") {
		t.Errorf("panes are not side by side:\n%s", out)
	}
}

// The user's choice survives a shrink: once the terminal is tall enough again
// the panes return to the stacked layout they asked for.
func TestLayoutStackedChoiceRememberedAfterShrink(t *testing.T) {
	theme := config.DefaultTheme()
	l := NewLayout(120, 40, theme)
	l.PanesStacked = true

	l.Height = 12
	if out := l.Render("top", "LEFT", "RIGHT", "", "bottom"); !rowContains(out, "LEFT", "RIGHT") {
		t.Fatalf("shrunk layout did not fall back to side by side:\n%s", out)
	}

	l.Height = 40
	if !l.CanStack() {
		t.Fatal("120x40 should be tall enough to stack again")
	}
	if l.PaneWidth() != 120-l.PreviewWidth() {
		t.Errorf("regrown width %d, want the full panes area", l.PaneWidth())
	}
	if got, want := l.PaneHeight(true)+l.PaneHeight(false), l.MainAreaHeight(); got != want {
		t.Errorf("regrown panes sum to %d, want %d", got, want)
	}
	out := l.Render("top", "LEFT", "RIGHT", "", "bottom")
	if rowContains(out, "LEFT", "RIGHT") {
		t.Errorf("panes stayed side by side instead of returning to stacked:\n%s", out)
	}
	if !l.PanesStacked {
		t.Error("the remembered choice was lost")
	}
}

// Whatever orientation is in effect, the panes and preview still account for
// the area: heights sum exactly, widths leave at most the single column that
// cannot be split between two side-by-side panes.
func TestLayoutGeometryFillsAreaInBothOrientations(t *testing.T) {
	theme := config.DefaultTheme()
	for _, tc := range []struct {
		w, h          int
		stacked       bool
		previewHidden bool
	}{
		{120, 40, true, false},  // stacked
		{121, 41, true, true},   // stacked, no preview, odd width
		{120, 12, true, false},  // request remembered, too short: side by side
		{121, 12, true, true},   // request remembered, no preview, odd width
		{120, 40, false, false}, // plain side by side
		{121, 40, false, true},  // plain side by side, no preview, odd width
	} {
		l := NewLayout(tc.w, tc.h, theme)
		l.PanesStacked = tc.stacked
		l.PreviewHidden = tc.previewHidden

		stacked := l.PanesStacked && l.CanStack()
		used := l.PreviewWidth() + l.PaneWidth()
		if !stacked {
			used += l.PaneWidth()
		}
		if used > tc.w {
			t.Errorf("%dx%d: panes and preview use %d columns", tc.w, tc.h, used)
		}
		if tc.w-used > 1 {
			t.Errorf("%dx%d: %d columns left unused", tc.w, tc.h, tc.w-used)
		}

		if stacked {
			if got, want := l.PaneHeight(true)+l.PaneHeight(false), l.MainAreaHeight(); got != want {
				t.Errorf("%dx%d: stacked panes sum to %d rows, want %d", tc.w, tc.h, got, want)
			}
		} else {
			main := l.MainAreaHeight()
			if l.PaneHeight(true) != main || l.PaneHeight(false) != main {
				t.Errorf("%dx%d: side-by-side panes must take the full main height %d", tc.w, tc.h, main)
			}
		}
	}
}
