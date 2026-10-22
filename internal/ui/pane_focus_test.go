package ui

import (
	"strings"
	"testing"

	"github.com/bjornramberg/omakaiju/internal/config"
	"github.com/bjornramberg/omakaiju/internal/fs"
)

// focusFixture builds a pane in the given focus state with a small listing.
func focusFixture(t *testing.T, active bool, width int) Pane {
	t.Helper()

	files := []fs.Entry{
		{Name: "alpha.txt", Path: "/tmp/alpha.txt"},
		{Name: "subdir", Path: "/tmp/subdir", IsDir: true},
	}
	pane := NewPane(width, 10, active, config.DefaultTheme())
	pane.Path = "/home/user/projects"
	pane.Files = files
	pane.Cursor = 0
	return pane
}

// The focused pane is marked with a glyph, not only with a colour. The
// marker has to survive a palette whose two border colours are too close to
// tell apart, and it has to be readable by someone who cannot rely on hue at
// all, so the test looks at the text rather than the escape codes.
func TestFocusedPaneRendersAMarkerTheUnfocusedPaneDoesNot(t *testing.T) {
	focused := stripANSI(focusFixture(t, true, 40).Render())
	unfocused := stripANSI(focusFixture(t, false, 40).Render())

	if !strings.Contains(focused, "▸") {
		t.Error("the focused pane should render a marker glyph")
	}
	if strings.Contains(unfocused, "▸") {
		t.Error("the unfocused pane should not render a marker glyph")
	}
}

// The marker is a cue, not a layout change: both panes keep the same size,
// and the paths still start in the same column so the two listings line up.
func TestMarkerDoesNotChangeThePaneGeometry(t *testing.T) {
	focused := focusFixture(t, true, 40).Render()
	unfocused := focusFixture(t, false, 40).Render()

	fl := strings.Split(focused, "\n")
	ul := strings.Split(unfocused, "\n")
	if len(fl) != len(ul) {
		t.Fatalf("focused pane has %d lines, unfocused has %d", len(fl), len(ul))
	}
	for i := range fl {
		if w, uw := DisplayWidth(fl[i]), DisplayWidth(ul[i]); w != uw {
			t.Errorf("line %d: focused is %d cells wide, unfocused is %d", i, w, uw)
		}
	}

	// The path starts at the same column in both panes.
	fCol := cellColumn(stripANSI(focused), "/home/user")
	uCol := cellColumn(stripANSI(unfocused), "/home/user")
	if fCol != uCol {
		t.Errorf("path column differs: focused=%d unfocused=%d", fCol, uCol)
	}
}

// cellColumn returns the cell offset of a substring, which is not the same as
// its byte offset: the marker glyph takes three bytes but one cell.
func cellColumn(s, sub string) int {
	i := strings.Index(s, sub)
	if i < 0 {
		return -1
	}
	return DisplayWidth(s[:i])
}

// A pane too narrow for a marker keeps the whole width for the path instead
// of spilling past the border.
func TestMarkerIsDroppedWhenThePaneIsTooNarrow(t *testing.T) {
	pane := focusFixture(t, true, 6)
	out := pane.Render()

	for i, line := range strings.Split(stripANSI(out), "\n") {
		if w := DisplayWidth(line); w > 6 {
			t.Errorf("line %d is %d cells wide, want at most 6", i, w)
		}
	}
}
