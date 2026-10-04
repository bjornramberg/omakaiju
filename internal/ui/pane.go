package ui

import (
	"omakaiju/internal/config"
	"omakaiju/internal/fs"

	"charm.land/lipgloss/v2"
)

type Pane struct {
	Width  int
	Height int
	Active bool
	Theme  config.Theme
	Path   string
	Files  []fs.Entry
	Cursor int
}

func NewPane(width, height int, active bool, theme config.Theme) Pane {
	return Pane{
		Width:  width,
		Height: height,
		Active: active,
		Theme:  theme,
	}
}

func (p Pane) Render() string {
	var borderStyle lipgloss.Style
	if p.Active {
		borderStyle = p.Theme.PaneActive()
	} else {
		borderStyle = p.Theme.PaneInactive()
	}

	var content string
	content += p.Theme.PathText().Render(p.Path) + "\n\n"

	for i, entry := range p.Files {
		item := NewFileItem(entry, i == p.Cursor, p.Theme, p.Width-4)
		content += item.Render() + "\n"
	}

	return borderStyle.Width(p.Width).Height(p.Height).Render(content)
}
