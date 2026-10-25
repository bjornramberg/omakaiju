package app

import (
	"unicode"

	tea "charm.land/bubbletea/v2"
)

// key returns a KeyPressMsg for a printable character.
func key(r rune) tea.KeyMsg {
	return tea.KeyPressMsg(tea.Key{Code: r})
}

// arrow returns a KeyPressMsg for an arrow key.
func arrow(code rune) tea.KeyMsg {
	return tea.KeyPressMsg(tea.Key{Code: code})
}

// shiftKey returns a KeyPressMsg for a Shift+key combination, shaped the way
// a terminal actually delivers it: the printable character in Text, the base
// key in Code, and the shift in Mod.
func shiftKey(r rune) tea.KeyMsg {
	upper := unicode.ToUpper(r)
	return tea.KeyPressMsg(tea.Key{
		Text:        string(upper),
		Code:        unicode.ToLower(r),
		ShiftedCode: upper,
		Mod:         tea.ModShift,
	})
}

// ctrlKey returns a KeyPressMsg for a Ctrl+key combination.
func ctrlKey(r rune) tea.KeyMsg {
	return tea.KeyPressMsg(tea.Key{Code: r, Mod: tea.ModCtrl})
}

// space returns a KeyPressMsg for the Space key, shaped the way a terminal
// actually delivers it: the space in Text and KeySpace in Code. Bubble Tea
// names the keystroke "space", so the Text is what carries the character.
func space() tea.KeyMsg {
	return tea.KeyPressMsg(tea.Key{Code: tea.KeySpace, Text: " "})
}
