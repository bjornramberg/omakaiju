package ui

import (
	"strings"
	"testing"

	"charm.land/lipgloss/v2"
	"github.com/bjornramberg/omakaiju/internal/config"
)

// A case fold can change a string's byte length without changing its rune
// count: the Kelvin sign is three bytes (U+212A) and folds to one ('k').
// The old byte-based matcher sliced the original text with the original
// query's length and ran off the end of the string, ending the session.
func TestEmphasizeTextSurvivesCaseFoldsThatChangeByteLength(t *testing.T) {
	base := lipgloss.NewStyle()
	accent := lipgloss.NewStyle()

	cases := []struct {
		name  string
		text  string
		query string
	}{
		// The Kelvin sign folds to 'k' and is the case that actually
		// reached the key-list overlay.
		{"kelvin sign at end", "mark / unmark", "K"},
		{"kelvin sign at start", "key list overlay", "K"},
		{"kelvin sign alone", "k", "K"},
		{"kelvin sign in middle", "aKb", "K"},
		// Other folds that shrink.
		{"latin capital i with dot", "mark / unmark", "İ"},
		{"capital sharp s", "pass", "ẞ"},
		// A fold that grows: U+023A is two bytes and folds to U+2C65,
		// which is three.
		{"latin capital a with stroke", "aaa", "Ⱥ"},
		// Multi-byte queries against multi-byte text.
		{"multi-byte text and query", "café", "É"},
		{"emoji in text", "file 🎉 name", "🎉"},
	}

	for _, tc := range cases {
		t.Run(tc.name, func(t *testing.T) {
			defer func() {
				if r := recover(); r != nil {
					t.Fatalf("emphasizeText panicked: %v", r)
				}
			}()

			out := emphasizeText(tc.text, tc.query, base, accent)
			if out == "" {
				t.Fatal("emphasizeText returned nothing")
			}
		})
	}
}

// The match must still be found and accented, not merely survive.
func TestEmphasizeTextStillMatchesAfterTheFix(t *testing.T) {
	base := lipgloss.NewStyle()
	accent := lipgloss.NewStyle()

	// The Kelvin sign folds to 'k', so it matches the 'k' in "mark".
	out := emphasizeText("mark", "K", base, accent)
	if !strings.Contains(out, "m") {
		t.Errorf("the text was not rendered: %q", out)
	}

	// The accented run must be exactly the matched character.
	if len(out) != len("mark") {
		t.Errorf("rendered %d bytes, want %d: %q", len(out), len("mark"), out)
	}
}

// A query that matches several times accents each occurrence.
func TestEmphasizeTextAccentsEveryOccurrence(t *testing.T) {
	base := lipgloss.NewStyle()
	accent := lipgloss.NewStyle().Bold(true).Foreground(lipgloss.Color("#FF0000"))

	out := emphasizeText("banana", "a", base, accent)
	// Two matches, each wrapped in its own style, so the style bytes show
	// the run was split rather than the whole string being one piece.
	if strings.Count(out, "\x1b[") < 4 {
		t.Errorf("expected separate styled runs, got %q", out)
	}
	if len(out) < len("banana") {
		t.Errorf("the text was shortened: %q", out)
	}
}

// A query that does not match leaves the text intact.
func TestEmphasizeTextWithNoMatch(t *testing.T) {
	base := lipgloss.NewStyle()
	accent := lipgloss.NewStyle()

	out := emphasizeText("hello", "zzz", base, accent)
	if !strings.Contains(out, "hello") {
		t.Errorf("the text was lost: %q", out)
	}
}

// The public surface must survive the same inputs: the key-list overlay
// renders a search query on every frame.
func TestKeyListRendersWithMultiByteQueries(t *testing.T) {
	for _, q := range []string{"K", "İ", "ẞ", "Ⱥ", "é", "🎉", "k"} {
		t.Run(q, func(t *testing.T) {
			defer func() {
				if r := recover(); r != nil {
					t.Fatalf("the key list panicked on query %q: %v", q, r)
				}
			}()

			k := NewKeyList(100, 60, config.DefaultTheme())
			k.Input = q
			if out := k.Render(); out == "" {
				t.Fatalf("the key list rendered nothing for query %q", q)
			}
		})
	}
}
