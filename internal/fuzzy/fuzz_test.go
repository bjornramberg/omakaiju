package fuzzy

import (
	"strings"
	"testing"
)

// The matcher's query comes from the user and its haystack comes from the
// filesystem, so both are attacker-controlled in the sense that matters
// here: they can be anything at all.
//
// The fuzzer is what keeps the matcher from panicking on a case fold that
// changes a string's byte length.
func FuzzMatch(f *testing.F) {
	seeds := []string{
		"",
		"a",
		"k",
		"K", // KELVIN SIGN: three bytes, folds to one
		"İ", // LATIN CAPITAL LETTER I WITH DOT ABOVE
		"ẞ", // LATIN CAPITAL LETTER SHARP S
		"Ⱥ", // LATIN CAPITAL LETTER A WITH STROKE
		"context.md",
		"/usr/lib/main.go",
		"日本語",
		"🎉",
		"\x00",
		"\x1b[31m",
		"\xff\xfe",
	}
	for _, s := range seeds {
		f.Add(s, "main.go")
		f.Add(s, "")
		f.Add(s, "/usr/lib/main.go")
	}

	f.Fuzz(func(t *testing.T, pattern, text string) {
		// Matching must never panic, whatever it is handed.
		result, ok := Match(pattern, text)
		if !ok {
			return
		}
		// A match reports where it starts. The position has to be inside
		// the text, or the caller would slice out of range.
		if result.Index < 0 || result.Index > len(text) {
			t.Errorf("Match(%q, %q).Index = %d, which is outside the text", pattern, text, result.Index)
		}
		for _, p := range result.Positions {
			if p < 0 || p >= len(text) {
				t.Errorf("Match(%q, %q) reported position %d, which is outside the text", pattern, text, p)
			}
		}
	})
}

// The fuzzer found this: the matcher reported a position past the end of
// the text, because the position was an offset into the lowercased string
// and the case fold had changed its length.
//
// The corpus entry under testdata/fuzz replays it on every run. The
// explicit case is here so the reason is written down rather than encoded
// in a hash.
func TestMatchPositionsAddressTheOriginalText(t *testing.T) {
	result, ok := Match("0", "\xd00")
	if !ok {
		t.Fatal("the pattern should match")
	}
	if result.Index < 0 || result.Index > len("\xd00") {
		t.Errorf("Index = %d, which is outside the text", result.Index)
	}
	for _, p := range result.Positions {
		if p < 0 || p >= len("\xd00") {
			t.Errorf("position %d is outside the text", p)
		}
	}
}

// FuzzFilter covers the ranked matcher over a list of paths, which is what
// the finder runs on every keystroke.
func FuzzFilter(f *testing.F) {
	seeds := []string{
		"",
		"a",
		"K",
		"context.md",
		"/usr/lib/main.go",
		"日本語",
		"\x00",
		"\xff\xfe",
	}
	for _, s := range seeds {
		f.Add(s, []byte(s))
		f.Add(s, []byte("other.go"))
		f.Add(s, []byte{})
	}

	f.Fuzz(func(t *testing.T, pattern string, raw []byte) {
		items := strings.Split(string(raw), "\x00")

		// Filtering must never panic, whatever it is handed.
		results := Filter(pattern, items)
		if len(results) > len(items) {
			t.Errorf("Filter returned %d results for %d items", len(results), len(items))
		}
		// Every result must be one of the inputs.
		for _, r := range results {
			found := false
			for _, item := range items {
				if item == r {
					found = true
					break
				}
			}
			if !found {
				t.Errorf("Filter returned %q, which was not an input", r)
			}
		}
	})
}
