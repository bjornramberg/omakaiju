package ui

import "testing"

// The status line gives up its least important piece first, so a narrow panel
// keeps the hints the user is most likely to need.
func TestStatusLineDropsTheLeastImportantFirst(t *testing.T) {
	pieces := []statusPiece{
		{Text: "1/3"},
		{Text: "first", Drop: 1},
		{Text: "second", Drop: 2},
		{Text: "always"},
	}

	if got, want := statusLine(pieces, 100), "1/3 | first | second | always"; got != want {
		t.Errorf("statusLine = %q, want %q", got, want)
	}

	// Too narrow for the first hint: it goes first.
	if got, want := statusLine(pieces, 21), "1/3 | second | always"; got != want {
		t.Errorf("statusLine = %q, want %q", got, want)
	}

	// Too narrow for anything droppable: only the keepers are left.
	if got, want := statusLine(pieces, 12), "1/3 | always"; got != want {
		t.Errorf("statusLine = %q, want %q", got, want)
	}
}

// A line that fits nothing is cut rather than allowed to wrap, because a
// wrapped status line pushes the panel's contents around.
func TestStatusLineCutsRatherThanWraps(t *testing.T) {
	pieces := []statusPiece{
		{Text: "1/3"},
		{Text: "always"},
	}

	got := statusLine(pieces, 8)
	if DisplayWidth(got) > 8 {
		t.Errorf("statusLine = %q is %d cells, want at most 8", got, DisplayWidth(got))
	}
}

// The pieces keep the order they were given in, whatever is dropped.
func TestStatusLineKeepsTheOrderOfThePieces(t *testing.T) {
	pieces := []statusPiece{
		{Text: "1/3"},
		{Text: "middle", Drop: 1},
		{Text: "last"},
	}

	if got, want := statusLine(pieces, 10), "1/3 | last"; got != want {
		t.Errorf("statusLine = %q, want %q", got, want)
	}
}
