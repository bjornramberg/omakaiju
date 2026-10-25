package app

import (
	"errors"
	"os"
	"path/filepath"
	"strings"
	"testing"
	"time"

	tea "charm.land/bubbletea/v2"

	"github.com/bjornramberg/omakaiju/internal/config"
	"github.com/bjornramberg/omakaiju/internal/fs"
)

// A load the pane has already left must not change it. Bubble Tea runs every
// command in its own goroutine, so entering a large directory and then leaving
// it can deliver the first listing last.
func TestStaleDirectoryLoadDoesNotChangePane(t *testing.T) {
	m, dir := fixture(t)
	m = cursorOn(t, m, "subdir")

	// Enter subdir but do not apply its listing yet: it is still "loading".
	updated, enterCmd := m.Update(tea.KeyPressMsg(tea.Key{Code: tea.KeyEnter}))
	m = updated.(Model)
	if enterCmd == nil {
		t.Fatal("enter should dispatch a load")
	}
	stale, ok := enterCmd().(dirLoadedMsg)
	if !ok {
		t.Fatal("enter did not produce a directory load")
	}

	// Leave for the parent before the listing lands.
	updated, _ = m.Update(arrow(tea.KeyLeft))
	m = updated.(Model)

	pathBefore := m.leftPath
	filesBefore := len(m.leftFiles)
	if pathBefore != dir {
		t.Fatalf("setup: leftPath = %q, want the parent %q", pathBefore, dir)
	}

	// The abandoned load arrives last.
	updated, _ = m.Update(stale)
	m = updated.(Model)

	if m.leftPath != pathBefore {
		t.Errorf("stale load changed the path: %q, want %q", m.leftPath, pathBefore)
	}
	if len(m.leftFiles) != filesBefore {
		t.Errorf("stale load changed the listing: %d entries, want %d", len(m.leftFiles), filesBefore)
	}
	if m.loadErr != nil {
		t.Errorf("a stale load must be dropped silently, got loadErr = %v", m.loadErr)
	}
}

// A failed listing must not commit the path it was loading. The header and the
// rows have to keep naming the same directory, and the failure must be shown.
func TestFailedDirectoryLoadLeavesPathAndListingUnchanged(t *testing.T) {
	m, _ := fixture(t)
	m = cursorOn(t, m, "subdir")

	updated, enterCmd := m.Update(tea.KeyPressMsg(tea.Key{Code: tea.KeyEnter}))
	m = updated.(Model)
	if enterCmd == nil {
		t.Fatal("enter should dispatch a load")
	}
	load, ok := enterCmd().(dirLoadedMsg)
	if !ok {
		t.Fatal("enter did not produce a directory load")
	}

	pathBefore := m.leftPath
	filesBefore := append([]string(nil), entryNames(m.leftFiles)...)

	// Simulate a directory that cannot be read: the listing never arrives.
	load.err = errors.New("permission denied")
	updated, _ = m.Update(load)
	m = updated.(Model)

	if m.leftPath != pathBefore {
		t.Errorf("failed load changed the path: %q, want %q", m.leftPath, pathBefore)
	}
	if got := entryNames(m.leftFiles); strings.Join(got, ",") != strings.Join(filesBefore, ",") {
		t.Errorf("failed load changed the listing: %v, want %v", got, filesBefore)
	}
	if m.loadErr == nil {
		t.Fatal("a failed listing was not reported")
	}
	if !strings.Contains(m.loadErr.Error(), "permission denied") {
		t.Errorf("loadErr = %q, want the reason", m.loadErr)
	}
}

func entryNames(files []fs.Entry) []string {
	names := make([]string, 0, len(files))
	for _, f := range files {
		names = append(names, f.Name)
	}
	return names
}

