package app

import (
	"archive/zip"
	"os"
	"path/filepath"
	"strings"
	"testing"

	"omakaiju/internal/fs"

	tea "charm.land/bubbletea/v2"
)

func typeString(t *testing.T, m Model, s string) Model {
	t.Helper()
	for _, r := range s {
		updated, _ := m.Update(key(r))
		m = updated.(Model)
	}
	return m
}

func enter(t *testing.T, m Model) (Model, tea.Cmd) {
	t.Helper()
	updated, cmd := m.Update(tea.KeyPressMsg{Code: tea.KeyEnter})
	return updated.(Model), cmd
}

func esc(t *testing.T, m Model) Model {
	t.Helper()
	updated, _ := m.Update(tea.KeyPressMsg{Code: tea.KeyEscape})
	return updated.(Model)
}

func TestAddCreatesFile(t *testing.T) {
	m, dir := fixture(t)

	updated, _ := m.Update(key('a'))
	m = updated.(Model)
	if m.mode != inputAdd {
		t.Fatal("a should enter add mode")
	}

	m = typeString(t, m, "newfile.txt")
	m, cmd := enter(t, m)

	if m.mode != inputNone {
		t.Error("enter should leave add mode")
	}
	if cmd == nil {
		t.Error("add should trigger a directory reload")
	}
	if _, err := os.Stat(filepath.Join(dir, "newfile.txt")); err != nil {
		t.Fatalf("file not created: %v", err)
	}
	if m.opResult != "created newfile.txt" {
		t.Errorf("opResult = %q, want %q", m.opResult, "created newfile.txt")
	}
}

func TestAddCreatesDirectoryWithTrailingSlash(t *testing.T) {
	m, dir := fixture(t)

	updated, _ := m.Update(key('a'))
	m = updated.(Model)
	m = typeString(t, m, "newdir/")
	m, _ = enter(t, m)

	info, err := os.Stat(filepath.Join(dir, "newdir"))
	if err != nil {
		t.Fatalf("directory not created: %v", err)
	}
	if !info.IsDir() {
		t.Error("newdir should be a directory")
	}
	// The trailing slash must be trimmed from the stored entry name.
	entries, err := os.ReadDir(dir)
	if err != nil {
		t.Fatal(err)
	}
	for _, e := range entries {
		if e.Name() == "newdir/" {
			t.Errorf("entry name kept the trailing slash: %q", e.Name())
		}
	}
	if m.opResult != "created dir newdir" {
		t.Errorf("opResult = %q, want %q", m.opResult, "created dir newdir")
	}
}

func TestAddEmptyNameIsNoOp(t *testing.T) {
	m, dir := fixture(t)

	updated, _ := m.Update(key('a'))
	m = updated.(Model)
	m, cmd := enter(t, m)

	if m.mode != inputNone {
		t.Error("enter should leave add mode")
	}
	if cmd != nil {
		t.Error("empty add should not trigger a reload")
	}
	entries, err := os.ReadDir(dir)
	if err != nil {
		t.Fatal(err)
	}
	if len(entries) != 3 {
		t.Errorf("directory changed unexpectedly: %d entries, want 3", len(entries))
	}
}

func TestAddEscapeCancels(t *testing.T) {
	m, dir := fixture(t)

	updated, _ := m.Update(key('a'))
	m = updated.(Model)
	m = typeString(t, m, "should-not-exist")
	m = esc(t, m)

	if m.mode != inputNone {
		t.Error("esc should leave add mode")
	}
	if _, err := os.Stat(filepath.Join(dir, "should-not-exist")); !os.IsNotExist(err) {
		t.Error("esc should not create anything")
	}
}

