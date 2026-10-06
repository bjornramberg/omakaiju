package ui

import (
	"omakaiju/internal/config"

	"charm.land/lipgloss/v2"
)

type Layout struct {
	Width  int
	Height int
	Theme  config.Theme
}

func NewLayout(width, height int, theme config.Theme) Layout {
	return Layout{Width: width, Height: height, Theme: theme}
}

func (l Layout) TopBarHeight() int {
	return 3
}

func (l Layout) BottomBarHeight() int {
	return 3
}

func (l Layout) MainAreaHeight() int {
	return l.Height - l.TopBarHeight() - l.BottomBarHeight()
}

func (l Layout) PreviewWidth() int {
	return l.Width / 3
}

func (l Layout) PaneWidth() int {
	return (l.Width - l.PreviewWidth()) / 2
}

func (l Layout) Render(topBar, leftPane, rightPane, preview, bottomBar string) string {
	main := lipgloss.JoinHorizontal(lipgloss.Top, leftPane, rightPane, preview)
	return lipgloss.JoinVertical(lipgloss.Left, topBar, main, bottomBar)
}
