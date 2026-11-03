package ui

import (
	"fmt"
	"strings"
	"testing"

	"github.com/bjornramberg/omakaiju/internal/config"
	"github.com/bjornramberg/omakaiju/internal/nav"
)

func TestKeyListRendersRowsFromKeybinds(t *testing.T) {
	// Use larger height to accommodate section headers, separators, and blank lines
	k := NewKeyList(100, 80, config.DefaultTheme())
	out := stripANSI(k.Render())

	if !strings.Contains(out, "key list") {
		t.Errorf("header missing, got %q", out)
	}
	// The footer mentions all the fuzzy-style affordances.
	for _, want := range []string{"type to search", "up/down:move", "esc:close"} {
		if !strings.Contains(out, want) {
			t.Errorf("footer hint %q missing, got %q", want, out)
		}
	}
	// Every action that dispatches shows up.
	for _, want := range []string{"copy", "rename", "fuzzy find", "key list"} {
		if !strings.Contains(out, want) {
			t.Errorf("expected desc %q in the overlay, got %q", want, out)
		}
	}
}

// The search matches on both columns: "copy" finds the copy binding, and
// "pane" finds everything mentioning panes.
func TestFilterKeyRowsSearchesBothColumns(t *testing.T) {
	rows := TableKeyRows()

	copy := FilterKeyRows(rows, "copy")
	if len(copy) != 1 || copy[0].Desc != "copy (yank)" {
		t.Errorf("searching copy should leave one copy row, got %v", copy)
	}

	pane := FilterKeyRows(rows, "pane")
	if len(pane) != 5 {
		t.Fatalf("searching pane should find the five pane rows, got %v", pane)
	}
	for _, row := range pane {
		if !strings.Contains(row.Desc, "pane") {
			t.Errorf("unexpected pane row %q (%q)", row.Keys, row.Desc)
		}
	}

	// Key text is searchable too, not only descriptions.
	byKey := FilterKeyRows(rows, "shift+f")
	if len(byKey) != 1 || byKey[0].Desc != "filter pane" {
		t.Errorf("key-text search failed, got %v", byKey)
	}
}

func TestKeyListSearchNarrowsRows(t *testing.T) {
	k := NewKeyList(100, 40, config.DefaultTheme())
	k.Input = "copy"
	out := stripANSI(k.Render())

	if !strings.Contains(out, "copy (yank)") {
		t.Errorf("copy row missing: %q", out)
	}
	if strings.Contains(out, "switch pane") {
		t.Errorf("unrelated rows should be filtered away: %q", out)
	}
	if !strings.Contains(out, "1/1") {
		t.Errorf("position should read 1/1 after the filter, got %q", out)
	}
}

func TestKeyListNoMatchesRendersHint(t *testing.T) {
	k := NewKeyList(100, 40, config.DefaultTheme())
	k.Input = "zzz"
	if out := stripANSI(k.Render()); !strings.Contains(out, "no matching key") {
		t.Errorf("empty result hint missing, got %q", out)
	}
}

// Injected rows are honored so tests (and future panels) can override the
// table without touching the dispatch path.
func TestKeyListHonorsRowsOverride(t *testing.T) {
	k := NewKeyList(100, 40, config.DefaultTheme())
	k.Rows = []KeyRow{{Keys: "x", Desc: "custom", Section: nav.SectionSystem}}
	out := stripANSI(k.Render())
	if !strings.Contains(out, "custom") || strings.Contains(out, "copy") {
		t.Errorf("override rows not respected, got %q", out)
	}
}