func TestRenameRenamesEntry(t *testing.T) {
	m, dir := fixture(t)

	idx := indexOf(m.visibleFiles(0), "alpha.txt")
	if idx < 0 {
		t.Fatal("alpha.txt missing from listing")
	}
	for i := 0; i < idx; i++ {
		updated, _ := m.Update(key('j'))
		m = updated.(Model)
	}

	updated, _ := m.Update(key('r'))
	m = updated.(Model)
	if m.mode != inputRename {
		t.Fatal("r should enter rename mode")
	}
	if m.inputBuf != "alpha.txt" {
		t.Errorf("inputBuf = %q, want the current name pre-filled", m.inputBuf)
	}

	// Clear the pre-filled name, then type a new one.
	for i := 0; i < len("alpha.txt"); i++ {
		updated, _ = m.Update(tea.KeyPressMsg{Code: tea.KeyBackspace})
		m = updated.(Model)
	}
	m = typeString(t, m, "renamed.txt")
	m, cmd := enter(t, m)

	if m.mode != inputNone {
		t.Error("enter should leave rename mode")
	}
	if cmd == nil {
		t.Error("rename should trigger a directory reload")
	}
	if _, err := os.Stat(filepath.Join(dir, "renamed.txt")); err != nil {
		t.Fatalf("renamed file missing: %v", err)
	}
	if _, err := os.Stat(filepath.Join(dir, "alpha.txt")); !os.IsNotExist(err) {
		t.Error("original name should be gone")
	}
	if m.opResult != "renamed renamed.txt" {
		t.Errorf("opResult = %q, want %q", m.opResult, "renamed renamed.txt")
	}
}

func TestRenameEscapeCancels(t *testing.T) {
	m, dir := fixture(t)

	idx := indexOf(m.visibleFiles(0), "alpha.txt")
	for i := 0; i < idx; i++ {
		updated, _ := m.Update(key('j'))
		m = updated.(Model)
	}
	updated, _ := m.Update(key('r'))
	m = updated.(Model)
	for i := 0; i < len("alpha.txt"); i++ {
		updated, _ = m.Update(tea.KeyPressMsg{Code: tea.KeyBackspace})
		m = updated.(Model)
	}
	m = typeString(t, m, "nope.txt")
	m = esc(t, m)

	if _, err := os.Stat(filepath.Join(dir, "alpha.txt")); err != nil {
		t.Fatalf("original file should be untouched: %v", err)
	}
	if _, err := os.Stat(filepath.Join(dir, "nope.txt")); !os.IsNotExist(err) {
		t.Error("esc should not rename")
	}
}

func TestRenameSameNameIsNoOp(t *testing.T) {
	m, dir := fixture(t)

	idx := indexOf(m.visibleFiles(0), "alpha.txt")
	for i := 0; i < idx; i++ {
		updated, _ := m.Update(key('j'))
		m = updated.(Model)
	}
	updated, _ := m.Update(key('r'))
	m = updated.(Model)
	m, cmd := enter(t, m)

	if cmd != nil {
		t.Error("renaming to the same name should not reload")
	}
	if _, err := os.Stat(filepath.Join(dir, "alpha.txt")); err != nil {
		t.Fatalf("file should still exist: %v", err)
	}
}

func TestAddAndRenameTargetActivePaneOnly(t *testing.T) {
	m, dir := fixture(t)

	updated, _ := m.Update(tea.KeyPressMsg{Code: tea.KeyTab})
	m = updated.(Model)
	if m.activePane != 1 {
		t.Fatal("tab should activate the right pane")
	}

	updated, _ = m.Update(key('a'))
	m = updated.(Model)
	m = typeString(t, m, "right-only.txt")
	m, _ = enter(t, m)

	// Both panes point at the same directory here, so verify the model routed
	// through the active pane's path and left the other pane's state alone.
	if m.rightFilter != "" || m.leftFilter != "" {
		t.Error("add should not set filters")
	}
	if m.mode != inputNone {
		t.Error("mode should be cleared")
	}
	if _, err := os.Stat(filepath.Join(dir, "right-only.txt")); err != nil {
		t.Fatalf("file not created: %v", err)
	}
}

// runBatch executes a Cmd that may be a tea.Batch and returns every resulting
// fileOpMsg, so tests can observe the async moves actually completing.
func runBatch(t *testing.T, cmd tea.Cmd) []fileOpMsg {
	t.Helper()
	if cmd == nil {
		return nil
	}
	msg := cmd()
	if batch, ok := msg.(tea.BatchMsg); ok {
		var out []fileOpMsg
		for _, c := range batch {
			if c == nil {
				continue
			}
			if m, ok := c().(fileOpMsg); ok {
				out = append(out, m)
			}
		}
		return out
	}
	if m, ok := msg.(fileOpMsg); ok {
		return []fileOpMsg{m}
	}
	return nil
}

