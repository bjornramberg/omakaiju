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
	Marked bool
	Theme  config.Theme
	Width  int
}

func NewFileItem(entry fs.Entry, active bool, marked bool, theme config.Theme, width int) FileItem {
	return FileItem{
		Entry:  entry,
		Active: active,
		Marked: marked,
		Theme:  theme,
		Width:  width,
	}
}

func (f FileItem) Render() string {
	var style lipgloss.Style
	var icon string

	if f.Entry.IsDir {
		icon = icons.ForDir()
		style = f.Theme.FileItemDir()
	} else {
		icon = icons.ForFile(f.Entry.Name)
		style = f.Theme.FileItem()
	}

	// Fixed-width marker column so marked and unmarked rows stay aligned.
	marker := " "
	if f.Marked {
		marker = f.Theme.AccentText().Render("*")
	}

	if f.Active {
		style = f.Theme.FileItemActive()
	}

	// Budget the row before rendering: marker, two spaces, and the icon (which
	// may be a double-width Nerd Font glyph). Width alone is a minimum, so an
	// over-long name would otherwise spill past the border and wrap, producing
	// phantom rows that break cursor alignment.
	used := DisplayWidth(marker) + 2 + DisplayWidth(icon)
	budget := f.Width - used
	name := TruncateName(f.Entry.Name, budget)
	if f.Entry.IsDir {
		name += "/"
	}

	content := fmt.Sprintf("%s %s %s", marker, icon, name)

	// The theme's row style carries horizontal padding, but the row already
	// spaces itself with the marker and icon. Leaving padding on would add
	// unaccounted columns and push the row past MaxWidth, so it is removed and
	// MaxWidth acts as a backstop against any future miscalculation.
	row := style.UnsetPaddingLeft().UnsetPaddingRight()
	return row.Width(f.Width).MaxWidth(f.Width).Render(content)
}
