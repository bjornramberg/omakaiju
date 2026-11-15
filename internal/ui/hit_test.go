package ui

import (
	"fmt"
	"strings"
	"testing"

	"github.com/bjornramberg/omakaiju/internal/config"
	"github.com/bjornramberg/omakaiju/internal/fs"
)

// hitFiles builds a listing of n short, untruncated names so a rendered row can
// be matched back to its entry by name.
func hitFiles(n int) []fs.Entry {
	out := make([]fs.Entry, n)
	for i := range out {
		out[i] = fs.Entry{
			Name: fmt.Sprintf("f%02d", i),
			Path: fmt.Sprintf("/tmp/f%02d", i),
		}
	}
	return out
}

// hitPanes builds the pair of panes a layout draws, sized exactly as the app
// sizes them.
func hitPanes(l Layout, theme config.Theme) [2]Pane {
	return [2]Pane{
		NewPane(l.PaneWidth(), l.PaneHeight(true), true, theme),
		NewPane(l.PaneWidth(), l.PaneHeight(false), false, theme),
	}
}

// A side-by-side layout resolves a click in either pane, including the first
// and last visible rows, and reports the focused pane's listing index.
func TestHitPaneSideBySide(t *testing.T) {
	theme := config.DefaultTheme()
	l := NewLayout(100, 30, theme)
	panes := hitPanes(l, theme)
	panes[0].Files = hitFiles(10)
	panes[1].Files = hitFiles(10)
	panes[1].Cursor = 2

	pw := l.PaneWidth()
	cases := []struct {
		name       string
		x, y       int
		wantPane   int
		wantRow    int
		wantInside bool
	}{
		{"left first row", 3, 3 + paneRowsTop, 0, 0, true},
		{"left last row", 3, 3 + paneRowsTop + 9, 0, 9, true},
		{"right first row", pw + 3, 3 + paneRowsTop, 1, 0, true},
		{"right last row", pw + 3, 3 + paneRowsTop + 9, 1, 9, true},
		{"left border column", 0, 3 + paneRowsTop, 0, 0, true},
		{"right pane first column", pw, 3 + paneRowsTop, 1, 0, true},
	}
	for _, tc := range cases {
		t.Run(tc.name, func(t *testing.T) {
			hit, ok := l.HitPane(panes, tc.x, tc.y)
			if ok != tc.wantInside {
				t.Fatalf("HitPane(%d,%d) inside = %v, want %v", tc.x, tc.y, ok, tc.wantInside)
			}
			if hit.Pane != tc.wantPane || hit.Row != tc.wantRow {
				t.Errorf("HitPane(%d,%d) = %+v, want pane %d row %d", tc.x, tc.y, hit, tc.wantPane, tc.wantRow)
			}
		})
	}
}

// The chrome rows above the listing focus a pane without naming a row, and the
// padding under a short listing is chrome too: there is no entry there.
func TestHitPaneChromeHasNoRow(t *testing.T) {
	theme := config.DefaultTheme()
	l := NewLayout(100, 30, theme)
	panes := hitPanes(l, theme)
	panes[0].Files = hitFiles(2)

	// The top border, the header, the blank line under it, and the padding
	// left by a two-entry listing in a tall pane.
	for _, y := range []int{3, 3 + 1, 3 + 3, 3 + paneRowsTop + 2, 3 + 20} {
		hit, ok := l.HitPane(panes, 3, y)
		if !ok {
			t.Fatalf("y=%d should still be inside the left pane", y)
		}
		if hit.Pane != 0 {
			t.Errorf("y=%d resolved to pane %d, want 0", y, hit.Pane)
		}
		if hit.Row != -1 {
			t.Errorf("y=%d resolved to row %d, want chrome (-1)", y, hit.Row)
		}
	}
}

// A pane that has not loaded anything draws its placeholder, which is not an
// entry and cannot be selected.
func TestHitPaneEmptyListingIsChrome(t *testing.T) {
	theme := config.DefaultTheme()
	l := NewLayout(100, 30, theme)
	panes := hitPanes(l, theme)

	hit, ok := l.HitPane(panes, 3, 3+paneRowsTop)
	if !ok || hit.Pane != 0 || hit.Row != -1 {
		t.Errorf("HitPane on an empty pane = %+v, %v; want pane 0 chrome", hit, ok)
	}
}

