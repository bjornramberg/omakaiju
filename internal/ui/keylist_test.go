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
	if !strings.Contains(out, "ctrl+k/esc:close") {
		t.Errorf("footer missing, got %q", out)
	}
	// Aliases merge, so both the alias and one half of it appear once.
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

// Injected rows are honored so tests (and future tweaks) can override the
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
