package ui

import (
	"errors"
	"fmt"
	"strings"

	"omakaiju/internal/config"
	"omakaiju/internal/fs"
	"omakaiju/internal/icons"
)

// ArchiveLines formats archive entries for the preview panel: icon, name, and a
// faint size, with directories in the directory style. maxCells bounds each row
// so long member paths cannot wrap and break the panel height.
func ArchiveLines(entries []fs.ArchiveEntry, truncated bool, theme config.Theme, maxCells int) []string {
	lines := make([]string, 0, len(entries)+1)

	for _, e := range entries {
		name := e.Name
		style := theme.FileItem()
		if e.IsDir {
			name = strings.TrimSuffix(name, "/") + "/"
			style = theme.FileItemDir()
		}

		icon := icons.ForFile(e.Name)
		if e.IsDir {
			icon = icons.ForDir()
		}

		row := fmt.Sprintf("%s %s", icon, TruncateName(name, maxCells-2-DisplayWidth(icon)))
		if !e.IsDir {
			row += "  " + theme.StatusText().Render(fs.FormatSize(e.Size))
		}
		lines = append(lines, style.UnsetPaddingLeft().UnsetPaddingRight().Render(row))
	}

	if truncated {
		lines = append(lines, theme.StatusText().Render("... listing truncated"))
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
