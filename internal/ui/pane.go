package ui

import (
	"fmt"

	"omakaiju/internal/config"
	"omakaiju/internal/fs"

	"charm.land/lipgloss/v2"
)

type Pane struct {
	Width         int
	Height        int
	Active        bool
	Theme         config.Theme
	Path          string
	Filter        string
	Files         []fs.Entry
	TotalFiles    int
	Cursor        int
	VisibleHeight int
	Status        string
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

	if len(p.Files) == 0 {
		if p.Filter != "" {
			content += p.Theme.StatusText().Render("no matches for " + p.Filter)
		} else {
			content += p.Theme.StatusText().Render("empty directory")
		}
	} else {
		visibleHeight := p.VisibleHeight
		if visibleHeight <= 0 {
			visibleHeight = p.Height - 4
		}

		start := 0
		if p.Cursor >= visibleHeight {
			start = p.Cursor - visibleHeight + 1
		}

		end := start + visibleHeight
		if end > len(p.Files) {
			end = len(p.Files)
		}

		for i := start; i < end; i++ {
			item := NewFileItem(p.Files[i], i == p.Cursor, p.Theme, p.Width-4)
			content += item.Render() + "\n"
		}
	}

	if p.Filter != "" {
		status := fmt.Sprintf("%d/%d", len(p.Files), p.TotalFiles)
		if p.Status != "" {
			status += "  " + p.Status
		}
		content += "\n" + p.Theme.AccentText().Render(status)
	} else if p.Status != "" {
		content += "\n" + p.Theme.AccentText().Render(p.Status)
	}

	return borderStyle.Width(p.Width).Height(p.Height).Render(content)
}
