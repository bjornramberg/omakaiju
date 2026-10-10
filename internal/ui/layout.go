package ui

import (
	"fmt"

	"omakaiju/internal/config"

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
}

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
	w := l.Width / 3
	if w < 1 {
		w = 1
	}
	return w
}

// PaneWidth keeps at least one column for each pane even when the preview takes
// a third of a very narrow terminal.
func (l Layout) PaneWidth() int {
	w := (l.Width - l.PreviewWidth()) / 2
	if w < 1 {
		w = 1
	}
	return w
}

func (l Layout) Render(topBar, leftPane, rightPane, preview, bottomBar string) string {
	main := lipgloss.JoinHorizontal(lipgloss.Top, leftPane, rightPane, preview)
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
