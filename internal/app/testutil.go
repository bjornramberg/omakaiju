package app

import (
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

// shiftKey returns a KeyPressMsg for a Shift+key combination.
func shiftKey(r rune) tea.KeyMsg {
	return tea.KeyPressMsg(tea.Key{Code: r, Mod: tea.ModShift})
}

// ctrlKey returns a KeyPressMsg for a Ctrl+key combination.
func ctrlKey(r rune) tea.KeyMsg {
	return tea.KeyPressMsg(tea.Key{Code: r, Mod: tea.ModCtrl})
}

// space returns the Space key.
func space() tea.KeyMsg {
	return tea.KeyPressMsg(tea.Key{Code: ' '})
}
