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

// OverlayAt draws box on top of the background with its top-left corner at
// (x, y). The box is spliced into the background one row at a time, keeping
// the background's line count and exact width unchanged so the layout
// underneath never shifts.
func OverlayAt(background, box string, x, y, width, height int) string {
	bgLines := strings.Split(background, "\n")
	boxLines := strings.Split(box, "\n")

	if len(bgLines) == 0 || width <= 0 || height <= 0 {
		return box
	}

	bgWidth := DisplayWidth(bgLines[0])
	if width > bgWidth {
		width = bgWidth
		x = 0
	}
	if x < 0 {
		x = 0
	}
	if y < 0 {
		y = 0
	}

	for i := 0; i < height && y+i < len(bgLines); i++ {
		if i >= len(boxLines) {
			break
		}
		bgLines[y+i] = spliceAt(bgLines[y+i], boxLines[i], x, width, bgWidth)
	}

	return strings.Join(bgLines, "\n")
}

// OverlayPosition clamps a box's top-left corner so the whole box stays inside
// the screen. The app uses the returned origin both for drawing and for
// hit-testing, so the clamping lives in exactly one place.
func OverlayPosition(x, y, boxW, boxH, screenW, screenH int) (int, int) {
	if boxW > screenW {
		boxW = screenW
	}
	if boxH > screenH {
		boxH = screenH
	}
	if x+boxW > screenW {
		x = screenW - boxW
	}
	if y+boxH > screenH {
		y = screenH - boxH
	}
	if x < 0 {
		x = 0
	}
	if y < 0 {
		y = 0
	}
	return x, y
}

// spliceAt replaces the cells starting at x with line, keeping the row at
// exactly bgWidth cells. The area covered by the box is padded with spaces if
// the line is shorter, and the original background is preserved on both sides.
func spliceAt(row, line string, x, boxW, bgW int) string {
	if x > bgW {
		x = bgW
	}

	// The box may run past the right edge of the background; clip it to the
	// cells that are actually available so the resulting row never exceeds
	// the background's width.
	avail := boxW
	if x+avail > bgW {
		avail = bgW - x
		if avail < 0 {
			avail = 0
		}
	}

	prefix := ansi.Truncate(row, x, "")
	leftPad := x - DisplayWidth(prefix)

	lineW := DisplayWidth(line)
	if lineW > avail {
		// Clip without an ellipsis: the box is being cut by the screen edge,
		// and a truncation marker floating at the border looks like content
		// rather than a deliberate clip.
		line = ansi.Truncate(line, avail, "")
		lineW = avail
	}

	var suffix string
	if x+avail < bgW {
		suffix = ansi.TruncateLeft(row, x+avail, "")
	}

	var b strings.Builder
	b.WriteString(prefix)
	for i := 0; i < leftPad; i++ {
		b.WriteByte(' ')
	}
	b.WriteString(line)
	for i := lineW; i < avail; i++ {
		b.WriteByte(' ')
	}
	b.WriteString(suffix)

	used := DisplayWidth(prefix) + leftPad + avail + DisplayWidth(suffix)
	for i := used; i < bgW; i++ {
		b.WriteByte(' ')
	}
	return b.String()
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
