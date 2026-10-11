package app

import (
	"path/filepath"
	"testing"

	tea "charm.land/bubbletea/v2"
)

// Right arrow used to share a branch with Enter, so moving onto a file and
// pressing right handed it to the desktop handler.
func TestRightArrowOnFileDoesNotOpen(t *testing.T) {
	m, _ := fixture(t)
	m = cursorOn(t, m, "alpha.txt")
	pathBefore := m.leftPath
	cursorBefore := m.rawCursor(m.activePane)

	updated, cmd := m.Update(tea.KeyPressMsg{Code: tea.KeyRight})
	m = updated.(Model)

	if cmd != nil {
		t.Error("right arrow on a file must not dispatch a command")
	}
	if m.leftPath != pathBefore {
		t.Errorf("leftPath = %q, want %q unchanged", m.leftPath, pathBefore)
	}
	if m.rawCursor(m.activePane) != cursorBefore {
		t.Error("right arrow on a file must not move the cursor")
	}
	if m.loadErr != nil {
		t.Errorf("unexpected error: %v", m.loadErr)
	}
}

// Right arrow still enters directories, which is what makes it a navigation key.
func TestRightArrowEntersDirectory(t *testing.T) {
	m, dir := fixture(t)
	m = cursorOn(t, m, "subdir")

	updated, _ := m.Update(tea.KeyPressMsg{Code: tea.KeyRight})
	m = updated.(Model)

	if m.leftPath != filepath.Join(dir, "subdir") {
		t.Errorf("leftPath = %q, want the subdirectory", m.leftPath)
	}
}

// Enter is the only key that opens a file.
func TestEnterStillOpensFile(t *testing.T) {
	m, _ := fixture(t)
	m = cursorOn(t, m, "alpha.txt")

	updated, cmd := m.Update(tea.KeyPressMsg{Code: tea.KeyEnter})
	if cmd == nil {
		t.Fatal("enter on a file should dispatch an open command")
	}
	m = updated.(Model)

	if m.leftPath == "" {
		t.Error("enter should not clear the path")
	}
}
