package ui

import (
	"strings"
	"testing"

	"github.com/bjornramberg/omakaiju/internal/config"
)

// The default row is deliberately minimal: keys live behind ctrl+k's
// overlay, so the bar only cares about the exit and the list both users can
// find, plus the fading version splash on the right.
func TestBottomBarMinimalHints(t *testing.T) {
	theme := config.DefaultTheme()

	bar := NewBottomBar(100, theme)
	out := stripANSI(bar.Render())
	if !strings.Contains(out, "ctrl+k:keys") || !strings.Contains(out, "q:quit") {
		t.Errorf("minimal hints missing from %q", out)
	}
	if strings.Contains(out, "yank") || strings.Contains(out, "paste") || strings.Contains(out, "filter") {
		t.Errorf("long help should be gone from the default row, got %q", out)
	}
	if strings.Contains(out, "s:stack") || strings.Contains(out, "s:side-by-side") {
		t.Errorf("stack hint should stay out of the bar, got %q", out)
	}
}

// The hints must never wrap the row, on any terminal.
func TestBottomBarHintsNeverExceedsWidth(t *testing.T) {
	theme := config.DefaultTheme()
	for _, w := range []int{20, 40, 60, 80, 120, 200} {
		bar := NewBottomBar(w, theme)
		if !noLineExceeds(bar.Render(), w) {
			t.Errorf("width %d: hints overflowed", w)
		}
	}
}
