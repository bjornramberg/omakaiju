package ui

import (
	"fmt"
	"strings"
	"testing"

	"github.com/bjornramberg/omakaiju/internal/config"
	"github.com/bjornramberg/omakaiju/internal/fs"
)

// focusFixture builds a pane in the given focus state with a small listing.
// The cursor sits on the second entry, so the first one is a plain row that
// can be compared across the two panes.
func focusFixture(t *testing.T, active bool, width int) Pane {
	t.Helper()

	files := []fs.Entry{
		{Name: "alpha.txt", Path: "/tmp/alpha.txt"},
		{Name: "subdir", Path: "/tmp/subdir", IsDir: true},
	}
	pane := NewPane(width, 10, active, config.DefaultTheme())
	pane.Path = "/home/user/projects"
	pane.Files = files
	pane.Cursor = 1
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

// The unfocused pane used to draw the same selection bar as the focused one,
// which made it the brighter of the two. The bar now belongs to the focused
// pane alone; the unfocused cursor stays visible, but without it.
func TestUnfocusedPaneDropsTheSelectionBar(t *testing.T) {
	theme := config.DefaultTheme()
	bar := ansiBackground(theme.SelectionBG)

	focused := focusFixture(t, true, 40).Render()
	unfocused := focusFixture(t, false, 40).Render()

	if !strings.Contains(focused, bar) {
		t.Fatal("the focused pane should keep the selection bar on its cursor row")
	}
	if strings.Contains(unfocused, bar) {
		t.Error("the unfocused pane should not draw a selection bar")
	}
}

// The unfocused pane's rows are dimmer than the focused pane's, which is
// what makes the focused pane the one that stands out.
func TestUnfocusedPaneRowsAreDimmer(t *testing.T) {
	theme := config.DefaultTheme()

	focusedRow := rawLineWith(focusFixture(t, true, 40).Render(), "alpha.txt")
	unfocusedRow := rawLineWith(focusFixture(t, false, 40).Render(), "alpha.txt")

	if focusedRow == "" || unfocusedRow == "" {
		t.Fatal("both panes should list the same entry")
	}
	if !strings.Contains(focusedRow, ansiForeground(theme.FGMain)) {
		t.Error("the focused pane should draw its rows in the main foreground")
	}
	if strings.Contains(unfocusedRow, ansiForeground(theme.FGMain)) {
		t.Error("the unfocused pane should not draw its rows in the main foreground")
	}
	if !strings.Contains(unfocusedRow, ansiForeground(theme.DimmedColor())) {
		t.Error("the unfocused pane should draw its rows in the dimmed colour")
	}
}

// rawLineWith returns the first line of s that contains sub, escape codes
// and all.
func rawLineWith(s, sub string) string {
	for _, line := range strings.Split(s, "\n") {
		if strings.Contains(stripANSI(line), sub) {
			return line
		}
	}
	return ""
}

// The focused pane's header sits on a band of its own, so the pane holding
// the focus reads apart even where the border colours do not.
func TestFocusedPaneHasItsOwnHeaderBand(t *testing.T) {
	theme := config.DefaultTheme()
	band := ansiBackground(theme.ActiveHeaderBG())

	focusedRow := rawLineWith(focusFixture(t, true, 40).Render(), "/home/user")
	unfocusedRow := rawLineWith(focusFixture(t, false, 40).Render(), "/home/user")

	if !strings.Contains(focusedRow, band) {
		t.Error("the focused pane's header should sit on the theme's band colour")
	}
	if strings.Contains(unfocusedRow, band) {
		t.Error("the unfocused pane should not carry a band")
	}
}

// The band belongs to the header row alone. A tinted pane background would
// run into the selection bar, which is drawn in the theme's selection colour
// and is often the same hue as the accent: the cursor row would disappear
// into the surface it sits on.
func TestTheBandStaysOffTheListing(t *testing.T) {
	theme := config.DefaultTheme()
	band := ansiBackground(theme.ActiveHeaderBG())

	row := rawLineWith(focusFixture(t, true, 40).Render(), "alpha.txt")
	if strings.Contains(row, band) {
		t.Error("the band should stay on the header row, not under the listing")
	}
}

// ansiForeground renders the escape sequence lipgloss emits for a hex
// foreground colour.
func ansiForeground(hex string) string {
	return "38;2;" + ansiChannels(hex)
}

// ansiBackground renders the escape sequence lipgloss emits for a hex
// background colour.
func ansiBackground(hex string) string {
	return "48;2;" + ansiChannels(hex)
}

// ansiChannels turns "#rrggbb" into the "r;g;b" lipgloss writes.
func ansiChannels(hex string) string {
	var r, g, b int
	if _, err := fmt.Sscanf(hex, "#%02x%02x%02x", &r, &g, &b); err != nil {
		return ""
	}
	return fmt.Sprintf("%d;%d;%d", r, g, b)
}
