package app

import (
	"strings"
	"testing"

	"github.com/bjornramberg/omakaiju/internal/ui"
)

// Stacking is off until asked for.
func TestPanesDefaultToSideBySide(t *testing.T) {
	m := modelIn(t, t.TempDir())
	if m.panesStacked {
		t.Error("stacking must default to off")
	}
}

// The Shift+S key flips the orientation without disturbing anything else.
func TestStackTogglePreservesState(t *testing.T) {
	m, _ := fixture(t)
	m.width, m.height = 100, 30
	m = cursorOn(t, m, "beta.txt")
	cursorBefore := m.rawCursor(m.activePane)
	pathBefore := m.leftPath

	updated, _ := m.Update(shiftKey('s'))
	m = updated.(Model)
	if !m.panesStacked {
		t.Fatal("Shift+S should enable stacking")
	}
	if m.rawCursor(m.activePane) != cursorBefore {
		t.Error("toggling must not move the cursor")
	}
	if m.leftPath != pathBefore {
		t.Error("toggling must not change the path")
	}

	updated, _ = m.Update(shiftKey('s'))
	m = updated.(Model)
	if m.panesStacked {
		t.Error("Shift+S again should restore side by side")
	}
	if m.rawCursor(m.activePane) != cursorBefore {
		t.Error("toggling back must not move the cursor")
	}
}

// Below the floor the toggle refuses and says why, instead of rendering
// panes too short to use.
func TestStackToggleRefusesShortTerminal(t *testing.T) {
	m := modelIn(t, t.TempDir())
	m.width, m.height = 100, 15 // main area 9, below the floor of 10

	updated, _ := m.Update(shiftKey('s'))
	m = updated.(Model)

	if m.panesStacked {
		t.Error("stacking must stay off below the floor")
	}
	if m.opResult == "" {
		t.Error("expected a note explaining why stacking refused")
	}
}

// The stacked view fits exactly at every size, like the horizontal one does.
func TestStackedViewKeepsExactFit(t *testing.T) {
	for _, tc := range []struct{ w, h int }{
		{40, 16}, {60, 20}, {80, 24}, {100, 31}, {160, 50},
	} {
		m := modelIn(t, t.TempDir())
		m.width, m.height = tc.w, tc.h
		m.panesStacked = true
		view := m.View().Content

		lines := strings.Split(view, "\n")
		if len(lines) != tc.h {
			t.Errorf("%dx%d: rendered %d rows", tc.w, tc.h, len(lines))
		}
		for i, line := range lines {
			if got := ui.DisplayWidth(line); got != tc.w {
				t.Errorf("%dx%d: row %d is %d cells", tc.w, tc.h, i, got)
			}
		}
	}
}

// The preview stays on the right at full height in both orientations.
func TestStackedViewKeepsPreview(t *testing.T) {
	m, _ := fixture(t)
	m.width, m.height = 100, 30
	m.updatePreview()
	m = cursorOn(t, m, "beta.txt")
	m.updatePreview()
	m.panesStacked = true
	view := plainView.ReplaceAllString(m.View().Content, "")

	// The preview follows the cursor, so the selected file's contents stay
	// visible on the right whatever the panes do.
	if !strings.Contains(view, "b\n") && !strings.Contains(view, "beta.txt") {
		t.Errorf("expected the preview behind the stacked panes, got:\n%s", view)
	}
}

// With the long help gone, the stack toggle is discoverable from the
// key overlay rather than the bar; the bar only shows minimal hints.
func TestStackedViewBarStaysMinimal(t *testing.T) {
	m, _ := fixture(t)
	m.width, m.height = 100, 30
	m.panesStacked = true
	view := plainView.ReplaceAllString(m.View().Content, "")

	if strings.Contains(view, "s:side-by-side") || strings.Contains(view, "s:stack") {
		t.Errorf("stack hints should be gone from the bar, got:\n%s", view)
	}
	if !strings.Contains(view, "shift+k:keys") || !strings.Contains(view, "q:quit") {
		t.Errorf("minimal hints missing from the bar, got:\n%s", view)
	}
}
