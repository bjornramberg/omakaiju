package ui

import "strings"

// statusPiece is one part of a panel's status line.
//
// The pieces are printed in the order they appear in the slice. Drop says
// when the piece is given up to make the line fit: the piece with the
// lowest number goes first, and a piece with Drop of zero is never dropped.
type statusPiece struct {
	Text string
	Drop int
}

// statusLine joins the pieces into one line and drops the droppable ones,
// least important first, until the line fits within width.
//
// A status line that wrapped would push the panel's contents around, so a
// narrow panel shows fewer hints rather than more lines. When nothing is
// left to drop the line is cut rather than allowed to wrap.
func statusLine(pieces []statusPiece, width int) string {
	kept := make([]statusPiece, len(pieces))
	copy(kept, pieces)

	for {
		parts := make([]string, 0, len(kept))
		for _, p := range kept {
			parts = append(parts, p.Text)
		}
		line := strings.Join(parts, " | ")
		if DisplayWidth(line) <= width {
			return line
		}

		// Drop the piece that goes first.
		idx := -1
		for i, p := range kept {
			if p.Drop == 0 {
				continue
			}
			if idx < 0 || p.Drop < kept[idx].Drop {
				idx = i
			}
		}
		if idx < 0 {
			return TruncateEnd(line, width)
		}
		kept = append(kept[:idx], kept[idx+1:]...)
	}
}
