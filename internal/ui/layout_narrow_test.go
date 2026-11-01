package ui

import (
	"strings"
	"testing"

	"github.com/bjornramberg/omakaiju/internal/config"
)

// At the minimum width two side-by-side panes must each have room for a
// filename. The preview yields rather than taking its third and leaving the
// panes at thirteen columns, which is what made a 40-column terminal useless.
func TestLayoutMinimumWidthLeavesReadablePane(t *testing.T) {
	theme := config.DefaultTheme()

	l := NewLayout(MinWidth, 30, theme)
	if l.TooSmall() {
		t.Fatalf("%dx30 is the advertised minimum but reports too small", MinWidth)
	}
	if got := l.PaneWidth(); got < minReadablePaneWidth {
		t.Errorf("pane width %d at the minimum width, want at least %d", got, minReadablePaneWidth)
	}

	// One column narrower cannot fit two readable panes, so the placeholder is
	// the honest answer.
	if !NewLayout(MinWidth-1, 30, theme).TooSmall() {
		t.Errorf("%d columns should be too small for two readable panes", MinWidth-1)
	}

	// Assert the row's own budget, not just the pane number: the content left
	// after the marker, icon and separators must still hold minNameWidth cells.
	pane := NewPane(l.PaneWidth(), l.MainAreaHeight(), true, theme)
	entry := fileEntry("a-long-enough-filename.txt", 1234, 0o644)
	row := NewFileItem(entry, false, false, theme, pane.ContentWidth())
	used := 1 /*marker*/ + 2 /*spaces around the icon*/ + DisplayWidth(iconsIconForTest())
	budget := pane.ContentWidth() - used
	size, perms := row.detailColumns(budget)
	if nameBudget := budget - detailWidth(size, perms); nameBudget < minNameWidth {
		t.Errorf("name budget %d at the minimum width, want at least %d", nameBudget, minNameWidth)
	}
}

// A preview that would leave the panes narrower than a readable row yields:
// it is not drawn, the panes take the whole width, and the user's "preview on"
// request is not changed.
func TestLayoutPreviewYieldsWhenPanesWouldBeTooNarrow(t *testing.T) {
	theme := config.DefaultTheme()

	const width = 60
	l := NewLayout(width, 30, theme)
	if share := l.PreviewWidth(); width-share >= 2*minReadablePaneWidth {
		t.Fatalf("precondition: a %d-column preview still leaves readable panes", share)
	}
	if l.PreviewVisible() {
		t.Error("the preview should yield when it would starve the panes")
	}
	if !l.PreviewSuppressed() {
		t.Error("a yielding preview should be reported as suppressed")
	}
	if l.PreviewHidden {
		t.Error("yielding must not look like the user turned the preview off")
	}
	if got := l.RightColumnWidth(); got != 0 {
		t.Errorf("right column is %d columns while the preview yields, want 0", got)
	}
	if got := l.PaneWidth(); got < minReadablePaneWidth {
		t.Errorf("pane width %d while the preview yields, want at least %d", got, minReadablePaneWidth)
	}
	if got := 2*l.PaneWidth() + l.RightColumnWidth(); got > width || width-got > 1 {
		t.Errorf("panes and right column use %d of %d columns", got, width)
	}

	out := l.Render("top", "LEFT", "RIGHT", "PREVIEW", "", "bottom")
	if strings.Contains(out, "PREVIEW") {
		t.Errorf("a yielded preview was drawn:\n%s", out)
	}
	if !rowContains(out, "LEFT", "RIGHT") {
		t.Errorf("panes are not side by side after the preview yielded:\n%s", out)
	}

	// The choice was not discarded: when there is room again the preview is
	// back.
	l.Width = 120
	if !l.PreviewVisible() {
		t.Error("the preview did not return when the terminal grew")
	}
	if l.PreviewSuppressed() {
		t.Error("the preview still reports suppressed at 120 columns")
	}
}

// At a wide width the preview is drawn and the parts still account for the
// terminal, with at most the single column two side-by-side panes cannot split.
func TestLayoutPreviewReturnsWhenWideAndFillsWidth(t *testing.T) {
	theme := config.DefaultTheme()

	l := NewLayout(120, 30, theme)
	if !l.PreviewVisible() {
		t.Fatal("the preview should be drawn at 120 columns")
	}
	if l.PreviewSuppressed() {
		t.Fatal("the preview is drawn but reports suppressed")
	}
	if got, want := l.RightColumnWidth(), l.PreviewWidth(); got != want {
		t.Errorf("right column is %d, want the preview's %d", got, want)
	}
	if got := 2*l.PaneWidth() + l.RightColumnWidth(); got != 120 {
		t.Errorf("panes and preview use %d of 120 columns, want all of them", got)
	}
	if out := l.Render("top", "LEFT", "RIGHT", "PREVIEW", "", "bottom"); !strings.Contains(out, "PREVIEW") {
		t.Errorf("the preview was not drawn at 120 columns:\n%s", out)
	}

	// An odd width leaves exactly one column that cannot be split between the
	// two panes.
	l.Width = 121
	used := 2*l.PaneWidth() + l.RightColumnWidth()
	if used > 121 || 121-used > 1 {
		t.Errorf("panes and preview use %d of 121 columns", used)
	}
}

