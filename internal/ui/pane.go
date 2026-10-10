package ui

import (
	"fmt"
	"strings"

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
	// Both dimensions are bounded. Width alone left the frame free to grow
	// taller than the terminal, which scrolls the view and makes the whole UI
	// jump while a window is being resized.
	borderStyle = borderStyle.MaxWidth(p.Width).MaxHeight(p.Height)

	inner := p.ContentWidth()

	// Rows available for file entries: the frame (border 2 + padding 2), the
	// path header plus its blank line, and an optional status line.
	statusRows := 0
	status := ""
	if p.Filter != "" {
		statusRows = 2 // blank line plus the status row
		status = fmt.Sprintf("%d/%d", len(p.Files), p.TotalFiles)
		if p.Status != "" {
			status += "  " + p.Status
		}
	} else if p.Status != "" {
		statusRows = 1
		status = p.Status
	}

	listRows := p.Height - 4 - 2 - statusRows
	if listRows < 1 {
		listRows = 1
	}

	pathStyle := p.Theme.PathText().UnsetPaddingLeft().UnsetPaddingRight()
	header := pathStyle.Width(inner).MaxWidth(inner).Render(TruncateName(p.Path, inner))

	var body string
	if len(p.Files) == 0 {
		// One placeholder row, padded out so the frame keeps its height.
		msg := "empty directory"
		if p.Filter != "" {
			msg = "no matches for " + p.Filter
		}
		body = p.Theme.StatusText().Render(TruncateName(msg, inner))
	} else {
		visibleHeight := p.VisibleHeight
		if visibleHeight <= 0 || visibleHeight > listRows {
			visibleHeight = listRows
		}

		start := 0
		if p.Cursor >= visibleHeight {
			start = p.Cursor - visibleHeight + 1
		}
		end := start + visibleHeight
		if end > len(p.Files) {
			end = len(p.Files)
		}

		rows := make([]string, 0, visibleHeight)
		for i := start; i < end; i++ {
			item := NewFileItem(p.Files[i], i == p.Cursor, p.Marked[p.Files[i].Path], p.Theme, inner)
			rows = append(rows, item.Render())
		}
		// Pad short lists so the pane always occupies its full column height.
		for len(rows) < visibleHeight {
			rows = append(rows, "")
		}
		body = strings.Join(rows, "\n")
	}

	content := header + "\n\n" + body
	if status != "" {
		content += "\n" + strings.Repeat("\n", statusRows-1) + p.Theme.AccentText().Render(status)
	}

	// Height pads a short pane out to its allocation; MaxHeight clips one that
	// somehow still runs long.
	return borderStyle.Width(p.Width).Height(p.Height).Render(content)
}