func requireNoOpErrors(t *testing.T, msgs []fileOpMsg) {
	t.Helper()
	for _, m := range msgs {
		if m.err != nil {
			t.Fatalf("file op failed: %v", m.err)
		}
	}
}

// moveCursorTo advances the active pane cursor onto the named entry.
func moveCursorTo(t *testing.T, m Model, name string) Model {
	t.Helper()
	idx := indexOf(m.visibleFiles(m.activePane), name)
	if idx < 0 {
		t.Fatalf("%s missing from listing", name)
	}
	for i := 0; i < idx; i++ {
		updated, _ := m.Update(key('j'))
		m = updated.(Model)
	}
	return m
}

// twoPaneFixture points the panes at separate directories so moves are visible.
func twoPaneFixture(t *testing.T) (Model, string, string) {
	t.Helper()
	m, src := fixture(t)

	dst := filepath.Join(src, "..", filepath.Base(src)+"-dest")
	if err := os.MkdirAll(dst, 0o755); err != nil {
		t.Fatal(err)
	}

	m.rightPath = dst
	rightFiles, err := fs.ReadDir(dst)
	if err != nil {
		t.Fatal(err)
	}
	m.rightFiles = rightFiles
	return m, src, dst
}

func TestMarkTogglesOnAndOff(t *testing.T) {
	m, dir := fixture(t)
	m = moveCursorTo(t, m, "alpha.txt")

	updated, _ := m.Update(key('m'))
	m = updated.(Model)
	if len(m.marked) != 1 {
		t.Fatalf("marked = %d, want 1", len(m.marked))
	}
	if _, ok := m.marked[filepath.Join(dir, "alpha.txt")]; !ok {
		t.Error("alpha.txt should be marked")
	}

	updated, _ = m.Update(key('m'))
	m = updated.(Model)
	if len(m.marked) != 0 {
		t.Fatalf("second m should unmark, marked = %d", len(m.marked))
	}
}

func TestMarksAccumulate(t *testing.T) {
	m, _ := fixture(t)

	for _, name := range []string{"alpha.txt", "beta.txt"} {
		m = moveCursorTo(t, m, name)
		updated, _ := m.Update(key('m'))
		m = updated.(Model)
	}

	if len(m.marked) != 2 {
		t.Fatalf("marked = %d, want 2", len(m.marked))
	}
}

func TestPasteMovesMarkedSetToOtherPane(t *testing.T) {
	m, src, dst := twoPaneFixture(t)

	for _, name := range []string{"alpha.txt", "beta.txt"} {
		m = moveCursorTo(t, m, name)
		updated, _ := m.Update(key('m'))
		m = updated.(Model)
	}

	updated, cmd := m.Update(key('p'))
	m = updated.(Model)
	if cmd == nil {
		t.Fatal("paste should return move commands")
	}
	if len(m.marked) != 0 {
		t.Errorf("marks should clear after paste, got %d", len(m.marked))
	}

	// Drain the returned commands so the moves actually run.
	msgs := runBatch(t, cmd)
	if len(msgs) != 2 {
		t.Fatalf("expected 2 move operations, got %d", len(msgs))
	}
	requireNoOpErrors(t, msgs)

	for _, name := range []string{"alpha.txt", "beta.txt"} {
		if _, err := os.Stat(filepath.Join(dst, name)); err != nil {
			t.Errorf("%s missing from destination: %v", name, err)
		}
		if _, err := os.Stat(filepath.Join(src, name)); !os.IsNotExist(err) {
			t.Errorf("%s should have left the source", name)
		}
	}
}