// The preview column and both bars are outside the panes: the preview is not a
// file list and must not be hit-testable as one.
func TestHitPaneOutsidePanes(t *testing.T) {
	theme := config.DefaultTheme()
	l := NewLayout(100, 30, theme)
	panes := hitPanes(l, theme)
	panes[0].Files = hitFiles(4)
	panes[1].Files = hitFiles(4)

	pw := l.PaneWidth()
	for _, tc := range []struct {
		name string
		x, y int
	}{
		{"top bar", 3, 0},
		{"top bar bottom row", 3, 2},
		{"bottom bar", 3, 27},
		{"preview column", 2 * pw, 7},
		{"last column", 99, 7},
		{"beyond the terminal", 200, 7},
		{"below the terminal", 3, 40},
	} {
		t.Run(tc.name, func(t *testing.T) {
			if hit, ok := l.HitPane(panes, tc.x, tc.y); ok {
				t.Errorf("HitPane(%d,%d) = %+v, want outside", tc.x, tc.y, hit)
			}
		})
	}
}

// When the preview yields its column the panes span the terminal, and a click
// where the preview would have been is a pane click.
func TestHitPanePreviewYielded(t *testing.T) {
	theme := config.DefaultTheme()
	l := NewLayout(60, 30, theme)
	if l.PreviewVisible() {
		t.Fatal("width 60 should yield the preview")
	}
	panes := hitPanes(l, theme)
	panes[0].Files = hitFiles(4)
	panes[1].Files = hitFiles(4)

	pw := l.PaneWidth()
	hit, ok := l.HitPane(panes, 2*pw-1, 3+paneRowsTop)
	if !ok || hit.Pane != 1 || hit.Row != 0 {
		t.Errorf("HitPane over the right pane = %+v, %v; want pane 1 row 0", hit, ok)
	}
	if _, ok := l.HitPane(panes, 2*pw, 3+paneRowsTop); ok {
		t.Error("a column past the right pane must not resolve")
	}
}

// Stacked panes put the second one below the first; the hit-test follows the
// orientation the Layout actually chose.
func TestHitPaneStacked(t *testing.T) {
	theme := config.DefaultTheme()
	l := NewLayout(100, 30, theme)
	l.PanesStacked = true
	if !l.CanStack() {
		t.Fatal("30 rows should allow stacking")
	}
	panes := hitPanes(l, theme)
	panes[0].Files = hitFiles(8)
	panes[1].Files = hitFiles(8)
	panes[1].Cursor = 1

	rects := l.PaneRects()
	if rects[1].Y != rects[0].Y+rects[0].Height {
		t.Fatalf("bottom pane starts at %d, want %d", rects[1].Y, rects[0].Y+rects[0].Height)
	}
	// The top pane's last row and the bottom pane's first row must not both
	// resolve to the same pane.
	hit, ok := l.HitPane(panes, 3, rects[0].Y+rects[0].Height-1)
	if !ok || hit.Pane != 0 {
		t.Errorf("last row of the top pane = %+v, %v; want pane 0", hit, ok)
	}
	hit, ok = l.HitPane(panes, 3, rects[1].Y+paneRowsTop)
	if !ok || hit.Pane != 1 || hit.Row != 0 {
		t.Errorf("first row of the bottom pane = %+v, %v; want pane 1 row 0", hit, ok)
	}
}

// A filter adds a blank line and a count under the listing, so the pane shows
// fewer rows. The hit-test has to use the pane's own window, not the generic
// height.
func TestHitPaneUsesFilterStatusRows(t *testing.T) {
	theme := config.DefaultTheme()
	l := NewLayout(100, 30, theme)
	panes := hitPanes(l, theme)
	panes[0].Files = hitFiles(20)
	panes[0].Filter = "f"

	window := panes[0].Rows()
	if want := panes[0].Height - 6 - 2; window.Count != want {
		t.Fatalf("filtered pane rows = %d, want %d", window.Count, want)
	}

	last := 3 + paneRowsTop + window.Count - 1
	hit, ok := l.HitPane(panes, 3, last)
	if !ok || hit.Row != window.Count-1 {
		t.Errorf("last filtered row = %+v, %v; want row %d", hit, ok, window.Count-1)
	}
	// The row just below the listing is the filter's blank line, not an
	// entry: a filter has pushed it out of the body.
	hit, ok = l.HitPane(panes, 3, last+1)
	if !ok || hit.Row != -1 {
		t.Errorf("row under the filtered listing = %+v, %v; want chrome", hit, ok)
	}
}

