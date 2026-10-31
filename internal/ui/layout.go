package ui

import (
	"fmt"

	"github.com/bjornramberg/omakaiju/internal/config"

	"charm.land/lipgloss/v2"
)

// Minimum usable terminal size. Below this the dual-pane layout has no room to
// render anything meaningful, so a placeholder is shown instead of letting
// Lipgloss produce undefined output from zero or negative dimensions.
const (
	MinWidth  = 40
	MinHeight = 10
)

type Layout struct {
	Width  int
	Height int
	Theme  config.Theme
	// PanesStacked is the user's choice to put the two file panes on top of
	// each other instead of side by side. It is remembered even when the
	// terminal is too short to honour it: the layout falls back to side by
	// side until there is room again. The preview stays on the right at full
	// height either way.
	PanesStacked bool
	// PreviewHidden hides the preview pane, giving full width to the file panes.
	PreviewHidden bool
	// DebugActive requests the hidden debug pane. It is independent of the
	// preview: it can sit underneath it, or alone in the right column.
	DebugActive bool
}

// minStackedMainHeight is the smallest main area that still fits two stacked
// panes. Below this the panes would degrade to little more than borders, so
// they are drawn side by side instead.
const minStackedMainHeight = 10

// minDebugPaneHeight is the smallest row count worth drawing for the debug
// pane. The preview keeps the same floor when it shares the column.
const minDebugPaneHeight = 3

func NewLayout(width, height int, theme config.Theme) Layout {
	return Layout{Width: width, Height: height, Theme: theme}
}

// TooSmall reports whether the terminal cannot host the layout.
func (l Layout) TooSmall() bool {
	return l.Width < MinWidth || l.Height < MinHeight
}

func (l Layout) TopBarHeight() int {
	return 3
}

func (l Layout) BottomBarHeight() int {
	return 3
}

// MainAreaHeight never goes below one row, so a tiny terminal cannot produce a
// negative content height.
func (l Layout) MainAreaHeight() int {
	h := l.Height - l.TopBarHeight() - l.BottomBarHeight()
	if h < 1 {
		return 1
	}
	return h
}

func (l Layout) PreviewWidth() int {
	if l.PreviewHidden {
		return 0
	}
	w := l.Width / 3
	if w < 1 {
		w = 1
	}
	return w
}

// DebugHeight is the height of the debug pane. Sharing the right column, the
// main area splits in half: the preview keeps the extra row on an odd height,
// like the top pane of a stacked pair, and each half must still have
// minDebugPaneHeight rows. When the preview is hidden the pane is alone in the
// column and takes all of it. Zero means the pane is off or there is no room
// for it.
func (l Layout) DebugHeight() int {
	if !l.DebugActive {
		return 0
	}
	h := l.MainAreaHeight()
	if l.PreviewHidden {
		if h < minDebugPaneHeight {
			return 0
		}
		return h
	}
	debug := h / 2
	if debug < minDebugPaneHeight || h-debug < minDebugPaneHeight {
		return 0
	}
	return debug
}

// DebugVisible reports whether the debug pane is actually drawn. The request
// is kept when the terminal is too short, so the pane returns once it grows.
func (l Layout) DebugVisible() bool {
	return l.DebugHeight() > 0
}

// PreviewHeight is the preview's share of the right column: the whole main
// area unless the debug pane is sharing it, in which case it is what is left.
func (l Layout) PreviewHeight() int {
	if l.PreviewHidden {
		return 0
	}
	h := l.MainAreaHeight()
	if debug := l.DebugHeight(); debug > 0 {
		return h - debug
	}
	return h
}

// DebugWidth is the debug column's width. Beside the preview it shares the
// preview's column; alone it keeps the same share of the terminal, so the file
// panes do not suddenly span everything when the preview is toggled off.
func (l Layout) DebugWidth() int {
	if !l.DebugVisible() || !l.PreviewHidden {
		return 0
	}
	w := l.Width / 3
	if w < 1 {
		w = 1
	}
	return w
}

