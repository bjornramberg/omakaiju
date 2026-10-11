package ui

import (
	"strings"

	"github.com/charmbracelet/x/ansi"
)

// OverlayCentered draws box on top of background, centred on both axes.
//
// Lipgloss joins blocks but does not layer them, so the box is spliced into the
// background one row at a time. Every row is padded or clipped to exactly the
// background's width in cells, so the result keeps the line count and the
// exact-fit invariant the rest of the layout depends on.
func OverlayCentered(background, box string, width int) string {
	bgLines := strings.Split(background, "\n")
	boxLines := strings.Split(box, "\n")

	if len(bgLines) == 0 {
		return box
	}

	bgWidth := DisplayWidth(bgLines[0])
	if width > 0 && width < bgWidth {
		bgWidth = width
	}

	top := (len(bgLines) - len(boxLines)) / 2
	if top < 0 {
		top = 0
	}

	for i, line := range boxLines {
		row := top + i
		if row >= len(bgLines) {
			break
		}
		bgLines[row] = spliceRow(bgLines[row], line, bgWidth)
	}

	return strings.Join(bgLines, "\n")
}

// spliceRow centres line within row, clipping or padding row to exactly width
// cells so surrounding content can never be left dangling or re-wrapped.
func spliceRow(row, line string, width int) string {
	rowW := DisplayWidth(row)
	lineW := DisplayWidth(line)
	if lineW > width {
		line = TruncateEnd(line, width)
		lineW = DisplayWidth(line)
	}

	left := (rowW - lineW) / 2
	if left < 0 {
		left = 0
	}

	// Pad rather than truncate: the background is being clipped to make room
	// for the box, not shortened for display, so an ellipsis marker here would
	// be misleading.
	prefix := ansi.Truncate(row, left, "")
	leftPad := left - DisplayWidth(prefix)

	var b strings.Builder
	b.WriteString(prefix)
	for i := 0; i < leftPad; i++ {
		b.WriteByte(' ')
	}
	b.WriteString(line)

	used := DisplayWidth(prefix) + leftPad + lineW
	for i := used; i < width; i++ {
		b.WriteByte(' ')
	}
	return b.String()
}
