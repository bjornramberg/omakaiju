package ui

import (
	"strings"
	"testing"

	"github.com/bjornramberg/omakaiju/internal/config"
)

func TestKeyListRendersRowsFromKeybinds(t *testing.T) {
	k := NewKeyList(100, 40, config.DefaultTheme())
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
	if !strings.Contains(out, "h/left") {
		t.Errorf("merged motion aliases missing, got %q", out)
	}
	// Every action that dispatches shows up.
	for _, want := range []string{"yank", "rename", "fuzzy find", "key list"} {
		if !strings.Contains(out, want) {
			t.Errorf("expected desc %q in the overlay, got %q", want, out)
		}
	}
}

// The search matches on both columns: "yank" finds the copy binding, and
// "pane" finds everything mentioning panes.
func TestFilterKeyRowsSearchesBothColumns(t *testing.T) {
	rows := TableKeyRows()

	yank := FilterKeyRows(rows, "yank")
	if len(yank) != 1 || yank[0].Desc != "yank (copy)" {
		t.Errorf("searching yank should leave one copy row, got %v", yank)
	}

	pane := FilterKeyRows(rows, "pane")
	if len(pane) != 4 {
		t.Fatalf("searching pane should find the four pane rows, got %v", pane)
	}
	for _, row := range pane {
		if !strings.Contains(row.Desc, "pane") {
			t.Errorf("unexpected pane row %q (%q)", row.Keys, row.Desc)
		}
	}

	// Key text is searchable too, not only descriptions.
	byKey := FilterKeyRows(rows, "ctrl+f")
	if len(byKey) != 1 || byKey[0].Desc != "fuzzy find" {
		t.Errorf("key-text search failed, got %v", byKey)
	}
}

func TestKeyListSearchNarrowsRows(t *testing.T) {
	k := NewKeyList(100, 40, config.DefaultTheme())
	k.Input = "yank"
	out := stripANSI(k.Render())

	if !strings.Contains(out, "yank (copy)") {
		t.Errorf("yank row missing: %q", out)
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
	k.Rows = []KeyRow{{Keys: "x", Desc: "custom"}}
	out := stripANSI(k.Render())
	if !strings.Contains(out, "custom") || strings.Contains(out, "yank") {
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
		{Keys: "z", Desc: "a " + strings.Repeat("very ", 40) + "long description"},
	}
	out := k.Render()
	for _, line := range strings.Split(out, "\n") {
		if w := DisplayWidth(line); w > k.BoxWidth()-2 {
			t.Errorf("row overflows the panel: %d > %d", w, k.BoxWidth()-2)
		}
	}
}