// RightColumnWidth is the width of whatever occupies the right column.
func (l Layout) RightColumnWidth() int {
	if !l.PreviewHidden {
		return l.PreviewWidth()
	}
	return l.DebugWidth()
}

// stacked reports whether the panes are actually drawn on top of each other.
// PanesStacked is only the request: a terminal too short for the stacked
// minimum falls back to side by side, and returns to stacked once it grows,
// because the choice itself is never discarded.
func (l Layout) stacked() bool {
	return l.PanesStacked && l.CanStack()
}

// PaneWidth keeps at least one column for each pane even when the preview takes
// a third of a very narrow terminal. Stacked panes span the whole panes area.
func (l Layout) PaneWidth() int {
	if l.stacked() {
		w := l.Width - l.RightColumnWidth()
		if w < 1 {
			w = 1
		}
		return w
	}
	w := (l.Width - l.RightColumnWidth()) / 2
	if w < 1 {
		w = 1
	}
	return w
}

// PaneHeight is the height of one file pane. Side by side both panes take the
// full main area; stacked they split it, with the top pane taking the extra
// row on odd heights so the pair always sums back to the main area exactly.
func (l Layout) PaneHeight(top bool) int {
	h := l.MainAreaHeight()
	if !l.stacked() {
		return h
	}
	half := h / 2
	if top {
		return h - half
	}
	if half < 1 {
		half = 1
	}
	return half
}

// CanStack reports whether the terminal is tall enough for stacked panes to be
// usable. It is checked on every access, not just when the user toggles, so a
// terminal that shrinks under a stacked layout falls back rather than showing
// panes that are nothing but borders.
func (l Layout) CanStack() bool {
	return l.MainAreaHeight() >= minStackedMainHeight
}

// rightColumn composes whatever occupies the right column. The debug pane sits
// underneath the preview when both are drawn, and alone when the preview is
// hidden. An empty string means there is no right column at all.
func (l Layout) rightColumn(preview, debug string) string {
	switch {
	case l.PreviewHidden:
		return debug
	case l.DebugVisible():
		return lipgloss.JoinVertical(lipgloss.Left, preview, debug)
	default:
		return preview
	}
}

func (l Layout) Render(topBar, leftPane, rightPane, preview, debug, bottomBar string) string {
	right := l.rightColumn(preview, debug)

	var main string
	if l.stacked() {
		stack := lipgloss.JoinVertical(lipgloss.Left, leftPane, rightPane)
		if right == "" {
			main = stack
		} else {
			main = lipgloss.JoinHorizontal(lipgloss.Top, stack, right)
		}
	} else {
		parts := []string{leftPane, rightPane}
		if right != "" {
			parts = append(parts, right)
		}
		main = lipgloss.JoinHorizontal(lipgloss.Top, parts...)
	}
	return lipgloss.JoinVertical(lipgloss.Left, topBar, main, bottomBar)
}

// TooSmallPlaceholder is shown when the terminal cannot fit the layout. It is
// bounded to whatever space actually exists so it cannot itself wrap.
func (l Layout) TooSmallPlaceholder() string {
	w := l.Width
	if w < 1 {
		return ""
	}
	msg := fmt.Sprintf("terminal too small (need %dx%d, have %dx%d)",
		MinWidth, MinHeight, l.Width, l.Height)
	h := l.Height
	if h < 1 {
		h = 1
	}

	style := l.Theme.PreviewPanel()
	// The panel's own border and padding are chrome, not content space.
	if avail := w - 2 /*border*/ - 4; /*padding*/ avail > 0 {
		msg = TruncateEnd(msg, avail)
	} else {
		msg = ""
	}
	return style.
		Width(w).
		MaxWidth(w).
		Height(h).
		MaxHeight(h).
		Render(l.Theme.StatusText().Render(msg))
}
