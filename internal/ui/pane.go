package ui

import (
	"fmt"
	"strings"

	"github.com/bjornramberg/omakaiju/internal/config"
	"github.com/bjornramberg/omakaiju/internal/fs"

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

// paneRowsTop is the pane-local row the first file row occupies: the top
// border, the top padding, the path header, and the blank line under it.
const paneRowsTop = 4

// Rows is the window of Files a pane draws: the index of the first entry, how
// many rows the body has, and where the first one sits inside the pane frame.
//
// Render walks this window, and the mouse hit-test maps a screen row back
// through it. Both read the same numbers, so the entry under the pointer is
// always the entry the renderer drew there.
type Rows struct {
	Start int
	Count int
	Top   int
}

// Rows computes the listing window for the current cursor and height, scrolled
// so the cursor is always inside it. It is the single source of truth for the
// pane's row math; Render must not compute its own.
func (p Pane) Rows() Rows {
	// An empty listing draws one placeholder row, which is not an entry and
	// cannot be selected.
	if len(p.Files) == 0 {
		return Rows{Top: paneRowsTop}
	}
	visible := p.visibleRows()
	start := 0
	if p.Cursor >= visible {
		start = p.Cursor - visible + 1
	}
	return Rows{Start: start, Count: visible, Top: paneRowsTop}
}

// visibleRows is the number of file rows the pane has room for after its frame,
// its header, and any status row. At least one row is always reported so a
// degenerate height cannot produce an empty body.
func (p Pane) visibleRows() int {
	listRows := p.Height - 4 - 2 - p.statusRows()
	if listRows < 1 {
		listRows = 1
	}
	visible := p.VisibleHeight
	if visible <= 0 || visible > listRows {
		visible = listRows
	}
	return visible
}

// statusRows is how many rows the status area takes at the bottom of the pane:
// a filter gets a blank line plus its count, a status alone gets one row.
func (p Pane) statusRows() int {
	if p.Filter != "" {
		return 2
	}
	if p.Status != "" {
		return 1
	}
	return 0
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
	// path header plus its blank line, and an optional status line. Rows
	// shares the status-row count with the hit-test.
	statusRows := p.statusRows()
	status := ""
	if p.Filter != "" {
		status = fmt.Sprintf("%d/%d", len(p.Files), p.TotalFiles)
		if p.Status != "" {
			// The status can quote a filesystem name, which may contain
			// control bytes. Clean it before it reaches the terminal.
			status += "  " + SanitizeName(p.Status)
		}
	} else if p.Status != "" {
		status = SanitizeName(p.Status)
	}

	// The header reserves a marker column in both panes, so the path starts
	// in the same column either way and the marker is a shape cue first: it
	// says which pane has focus even where the colours are too close to tell
	// apart. A pane too narrow for a marker keeps the whole width for the
	// path.
	marker, sep, pathWidth := "", "", inner
	if inner >= 3 {
		marker = " "
		if p.Active {
			marker = "▸"
		}
		sep = " "
		pathWidth = inner - 2
	}

	// The header is rendered as a single run, so the focused pane's band
	// covers the marker cell along with the path.
	headerStyle := p.Theme.PaneHeaderInactive()
	if p.Active {
		headerStyle = p.Theme.PaneHeaderActive()
	}
	header := headerStyle.Width(inner).MaxWidth(inner).Render(marker + sep + TruncateName(SanitizeName(p.Path), pathWidth))

	var body string
	if len(p.Files) == 0 {
		// One placeholder row, padded out so the frame keeps its height.
		msg := "empty directory"
		if p.Filter != "" {
			msg = "no matches for " + SanitizeName(p.Filter)
		}
		body = p.Theme.StatusText().Render(TruncateName(msg, inner))
	} else {
		// The row window comes from Rows, the same one the mouse hit-test
		// resolves a click through.
		window := p.Rows()
		start := window.Start
		end := start + window.Count
		if end > len(p.Files) {
			end = len(p.Files)
		}

		rows := make([]string, 0, window.Count)
		for i := start; i < end; i++ {
			item := NewFileItem(p.Files[i], i == p.Cursor, p.Marked[p.Files[i].Path], p.Theme, inner)
			item.Unfocused = !p.Active
			rows = append(rows, item.Render())
		}
		// Pad short lists so the pane always occupies its full column height.
		for len(rows) < window.Count {
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