func TestPasteSkipsStaleMarks(t *testing.T) {
	m, src, dst := twoPaneFixture(t)

	m = moveCursorTo(t, m, "alpha.txt")
	updated, _ := m.Update(key('m'))
	m = updated.(Model)
	m = moveCursorTo(t, m, "beta.txt")
	updated, _ = m.Update(key('m'))
	m = updated.(Model)

	// Delete one marked file behind the model's back.
	if err := os.Remove(filepath.Join(src, "beta.txt")); err != nil {
		t.Fatal(err)
	}

	updated, cmd := m.Update(key('p'))
	m = updated.(Model)

	if m.opResult != "moved 1, skipped 1" {
		t.Errorf("opResult = %q, want %q", m.opResult, "moved 1, skipped 1")
	}
	requireNoOpErrors(t, runBatch(t, cmd))
	if _, err := os.Stat(filepath.Join(dst, "alpha.txt")); err != nil {
		t.Errorf("alpha.txt should have moved: %v", err)
	}
}

func TestPasteSkipsEntryAlreadyInTarget(t *testing.T) {
	m, src, _ := twoPaneFixture(t)

	m = moveCursorTo(t, m, "alpha.txt")
	updated, _ := m.Update(key('m'))
	m = updated.(Model)

	// Point the destination at the same directory as the source so the
	// marked entry is already in place.
	m.rightPath = src

	updated, _ = m.Update(key('p'))
	m = updated.(Model)

	if m.opResult != "nothing to move" {
		t.Errorf("opResult = %q, want %q", m.opResult, "nothing to move")
	}
}

func TestYankPasteStillWorksWithoutMarks(t *testing.T) {
	m, _, dst := twoPaneFixture(t)

	m = moveCursorTo(t, m, "alpha.txt")
	updated, _ := m.Update(key('y'))
	m = updated.(Model)

	updated, cmd := m.Update(key('p'))
	m = updated.(Model)

	if m.opResult != "pasted 1 file(s)" {
		t.Errorf("opResult = %q, want %q", m.opResult, "pasted 1 file(s)")
	}
	requireNoOpErrors(t, runBatch(t, cmd))
	// A copy leaves the original in place.
	if _, err := os.Stat(filepath.Join(dst, "alpha.txt")); err != nil {
		t.Errorf("copy missing from destination: %v", err)
	}
	if len(m.marked) != 0 {
		t.Error("yank should not populate marks")
	}
}

func TestArchivePreviewIsCached(t *testing.T) {
	m, dir := fixture(t)

	zipPath := filepath.Join(dir, "bundle.zip")
	f, err := os.Create(zipPath)
	if err != nil {
		t.Fatal(err)
	}
	zw := zip.NewWriter(f)
	w, err := zw.Create("hello.txt")
	if err != nil {
		t.Fatal(err)
	}
	if _, err := w.Write([]byte("hi")); err != nil {
		t.Fatal(err)
	}
	if err := zw.Close(); err != nil {
		t.Fatal(err)
	}
	f.Close()

	files, err := fs.ReadDir(dir)
	if err != nil {
		t.Fatal(err)
	}
	m.leftFiles = files
	m.leftPath = dir

	m = moveCursorTo(t, m, "bundle.zip")
	if m.previewPath != zipPath {
		t.Fatalf("previewPath = %q, want %q", m.previewPath, zipPath)
	}
	if len(m.previewArchive) == 0 {
		t.Fatal("expected a cached archive listing")
	}
	if m.previewCachePath != zipPath {
		t.Errorf("previewCachePath = %q, want %q", m.previewCachePath, zipPath)
	}
	if !strings.Contains(strings.Join(m.previewArchive, "\n"), "hello.txt") {
		t.Error("listing should contain the archive member")
	}

	// Re-running the preview update with the same selection must reuse the
	// cached listing rather than re-listing the archive.
	before := strings.Join(m.previewArchive, "\n")
	m.updatePreview()
	if after := strings.Join(m.previewArchive, "\n"); after != before {
		t.Error("archive listing should be reused while the selection is unchanged")
	}
	if m.previewCachePath != zipPath {
		t.Errorf("previewCachePath = %q, want %q", m.previewCachePath, zipPath)
	}

	// Rendering must not disturb the cache.
	m.width, m.height = 120, 40
	_ = m.View()
	if strings.Join(m.previewArchive, "\n") != before {
		t.Error("View should not re-list the archive")
	}
}
