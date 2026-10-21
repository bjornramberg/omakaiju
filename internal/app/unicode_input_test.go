package app

import (
	"strings"
	"testing"
	"unicode/utf8"

	tea "charm.land/bubbletea/v2"
)

// A byte-length test rejects every non-ASCII character, and filenames are
// full of them. The filter has to be able to type "café".
func TestFilterAcceptsNonASCIICharacters(t *testing.T) {
	m, _ := fixture(t)

	updated, _ := m.Update(shiftKey('f'))
	m = updated.(Model)
	if m.mode != inputFilter {
		t.Fatalf("mode = %v, want the filter prompt open", m.mode)
	}

	for _, r := range "café" {
		updated, _ = m.Update(key(r))
		m = updated.(Model)
	}

	if m.inputBuf != "café" {
		t.Errorf("inputBuf = %q, want %q", m.inputBuf, "café")
	}
	if m.leftFilter != "café" {
		t.Errorf("leftFilter = %q, want %q", m.leftFilter, "café")
	}
}

// Backspace has to remove a whole character. Cutting the last byte of a
// multi-byte character leaves invalid UTF-8 behind, which then flows into
// the matcher and the width calculations.
func TestBackspaceRemovesAWholeCharacter(t *testing.T) {
	m, _ := fixture(t)

	updated, _ := m.Update(shiftKey('f'))
	m = updated.(Model)

	for _, r := range "café日" {
		updated, _ = m.Update(key(r))
		m = updated.(Model)
	}

	updated, _ = m.Update(tea.KeyPressMsg(tea.Key{Code: tea.KeyBackspace}))
	m = updated.(Model)

	if m.inputBuf != "café" {
		t.Errorf("inputBuf = %q, want %q", m.inputBuf, "café")
	}
	if !utf8.ValidString(m.inputBuf) {
		t.Error("backspace left invalid UTF-8 behind")
	}
}

// The fuzzy finder takes the same input and has to behave the same way.
func TestFuzzyInputAcceptsAndErasesNonASCII(t *testing.T) {
	m, _ := fixture(t)

	updated, _ := m.Update(shiftKey('p'))
	m = updated.(Model)
	if !m.fuzzyActive {
		t.Fatal("the fuzzy finder did not open")
	}

	for _, r := range "naïve" {
		updated, _ = m.Update(key(r))
		m = updated.(Model)
	}
	if m.fuzzyInput != "naïve" {
		t.Errorf("fuzzyInput = %q, want %q", m.fuzzyInput, "naïve")
	}

	updated, _ = m.Update(tea.KeyPressMsg(tea.Key{Code: tea.KeyBackspace}))
	m = updated.(Model)

	if m.fuzzyInput != "naïv" {
		t.Errorf("fuzzyInput = %q, want %q", m.fuzzyInput, "naïv")
	}
	if !utf8.ValidString(m.fuzzyInput) {
		t.Error("backspace left invalid UTF-8 behind")
	}
}

// The key-list overlay already took runes; it must keep doing so.
func TestKeyListQueryAcceptsNonASCII(t *testing.T) {
	m, _ := fixture(t)

	updated, _ := m.Update(shiftKey('k'))
	m = updated.(Model)

	updated, _ = m.Update(key('é'))
	m = updated.(Model)

	if m.keyListQuery != "é" {
		t.Errorf("keyListQuery = %q, want %q", m.keyListQuery, "é")
	}
}

// A control character must not end up in a query. Bubble Tea names the
// control keys, so they arrive as words rather than as raw bytes, but the
// printable check is the backstop.
func TestControlCharactersAreNotAccepted(t *testing.T) {
	for _, r := range []rune{0x00, 0x07, 0x1b, 0x7f} {
		if _, ok := printableRune(string(r)); ok {
			t.Errorf("control character %U was accepted", r)
		}
	}
}

// A printable character of any width is accepted.
func TestPrintableCharactersAreAccepted(t *testing.T) {
	for _, r := range []rune{'a', 'Z', '0', ' ', 'é', '日', '👍', '☺'} {
		got, ok := printableRune(string(r))
		if !ok {
			t.Errorf("printable character %U was rejected", r)
			continue
		}
		if got != r {
			t.Errorf("printableRune(%q) = %q, want %q", r, got, r)
		}
	}
}

// More than one rune is not a single character and must be rejected.
func TestMultiRuneStringsAreRejected(t *testing.T) {
	for _, s := range []string{"ab", "", "a b"} {
		if _, ok := printableRune(s); ok {
			t.Errorf("%q was accepted as a single character", s)
		}
	}
}

// A raw escape byte must never reach the query. The overlay renders the
// query, so a byte that a terminal would act on could rewrite the screen.
func TestEscapeByteNeverReachesTheQuery(t *testing.T) {
	m, _ := fixture(t)
	m.width, m.height = 120, 40

	updated, _ := m.Update(shiftKey('k'))
	m = updated.(Model)

	updated, _ = m.Update(key('\x1b'))
	m = updated.(Model)

	if strings.ContainsRune(m.keyListQuery, '\x1b') {
		t.Errorf("keyListQuery = %q carries a raw escape byte", m.keyListQuery)
	}
	if !utf8.ValidString(m.keyListQuery) {
		t.Errorf("keyListQuery = %q is not valid UTF-8", m.keyListQuery)
	}
}
