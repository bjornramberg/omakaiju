package app

import (
	"testing"
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

// space returns a KeyPressMsg for the Space key, shaped the way a terminal
// actually delivers it: the space in Text and KeySpace in Code. Bubble Tea
// names the keystroke "space", so the Text is what carries the character.
func space() tea.KeyMsg {
	return tea.KeyPressMsg(tea.Key{Code: tea.KeySpace, Text: " "})
}

// updateModel feeds one message and returns the model it produced.
func updateModel(t *testing.T, m Model, msg tea.Msg) Model {
	t.Helper()
	got, _ := updateModelCmd(t, m, msg)
	return got
}

// updateModelCmd is updateModel for the tests that care what the update asked
// the runtime to do, such as open a file or load a directory.
func updateModelCmd(t *testing.T, m Model, msg tea.Msg) (Model, tea.Cmd) {
	t.Helper()
	updated, cmd := m.Update(msg)
	got, ok := updated.(Model)
	if !ok {
		t.Fatalf("Update returned %T, want Model", updated)
	}
	return got, cmd
}
