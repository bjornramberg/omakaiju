package ui

import (
	"os"

	"omakaiju/internal/config"
)

type TopBar struct {
	Width  int
	Filter string
	Theme  config.Theme
}

func NewTopBar(width int, theme config.Theme) TopBar {
	return TopBar{Width: width, Theme: theme}
}

func (t TopBar) Render() string {
	hostname, _ := os.Hostname()
	path := "/"

	left := t.Theme.AccentText().Bold(true).Render(" omakaiju ")
	sep := t.Theme.StatusText().Render(" │ ")
	pathText := t.Theme.PathText().Render(path)
	hostText := t.Theme.StatusText().Render(hostname)

	content := left + sep + pathText + sep + hostText

	if t.Filter != "" {
		content += sep + t.Theme.AccentText().Render("filter: "+t.Filter)
	}

	return t.Theme.TopBar().Width(t.Width).Render(content)
}
