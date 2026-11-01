package ui

import (
	"strings"
	"testing"

	"github.com/bjornramberg/omakaiju/internal/config"
)

// The debug pane's split must always account for the whole main area: sharing
// the right column it halves it (preview keeps the odd row), alone it takes
// the column, and when there is no room the preview keeps everything.
func TestLayoutDebugSplitSumsToMainArea(t *testing.T) {
	theme := config.DefaultTheme()

	for _, h := range []int{8, 10, 11, 12, 14, 15, 16, 24, 31, 50} {
		l := NewLayout(100, h, theme)
		l.DebugActive = true

		main, preview, debug := l.MainAreaHeight(), l.PreviewHeight(), l.DebugHeight()
		if debug == 0 {
			// Too short to split: the pane does not appear and the preview
			// keeps the whole column.
			if preview != main {
				t.Errorf("height %d: no debug pane, preview is %d, want %d", h, preview, main)
			}
			continue
		}
		if preview+debug != main {
			t.Errorf("height %d: preview %d + debug %d = %d, want %d", h, preview, debug, preview+debug, main)
		}
		if preview < minDebugPaneHeight || debug < minDebugPaneHeight {
			t.Errorf("height %d: split %d/%d leaves a pane under %d rows", h, preview, debug, minDebugPaneHeight)
		}
		if debug != main/2 {
			t.Errorf("height %d: debug is %d rows, want half of %d", h, debug, main)
		}
		if debug > preview {
			t.Errorf("height %d: debug (%d) is taller than preview (%d)", h, debug, preview)
		}
	}

	// With no debug pane the preview keeps the full main area.
	for _, h := range []int{12, 24, 50} {
		l := NewLayout(100, h, theme)
		if l.DebugHeight() != 0 {
			t.Errorf("height %d: debug height %d with the pane closed", h, l.DebugHeight())
		}
		if l.PreviewHeight() != l.MainAreaHeight() {
			t.Errorf("height %d: preview %d, want the full main area %d", h, l.PreviewHeight(), l.MainAreaHeight())
		}
	}
}

// The debug pane is independent of the preview: with the preview hidden it is
// alone in the right column and takes the full height.
func TestLayoutDebugIndependentOfPreview(t *testing.T) {
	theme := config.DefaultTheme()

	for _, h := range []int{8, 10, 12, 24, 50} {
		l := NewLayout(100, h, theme)
		l.DebugActive = true
		l.PreviewHidden = true

		main := l.MainAreaHeight()
		if main < minDebugPaneHeight {
			if l.DebugHeight() != 0 || l.PreviewHeight() != 0 {
				t.Errorf("height %d: too short, debug %d preview %d", h, l.DebugHeight(), l.PreviewHeight())
			}
			continue
		}
		if l.PreviewHeight() != 0 {
			t.Errorf("height %d: preview height %d while hidden", h, l.PreviewHeight())
		}
		if l.DebugHeight() != main {
			t.Errorf("height %d: debug height %d, want the full main area %d", h, l.DebugHeight(), main)
		}
		if l.DebugWidth() != 100/3 {
			t.Errorf("height %d: debug width %d, want a third of the terminal", h, l.DebugWidth())
		}
		if l.RightColumnWidth() != l.DebugWidth() {
			t.Errorf("height %d: right column %d, want the debug width %d", h, l.RightColumnWidth(), l.DebugWidth())
		}
		if got, want := 2*l.PaneWidth()+l.RightColumnWidth(), 100; got > want {
			t.Errorf("height %d: panes and debug use %d columns, want at most %d", h, got, want)
		}
	}

	// Both hidden: there is no right column and the panes get everything.
	l := NewLayout(100, 40, theme)
	l.PreviewHidden = true
	if l.RightColumnWidth() != 0 || l.DebugWidth() != 0 {
		t.Errorf("both panes off: right column %d, debug width %d", l.RightColumnWidth(), l.DebugWidth())
	}
	if got, want := 2*l.PaneWidth(), 100; got != want {
		t.Errorf("both panes off: panes use %d columns, want %d", got, want)
	}
}

// Rendering with the debug pane on must still fit the terminal exactly, both
// beside the preview and alone in the column.
func TestLayoutDebugRenderExactFit(t *testing.T) {
	theme := config.DefaultTheme()

	for _, tc := range []struct {
		w, h          int
		previewHidden bool
		stacked       bool
	}{
		{80, 24, false, false},
		{100, 31, false, false},
		{100, 31, true, false},
		{120, 40, true, true},
		{80, 20, true, false},
	} {
		l := NewLayout(tc.w, tc.h, theme)
		l.DebugActive = true
		l.PreviewHidden = tc.previewHidden
		l.PanesStacked = tc.stacked

		left := NewPane(l.PaneWidth(), l.PaneHeight(true), true, theme)
		left.Path = "/left"
		right := NewPane(l.PaneWidth(), l.PaneHeight(false), false, theme)
		right.Path = "/right"

		var preview string
		if !l.PreviewHidden {
			preview = NewPreview(l.PreviewWidth(), l.PreviewHeight(), theme).RenderContent(Content{Kind: KindEmpty})
		}
		var debug string
		if l.DebugVisible() {
			debug = NewDebug(l.RightColumnWidth(), l.DebugHeight(), theme).Render()
		}

		top := NewTopBar(tc.w, theme)
		top.Path = "/"
		bottom := NewBottomBar(tc.w, theme)

		out := l.Render(top.Render(), left.Render(), right.Render(), preview, debug, bottom.Render())
		lines := strings.Split(out, "\n")
		if len(lines) != tc.h {
			t.Errorf("%dx%d: rendered %d rows, want %d", tc.w, tc.h, len(lines), tc.h)
		}
		for i, line := range lines {
			if got := DisplayWidth(line); got != tc.w {
				t.Errorf("%dx%d: row %d is %d cells, want %d", tc.w, tc.h, i, got, tc.w)
			}
		}
	}
}
