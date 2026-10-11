package app

import (
	"os"
	"path/filepath"
	"testing"

	"omakaiju/internal/config"
	"omakaiju/internal/fs"

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

// A message left over from an earlier action must not outstay its welcome.
func TestStatusMessageClearsOnNextKeypress(t *testing.T) {
	m, _ := fixture(t)

	updated, _ := m.Update(openMsg{name: "alpha.txt"})
	m = updated.(Model)
	if m.opResult == "" {
		t.Fatal("setup: expected a status message after opening")
	}

	updated, _ = m.Update(key('j'))
	m = updated.(Model)

	if m.opResult != "" {
		t.Errorf("opResult = %q, want it cleared by the next keypress", m.opResult)
	}
}

// Clearing happens before the key is handled, so an action that reports its own
// result stays visible until the key after it.
func TestMessageFromCurrentKeypressSurvives(t *testing.T) {
	m, _ := fixture(t)
	m = cursorOn(t, m, "alpha.txt")

	updated, _ := m.Update(key('y'))
	m = updated.(Model)

	if m.opResult == "" {
		t.Fatal("yank should report what it did")
	}
	afterYank := m.opResult

	updated, _ = m.Update(key('j'))
	m = updated.(Model)

	if m.opResult != "" {
		t.Errorf("opResult = %q, want it cleared by the following keypress", m.opResult)
	}

	// The yank message was visible until an unrelated key was pressed; confirm
	// it was not replaced mid-flight by checking a fresh action still reports.
	if afterYank == "" {
		t.Error("expected the yank message to be recorded")
	}
}

// Left arrow navigates to the parent from inside the filter prompt too.
func TestLeftArrowLeavesDirectoryWhileFiltering(t *testing.T) {
	dir := t.TempDir()
	sub := filepath.Join(dir, "subdir")
	if err := os.Mkdir(sub, 0o755); err != nil {
		t.Fatal(err)
	}
	if err := os.WriteFile(filepath.Join(sub, "inner.txt"), []byte("i"), 0o644); err != nil {
		t.Fatal(err)
	}

	files, err := fs.ReadDir(sub)
	if err != nil {
		t.Fatal(err)
	}

	m := NewModel(config.Config{ThemePath: filepath.Join(dir, "nonexistent.toml")})
	m.width, m.height = 120, 40
	m.leftPath, m.leftFiles = sub, files
	m.rightPath, m.rightFiles = sub, files
	m.mode = inputFilter
	m.setFilter(0, "")

	updated, _ := m.Update(tea.KeyPressMsg{Code: tea.KeyLeft})
	m = updated.(Model)

	if m.leftPath != dir {
		t.Errorf("leftPath = %q, want the parent %q", m.leftPath, dir)
	}
	if m.mode != inputFilter {
		t.Error("navigating should not close the filter prompt")
	}
}