// A marked pane carries a status row for the active pane only, which shortens
// that pane's listing by one row relative to the other. Both must report the
// window they were built with.
func TestHitPaneUsesMarkedStatusRow(t *testing.T) {
	theme := config.DefaultTheme()
	l := NewLayout(100, 30, theme)
	panes := hitPanes(l, theme)
	panes[0].Files = hitFiles(20)
	panes[1].Files = hitFiles(20)
	panes[0].Status = "3 marked"

	leftWindow := panes[0].Rows()
	rightWindow := panes[1].Rows()
	if leftWindow.Count != rightWindow.Count-1 {
		t.Fatalf("marked pane shows %d rows, unmarked %d; want one fewer", leftWindow.Count, rightWindow.Count)
	}

	// One row below the marked listing is its status line: chrome.
	hit, ok := l.HitPane(panes, 3, 3+paneRowsTop+leftWindow.Count)
	if !ok || hit.Row != -1 {
		t.Errorf("status row = %+v, %v; want chrome", hit, ok)
	}
	// The unmarked pane still has an entry on that screen row.
	hit, ok = l.HitPane(panes, l.PaneWidth()+3, 3+paneRowsTop+leftWindow.Count)
	if !ok || hit.Row != leftWindow.Count {
		t.Errorf("unmarked row = %+v, %v; want row %d", hit, ok, leftWindow.Count)
	}
}

// The parent row is the pane's first body row, and a click there names the
// parent rather than any entry of the listing. Both layouts have to resolve it
// against the rectangle the pane was actually drawn at.
func TestHitPaneParentRow(t *testing.T) {
	theme := config.DefaultTheme()
	for _, tc := range []struct {
		name    string
		stacked bool
	}{
		{"side by side", false},
		{"stacked", true},
	} {
		t.Run(tc.name, func(t *testing.T) {
			l := NewLayout(100, 30, theme)
			l.PanesStacked = tc.stacked
			if tc.stacked && !l.CanStack() {
				t.Fatal("30 rows should allow stacking")
			}
			panes := hitPanes(l, theme)
			for pane := range panes {
				panes[pane].Files = hitFiles(10)
				panes[pane].Parent = "/tmp"
			}

			rects := l.PaneRects()
			for pane := range panes {
				hit, ok := l.HitPane(panes, rects[pane].X+3, rects[pane].Y+paneRowsTop)
				if !ok {
					t.Fatalf("pane %d: the parent row is inside the pane", pane)
				}
				if hit.Pane != pane || !hit.Parent || hit.Row != -1 {
					t.Errorf("pane %d parent row = %+v, want that pane's parent and no row", pane, hit)
				}

				// The blank line above the parent row is still chrome. The
				// parent row must not creep up into it.
				hit, ok = l.HitPane(panes, rects[pane].X+3, rects[pane].Y+paneRowsTop-1)
				if !ok || hit.Parent || hit.Row != -1 {
					t.Errorf("pane %d chrome above the parent row = %+v, %v; want chrome", pane, hit, ok)
				}
			}
		})
	}
}

// The parent row takes a row of its own, so the first entry moves down with
// it. A listing that still started one row higher would open the second file
// whenever the first one was clicked.
func TestHitPaneParentRowKeepsListingInPlace(t *testing.T) {
	theme := config.DefaultTheme()
	l := NewLayout(100, 30, theme)
	panes := hitPanes(l, theme)
	panes[0].Files = hitFiles(10)
	panes[0].Parent = "/tmp"

	window := panes[0].Rows()
	if window.Top != paneRowsTop+1 {
		t.Fatalf("setup: listing starts at %d, want %d", window.Top, paneRowsTop+1)
	}
	hit, ok := l.HitPane(panes, 3, 3+window.Top)
	if !ok || hit.Parent || hit.Row != 0 {
		t.Errorf("first entry row = %+v, %v; want row 0", hit, ok)
	}
	hit, ok = l.HitPane(panes, 3, 3+window.Top+1)
	if !ok || hit.Parent || hit.Row != 1 {
		t.Errorf("second entry row = %+v, %v; want row 1", hit, ok)
	}
}

