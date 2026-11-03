package app

import (
	"os"
	"path/filepath"
	"testing"
	"time"

	tea "charm.land/bubbletea/v2"
)

// watchFixture gives both panes the fixture directory and registers it with
// the pane watcher, which NewModel only knows about before the test replaces
// the starting paths.
func watchFixture(t *testing.T) (Model, string) {
	t.Helper()
	m, dir := fixture(t)
	m.followPaneWatch(0, dir)
	m.followPaneWatch(1, dir)
	t.Cleanup(func() {
		if m.dirWatcher != nil {
			_ = m.dirWatcher.Close()
		}
	})
	return m, dir
}

// awaitDirChange runs one watcher wait and returns the message it produces.
func awaitDirChange(t *testing.T, wait tea.Cmd) dirChangedMsg {
	t.Helper()
	if wait == nil {
		t.Fatal("expected a directory watcher command")
	}
	ch := make(chan tea.Msg, 1)
	go func() { ch <- wait() }()

	select {
	case msg := <-ch:
		changed, ok := msg.(dirChangedMsg)
		if !ok {
			t.Fatalf("watcher produced %T, want dirChangedMsg", msg)
		}
		return changed
	case <-time.After(5 * time.Second):
		t.Fatal("timed out waiting for a directory change")
		return dirChangedMsg{}
	}
}

func applyRefresh(t *testing.T, m Model, cmd tea.Cmd) Model {
	t.Helper()
	if cmd == nil {
		t.Fatal("expected a pane refresh")
	}
	updated, _ := m.Update(cmd())
	return updated.(Model)
}

// A file created by another program has to appear in a pane that is already
// showing its directory, without the user leaving and coming back.
func TestWatcherRefreshesPaneWhenFileAppears(t *testing.T) {
	m, dir := watchFixture(t)
	wait := m.waitDirCmd()

	created := filepath.Join(dir, "outside.txt")
	if err := os.WriteFile(created, []byte("new"), 0o644); err != nil {
		t.Fatal(err)
	}

	changed := awaitDirChange(t, wait)
	if changed.closed || changed.err != nil {
		t.Fatalf("watcher reported closed=%v err=%v", changed.closed, changed.err)
	}
	if changed.path != created {
		t.Fatalf("watcher reported %q, want %q", changed.path, created)
	}
	if panes := m.panesShowing(changed.path); len(panes) != 2 {
		t.Fatalf("event matched panes %v, want both", panes)
	}

	m = applyRefresh(t, m, m.refreshPaneCmd(0))
	if idx := indexOf(m.visibleFiles(0), "outside.txt"); idx < 0 {
		t.Fatal("the externally created file did not appear in the pane")
	}
}

// Deleting an entry outside the app has to remove its row.
func TestWatcherRefreshesPaneWhenFileDisappears(t *testing.T) {
	m, dir := watchFixture(t)
	wait := m.waitDirCmd()

	removed := filepath.Join(dir, "beta.txt")
	if err := os.Remove(removed); err != nil {
		t.Fatal(err)
	}

	changed := awaitDirChange(t, wait)
	if changed.closed || changed.err != nil {
		t.Fatalf("watcher reported closed=%v err=%v", changed.closed, changed.err)
	}
	m = applyRefresh(t, m, m.refreshPaneCmd(0))
	if idx := indexOf(m.visibleFiles(0), "beta.txt"); idx >= 0 {
		t.Fatal("the externally deleted file is still in the pane")
	}
}

// An unrelated external change must not move the cursor off what the user was
// looking at, and must not clear an active filter.
func TestWatcherRefreshKeepsCursorAndFilter(t *testing.T) {
	m, dir := watchFixture(t)
	m.leftFilter = "txt"
	idx := indexOf(m.visibleFiles(0), "beta.txt")
	if idx < 0 {
		t.Fatal("setup: beta.txt is missing from the filtered listing")
	}
	m.setCursor(0, idx)
	if got := m.visibleFiles(0)[m.leftCursor].Name; got != "beta.txt" {
		t.Fatalf("setup: cursor is on %q, want beta.txt", got)
	}

	if err := os.WriteFile(filepath.Join(dir, "aaa.txt"), []byte("new"), 0o644); err != nil {
		t.Fatal(err)
	}

	m = applyRefresh(t, m, m.refreshPaneCmd(0))
	if m.leftFilter != "txt" {
		t.Errorf("leftFilter = %q, want the filter kept", m.leftFilter)
	}
	if m.leftCursor >= len(m.visibleFiles(0)) {
		t.Fatalf("leftCursor = %d, outside the new listing", m.leftCursor)
	}
	if got := m.visibleFiles(0)[m.leftCursor].Name; got != "beta.txt" {
		t.Errorf("cursor landed on %q, want beta.txt", got)
	}
}

// The watcher follows a pane when it navigates: an event under the old
// directory is not applied to the new one.
func TestPaneWatcherFollowsNavigation(t *testing.T) {
	m, dir := watchFixture(t)
	sub := filepath.Join(dir, "subdir")
	m.leftPath = sub
	m.followPaneWatch(0, sub)

	if m.watchedPath(0) != sub {
		t.Fatalf("leftWatched = %q, want %q", m.watchedPath(0), sub)
	}
	if panes := m.panesShowing(filepath.Join(dir, "beta.txt")); len(panes) != 1 || panes[0] != 1 {
		t.Fatalf("an event in the old directory matched panes %v, want only the right pane", panes)
	}
}
