package ui

import (
	"testing"

	"github.com/bjornramberg/omakaiju/internal/config"
)

// A window the pane was scrolled to stays where it is while the cursor moves
// inside it. This is the whole point of the scroll field: a cursor move is not
// a scroll, and only the renderer's window decides which entries are on screen.
func TestPaneRowsStartsAtScrollWhenCursorInside(t *testing.T) {
	p := NewPane(40, 20, true, config.DefaultTheme())
	p.Files = hitFiles(30)
	p.Scroll = 10
	p.Cursor = 12

	window := p.Rows()
	if window.Start != 10 {
		t.Errorf("window starts at %d, want the pane's scroll 10", window.Start)
	}
	if window.Start+window.Count <= p.Cursor {
		t.Errorf("window %+v does not contain cursor %d", window, p.Cursor)
	}
}

// A cursor that leaves the window at the bottom pulls the window down just far
// enough to include it, rather than jumping the window so the cursor sits on
// its last row.
func TestPaneRowsMovesWindowDownForCursorBelow(t *testing.T) {
	p := NewPane(40, 20, true, config.DefaultTheme())
	p.Files = hitFiles(30)
	p.Scroll = 0
	p.Cursor = 20

	window := p.Rows()
	if window.Count != 14 {
		t.Fatalf("setup: window has %d rows, want 14", window.Count)
	}
	if window.Start != 20-14+1 {
		t.Errorf("window starts at %d, want %d", window.Start, 20-14+1)
	}
}

// A cursor that moves above the window pushes the window up to the cursor's
// row. The window is never left above a cursor the user has selected.
func TestPaneRowsMovesWindowUpForCursorAbove(t *testing.T) {
	p := NewPane(40, 20, true, config.DefaultTheme())
	p.Files = hitFiles(30)
	p.Scroll = 10
	p.Cursor = 4

	window := p.Rows()
	if window.Start != p.Cursor {
		t.Errorf("window starts at %d, want the cursor's row %d", window.Start, p.Cursor)
	}
}

// The window never shows blank rows past the end of the listing: a scroll past
// the last full window is clamped to the last window that is full. Without the
// clamp a wheel scroll near the end would leave the pane half empty.
func TestPaneRowsNeverStartsPastEnd(t *testing.T) {
	p := NewPane(40, 20, true, config.DefaultTheme())
	p.Files = hitFiles(30)
	p.Scroll = 25
	p.Cursor = 25

	window := p.Rows()
	if want := len(p.Files) - window.Count; window.Start != want {
		t.Errorf("window starts at %d, want %d", window.Start, want)
	}
	if window.Start+window.Count > len(p.Files) {
		t.Errorf("window %+v reaches past the %d entries", window, len(p.Files))
	}
}

// ListRows is what the model asks for instead of building a Pane and copying
// the listing. It must give the same answer the pane's own row math does, in
// every combination of filter and status row, or the model and the renderer
// would scroll by different amounts.
func TestListRowsAgreesWithPane(t *testing.T) {
	theme := config.DefaultTheme()
	for _, tc := range []struct {
		name      string
		height    int
		filter    string
		hasStatus bool
	}{
		{"plain", 24, "", false},
		{"filter", 24, "f", false},
		{"status", 24, "", true},
		{"filter and status", 24, "f", true},
		{"degenerate height", 3, "", false},
	} {
		t.Run(tc.name, func(t *testing.T) {
			p := NewPane(40, tc.height, true, theme)
			p.Filter = tc.filter
			if tc.hasStatus {
				p.Status = "1 marked"
			}
			if got, want := ListRows(tc.height, tc.filter, tc.hasStatus), p.visibleRows(); got != want {
				t.Errorf("ListRows = %d, pane reports %d", got, want)
			}
		})
	}
}
