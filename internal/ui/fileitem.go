package ui

import (
	"fmt"
	"omakaiju/internal/config"
	"omakaiju/internal/fs"
	"omakaiju/internal/icons"

	"charm.land/lipgloss/v2"
)

type FileItem struct {
	Entry  fs.Entry
	Active bool
	Theme  config.Theme
	Width  int
}

func NewFileItem(entry fs.Entry, active bool, theme config.Theme, width int) FileItem {
	return FileItem{
		Entry:  entry,
		Active: active,
		Theme:  theme,
		Width:  width,
	}
}

func (f FileItem) Render() string {
	var style lipgloss.Style
	var icon string
	var name string

	if f.Entry.IsDir {
		icon = icons.ForDir()
		name = f.Entry.Name + "/"
		style = f.Theme.FileItemDir()
	} else {
		icon = icons.ForFile(f.Entry.Name)
		name = f.Entry.Name
		style = f.Theme.FileItem()
	}

	if f.Active {
		style = f.Theme.FileItemActive()
	}

	content := fmt.Sprintf("%s %s", icon, name)

	return style.Width(f.Width).Render(content)
}
