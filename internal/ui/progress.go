package ui

import (
	"fmt"
	"strings"

	"charm.land/lipgloss/v2"

	"omakaiju/internal/config"
	"omakaiju/internal/fs"
)

// Progress describes an in-flight copy for display. Copied and Total are the
// aggregate byte counts across every queued file, while Name and Percent
// describe the file currently being written.
type Progress struct {
	Name      string
	Copied    int64
	Total     int64
	Percent   int
	FileIndex int
	FileTotal int
	Theme     config.Theme
}

// NewProgress snapshots a copier for rendering.
func NewProgress(c *fs.Copier, theme config.Theme) Progress {
	return Progress{
		Name:      c.CurrentName(),
		Copied:    c.Copied(),
		Total:     c.Total(),
		Percent:   c.Percent(),
		FileIndex: c.FilesDone() + 1,
		FileTotal: c.FilesTotal(),
		Theme:     theme,
	}
}

// bar renders a proportional block bar at the given pixel width.
func (p Progress) bar(pixels int) string {
	if pixels < 4 {
		pixels = 4
	}
	ratio := 0.0
	if p.Total > 0 {
		ratio = float64(p.Copied) / float64(p.Total)
	}
	if ratio < 0 {
		ratio = 0
	}
	if ratio > 1 {
		ratio = 1
	}

	filled := int(ratio * float64(pixels))
	if filled > pixels {
		filled = pixels
	}

	return strings.Repeat("█", filled) + strings.Repeat("░", pixels-filled)
}

// Render draws the copy line, guaranteed to fit maxCells columns.
func (p Progress) Render(maxCells int) string {
	if maxCells <= 0 {
		return ""
	}

	// Trailing counters are fixed-width so only the name is variable.
	suffix := fmt.Sprintf(" %d%% %s/%s", p.Percent,
		fs.FormatSize(p.Copied), fs.FormatSize(p.Total))
	if p.FileTotal > 1 {
		suffix += fmt.Sprintf(" file %d/%d", p.FileIndex, p.FileTotal)
	}
	suffix += " (esc: cancel)"

	// "copying [bar] " + suffix, reserving a 10-cell bar when there is room.
	prefix := "copying "
	chrome := DisplayWidth(prefix) + 2 + 10 + DisplayWidth(suffix)

	if chrome > maxCells {
		// Too tight for a bar: show the counters alone.
		text := fmt.Sprintf("%s%s", prefix, suffix)
		if DisplayWidth(text) > maxCells {
			return TruncateName(text, maxCells)
		}
		return text
	}

	barCells := maxCells - DisplayWidth(prefix) - 2 - DisplayWidth(suffix)
	bar := p.Theme.AccentText().Render(p.bar(barCells))

	return prefix + bar + " " + suffix
}

// ProgressStyle keeps the bar visually consistent with the rest of the bar.
func (p Progress) ProgressStyle() lipgloss.Style {
	return lipgloss.NewStyle().Foreground(lipgloss.Color(p.Theme.Primary))
}