// A pane scrolled down resolves clicks through the window it was given: the
// top screen row is Start, not zero. The cursor rides inside that window, so a
// click on one of its rows selects the entry drawn there with no re-scroll.
func TestHitPaneScrolledWindow(t *testing.T) {
	theme := config.DefaultTheme()
	l := NewLayout(100, 30, theme)
	panes := hitPanes(l, theme)
	panes[0].Files = hitFiles(20)
	// Shorter than the body so the listing can be scrolled at all.
	panes[0].Height = 12
	panes[0].Scroll = 10
	panes[0].Cursor = 10

	window := panes[0].Rows()
	if window.Start != panes[0].Scroll {
		t.Fatalf("window starts at %d, want the pane's scroll %d", window.Start, panes[0].Scroll)
	}
	if window.Start+window.Count <= panes[0].Cursor {
		t.Fatalf("window %+v does not contain the cursor", window)
	}

	hit, ok := l.HitPane(panes, 3, 3+paneRowsTop)
	if !ok || hit.Row != window.Start {
		t.Errorf("first drawn row = %+v, %v; want %d", hit, ok, window.Start)
	}
}

// Below the minimum size the placeholder is drawn and there is no pane to hit.
func TestHitPaneTooSmall(t *testing.T) {
	theme := config.DefaultTheme()
	l := NewLayout(MinWidth-1, MinHeight-1, theme)
	panes := hitPanes(l, theme)
	panes[0].Files = hitFiles(4)

	if _, ok := l.HitPane(panes, 0, 0); ok {
		t.Error("a too-small terminal has no hit-testable panes")
	}
}

// The row window is the renderer's own: every row HitPane names must be the
// entry drawn on that line, and the row after the window must be chrome.
func TestHitPaneMatchesRenderedRows(t *testing.T) {
	theme := config.DefaultTheme()
	for _, tc := range []struct {
		name string
		cfg  func(*Layout)
	}{
		{"side by side", func(*Layout) {}},
		{"stacked", func(l *Layout) { l.PanesStacked = true }},
		{"preview hidden", func(l *Layout) { l.PreviewHidden = true }},
	} {
		t.Run(tc.name, func(t *testing.T) {
			l := NewLayout(100, 30, theme)
			tc.cfg(&l)
			panes := hitPanes(l, theme)
			panes[0].Files = hitFiles(15)
			panes[0].Cursor = 7
			// The left pane has a way up, so its listing is drawn one row
			// lower than the right pane's. The frame is the oracle for both.
			panes[0].Parent = "/tmp"
			panes[1].Files = hitFiles(15)
			panes[1].Filter = "f"
			panes[1].Status = "2 marked"

			rects := l.PaneRects()
			for pane := range panes {
				lines := strings.Split(stripANSI(panes[pane].Render()), "\n")
				window := panes[pane].Rows()
				for offset := 0; offset < window.Count; offset++ {
					idx := window.Start + offset
					if idx >= len(panes[pane].Files) {
						continue
					}
					y := rects[pane].Y + window.Top + offset
					hit, ok := l.HitPane(panes, rects[pane].X+3, y)
					if !ok || hit.Pane != pane || hit.Row != idx {
						t.Fatalf("pane %d offset %d: hit = %+v, %v; want row %d", pane, offset, hit, ok, idx)
					}
					row := window.Top + offset
					if row >= len(lines) {
						t.Fatalf("pane %d: rendered %d lines, no row %d", pane, len(lines), row)
					}
					if !strings.Contains(lines[row], panes[pane].Files[idx].Name) {
						t.Errorf("pane %d offset %d: line %q does not hold %q",
							pane, offset, lines[row], panes[pane].Files[idx].Name)
					}
				}

				// One past the window is chrome, not the next entry.
				y := rects[pane].Y + window.Top + window.Count
				if y < rects[pane].Y+rects[pane].Height {
					if hit, ok := l.HitPane(panes, rects[pane].X+3, y); ok && hit.Row != -1 {
						t.Errorf("pane %d: row past the window resolved to %d, want chrome", pane, hit.Row)
					}
				}
			}
		})
	}
}
