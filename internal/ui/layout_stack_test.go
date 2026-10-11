package ui

import (
	"strings"
	"testing"

	"github.com/bjornramberg/omakaiju/internal/config"
)

// Side by side is the default and must be unchanged by the orientation work.
func TestLayoutSideBySideGeometryUnchanged(t *testing.T) {
	theme := config.DefaultTheme()
	for _, w := range []int{40, 80, 100, 160, 200} {
		l := NewLayout(w, 30, theme)
		if l.PanesStacked {
			t.Fatal("stacking must default to off")
		}
		if got := 2*l.PaneWidth() + l.PreviewWidth(); got > w {
			t.Errorf("width %d: panes plus preview use %d columns", w, got)
		}
		if l.PaneHeight(true) != l.MainAreaHeight() || l.PaneHeight(false) != l.MainAreaHeight() {
			t.Errorf("width %d: side-by-side panes must take the full main height", w)
		}
	}
}

// Stacked panes span the panes area and always sum back to the main height,
// including odd heights where one of them takes the extra row.
func TestLayoutStackedGeometry(t *testing.T) {
	theme := config.DefaultTheme()
	for _, h := range []int{10, 11, 16, 24, 25, 31, 50} {
		l := NewLayout(100, h, theme)
		l.PanesStacked = true

		if l.PaneWidth() != 100-l.PreviewWidth() {
			t.Errorf("height %d: stacked width %d, want the full panes area", h, l.PaneWidth())
		}
		if got := l.PaneHeight(true) + l.PaneHeight(false); got != l.MainAreaHeight() {
			t.Errorf("height %d: stacked panes sum to %d, want %d", h, got, l.MainAreaHeight())
		}
		if l.PaneHeight(true) < l.PaneHeight(false) {
			t.Errorf("height %d: the top pane takes the odd row", h)
		}
	}
}

func TestLayoutCanStackGate(t *testing.T) {
	theme := config.DefaultTheme()
	// Main area is height minus top and bottom bars (3 each).
	if l := NewLayout(100, 16, theme); !l.CanStack() {
		t.Error("main area 10 should allow stacking")
	}
	if l := NewLayout(100, 15, theme); l.CanStack() {
		t.Error("main area 9 should refuse stacking")
	}
	if l := NewLayout(100, 10, theme); l.CanStack() {
		t.Error("main area 4 should refuse stacking")
	}
}

// The stacked composition must fit exactly like the horizontal one does.
func TestLayoutStackedRenderExactFit(t *testing.T) {
	theme := config.DefaultTheme()
	for _, tc := range []struct{ w, h int }{
		{40, 10}, {46, 16}, {80, 24}, {100, 31}, {160, 50},
	} {
		l := NewLayout(tc.w, tc.h, theme)
		l.PanesStacked = true

		pw, phTop, phBottom := l.PaneWidth(), l.PaneHeight(true), l.PaneHeight(false)
		left := NewPane(pw, phTop, true, theme)
		left.Path, left.Files = "/left", nil
		right := NewPane(pw, phBottom, false, theme)
		right.Path, right.Files = "/right", nil
		preview := NewPreview(l.PreviewWidth(), l.MainAreaHeight(), theme)

		top := NewTopBar(tc.w, theme)
		top.Path = "/"
		bottom := NewBottomBar(tc.w, theme)

		out := l.Render(top.Render(), left.Render(), right.Render(),
			preview.RenderFile("", 0), bottom.Render())
		lines := strings.Split(out, "\n")
		if len(lines) != tc.h {
			t.Errorf("%dx%d: rendered %d rows", tc.w, tc.h, len(lines))
		}
		for i, line := range lines {
			if got := DisplayWidth(line); got != tc.w {
				t.Errorf("%dx%d: row %d is %d cells", tc.w, tc.h, i, got)
			}
		}
	}
}
