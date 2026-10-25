package ui

import (
	"strings"
	"unicode/utf8"

	"github.com/charmbracelet/x/ansi"
)

// ellipsis is the single-cell truncation marker.
const ellipsis = "…"

// validText normalises a string so that measuring it and slicing it agree.
//
// Measuring a string's width and slicing it are two different operations,
// and on invalid UTF-8 they disagree: the width calculation sees the raw
// bytes, while a rune conversion sees replacement characters. A name cut to
// a budget could then come out wider than the budget, and the surplus bled
// into the neighbouring column.
//
// The replacement character is what the terminal would have drawn for the
// invalid bytes anyway, so nothing is lost.
func validText(s string) string {
	return strings.ToValidUTF8(s, string(utf8.RuneError))
}

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
	name = validText(name)
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

// TrimPathLeft shortens a path by dropping leading components, since the tail
// of a path is the part that identifies where you are.
//
// It always keeps the final component and marks the dropped components with an
// ellipsis. Without the marker a shortened path looks like a real one, and
// without the final component a deep path could collapse to "/" and claim the
// user is at the filesystem root.
func TrimPathLeft(path string, maxCells int) string {
	if maxCells <= 0 {
		return ""
	}
	path = validText(path)
	if ansi.StringWidth(path) <= maxCells {
		return path
	}
	if maxCells == 1 {
		return ellipsis
	}

	// A path that is only separators is the root; there is no component to
	// preserve and no elision to make.
	trimmed := strings.TrimRight(path, "/")
	if trimmed == "" {
		return TruncateEnd(path, maxCells)
	}

	absolute := strings.HasPrefix(path, "/")
	parts := strings.Split(trimmed, "/")
	// An absolute path's leading separator is not a component.
	if absolute && len(parts) > 0 && parts[0] == "" {
		parts = parts[1:]
	}
	if len(parts) == 0 {
		return TruncateEnd(path, maxCells)
	}

	// The marker says leading components were dropped. An absolute path keeps
	// its leading slash so it still reads as absolute.
	marker := ellipsis + "/"
	if absolute {
		marker = "/" + ellipsis + "/"
	}

	last := parts[len(parts)-1]
	avail := maxCells - ansi.StringWidth(marker)
	if avail < 1 {
		// Too narrow for the marker and a component. Keep as much of the
		// final component as fits, led by the root when the path is
		// absolute; the ellipsis still marks the shortening.
		if absolute && maxCells >= 2 {
			return "/" + TruncateName(last, maxCells-1)
		}
		return TruncateName(last, maxCells)
	}

	// Keep as many trailing components as fit, so the result still locates
	// the user rather than showing only the final name.
	tail := last
	for i := len(parts) - 2; i >= 0; i-- {
		candidate := parts[i] + "/" + tail
		if ansi.StringWidth(candidate) > avail {
			break
		}
		tail = candidate
	}
	if ansi.StringWidth(tail) > avail {
		tail = TruncateName(tail, avail)
	}
	return marker + tail
}

// TruncateEnd clips s to maxCells cells, marking the cut with a trailing
// ellipsis. Used for preview body lines, where the end of the line is the least
// informative part and clipping must not re-wrap.
func TruncateEnd(s string, maxCells int) string {
	if maxCells <= 0 {
		return ""
	}
	s = validText(s)
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
