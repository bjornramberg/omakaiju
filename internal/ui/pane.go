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
	Marked        map[string]bool
}

func NewPane(width, height int, active bool, theme config.Theme) Pane {
	return Pane{
		Width:  width,
		Height: height,
		Active: active,
		Theme:  theme,
	}
}

// ContentWidth is the usable width inside the pane border and its padding.
// Width alone is a minimum in Lipgloss, so rows must be sized to this or they
// spill past the border and wrap into phantom rows.
func (p Pane) ContentWidth() int {
	w := p.Width - 2 /*border*/ - 4 /*padding*/
	if w < 1 {
		return 1
	}
	return w
}

func (p Pane) Render() string {
	var borderStyle lipgloss.Style
	if p.Active {
		borderStyle = p.Theme.PaneActive()
	} else {
		borderStyle = p.Theme.PaneInactive()
	}
	// Backstop so no child can ever force the pane wider than its column.
	borderStyle = borderStyle.MaxWidth(p.Width)

	inner := p.ContentWidth()

	// PathText has no padding of its own, but the panel style does; drop it so
	// the budget matches the space actually available.
	pathStyle := p.Theme.PathText().UnsetPaddingLeft().UnsetPaddingRight()
	var content string
	content += pathStyle.Width(inner).MaxWidth(inner).Render(TruncateName(p.Path, inner)) + "\n\n"

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
			item := NewFileItem(p.Files[i], i == p.Cursor, p.Marked[p.Files[i].Path], p.Theme, inner)
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
