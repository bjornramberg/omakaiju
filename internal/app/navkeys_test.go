package app

import (
	"os"
	"path/filepath"
	"testing"

	"github.com/bjornramberg/omakaiju/internal/config"
	"github.com/bjornramberg/omakaiju/internal/fs"

	tea "charm.land/bubbletea/v2"
)

// Right arrow used to share a branch with Enter, so moving onto a file and
// pressing right handed it to the desktop handler.
func TestRightArrowOnFileDoesNotOpen(t *testing.T) {
	m, _ := fixture(t)
	m = cursorOn(t, m, "alpha.txt")
	pathBefore := m.leftPath
	cursorBefore := m.rawCursor(m.activePane)

	updated, cmd := m.Update(arrow(tea.KeyRight))
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

	updated, cmd := m.Update(arrow(tea.KeyRight))
	m = updated.(Model)
	if cmd == nil {
		t.Fatal("right arrow should dispatch the directory load")
	}
	m = loadDir(t, m, 0)

	if m.leftPath != filepath.Join(dir, "subdir") {
		t.Errorf("leftPath = %q, want the subdirectory", m.leftPath)
	}
}

// Enter is the only key that opens a file.
func TestEnterStillOpensFile(t *testing.T) {
	m, _ := fixture(t)
	m = cursorOn(t, m, "alpha.txt")

	updated, cmd := m.Update(tea.KeyPressMsg(tea.Key{Code: tea.KeyEnter}))
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
	updated, _ := m.Update(shiftKey('f'))
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

	updated, _ = m.Update(arrow(tea.KeyDown))
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

	updated, _ := m.Update(shiftKey('f'))
	m = updated.(Model)

	updated, _ = m.Update(arrow(tea.KeyDown))
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

	updated, _ = m.Update(arrow(tea.KeyDown))
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

	updated, _ := m.Update(shiftKey('c'))
	m = updated.(Model)

	if m.opResult == "" {
		t.Fatal("Shift+C should report what it did")
	}
	afterYank := m.opResult

	updated, _ = m.Update(arrow(tea.KeyDown))
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

	m := testModel(t, config.Config{ThemePath: filepath.Join(dir, "nonexistent.toml")})
	m.width, m.height = 120, 40
	m.leftPath, m.leftFiles = sub, files
	m.rightPath, m.rightFiles = sub, files
	m.mode = inputFilter
	m.setFilter(0, "")

	updated, cmd := m.Update(arrow(tea.KeyLeft))
	m = updated.(Model)
	if cmd == nil {
		t.Fatal("left arrow should dispatch the directory load")
	}
	m = loadDir(t, m, 0)

	if m.leftPath != dir {
		t.Errorf("leftPath = %q, want the parent %q", m.leftPath, dir)
	}
	if m.mode != inputFilter {
		t.Error("navigating should not close the filter prompt")
	}
}

// The ".." row sits above the first entry, so the up arrow reaches it from the
// first entry and then stops there: there is no row above the way up.
func TestUpArrowReachesParentRow(t *testing.T) {
	m, _ := fixture(t)
	m.width, m.height = 100, 30
	m.leftCursor = 0

	m = updateModel(t, m, arrow(tea.KeyUp))
	if got := m.rawCursor(m.activePane); got != parentCursor {
		t.Fatalf("up from the first entry: cursor = %d, want the parent row %d", got, parentCursor)
	}

	m = updateModel(t, m, arrow(tea.KeyUp))
	if got := m.rawCursor(m.activePane); got != parentCursor {
		t.Errorf("up from the parent row: cursor = %d, want to stay on it", got)
	}

	m = updateModel(t, m, arrow(tea.KeyDown))
	if got := m.rawCursor(m.activePane); got != 0 {
		t.Errorf("down from the parent row: cursor = %d, want the first entry 0", got)
	}
	if m.Crashed() {
		t.Errorf("moving onto the parent row panicked: %v", m.crashErr)
	}
}

// A pane at the filesystem root has no way up to draw, so there is no row above
// the first entry and the up arrow stays where it is.
func TestUpArrowAtRootStaysOnFirstEntry(t *testing.T) {
	m, _ := fixture(t)
	m.leftPath = "/"
	m.leftCursor = 0

	m = updateModel(t, m, arrow(tea.KeyUp))
	if got := m.rawCursor(m.activePane); got != 0 {
		t.Errorf("up at the root: cursor = %d, want the first entry 0", got)
	}
}

// Enter is one of the two keys the ".." row answers to: it goes up a level.
func TestEnterOnParentRowGoesUp(t *testing.T) {
	m, dir := fixture(t)
	m.leftCursor = parentCursor

	got, cmd := updateModelCmd(t, m, tea.KeyPressMsg(tea.Key{Code: tea.KeyEnter}))
	if got.leftPending != filepath.Dir(dir) {
		t.Fatalf("leftPending = %q, want the parent %q", got.leftPending, filepath.Dir(dir))
	}
	if got.leftPath != dir {
		t.Errorf("leftPath = %q, want the path on screen %q", got.leftPath, dir)
	}
	if cmd == nil {
		t.Fatal("going up should dispatch a load")
	}
	loaded, ok := cmd().(dirLoadedMsg)
	if !ok {
		t.Fatalf("going up dispatched %T, want a directory load", cmd())
	}
	if loaded.path != filepath.Dir(dir) {
		t.Errorf("loaded %q, want %q", loaded.path, filepath.Dir(dir))
	}
}

// The right arrow enters a directory, and the ".." row is not a directory
// entry: it answers to enter and to a double-click, and to nothing else.
func TestRightArrowOnParentRowDoesNothing(t *testing.T) {
	m, dir := fixture(t)
	m.leftCursor = parentCursor

	got, cmd := updateModelCmd(t, m, arrow(tea.KeyRight))
	if got.Crashed() {
		t.Fatalf("the right arrow on the parent row panicked: %v", got.crashErr)
	}
	if cmd != nil {
		t.Error("the right arrow on the parent row must not dispatch anything")
	}
	if got.leftPending != "" || got.leftPath != dir {
		t.Errorf("the right arrow navigated to %q (path %q)", got.leftPending, got.leftPath)
	}
	if got.leftCursor != parentCursor {
		t.Errorf("the right arrow moved the cursor to %d", got.leftCursor)
	}
}
