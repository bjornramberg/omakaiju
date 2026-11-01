package app

import (
	"os"
	"path/filepath"
	"strings"
	"testing"
	"time"

	"github.com/bjornramberg/omakaiju/internal/config"
	"github.com/bjornramberg/omakaiju/internal/fs"
	"github.com/bjornramberg/omakaiju/internal/ui"

	tea "charm.land/bubbletea/v2"
)

// A load already running for the file under the cursor must not be started
// again. The cache is empty while it runs, so asking for the preview again used
// to cost a second full read and highlight of the same file.
func TestPreviewStartsOneLoadPerFile(t *testing.T) {
	m, _ := fixture(t)
	m = cursorOn(t, m, "beta.txt")

	// The cursor movement started a load and discarded its command. Start the
	// one this test observes by hand.
	m.previewPending = previewRequest{}
	first := m.updatePreview()
	if first == nil {
		t.Fatal("expected a load for a file that has not been previewed yet")
	}
	version := m.previewVersion

	second := m.updatePreview()
	if second != nil {
		t.Error("a second load was started for a file that is already loading")
	}
	if m.previewVersion != version {
		t.Errorf("previewVersion = %d, want %d: a duplicate request must not invalidate the load in flight", m.previewVersion, version)
	}

	// The one load that was started still lands.
	updated, _ := m.Update(first())
	m = updated.(Model)
	if m.previewCachePath != m.previewPath {
		t.Errorf("previewCachePath = %q, want %q: the load in flight was lost", m.previewCachePath, m.previewPath)
	}
}

// Leaving a file and coming back is a new preview, so it gets a fresh load. The
// load that was in flight for the first visit is stale and must not overwrite
// the fresh result, even when the file's stamp cannot tell the two apart.
func TestPreviewMovingAwayAndBackLoadsFreshAndKeepsIt(t *testing.T) {
	m, dir := fixture(t)
	path := filepath.Join(dir, "beta.txt")

	const oldBody = "OLD-MARKER-AAAA\n"
	const newBody = "NEW-MARKER-BBBB\n"
	if len(oldBody) != len(newBody) {
		t.Fatal("the test bodies must be the same length for the stamp to match")
	}
	if err := os.WriteFile(path, []byte(oldBody), 0o644); err != nil {
		t.Fatal(err)
	}
	info, err := os.Stat(path)
	if err != nil {
		t.Fatal(err)
	}
	sameStamp := info.ModTime()

	m = cursorOn(t, m, "beta.txt")

	// Start the load whose result will be abandoned.
	m.previewPending = previewRequest{}
	first := m.updatePreview()
	if first == nil {
		t.Fatal("expected a load for beta.txt")
	}
	stamp := previewStamp(path)
	abandoned := first()

	// Rewrite the file without changing its size or its timestamp, the way a
	// filesystem with coarse timestamps can. The stamp alone cannot tell these
	// two readings apart; the preview version can.
	if err := os.WriteFile(path, []byte(newBody), 0o644); err != nil {
		t.Fatal(err)
	}
	if err := os.Chtimes(path, sameStamp, sameStamp); err != nil {
		t.Fatal(err)
	}
	if got := previewStamp(path); got != stamp {
		t.Fatalf("setup: the stamp changed from %q to %q", stamp, got)
	}

	// Move to the previous file and back, which starts a fresh load for
	// beta.txt.
	updated, away := m.Update(arrow(tea.KeyUp))
	m = updated.(Model)
	if away == nil {
		t.Fatal("expected a load for the file the cursor moved to")
	}
	updated, back := m.Update(arrow(tea.KeyDown))
	m = updated.(Model)
	if back == nil {
		t.Fatal("moving back to the file should start a fresh load for it")
	}

	// The fresh result lands first. The abandoned one arrives afterwards and
	// belongs to the old preview.
	updated, _ = m.Update(back())
	m = updated.(Model)
	if !previewContains(m, "NEW-MARKER") {
		t.Fatalf("the fresh load did not fill the preview: %q", previewText(m))
	}

	updated, _ = m.Update(abandoned)
	m = updated.(Model)
	if !previewContains(m, "NEW-MARKER") {
		t.Errorf("the abandoned load overwrote the fresh preview: %q", previewText(m))
	}
}

