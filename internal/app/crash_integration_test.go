package app

import (
	"strings"
	"testing"
)

// The key-list overlay used to panic on a multi-byte query: emphasizeText
// sliced the text with the query's byte length, and the Kelvin sign
// lowercases to a shorter string. The panic ended the session.
//
// The bug is fixed, so the query must now render cleanly. The recovery added
// alongside it means that even if the bug returns, the app survives — this
// test therefore asserts both: no crash, and a rendered overlay.
func TestKeyListHandlesMultiByteQuery(t *testing.T) {
	m, _ := fixture(t)
	m.width, m.height = 120, 40

	// Open the key-list overlay.
	updated, _ := m.Update(shiftKey('k'))
	m = updated.(Model)
	if !m.keyListActive {
		t.Fatal("the key list did not open")
	}

	// Type the Kelvin sign, which used to panic while rendering.
	updated, _ = m.Update(key('K'))
	m = updated.(Model)

	view := m.View().Content
	if view == "" {
		t.Fatal("View returned nothing")
	}
	if !strings.Contains(view, "key list") {
		t.Errorf("the overlay did not render:\n%s", view)
	}
	if m.Crashed() {
		t.Errorf("the query crashed the app: %v", m.CrashReports()[0].Err)
	}
}
