package ui

import (
	"strings"
	"testing"

	"github.com/bjornramberg/omakaiju/internal/config"
)

// A multi-byte character is one terminal character. Highlighting it must not
// walk its bytes, which would write each byte as a code point and mangle the
// name into mojibake.
func TestFuzzyHighlightKeepsNonASCIICharacters(t *testing.T) {
	theme := config.DefaultTheme()

	cases := []struct {
		name  string
		query string
	}{
		{name: "日本語.txt", query: "日"},
		{name: "café.txt", query: "é"},
	}

	for _, tc := range cases {
		t.Run(tc.name, func(t *testing.T) {
			f := NewFuzzyFinder(100, 30, theme)
			f.Input = tc.query

			raw := f.highlight(tc.name)
			if plain := stripANSI(raw); plain != tc.name {
				t.Errorf("highlight(%q) rendered %q, want the name unchanged", tc.name, plain)
			}
			// The bytes of a multi-byte rune must never be reinterpreted as
			// Latin-1 code points.
			for _, mojibake := range []string{"æ", "Ã", "©", "\u0097"} {
				if strings.Contains(raw, mojibake) {
					t.Errorf("mojibake %q in highlighted output %q", mojibake, raw)
				}
			}
		})
	}
}

// An ASCII query must still be matched case-insensitively and styled, as it
// was before the rune rewrite.
func TestFuzzyHighlightStillMatchesASCIICaseInsensitively(t *testing.T) {
	theme := config.DefaultTheme()
	f := NewFuzzyFinder(100, 30, theme)
	f.Input = "MAIN"

	raw := f.highlight("main.go")
	if plain := stripANSI(raw); plain != "main.go" {
		t.Fatalf("rendered %q, want main.go", plain)
	}
	if !strings.Contains(raw, "\x1b[") {
		t.Error("the matched ASCII characters were not styled")
	}

	// A query that does not match must leave the name untouched.
	if got := f.highlight("other.go"); got != "other.go" {
		t.Errorf("non-matching name was altered: %q", got)
	}
}

func TestSubseqMatchLowerIsRuneAware(t *testing.T) {
	if !subseqMatchLower("日", "日本語.txt") {
		t.Error("a CJK query should match its whole character")
	}
	if !subseqMatchLower("é", "café.txt") {
		t.Error("an accented query should match its whole character")
	}
	if !subseqMatchLower("MAIN", "main.go") {
		t.Error("matching should stay case-insensitive")
	}
	if !subseqMatchLower("日本", "日x本") {
		t.Error("an ordered CJK subsequence should match")
	}
}
