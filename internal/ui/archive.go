package ui

import (
	"errors"
	"strings"

	"github.com/bjornramberg/omakaiju/internal/config"
	"github.com/bjornramberg/omakaiju/internal/fs"
	"github.com/bjornramberg/omakaiju/internal/icons"
)

// ArchiveLines formats archive entries for the preview panel: icon, name, and a
// faint size, with directories in the directory style. maxCells bounds each row
// so long member paths cannot wrap and break the panel height.
func ArchiveLines(entries []fs.ArchiveEntry, truncated bool, theme config.Theme, maxCells int) []string {
	lines := make([]string, 0, len(entries)+1)
	if maxCells < 1 {
		maxCells = 1
	}

	for _, e := range entries {
		// Entry names come from the archive and may hold raw bytes; the styled
		// row built below is ours and must be left alone.
		name := SanitizeName(e.Name)
		style := theme.FileItem()
		if e.IsDir {
			name = strings.TrimSuffix(name, "/") + "/"
			style = theme.FileItemDir()
		}

		icon := icons.ForFile(e.Name)
		if e.IsDir {
			icon = icons.ForDir()
		}

		// The icon column is the icon plus the space after it. On a panel too
		// narrow to leave even one cell of name, the icon is dropped rather
		// than letting it push the row past the panel.
		prefix := icon + " "
		if DisplayWidth(prefix) >= maxCells {
			prefix = ""
		}

		// Budget the size column before truncating the name, not after: the
		// name gets what is left, and the size is never the part that is cut
		// off. Directories carry no size. When the panel cannot fit the size
		// and a cell of name, the size is dropped so the row still fits.
		detail := ""
		if !e.IsDir {
			detail = " " + fs.FormatSize(e.Size)
			if DisplayWidth(prefix)+DisplayWidth(detail)+1 > maxCells {
				detail = ""
			}
		}

		nameBudget := maxCells - DisplayWidth(prefix) - DisplayWidth(detail)
		if nameBudget < 1 {
			nameBudget = 1
		}
		left := prefix + TruncateName(name, nameBudget)
		row := left

		// The size is pushed to the right edge, as the file rows push their
		// detail columns, so sizes line up down the listing instead of
		// trailing each name at a different column.
		if detail != "" {
			pad := maxCells - DisplayWidth(left) - DisplayWidth(detail)
			if pad < 0 {
				pad = 0
			}
			row = left + strings.Repeat(" ", pad) + detail
		}

		// Backstop: a wide glyph or a budget edge case must never produce a
		// row wider than the panel, which would wrap and add a phantom line.
		row = TruncateEnd(row, maxCells)
		lines = append(lines, style.UnsetPaddingLeft().UnsetPaddingRight().Render(row))
	}

	if truncated {
		note := TruncateEnd("... listing truncated", maxCells)
		lines = append(lines, theme.StatusText().Render(note))
	}
	return lines
}

// archiveMessage turns a listing error into something worth showing in the
// panel instead of a blank preview.
func archiveMessage(err error) string {
	if errors.Is(err, fs.ErrUnsupportedArchive) {
		return "listing not supported for this format"
	}
	return "could not read archive: " + err.Error()
}