// The panel must fit the terminal exactly wherever the layout itself fits,
// since OverlayCentered's invariant depends on exact lines and widths.
func TestKeyListOverlayKeepsExactFit(t *testing.T) {
	for _, tc := range []struct{ w, h int }{
		{40, 10}, {46, 16}, {80, 24}, {100, 30}, {160, 50}, {200, 12},
	} {
		k := NewKeyList(tc.w, tc.h, config.DefaultTheme())

		laidOut := OverlayCentered(strings.Repeat("x", tc.w)+"\n", k.Render(), tc.w)
		if len(laidOut) == 0 {
			t.Fatalf("%dx%d: overlay swallowed the background", tc.w, tc.h)
		}
		for _, line := range strings.Split(laidOut, "\n") {
			if got := DisplayWidth(line); got != tc.w {
				t.Errorf("%dx%d: row width %d, want %d", tc.w, tc.h, got, tc.w)
				break
			}
		}
	}
}

// Long descriptions are clipped rather than wrapping the row, so the table
// never grows a second content column.
func TestKeyListClipsLongRows(t *testing.T) {
	k := NewKeyList(60, 40, config.DefaultTheme())
	k.Rows = []KeyRow{
		{Keys: "z", Desc: "a " + strings.Repeat("very ", 40) + "long description", Section: nav.SectionSystem},
	}
	out := k.Render()
	for _, line := range strings.Split(out, "\n") {
		if w := DisplayWidth(line); w > k.BoxWidth() {
			t.Errorf("row overflows the panel: %d > %d", w, k.BoxWidth())
		}
	}
}

// The panel must keep its border at any width.
//
// A line that overflowed the content grew the panel past its own border, and
// the border was then cut off: the overlay came out with an open right edge
// on a narrow terminal, which read as a broken window.
func TestKeyListKeepsItsBorderOnNarrowTerminals(t *testing.T) {
	theme := config.DefaultTheme()

	for _, w := range []int{24, 30, 40, 60, 100, 200} {
		k := NewKeyList(w, 40, theme)

		lines := strings.Split(stripANSI(k.Render()), "\n")
		for i, line := range lines {
			if got := DisplayWidth(line); got != k.BoxWidth() {
				t.Errorf("width %d: row %d is %d cells, want %d", w, i, got, k.BoxWidth())
			}
		}
		if !strings.HasSuffix(lines[0], "╮") {
			t.Errorf("width %d: the top border is %q, want it closed on the right", w, lines[0])
		}
	}
}

// The panel's height must not depend on its width.
//
// A line that was too wide for the panel wrapped onto a second line, so the
// panel grew taller the narrower the terminal got. The footer was the usual
// culprit: it was never bounded, so it took three lines on a narrow terminal
// and pushed the rows around.
func TestKeyListHeightDoesNotDependOnItsWidth(t *testing.T) {
	theme := config.DefaultTheme()

	wide := NewKeyList(100, 40, theme)
	want := strings.Count(stripANSI(wide.Render()), "\n") + 1

	for _, w := range []int{24, 30, 40, 60} {
		k := NewKeyList(w, 40, theme)
		got := strings.Count(stripANSI(k.Render()), "\n") + 1
		if got != want {
			t.Errorf("width %d: panel has %d lines, want %d", w, got, want)
		}
	}
}

// The footer must be one line. A footer that wrapped pushed the rows around,
// so a narrow panel gives up hints instead of taking another line.
func TestKeyListFooterNeverWraps(t *testing.T) {
	theme := config.DefaultTheme()
	position := fmt.Sprintf("1/%d", len(TableKeyRows()))

	for _, w := range []int{24, 30, 40, 60, 100} {
		k := NewKeyList(w, 40, theme)

		plain := stripANSI(k.Render())
		found := false
		for _, line := range strings.Split(plain, "\n") {
			if !strings.Contains(line, "esc:close") {
				continue
			}
			found = true
			// The position and the cancel hint are never given up, so a
			// footer that holds both is a footer that did not wrap.
			if !strings.Contains(line, position) {
				t.Errorf("width %d: the footer wrapped: %q", w, line)
			}
		}
		if !found {
			t.Errorf("width %d: the footer is missing:\n%s", w, plain)
		}
	}
}
