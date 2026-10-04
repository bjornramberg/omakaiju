package ui

import (
	"omakaiju/internal/config"
)

type BottomBar struct {
	Width    int
	Input    string
	Error    string
	OpResult string
	Theme    config.Theme
}

func NewBottomBar(width int, theme config.Theme) BottomBar {
	return BottomBar{Width: width, Theme: theme}
}

func (b BottomBar) Render() string {
	if b.Error != "" {
		return b.Theme.ErrorText().
			Width(b.Width).
			Render(" Error: " + b.Error)
	}

	if b.Input != "" {
		prompt := b.Theme.AccentText().Render("> ")
		return b.Theme.BottomBar().Width(b.Width).Render(prompt + b.Input)
	}

	if b.OpResult != "" {
		return b.Theme.AccentText().
			Width(b.Width).
			Render(" " + b.OpResult)
	}

	help := b.Theme.StatusText().Render("h/j/k/l:nav  y:yank  m:move  p:paste  d:delete  /:filter  q:quit")
	return b.Theme.BottomBar().Width(b.Width).Render(help)
}