// A result gathered for another preview version was highlighted with whatever
// palette was current then. Drawing it would put the old colours back on
// screen, so it is dropped — silently, without an error.
func TestPreviewResultFromAnotherVersionIsDropped(t *testing.T) {
	m, _ := fixture(t)
	m = cursorOn(t, m, "beta.txt")
	m = settlePreview(t, m)
	before := previewText(m)

	stale := previewLoadedMsg{
		path:    m.previewPath,
		stamp:   m.previewStamp,
		version: m.previewVersion - 1,
		content: ui.Content{Kind: ui.KindText, Text: []string{"STALE-MARKER"}},
	}
	updated, cmd := m.Update(stale)
	m = updated.(Model)

	if cmd != nil {
		t.Error("dropping a stale preview must not dispatch anything")
	}
	if previewContains(m, "STALE-MARKER") {
		t.Error("a result from an outdated preview version was drawn")
	}
	if previewText(m) != before {
		t.Errorf("preview changed from %q to %q", before, previewText(m))
	}
	if m.loadErr != nil {
		t.Errorf("loadErr = %v, want nil: a stale result is not a failure", m.loadErr)
	}
	if m.opErr != nil {
		t.Errorf("opErr = %v, want nil: a stale result is not a failure", m.opErr)
	}
}

// The ordinary path still works: the file under the cursor is previewed,
// "loading" is shown while the load is in flight, and Shift+E picks up a change
// made outside the program.
func TestPreviewStillLoadsAndRefreshes(t *testing.T) {
	// A directory with a single file, because Shift+E resets the cursor to the
	// first row: the preview after the refresh is then this file's contents.
	dir := t.TempDir()
	path := filepath.Join(dir, "only.txt")
	if err := os.WriteFile(path, []byte("first body\n"), 0o644); err != nil {
		t.Fatal(err)
	}
	files, err := fs.ReadDir(dir)
	if err != nil {
		t.Fatal(err)
	}

	m := NewModel(config.Config{ThemePath: filepath.Join(dir, "nonexistent.toml")})
	m.width, m.height = 120, 40
	m.leftPath, m.leftFiles = dir, files
	m.rightPath, m.rightFiles = dir, files

	cmd := m.updatePreview()
	if cmd == nil {
		t.Fatal("the file under the cursor should be previewed")
	}

	// The panel says it is waiting rather than showing the previous file.
	if view := plainText(m.View().Content); !strings.Contains(view, "loading") {
		t.Error("the preview should show \"loading\" while the load is in flight")
	}

	updated, _ := m.Update(cmd())
	m = updated.(Model)
	if !previewContains(m, "first body") {
		t.Fatalf("the file under the cursor was not previewed: %q", previewText(m))
	}
	if view := plainText(m.View().Content); strings.Contains(view, "loading") {
		t.Error("the preview should stop saying \"loading\" once the file has arrived")
	}

	// An external edit is picked up by the explicit refresh.
	if err := os.WriteFile(path, []byte("second body\n"), 0o644); err != nil {
		t.Fatal(err)
	}
	future := time.Now().Add(2 * time.Second)
	if err := os.Chtimes(path, future, future); err != nil {
		t.Fatal(err)
	}

	updated, refresh := m.Update(shiftKey('e'))
	m = updated.(Model)
	if refresh == nil {
		t.Fatal("Shift+E should dispatch a refresh")
	}
	m = applyEveryMessage(t, m, refresh)

	if !previewContains(m, "second body") {
		t.Errorf("Shift+E did not refresh the preview: %q", previewText(m))
	}
}
