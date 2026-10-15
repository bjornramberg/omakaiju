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
	// PanesStacked puts the two file panes on top of each other instead of
	// side by side. The preview stays on the right at full height either way.
	PanesStacked bool
	// PreviewHidden hides the preview pane, giving full width to the file panes.
	PreviewHidden bool
}

// minStackedMainHeight is the smallest main area that still fits two stacked
// panes. Below this the panes would degrade to little more than borders, so
// the toggle stays horizontal instead.
const minStackedMainHeight = 10

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

// PaneWidth keeps at least one column for each pane even when the preview takes
// a third of a very narrow terminal. Stacked panes span the whole panes area.
func (l Layout) PaneWidth() int {
	if l.PanesStacked {
		w := l.Width - l.PreviewWidth()
		if w < 1 {
			w = 1
		}
		return w
	}
	w := (l.Width - l.PreviewWidth()) / 2
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
	if !l.PanesStacked {
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
// usable.
func (l Layout) CanStack() bool {
	return l.MainAreaHeight() >= minStackedMainHeight
}

func (l Layout) Render(topBar, leftPane, rightPane, preview, bottomBar string) string {
	var main string
	if l.PanesStacked {
		stack := lipgloss.JoinVertical(lipgloss.Left, leftPane, rightPane)
		if l.PreviewHidden {
			main = stack
		} else {
			main = lipgloss.JoinHorizontal(lipgloss.Top, stack, preview)
		}
	} else {
		if l.PreviewHidden {
			main = lipgloss.JoinHorizontal(lipgloss.Top, leftPane, rightPane)
		} else {
			main = lipgloss.JoinHorizontal(lipgloss.Top, leftPane, rightPane, preview)
		}
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
