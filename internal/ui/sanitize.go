package ui

import (
	"strings"
	"unicode/utf8"
)

// replacementRune is shown in place of a character that must not reach the
// terminal.
const replacementRune = "\uFFFD"

// SanitizeName replaces characters the terminal would interpret rather than
// display in a name or path. Escape sequences are the hazard: a torrent file
// opens with readable bencoded text and then carries raw SHA1 blocks, so a
// preview that showed it as text would hand those sequences to the terminal,
// which repaints, clears, or otherwise corrupts every pane on screen.
//
// A tab is a cursor instruction to the renderer, not a glyph: it advances the
// cursor to the next tab stop, and the width arithmetic counts it as zero
// cells. A filename is data, so a tab becomes one space here. Everything else
// below space, DEL, the C1 controls, and bytes that are not valid UTF-8 all
// become U+FFFD.
//
// This is the strict path for data that is displayed as a name. File content
// uses SanitizeContentLines, which keeps tabs because a source file's tab is
// its own text.
func SanitizeName(s string) string {
	return sanitize(s, false)
}

// SanitizeContentLines sanitizes each line of raw file content, returning the
// input untouched when every line is already safe, which is the usual case for
// ordinary text files.
//
// Unlike SanitizeName, a tab is preserved: in a Makefile, for instance, the tab
// is significant indentation and part of the file's own text. The other
// control characters get the same treatment as in a name.
//
// It applies to raw file content only. Lines that already carry ANSI styling
// from the theme or the highlighter must not be passed through, because the
// escape introducer would be replaced along with the file's own control bytes.
func SanitizeContentLines(lines []string) []string {
	first := -1
	for i, l := range lines {
		if sanitize(l, true) != l {
			first = i
			break
		}
	}
	if first < 0 {
		return lines
	}

	out := append([]string(nil), lines...)
	for i := first; i < len(out); i++ {
		out[i] = sanitize(out[i], true)
	}
	return out
}

// sanitize replaces the characters the terminal would act on. keepTabs selects
// the content contract: true leaves a tab as the file wrote it, while false
// substitutes a single space for the name contract.
func sanitize(s string, keepTabs bool) string {
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
		if r == '\t' {
			if keepTabs {
				b.WriteRune(r)
			} else {
				b.WriteByte(' ')
			}
			continue
		}
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
// harmless. A tab is unsafe too, but sanitize handles it first because the two
// paths treat it differently.
func unsafeRune(r rune) bool {
	switch {
	case r == utf8.RuneError:
		return true
	case r < 0x20:
		return true
	case r >= 0x7f && r <= 0x9f:
		return true
	}
	return false
}