// Turning the preview off with Shift+I wins over every width: it is never
// drawn, and it is not reported as merely suppressed by a narrow terminal.
func TestLayoutPreviewStaysOffWhenUserHidesIt(t *testing.T) {
	theme := config.DefaultTheme()

	for _, width := range []int{0, 1, 20, 40, 44, 60, 100, 200} {
		l := NewLayout(width, 30, theme)
		l.PreviewHidden = true

		if l.PreviewVisible() {
			t.Errorf("width %d: hidden preview reports visible", width)
		}
		if l.PreviewSuppressed() {
			t.Errorf("width %d: a user-hidden preview is not suppressed", width)
		}
		if got := l.PreviewWidth(); got != 0 {
			t.Errorf("width %d: hidden preview width %d", width, got)
		}
		if got := l.RightColumnWidth(); got != 0 {
			t.Errorf("width %d: right column %d with the preview hidden", width, got)
		}
	}

	l := NewLayout(120, 30, theme)
	l.PreviewHidden = true
	if out := l.Render("top", "LEFT", "RIGHT", "PREVIEW", "", "bottom"); strings.Contains(out, "PREVIEW") {
		t.Errorf("a hidden preview was drawn at a wide width:\n%s", out)
	}
}

// The two fallbacks must coexist: a short terminal falls back from stacked
// panes to side by side, and a narrow one makes the preview yield. Neither
// changes the user's choice, and widths and heights still sum to the space.
func TestLayoutPreviewYieldAndStackedFallbackCoexist(t *testing.T) {
	theme := config.DefaultTheme()

	// Short: the stacking minimum is not met, so the panes are side by side.
	// Narrow: the preview also yields, handing its third to the panes.
	l := NewLayout(60, 12, theme)
	l.PanesStacked = true
	if l.CanStack() {
		t.Fatal("60x12 should be too short to stack")
	}
	if !l.PanesStacked {
		t.Error("falling back must not discard the stacking choice")
	}
	if l.PreviewVisible() {
		t.Error("the preview should yield at 60 columns with side-by-side panes")
	}
	if got := l.PaneWidth(); got != 30 {
		t.Errorf("side-by-side pane width %d, want 30", got)
	}
	if l.PaneHeight(true) != l.MainAreaHeight() || l.PaneHeight(false) != l.MainAreaHeight() {
		t.Error("side-by-side fallback panes must take the full main height")
	}
	if used := 2*l.PaneWidth() + l.RightColumnWidth(); used > 60 || 60-used > 1 {
		t.Errorf("panes and right column use %d of 60 columns", used)
	}
	out := l.Render("top", "LEFT", "RIGHT", "PREVIEW", "", "bottom")
	if !rowContains(out, "LEFT", "RIGHT") {
		t.Errorf("panes are not side by side:\n%s", out)
	}
	if strings.Contains(out, "PREVIEW") {
		t.Errorf("a yielded preview was drawn:\n%s", out)
	}

	// Tall enough to stack: one pane spans the panes area, so the preview's
	// share still leaves a readable pane and the preview stays.
	stacked := NewLayout(60, 40, theme)
	stacked.PanesStacked = true
	if !stacked.CanStack() {
		t.Fatal("60x40 should be tall enough to stack")
	}
	if !stacked.PreviewVisible() {
		t.Error("stacked panes leave room for the preview at 60 columns")
	}
	if got, want := stacked.PaneWidth(), 60-stacked.PreviewWidth(); got != want {
		t.Errorf("stacked pane width %d, want the panes area %d", got, want)
	}
	if got, want := stacked.PaneHeight(true)+stacked.PaneHeight(false), stacked.MainAreaHeight(); got != want {
		t.Errorf("stacked panes sum to %d rows, want %d", got, want)
	}
	if used := stacked.PaneWidth() + stacked.RightColumnWidth(); used > 60 || 60-used > 1 {
		t.Errorf("stacked pane and right column use %d of 60 columns", used)
	}
	stackedOut := stacked.Render("top", "LEFT", "RIGHT", "PREVIEW", "", "bottom")
	if rowContains(stackedOut, "LEFT", "RIGHT") {
		t.Errorf("panes did not stay stacked:\n%s", stackedOut)
	}
	if !strings.Contains(stackedOut, "PREVIEW") {
		t.Errorf("the preview was not drawn beside the stacked panes:\n%s", stackedOut)
	}
}