// Pasting the marked set while still in its own directory has nothing to move,
// but the marks are still staged and must survive so the user can tab to the
// destination and paste there.
func TestPasteMarksInOwnDirectoryKeepsMarks(t *testing.T) {
	m, _, _ := twoPaneFixture(t)

	m = moveCursorTo(t, m, "alpha.txt")
	updated, _ := m.Update(shiftKey('m'))
	m = updated.(Model)
	if len(m.marked) != 1 {
		t.Fatalf("setup: marked = %d, want 1", len(m.marked))
	}

	updated, _ = m.Update(shiftKey('v'))
	m = updated.(Model)

	if m.opResult != "already in this directory" {
		t.Errorf("opResult = %q, want %q", m.opResult, "already in this directory")
	}
	if len(m.marked) != 1 {
		t.Errorf("marked = %d, want the marks kept for a paste elsewhere", len(m.marked))
	}
}

// A goto that fails has to be visible. The prompt used to keep precedence over
// the error line while it stayed open, and the next keypress cleared the error,
// so a bad path looked like nothing happened.
func TestFailedGotoIsVisible(t *testing.T) {
	m, dir := fixture(t)
	m.width, m.height = 240, 40
	m.mode = inputGoto
	m.inputBuf = filepath.Join(dir, "does-not-exist")

	updated, cmd := m.commitGoto()
	m = updated.(Model)

	if cmd != nil {
		t.Error("a failed goto must not dispatch a load")
	}
	if m.mode != inputNone {
		t.Error("the prompt should close so the error line can be seen")
	}
	if m.loadErr == nil {
		t.Fatal("a failed goto was not reported")
	}
	got := m.loadErr.Error()
	if !strings.Contains(got, "does-not-exist") {
		t.Errorf("loadErr = %q, want the path named", got)
	}
	if !strings.Contains(strings.ToLower(got), "no such file") &&
		!errors.Is(m.loadErr, os.ErrNotExist) {
		t.Errorf("loadErr = %q, want the reason", got)
	}

	view := m.View().Content
	if !strings.Contains(view, "does-not-exist") {
		t.Errorf("the bottom bar does not show the failed goto:\n%s", view)
	}
	if !strings.Contains(strings.ToLower(view), "no such file") {
		t.Errorf("the bottom bar does not show the reason:\n%s", view)
	}
}

// A transient fsnotify error used to end the wait, silently disabling theme
// hot-reload until the app restarted. The wait has to be armed again.
func TestThemeWatcherErrorDoesNotEndTheWatch(t *testing.T) {
	m, colors := themeFixture(t)

	updated, cmd := m.Update(themeReloadedMsg{err: errors.New("inotify: queue overflow")})
	m = updated.(Model)

	if cmd == nil {
		t.Fatal("a transient watcher error must re-arm the wait")
	}
	if m.loadErr == nil || !strings.Contains(m.loadErr.Error(), "queue overflow") {
		t.Errorf("loadErr = %q, want the watcher failure reported", m.loadErr)
	}

	// The command handed back must really be a fresh wait: a theme change is
	// noticed and applied through it.
	body := "background = \"#0d0d0d\"\naccent = \"#222222\"\n"
	if err := os.WriteFile(colors, []byte(body), 0o644); err != nil {
		t.Fatal(err)
	}
	msg := awaitReload(t, cmd)
	updated, next := m.Update(msg)
	m = updated.(Model)

	if m.theme.BGDark != "#0d0d0d" {
		t.Errorf("BGDark = %q, want the reloaded palette", m.theme.BGDark)
	}
	if next == nil {
		t.Error("a successful reload must re-arm the wait too")
	}
}

// Shutting the watcher down for good is the one case that ends the wait.
func TestClosedThemeWatcherStopsTheWatch(t *testing.T) {
	m, _ := themeFixture(t)
	if err := m.themeWatcher.Close(); err != nil {
		t.Fatalf("closing the watcher failed: %v", err)
	}

	wait := m.waitThemeCmd()
	if wait == nil {
		t.Fatal("expected a wait command")
	}
	ch := make(chan tea.Msg, 1)
	go func() { ch <- wait() }()

	select {
	case msg := <-ch:
		updated, next := m.Update(msg)
		m = updated.(Model)
		if next != nil {
			t.Error("a closed watcher must not be re-armed")
		}
	case <-time.After(3 * time.Second):
		t.Fatal("waiting on a closed watcher never returned")
	}
}

