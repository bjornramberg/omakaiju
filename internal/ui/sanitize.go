package ui

import (
	"strings"
	"unicode/utf8"
)

// replacementRune is shown in place of a character that must not reach the
// terminal.
const replacementRune = "\uFFFD"

// SanitizeDisplay replaces characters the terminal would interpret rather than
// display. Escape sequences are the hazard: a torrent file opens with readable
// bencoded text and then carries raw SHA1 blocks, so a preview that showed it
// as text would hand those sequences to the terminal, which repaints, clears,
// or otherwise corrupts every pane on screen.
//
// Tab is preserved because source files use it for indentation. Everything else
// below space, DEL, the C1 controls, and bytes that are not valid UTF-8 all
// become U+FFFD.
func SanitizeDisplay(s string) string {
	// Printable ASCII cannot be misinterpreted, and it is the overwhelmingly
	// common case, so it is returned untouched without allocating.
	plain := true
	for i := 0; i < len(s); i++ {
		if s[i] < 0x20 || s[i] >= 0x7f {
			plain = false
			break
		}
	}
	if plain {
		return s
	}

	var b strings.Builder
	b.Grow(len(s))
	for _, r := range s {
		if unsafeRune(r) {
			b.WriteString(replacementRune)
			continue
		}
		b.WriteRune(r)
	}
	return b.String()
}

// unsafeRune reports whether r would be acted on by the terminal. utf8.RuneError
// covers invalid encoding; a genuine U+FFFD is replaced with itself, which is
// harmless.
func unsafeRune(r rune) bool {
	switch {
	case r == utf8.RuneError:
		return true
	case r == '\t':
		return false
	case r < 0x20:
		return true
	case r >= 0x7f && r <= 0x9f:
		return true
	}
	return false
}

// SanitizeLines sanitizes each line, returning the input untouched when every
// line is already safe, which is the usual case for ordinary text files.
//
// It applies to raw file content only. Lines that already carry ANSI styling
// from the theme or the highlighter must not be passed through, because the
// escape introducer would be replaced along with the file's own control bytes.
func SanitizeLines(lines []string) []string {
	first := -1
	for i, l := range lines {
		if SanitizeDisplay(l) != l {
			first = i
			break
		}
	}
	if first < 0 {
		return lines
	}

	out := append([]string(nil), lines...)
	for i := first; i < len(out); i++ {
		out[i] = SanitizeDisplay(out[i])
	}
	return out
}
