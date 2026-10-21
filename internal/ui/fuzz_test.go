package ui

import (
	"strings"
	"testing"

	"charm.land/lipgloss/v2"
)

// The key-list overlay's matcher takes a query the user types and a
// description from the keybind table. A case fold can change a string's byte
// length without changing its rune count, which is how the old byte-based
// matcher sliced past the end of a string and ended the session.
//
// The fuzzer is what keeps that fixed: it tries case folds, invalid UTF-8
// and control bytes in every combination.
func FuzzEmphasizeText(f *testing.F) {
	seeds := []string{
		"",
		"a",
		"k",
		"K", // KELVIN SIGN: three bytes, folds to one
		"İ", // LATIN CAPITAL LETTER I WITH DOT ABOVE
		"ẞ", // LATIN CAPITAL LETTER SHARP S
		"Ⱥ", // LATIN CAPITAL LETTER A WITH STROKE
		"mark / unmark",
		"key list overlay",
		"é",
		"🎉",
		"\x00",
		"\x1b[31m",
		"\xff\xfe",
	}
	for _, s := range seeds {
		f.Add(s, "mark / unmark")
		f.Add(s, "k")
	}

	base := lipgloss.NewStyle()
	accent := lipgloss.NewStyle().Bold(true)

	f.Fuzz(func(t *testing.T, text, query string) {
		// The matcher must never panic, whatever it is handed.
		out := emphasizeText(text, query, base, accent)
		if out == "" && text != "" {
			t.Errorf("the text was lost: text=%q query=%q", text, query)
		}
	})
}

// FuzzSanitizeDisplay covers the sanitiser that every filename passes
// through before it reaches the screen. A filename may legally contain a
// newline or an escape byte on Linux, and the sanitiser exists to stop those
// from acting on the terminal.
func FuzzSanitizeDisplay(f *testing.F) {
	seeds := []string{
		"",
		"normal.txt",
		"evil\x1b[2Jname",
		"two\nlines",
		"tab\there",
		"null\x00byte",
		"del\x7f",
		"日本語.txt",
		"emoji 🎉.txt",
		"\xff\xfe",
	}
	for _, s := range seeds {
		f.Add(s)
	}

	f.Fuzz(func(t *testing.T, s string) {
		out := SanitizeDisplay(s)
		// The sanitiser must not leave an escape byte in the output.
		if strings.ContainsRune(out, '\x1b') {
			t.Errorf("an escape byte survived: %q -> %q", s, out)
		}
	})
}

// The fuzzer found these two, and the corpus entries under testdata/fuzz
// replay them on every run. The explicit cases are here so the reason is
// written down rather than encoded in a hash.
func TestTruncateRegressions(t *testing.T) {
	// Measuring a name and measuring its parts disagreed on invalid UTF-8,
	// so a name cut to a budget came out wider than the budget.
	if got := DisplayWidth(TruncateName("0\xeb0\xe70.", 5)); got > 5 {
		t.Errorf("TruncateName is %d cells wide, want at most 5", got)
	}
	// The first component of a path and its separator may not fit on their
	// own, and the path was allowed to overflow the budget.
	if got := DisplayWidth(TrimPathLeft("000000//", 6)); got > 6 {
		t.Errorf("TrimPathLeft is %d cells wide, want at most 6", got)
	}
}

// FuzzTruncate covers the clipping helpers. They bound text to a number of
// terminal cells, and the cell count of a styled string is not its byte
// count, so the arithmetic is easy to get wrong.
func FuzzTruncate(f *testing.F) {
	seeds := []string{
		"",
		"a",
		"short",
		strings.Repeat("x", 300),
		"日本語",
		"🎉🎉🎉",
		"\x1b[31mred\x1b[0m",
		"a\x1b[31mb\x1b[0mc",
	}
	for _, s := range seeds {
		f.Add(s, 0)
		f.Add(s, 1)
		f.Add(s, 5)
		f.Add(s, 100)
	}

	f.Fuzz(func(t *testing.T, s string, cells int) {
		// The result must never be wider than the budget.
		if got := DisplayWidth(TruncateEnd(s, cells)); cells > 0 && got > cells {
			t.Errorf("TruncateEnd(%q, %d) is %d cells wide", s, cells, got)
		}
		if got := DisplayWidth(TruncateName(s, cells)); cells > 0 && got > cells {
			t.Errorf("TruncateName(%q, %d) is %d cells wide", s, cells, got)
		}
	})
}

// FuzzTrimPathLeft covers the path shortener. It keeps the tail of a path,
// which is the part that says where you are.
func FuzzTrimPathLeft(f *testing.F) {
	seeds := []string{
		"",
		"/",
		"/home/user",
		"/home/user/projects/omakaiju/internal/ui",
		"relative/path",
		"日本語/パス",
		"/a/b/c/d/e/f/g/h/i/j/k/l/m/n/o/p",
	}
	for _, s := range seeds {
		f.Add(s, 0)
		f.Add(s, 10)
		f.Add(s, 100)
	}

	f.Fuzz(func(t *testing.T, path string, cells int) {
		out := TrimPathLeft(path, cells)
		if cells > 0 && DisplayWidth(out) > cells {
			t.Errorf("TrimPathLeft(%q, %d) is %d cells wide", path, cells, DisplayWidth(out))
		}
	})
}
