package ui

import (
	"fmt"
	"os"
	"strings"

	"github.com/bjornramberg/omakaiju/internal/config"
	"github.com/bjornramberg/omakaiju/internal/fs"
	"github.com/bjornramberg/omakaiju/internal/icons"

	"charm.land/lipgloss/v2"
)

type FileItem struct {
	Entry  fs.Entry
	Active bool
	Marked bool
	Theme  config.Theme
	Width  int
}

func NewFileItem(entry fs.Entry, active bool, marked bool, theme config.Theme, width int) FileItem {
	return FileItem{
		Entry:  entry,
		Active: active,
		Marked: marked,
		Theme:  theme,
		Width:  width,
	}
}

func (f FileItem) Render() string {
	var style lipgloss.Style
	var icon string

	if f.Entry.IsDir {
		icon = icons.ForDir()
		style = f.Theme.FileItemDir()
	} else {
		icon = icons.ForFile(f.Entry.Name)
		style = f.Theme.FileItem()
	}

	// Fixed-width marker column so marked and unmarked rows stay aligned.
	marker := " "
	if f.Marked {
		marker = f.Theme.AccentText().Render("*")
	}

	if f.Active {
		style = f.Theme.FileItemActive()
	}

	// Budget the row before rendering: marker, two spaces, and the icon (which
	// may be a double-width Nerd Font glyph). Width alone is a minimum, so an
	// over-long name would otherwise spill past the border and wrap, producing
	// phantom rows that break cursor alignment.
	used := DisplayWidth(marker) + 2 + DisplayWidth(icon)
	budget := f.Width - used
	if budget < 1 {
		budget = 1
	}

	size, perms := f.detailColumns(budget)
	// The trailing slash is part of the name before truncation, not an
	// addition after it. TruncateName preserves it as the directory suffix,
	// so a long dirname is cut to exactly its budget instead of one cell
	// past it, which used to spill the row and wrap the permissions.
	name := SanitizeDisplay(f.Entry.Name)
	if f.Entry.IsDir {
		name += "/"
	}
	name = TruncateName(name, budget-detailWidth(size, perms))

	left := fmt.Sprintf("%s %s %s", marker, icon, name)
	content := left

	// The details have to be pushed to a fixed column rather than trailing the
	// name. Padding the row style only adds cells at the far right, so a short
	// name would drag its size and permissions left and leave the columns
	// ragged down the listing.
	if detail := strings.TrimRight(size+perms, " "); detail != "" {
		right := " " + detail
		pad := f.Width - DisplayWidth(left) - DisplayWidth(right)
		if pad < 0 {
			pad = 0
		}
		content = left + strings.Repeat(" ", pad) + right
	}

	// The theme's row style carries horizontal padding, but the row already
	// spaces itself with the marker and icon. Leaving padding on would add
	// unaccounted columns and push the row past MaxWidth, so it is removed and
	// MaxWidth acts as a backstop against any future miscalculation.
	row := style.UnsetPaddingLeft().UnsetPaddingRight()
	return row.Width(f.Width).MaxWidth(f.Width).Render(content)
}

// Column geometry for the detail fields. The size column is wide enough for the
// largest FormatSize output and right-aligned so units line up; permissions
// are always ten characters.
const (
	sizeColumnWidth  = 9
	permsColumnWidth = 10
	gapWidth         = 1
	detailGap        = 1 /*space between the name and the details*/
	// minNameWidth is the narrowest a name may become before its column is
	// dropped. A filename squeezed to a couple of cells is worse than no
	// detail at all.
	minNameWidth = 12
)

// detailColumns decides which detail columns fit in the space left over, and
// returns them already padded. Size is given up first, then permissions, so a
// narrow pane degrades to today's name-only row rather than an unreadable one.
func (f FileItem) detailColumns(budget int) (size, perms string) {
	if budget-(sizeColumnWidth+gapWidth+permsColumnWidth) >= minNameWidth {
		// The gap belongs to the size column, so it goes first when size is
		// dropped along with it.
		return f.sizeCell() + strings.Repeat(" ", gapWidth), permsCell(f.Entry.Mode)
	}
	if budget-(permsColumnWidth+gapWidth) >= minNameWidth {
		return "", strings.Repeat(" ", gapWidth) + permsCell(f.Entry.Mode)
	}
	return "", ""
}

// sizeCell renders the size right-aligned in a fixed column. Directories have
// no meaningful byte count, so the cell is left blank.
func (f FileItem) sizeCell() string {
	if f.Entry.IsDir {
		return strings.Repeat(" ", sizeColumnWidth)
	}
	size := fs.FormatSize(f.Entry.Size)
	if DisplayWidth(size) > sizeColumnWidth {
		size = TruncateEnd(size, sizeColumnWidth)
	}
	return strings.Repeat(" ", sizeColumnWidth-DisplayWidth(size)) + size
}

// permsCell renders permission bits in a fixed column. Symlinks are shown with
// a lowercase l like ls does, rather than the capital L Go's String method
// emits, so the type characters stay consistent: -, d, l.
func permsCell(mode os.FileMode) string {
	if mode == 0 {
		// No mode recorded, such as an entry built by hand in a test.
		return strings.Repeat(" ", permsColumnWidth)
	}
	perms := mode.String()
	if mode&os.ModeSymlink != 0 {
		perms = "l" + perms[1:]
	}
	if DisplayWidth(perms) > permsColumnWidth {
		perms = TruncateEnd(perms, permsColumnWidth)
	}
	return perms + strings.Repeat(" ", permsColumnWidth-DisplayWidth(perms))
}

// detailWidth is the space the rendered details consume, including the gap that
// separates them from the name.
func detailWidth(size, perms string) int {
	w := DisplayWidth(size) + DisplayWidth(perms) + detailGap
	if w == detailGap {
		return 0
	}
	return w
}
