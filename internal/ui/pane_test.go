package ui

import (
	"strings"
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
// every combination of filter, status and parent row, or the model and the
// renderer would scroll by different amounts.
func TestListRowsAgreesWithPane(t *testing.T) {
	theme := config.DefaultTheme()
	for _, tc := range []struct {
		name      string
		height    int
		filter    string
		hasStatus bool
		hasParent bool
	}{
		{"plain", 24, "", false, false},
		{"filter", 24, "f", false, false},
		{"status", 24, "", true, false},
		{"filter and status", 24, "f", true, false},
		{"parent", 24, "", false, true},
		{"parent and filter", 24, "f", false, true},
		{"parent and status", 24, "", true, true},
		{"parent, filter and status", 24, "f", true, true},
		{"degenerate height", 3, "", false, false},
		{"degenerate height with parent", 3, "", false, true},
	} {
		t.Run(tc.name, func(t *testing.T) {
			p := NewPane(40, tc.height, true, theme)
			p.Filter = tc.filter
			if tc.hasStatus {
				p.Status = "1 marked"
			}
			if tc.hasParent {
				p.Parent = "/tmp"
			}
			got := ListRows(tc.height, tc.filter, tc.hasStatus, tc.hasParent)
			if want := p.fileRows(); got != want {
				t.Errorf("ListRows = %d, pane reports %d", got, want)
			}
			// The body always has a row for the listing, even when the parent
			// row and the status rows have eaten the height.
			if got < 1 {
				t.Errorf("ListRows = %d, want at least one row", got)
			}
		})
	}

	// The parent row costs the listing exactly one row, whatever else the
	// pane carries. Comparing the pairs keeps this honest even if the two
	// sides of the first check are changed together.
	for _, tc := range []struct {
		filter    string
		hasStatus bool
	}{
		{"", false},
		{"f", false},
		{"", true},
		{"f", true},
	} {
		without := ListRows(24, tc.filter, tc.hasStatus, false)
		with := ListRows(24, tc.filter, tc.hasStatus, true)
		if with != without-1 {
			t.Errorf("filter %q status %v: parent row leaves %d rows, want %d", tc.filter, tc.hasStatus, with, without-1)
		}
	}
}

// A pane with a parent row gives it one row of the body, so the listing starts
// one row lower and has one row fewer. The renderer and the mouse hit-test
// both read this window, so the two have to move together.
func TestPaneRowsReservesTheParentRow(t *testing.T) {
	p := NewPane(40, 20, true, config.DefaultTheme())
	p.Files = hitFiles(30)
	p.Scroll = 6
	p.Cursor = 8

	plain := p.Rows()
	p.Parent = "/tmp"
	parent := p.Rows()

	if want := plain.Top + 1; parent.Top != want {
		t.Errorf("parent row puts the listing at %d, want %d", parent.Top, want)
	}
	if want := plain.Count - 1; parent.Count != want {
		t.Errorf("parent row leaves %d listing rows, want %d", parent.Count, want)
	}
	// The window itself is unchanged: reserving the parent row moves the
	// listing down, it does not scroll it.
	if parent.Start != plain.Start {
		t.Errorf("parent row moved the window to %d, want the scroll %d", parent.Start, plain.Start)
	}
}

// A pane with a parent draws the ".." row directly above the listing, so the
// first entry is one row under it and never on it.
func TestPaneRenderDrawsParentRow(t *testing.T) {
	p := NewPane(40, 20, true, config.DefaultTheme())
	p.Path = "/tmp/here"
	p.Files = hitFiles(3)
	p.Parent = "/tmp"

	lines := strings.Split(stripANSI(p.Render()), "\n")
	if len(lines) <= paneRowsTop+1 {
		t.Fatalf("pane rendered %d lines, no room for a parent row and a listing", len(lines))
	}
	if row := lines[paneRowsTop]; !strings.Contains(row, "..") {
		t.Errorf("row %d = %q, want the parent row", paneRowsTop, row)
	}
	if row := lines[paneRowsTop+1]; !strings.Contains(row, p.Files[0].Name) {
		t.Errorf("row %d = %q, want the first entry %q", paneRowsTop+1, row, p.Files[0].Name)
	}
}

// Without a parent there is no way up to draw, so the listing stays where it
// was: the first body row is the first entry.
func TestPaneRenderHasNoParentRowAtRoot(t *testing.T) {
	p := NewPane(40, 20, true, config.DefaultTheme())
	p.Path = "/"
	p.Files = hitFiles(3)

	lines := strings.Split(stripANSI(p.Render()), "\n")
	if len(lines) <= paneRowsTop {
		t.Fatalf("pane rendered %d lines, no body", len(lines))
	}
	if row := lines[paneRowsTop]; strings.Contains(row, "..") {
		t.Errorf("row %d = %q, want no parent row at the root", paneRowsTop, row)
	}
	if row := lines[paneRowsTop]; !strings.Contains(row, p.Files[0].Name) {
		t.Errorf("row %d = %q, want the first entry %q", paneRowsTop, row, p.Files[0].Name)
	}
}

// A cursor parked on the ".." row is above the listing and is not one of its
// entries, so it must not drag the window above the first row. Reading it as an
// index would ask the renderer for row -1 and panic on the first frame.
func TestPaneRowsIgnoresParentRowCursor(t *testing.T) {
	p := NewPane(40, 20, true, config.DefaultTheme())
	p.Files = hitFiles(30)
	p.Scroll = 6
	// The cursor sits inside the window, so it has no say in where the
	// window starts; the ".." row must leave that window alone as well.
	p.Cursor = 6

	plain := p.Rows()
	p.Cursor = ParentCursor
	parent := p.Rows()

	if parent.Start < 0 {
		t.Fatalf("window starts at %d with the cursor on the parent row", parent.Start)
	}
	if parent.Start != plain.Start || parent.Count != plain.Count || parent.Top != plain.Top {
		t.Errorf("parent-row cursor moved the window: %+v, want %+v", parent, plain)
	}

	// The same pane must still draw every row of that window, starting with
	// the row the window starts on.
	lines := strings.Split(stripANSI(p.Render()), "\n")
	for offset := 0; offset < parent.Count; offset++ {
		idx := parent.Start + offset
		if row := lines[parent.Top+offset]; !strings.Contains(row, p.Files[idx].Name) {
			t.Fatalf("row %d = %q, want %q", parent.Top+offset, row, p.Files[idx].Name)
		}
	}
}

// The cursor can be on the ".." row, so the row has to show the selection.
// Without it the focus would look lost: a cursor nothing draws.
func TestPaneParentRowDrawsTheCursor(t *testing.T) {
	p := NewPane(40, 20, true, config.DefaultTheme())
	p.Path = "/tmp/here"
	p.Files = hitFiles(3)
	p.Parent = "/tmp"

	plain := strings.Split(p.Render(), "\n")[paneRowsTop]
	p.Cursor = ParentCursor
	selected := strings.Split(p.Render(), "\n")[paneRowsTop]

	if !strings.Contains(stripANSI(selected), "..") {
		t.Fatalf("the parent row = %q, want the \"..\" row", stripANSI(selected))
	}
	if plain == selected {
		t.Error("the parent row is drawn the same with and without the cursor")
	}

	// The unfocused pane draws its cursor too, just without the focus bar.
	p.Active = false
	if unfocused := strings.Split(p.Render(), "\n")[paneRowsTop]; unfocused == selected {
		t.Error("the unfocused pane draws the cursor exactly like the focused one")
	}
}

// An empty directory still draws ".." and is still a row the cursor can sit on:
// the way up does not depend on there being entries beside it.
func TestPaneParentRowOnEmptyListingWithCursor(t *testing.T) {
	p := NewPane(40, 20, true, config.DefaultTheme())
	p.Path = "/tmp/here"
	p.Parent = "/tmp"
	p.Cursor = ParentCursor

	lines := strings.Split(stripANSI(p.Render()), "\n")
	if row := lines[paneRowsTop]; !strings.Contains(row, "..") {
		t.Errorf("row %d = %q, want the parent row", paneRowsTop, row)
	}
	if window := p.Rows(); window.Count != 0 || window.Start != 0 {
		t.Errorf("empty listing window = %+v, want an empty window at the top", window)
	}
}

// An empty directory still draws its way up: the parent row and the
// placeholder are built on separate paths, and both have to agree on where the
// row goes.
func TestPaneRenderParentRowOnEmptyListing(t *testing.T) {
	p := NewPane(40, 20, true, config.DefaultTheme())
	p.Path = "/tmp/here"
	p.Parent = "/tmp"

	lines := strings.Split(stripANSI(p.Render()), "\n")
	if len(lines) <= paneRowsTop+1 {
		t.Fatalf("pane rendered %d lines, no room for a parent row and the placeholder", len(lines))
	}
	if row := lines[paneRowsTop]; !strings.Contains(row, "..") {
		t.Errorf("row %d = %q, want the parent row", paneRowsTop, row)
	}
	if row := lines[paneRowsTop+1]; !strings.Contains(row, "empty directory") {
		t.Errorf("row %d = %q, want the empty-directory placeholder", paneRowsTop+1, row)
	}
}
