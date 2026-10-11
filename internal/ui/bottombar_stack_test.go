package ui

import (
	"strings"
	"testing"

	"github.com/bjornramberg/omakaiju/internal/config"
)

// The help line names the action pressing s will take, so it follows the
// current layout instead of showing a fixed label.
func TestBottomBarStackHintFollowsLayout(t *testing.T) {
	theme := config.DefaultTheme()

	bar := NewBottomBar(100, theme)
	if out := stripANSI(bar.Render()); !strings.Contains(out, "s:stack") {
		t.Errorf("expected s:stack in the help, got %q", out)
	}

	bar.PanesStacked = true
	if out := stripANSI(bar.Render()); !strings.Contains(out, "s:side-by-side") {
		t.Errorf("expected s:side-by-side in the help, got %q", out)
	}
}

// The longer help must still fit, degrading by clipping like before.
func TestBottomBarHelpNeverExceedsWidth(t *testing.T) {
	theme := config.DefaultTheme()
	for _, w := range []int{20, 40, 60, 80, 120, 200} {
		for _, stacked := range []bool{false, true} {
			bar := NewBottomBar(w, theme)
			bar.PanesStacked = stacked
			if !noLineExceeds(bar.Render(), w) {
				t.Errorf("width %d stacked=%v: help overflowed", w, stacked)
			}
		}
	}
}
