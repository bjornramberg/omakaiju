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

// Once a filter is applied, the arrows must still move the cursor. They used to
// be swallowed by the prompt, leaving the cursor pinned to the first result.
func TestArrowsNavigateWhileFilterPromptIsOpen(t *testing.T) {
	m, _ := fixture(t)

	// Open the filter prompt.
	updated, _ := m.Update(key('/'))
	m = updated.(Model)
	if m.mode != inputFilter {
		t.Fatalf("mode = %v, want the filter prompt open", m.mode)
	}

	// Type a filter that keeps several entries visible.
	for _, r := range "a" {
		updated, _ = m.Update(key(r))
		m = updated.(Model)
	}

	before := m.rawCursor(m.activePane)

	updated, _ = m.Update(tea.KeyPressMsg{Code: tea.KeyDown})
	m = updated.(Model)

	if m.mode != inputFilter {
		t.Error("the arrow must not close the filter prompt")
	}
	if m.rawCursor(m.activePane) <= before {
		t.Errorf("cursor = %d, want it to move past %d", m.rawCursor(m.activePane), before)
	}
	if m.leftFilter != "a" {
		t.Errorf("leftFilter = %q, want the filter preserved while navigating", m.leftFilter)
	}
}

// Typing re-anchors on the first match, which is the whole point of filtering.
func TestTypingResetsCursorToTop(t *testing.T) {
	m, _ := fixture(t)

	updated, _ := m.Update(key('/'))
	m = updated.(Model)

	updated, _ = m.Update(tea.KeyPressMsg{Code: tea.KeyDown})
	m = updated.(Model)
	if m.rawCursor(m.activePane) == 0 {
		t.Skip("nothing to move down to")
	}

	updated, _ = m.Update(key('b'))
	m = updated.(Model)

	if m.rawCursor(m.activePane) != 0 {
		t.Errorf("cursor = %d, want typing to re-anchor at the first match", m.rawCursor(m.activePane))
	}
}
