package app

import (
	"strings"
	"testing"
	"unicode"

	tea "charm.land/bubbletea/v2"

	"github.com/bjornramberg/omakaiju/internal/nav"
)

// termKey builds the key event a terminal actually delivers for a canonical
// key id such as "shift+d".
//
// The shape matters: a terminal sends a shifted letter as the printable
// character, so the event carries the character in Text, the base key in
// Code and the shift in Mod. A key event built any other way is one a
// terminal never produces, and a test built on it proves nothing.
func termKey(id string) tea.KeyMsg {
	mod := tea.KeyMod(0)
	base := id
	if rest, ok := strings.CutPrefix(id, "shift+"); ok {
		mod |= tea.ModShift
		base = rest
	}

	switch base {
	case "esc":
		return tea.KeyPressMsg(tea.Key{Code: tea.KeyEscape})
	case "enter":
		return tea.KeyPressMsg(tea.Key{Code: tea.KeyEnter})
	case "tab":
		return tea.KeyPressMsg(tea.Key{Code: tea.KeyTab})
	case "space":
		return tea.KeyPressMsg(tea.Key{Code: tea.KeySpace, Text: " "})
	case "up":
		return tea.KeyPressMsg(tea.Key{Code: tea.KeyUp})
	case "down":
		return tea.KeyPressMsg(tea.Key{Code: tea.KeyDown})
	case "left":
		return tea.KeyPressMsg(tea.Key{Code: tea.KeyLeft})
	case "right":
		return tea.KeyPressMsg(tea.Key{Code: tea.KeyRight})
	}

	runes := []rune(base)
	if len(runes) != 1 {
		panic("termKey: unsupported key id " + id)
	}
	r := runes[0]
	k := tea.Key{Code: r, Text: string(r), Mod: mod}
	if unicode.IsUpper(r) {
		// A terminal sends the shifted character, and ultraviolet reads it
		// back as the base key plus the shift modifier.
		k.Text = string(r)
		k.Code = unicode.ToLower(r)
		k.ShiftedCode = r
		k.Mod |= tea.ModShift
	}
	return tea.KeyPressMsg(k)
}

// Every binding must be reachable from the key event a terminal sends.
//
// A binding the terminal cannot produce is a binding the user cannot press,
// and the table would be documenting a key that does nothing. This is the
// check that keeps the table and the terminal in step.
func TestEveryBindingIsReachableFromARealKeyEvent(t *testing.T) {
	for _, kb := range nav.Keybinds {
		msg := termKey(kb.Key)
		if got := nav.Lookup(keyID(msg)); got != kb.Action {
			t.Errorf("the key event for %q resolves to action %d, want %d", kb.Key, got, kb.Action)
		}
	}
}

// The overlay opens with shift+k and closes with it too.
func TestShiftKClosesTheKeyList(t *testing.T) {
	m, _ := fixture(t)

	updated, _ := m.Update(shiftKey('k'))
	m = updated.(Model)
	if !m.keyListActive {
		t.Fatal("shift+k should open the key list")
	}

	updated, _ = m.Update(shiftKey('k'))
	m = updated.(Model)
	if m.keyListActive {
		t.Error("shift+k should close the key list")
	}
}

// The file operations are reached by their shifted keys, the way a
// terminal delivers them.
func TestShiftedKeysReachTheirActions(t *testing.T) {
	m, _ := fixture(t)

	updated, _ := m.Update(shiftKey('d'))
	m = updated.(Model)
	if !m.confirmDelete {
		t.Error("shift+d should open the delete prompt")
	}
}
