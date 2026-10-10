package ui

import (
	"strings"

	"github.com/charmbracelet/x/ansi"
)

// ellipsis is the single-cell truncation marker.
const ellipsis = "…"

// DisplayWidth returns the rendered cell width of s, ignoring any styling.
// Cell measurement rather than rune counting matters because Nerd Font glyphs
// and CJK characters occupy two columns.
func DisplayWidth(s string) int {
	return ansi.StringWidth(s)
}

// TruncateName shortens a file or directory name to fit maxCells terminal cells,
// placing the ellipsis in the middle so the extension stays readable. That is
// the file-manager convention: "long-impo…rname.go" tells you more than
// "long-import-name.g…".
func TruncateName(name string, maxCells int) string {
	if maxCells <= 0 {
		return ""
	}
	if ansi.StringWidth(name) <= maxCells {
		return name
	}
	if maxCells == 1 {
		return ellipsis
	}

	suffix, stem := splitNameSuffix(name)

	// Keep the extension when it leaves room for at least part of the stem.
	extWidth := ansi.StringWidth(suffix)
	if extWidth > 0 && maxCells-extWidth-1 >= 1 {
		budget := maxCells - extWidth - 1
		if ansi.StringWidth(stem) <= budget {
			return stem + suffix
		}
		return truncateMiddle(stem, budget) + suffix
	}

	return truncateMiddle(name, maxCells)
}

// TrimPathLeft shortens a path by dropping leading components, since the tail of
// a path is the part that identifies where you are.
func TrimPathLeft(path string, maxCells int) string {
	if maxCells <= 0 {
		return ""
	}
	if ansi.StringWidth(path) <= maxCells {
		return path
	}
	if maxCells == 1 {
		return ellipsis
	}

	parts := strings.Split(path, "/")
	if len(parts) <= 2 {
		return TruncateEnd(path, maxCells)
	}

	// Keep the leading separator so it still reads as an absolute path.
	head := parts[0] + "/"
	for i := len(parts) - 1; i >= 2; i-- {
		candidate := head + strings.Join(parts[i:], "/")
		if ansi.StringWidth(candidate) > maxCells {
			break
		}
		head = candidate
	}
	// Whatever is left is the tail that fit, or the shortest useful slice.
	tail := strings.Join(parts[len(parts)-1:], "/")
	if ansi.StringWidth(head) == 0 {
		return TruncateEnd("/"+tail, maxCells)
	}
	return head
}

// TruncateEnd clips s to maxCells cells, marking the cut with a trailing
// ellipsis. Used for preview body lines, where the end of the line is the least
// informative part and clipping must not re-wrap.
func TruncateEnd(s string, maxCells int) string {
	if maxCells <= 0 {
		return ""
	}
	if ansi.StringWidth(s) <= maxCells {
		return s
	}
	if maxCells == 1 {
		return ellipsis
	}
	return ansi.Truncate(s, maxCells-1, "") + ellipsis
}

// truncateMiddle cuts s to maxCells cells with the ellipsis in the middle.
// Both halves are cell-budgeted so wide glyphs cannot push the result past
// maxCells and trigger a wrap.
func truncateMiddle(s string, maxCells int) string {
	if maxCells <= 0 {
		return ""
	}
	if ansi.StringWidth(s) <= maxCells {
		return s
	}
	if maxCells == 1 {
		return ellipsis
	}

	// One cell goes to the ellipsis; the rest splits between head and tail.
	remaining := maxCells - 1
	head := ansi.Truncate(s, (remaining+1)/2, "")
	tail := rightCells(s, remaining/2)

	if head == "" {
		return ellipsis
	}
	if tail == "" {
		return head
	}
	return head + ellipsis + tail
}

// rightCells returns the trailing portion of s that fits within maxCells.
func rightCells(s string, maxCells int) string {
	if maxCells <= 0 {
		return ""
	}
	runes := []rune(s)
	start := len(runes)
	width := 0
	for i := len(runes) - 1; i >= 0; i-- {
		rw := ansi.StringWidth(string(runes[i]))
		if width+rw > maxCells {
			break
		}
		width += rw
		start = i
	}
	return string(runes[start:])
}

// splitNameSuffix separates the extension from the stem. Dotfiles such as
// .gitignore are treated as stem with no extension, and a directory's trailing
// slash is preserved.
func splitNameSuffix(name string) (suffix, stem string) {
	trimmed := strings.TrimSuffix(name, "/")
	dir := name != trimmed

	dot := strings.LastIndex(trimmed, ".")
	if dot <= 0 {
		if dir {
			return "/", trimmed
		}
		return "", name
	}

	ext := trimmed[dot:]
	stem = trimmed[:dot]
	if dir {
		return ext + "/", stem
	}
	return ext, stem
}