// A file edited, overwritten or replaced on disk keeps its path, so a preview
// keyed on the path alone showed stale contents. Its size and modification
// time identify the version now, and Shift+E refreshes the preview of the
// entry under the cursor.
func TestPreviewRefreshesWhenFileChanges(t *testing.T) {
	m, dir := fixture(t)
	m = cursorOn(t, m, "beta.txt")
	m = settlePreview(t, m)

	path := filepath.Join(dir, "beta.txt")
	if m.previewPath != path {
		t.Fatalf("setup: previewPath = %q, want %q", m.previewPath, path)
	}

	// Rewrite the file in place and give it a distinct mtime, the way an
	// external editor would.
	if err := os.WriteFile(path, []byte("first revision: ALPHA-MARKER\n"), 0o644); err != nil {
		t.Fatal(err)
	}
	firstWrite := time.Now().Add(2 * time.Second)
	if err := os.Chtimes(path, firstWrite, firstWrite); err != nil {
		t.Fatal(err)
	}

	cmd := m.updatePreview()
	if cmd == nil {
		t.Fatal("a changed file must be re-read even though its path is unchanged")
	}
	updated, _ := m.Update(cmd())
	m = updated.(Model)
	if !previewContains(m, "ALPHA-MARKER") {
		t.Errorf("the preview was not refreshed after the file changed: %q", previewText(m))
	}

	// Shift+E is the explicit refresh, and it must refresh the preview too.
	// A plain refresh puts the cursor back on the first row (existing
	// behaviour), so this half uses a directory whose first row is the file
	// under the cursor: the preview after the reload is that file's fresh
	// contents.
	dir2 := t.TempDir()
	refreshPath := filepath.Join(dir2, "only.txt")
	if err := os.WriteFile(refreshPath, []byte("old contents\n"), 0o644); err != nil {
		t.Fatal(err)
	}
	files, err := fs.ReadDir(dir2)
	if err != nil {
		t.Fatal(err)
	}
	m2 := NewModel(config.Config{ThemePath: filepath.Join(dir2, "nonexistent.toml")})
	m2.width, m2.height = 120, 40
	m2.leftPath, m2.leftFiles = dir2, files
	m2.rightPath, m2.rightFiles = dir2, files
	m2 = settlePreview(t, m2)
	if m2.previewPath != refreshPath {
		t.Fatalf("setup: previewPath = %q, want %q", m2.previewPath, refreshPath)
	}

	if err := os.WriteFile(refreshPath, []byte("second revision: BETA-MARKER\n"), 0o644); err != nil {
		t.Fatal(err)
	}
	secondWrite := time.Now().Add(3 * time.Second)
	if err := os.Chtimes(refreshPath, secondWrite, secondWrite); err != nil {
		t.Fatal(err)
	}

	updated, refresh := m2.Update(shiftKey('e'))
	m2 = updated.(Model)
	if refresh == nil {
		t.Fatal("Shift+E should dispatch a refresh")
	}
	m2 = applyEveryMessage(t, m2, refresh)

	if !previewContains(m2, "BETA-MARKER") {
		t.Errorf("Shift+E did not refresh the preview: %q", previewText(m2))
	}
}

func previewText(m Model) string {
	return strings.Join(m.previewContent.Text, "\n")
}

func previewContains(m Model, want string) bool {
	return strings.Contains(previewText(m), want)
}

// applyEveryMessage runs a command and feeds every message it produces back,
// flattening a tea.Batch. It lets a test observe a refresh that dispatches more
// than one command (the listing and the preview).
func applyEveryMessage(t *testing.T, m Model, cmd tea.Cmd) Model {
	t.Helper()
	if cmd == nil {
		return m
	}
	msg := cmd()
	batch, ok := msg.(tea.BatchMsg)
	if !ok {
		updated, _ := m.Update(msg)
		return updated.(Model)
	}
	for _, c := range batch {
		if c == nil {
			continue
		}
		updated, _ := m.Update(c())
		m = updated.(Model)
	}
	return m
}
