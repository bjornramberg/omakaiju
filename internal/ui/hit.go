package ui

// PaneRect is the screen rectangle occupied by a file pane. Coordinates are
// zero-based terminal cells, x to the right and y downward.
type PaneRect struct {
	X      int
	Y      int
	Width  int
	Height int
}

// PaneRects reports where the two file panes are drawn, in the order the app
// builds them: index 0 is the left pane, or the top one when stacked; index 1
// is the right or bottom pane.
//
// The rectangles come from the same Layout that composes the frame, so the
// preview and debug columns are never included and the panes cannot be
// hit-tested at a position they were not drawn at.
func (l Layout) PaneRects() [2]PaneRect {
	w := l.PaneWidth()
	hTop, hBottom := l.PaneHeight(true), l.PaneHeight(false)
	top := l.TopBarHeight()

	if l.stacked() {
		return [2]PaneRect{
			{X: 0, Y: top, Width: w, Height: hTop},
			{X: 0, Y: top + hTop, Width: w, Height: hBottom},
		}
	}
	return [2]PaneRect{
		{X: 0, Y: top, Width: w, Height: hTop},
		{X: w, Y: top, Width: w, Height: hBottom},
	}
}

// Hit is what a screen position lands on inside the file panes.
type Hit struct {
	// Pane is the index of the pane the position is over.
	Pane int
	// Row is the index into that pane's filtered listing, or -1 when the
	// position is over the pane's chrome — its border, header, padding, status
	// area, or the blank rows left under a short listing.
	Row int
}

// HitPane resolves a screen position against the file panes. panes must be the
// Pane values the frame was rendered from, in the same order as PaneRects: the
// row window each one reports is the window Render used, so a click lands on
// exactly the entry that was drawn under the pointer.
//
// false means the position is outside both panes: the bars, the preview or
// debug column, or the too-small placeholder.
func (l Layout) HitPane(panes [2]Pane, x, y int) (Hit, bool) {
	if l.TooSmall() {
		return Hit{}, false
	}

	for i, rect := range l.PaneRects() {
		if x < rect.X || x >= rect.X+rect.Width || y < rect.Y || y >= rect.Y+rect.Height {
			continue
		}
		row := -1
		if window := panes[i].Rows(); window.Count > 0 {
			offset := y - rect.Y - window.Top
			if offset >= 0 && offset < window.Count {
				if idx := window.Start + offset; idx < len(panes[i].Files) {
					row = idx
				}
			}
		}
		return Hit{Pane: i, Row: row}, true
	}
	return Hit{}, false
}
