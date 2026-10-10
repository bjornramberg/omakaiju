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
	if len(m.previewArchive) == 0 || m.previewArchive[0].Name != "hello.txt" {
		t.Errorf("cached entries = %v, want hello.txt", m.previewArchive)
	}

	// Re-running the preview update with the same selection must reuse the
	// cached listing rather than re-listing the archive.
	before := m.previewArchive[0].Name
	m.updatePreview()
	if len(m.previewArchive) == 0 || m.previewArchive[0].Name != before {
		t.Error("archive listing should be reused while the selection is unchanged")
	}
	if m.previewCachePath != zipPath {
		t.Errorf("previewCachePath = %q, want %q", m.previewCachePath, zipPath)
	}

	// Rendering must not disturb the cache.
	m.width, m.height = 120, 40
	_ = m.View()
	if len(m.previewArchive) == 0 || m.previewArchive[0].Name != before {
		t.Error("View should not re-list the archive")
	}
}

// yankCopy stages a file and returns a model ready for paste.
func yankCopy(t *testing.T, m Model, name string) Model {
	t.Helper()
	m = moveCursorTo(t, m, name)
	updated, _ := m.Update(key('y'))
	return updated.(Model)
}

func TestSmallCopyUsesBlockingPath(t *testing.T) {
	m, _, _ := twoPaneFixture(t)

	// Well under the 1 MiB threshold, so no progress job should start.
	small := filepath.Join(m.leftPath, "small.bin")
	if err := os.WriteFile(small, make([]byte, 1024), 0o644); err != nil {
		t.Fatal(err)
	}
	files, err := fs.ReadDir(m.leftPath)
	if err != nil {
		t.Fatal(err)
	}
	m.leftFiles = files

	m = yankCopy(t, m, "small.bin")
	updated, cmd := m.Update(key('p'))
	m = updated.(Model)

	if m.copier != nil {
		t.Error("a small copy should not create a progress job")
	}
	if cmd == nil {
		t.Fatal("small copy should still dispatch a copy command")
	}
	requireNoOpErrors(t, runBatch(t, cmd))
}

func TestLargeCopyStartsProgressJob(t *testing.T) {
	m, _, dst := twoPaneFixture(t)

	// Larger than the 1 MiB threshold so the steppable copier is used.
	big := filepath.Join(m.leftPath, "big.bin")
	if err := os.WriteFile(big, make([]byte, 3<<20), 0o644); err != nil {
		t.Fatal(err)
	}
	files, err := fs.ReadDir(m.leftPath)
	if err != nil {
		t.Fatal(err)
	}
	m.leftFiles = files

	m = yankCopy(t, m, "big.bin")
	updated, cmd := m.Update(key('p'))
	m = updated.(Model)

	if m.copier != nil {
		t.Fatal("the copier should be created by the returned command, not inline")
	}
	if m.copyFiles != 1 {
		t.Errorf("copyFiles = %d, want 1", m.copyFiles)
	}

	// Run the creation command to adopt the copier.
	if _, ok := cmd().(copyProgressMsg); !ok {
		t.Fatal("expected a copyProgressMsg to start the job")
	}

	// Drive the job to completion.
	copier, err := fs.NewCopier([]string{big}, dst)
	if err != nil {
		t.Fatal(err)
	}
	updated, _ = m.Update(copyProgressMsg{copier: copier})
	m = updated.(Model)
	if m.copier == nil {
		t.Error("model should adopt the copier")
	}

	for {
		next := stepCopyCmd(m.copier)
		msg, ok := next().(copyProgressMsg)
		if !ok {
			t.Fatal("expected progress messages while stepping")
		}
		updated, _ = m.Update(msg)
		m = updated.(Model)
		if msg.done {
			break
		}
	}

	if m.copier != nil {
		t.Error("copier should be released on completion")
	}
	if m.copyFiles != 0 {
		t.Error("copyFiles should be reset on completion")
	}
	info, err := os.Stat(filepath.Join(dst, "big.bin"))
	if err != nil {
		t.Fatalf("destination missing: %v", err)
	}
	if info.Size() != 3<<20 {
		t.Errorf("copied size = %d, want %d", info.Size(), 3<<20)
	}
}

func TestEscapeCancelsCopyAndRemovesPartial(t *testing.T) {
	m, _, dst := twoPaneFixture(t)

	big := filepath.Join(m.leftPath, "big.bin")
	if err := os.WriteFile(big, make([]byte, 4<<20), 0o644); err != nil {
		t.Fatal(err)
	}
	files, err := fs.ReadDir(m.leftPath)
	if err != nil {
		t.Fatal(err)
	}
	m.leftFiles = files

	m = yankCopy(t, m, "big.bin")

	copier, err := fs.NewCopier([]string{big}, dst)
	if err != nil {
		t.Fatal(err)
	}
	updated, _ := m.Update(copyProgressMsg{copier: copier})
	m = updated.(Model)

	// Take one step so a partial file exists.
	msg := stepCopyCmd(m.copier)().(copyProgressMsg)
	updated, _ = m.Update(msg)
	m = updated.(Model)

	if _, err := os.Stat(filepath.Join(dst, "big.bin")); err != nil {
		t.Fatalf("partial file should exist mid-copy: %v", err)
	}

	updated, _ = m.Update(tea.KeyPressMsg{Code: tea.KeyEscape})
	m = updated.(Model)

	if m.copier != nil {
		t.Error("cancel should release the copier")
	}
	if m.opResult != "copy cancelled" {
		t.Errorf("opResult = %q, want %q", m.opResult, "copy cancelled")
	}
	if _, err := os.Stat(filepath.Join(dst, "big.bin")); !os.IsNotExist(err) {
		t.Error("cancel should remove the partial destination file")
	}
}

func TestKeysIgnoredWhileCopying(t *testing.T) {
	m, _, dst := twoPaneFixture(t)

	big := filepath.Join(m.leftPath, "big.bin")
	if err := os.WriteFile(big, make([]byte, 4<<20), 0o644); err != nil {
		t.Fatal(err)
	}
	m.leftFiles, _ = fs.ReadDir(m.leftPath)
	m = yankCopy(t, m, "big.bin")

	copier, err := fs.NewCopier([]string{big}, dst)
	if err != nil {
		t.Fatal(err)
	}
	updated, _ := m.Update(copyProgressMsg{copier: copier})
	m = updated.(Model)

	before := m.leftCursor
	updated, _ = m.Update(key('j'))
	m = updated.(Model)
	if m.leftCursor != before {
		t.Error("navigation should be ignored while a copy runs")
	}
}

// stageDelete points a pane at dir and opens the delete prompt on name.
func stageDelete(t *testing.T, m Model, name string) Model {
	t.Helper()
	files, err := fs.ReadDir(m.leftPath)
	if err != nil {
		t.Fatal(err)
	}
	m.leftFiles = files
	m = moveCursorTo(t, m, name)
	updated, _ := m.Update(key('d'))
	m = updated.(Model)
	if !m.confirmDelete {
		t.Fatalf("d should open the delete prompt for %s", name)
	}
	return m
}

func TestDeleteTrashesByDefault(t *testing.T) {
	// Trash is the safe choice, so it answers to t as well as the habitual y
	// and enter. Each key needs its own file since trashing consumes it.
	for _, k := range []tea.KeyPressMsg{
		{Code: 't'},
		{Code: 'y'},
		{Code: tea.KeyEnter},
	} {
		t.Run(k.String(), func(t *testing.T) {
			m, dir := fixture(t)
			t.Setenv("XDG_DATA_HOME", t.TempDir())

			m = stageDelete(t, m, "alpha.txt")
			updated, cmd := m.Update(k)
			m = updated.(Model)

			if cmd == nil {
				t.Fatal("the prompt should dispatch a trash command")
			}
			requireNoOpErrors(t, runBatch(t, cmd))

			target := filepath.Join(dir, "alpha.txt")
			if _, err := os.Stat(target); !os.IsNotExist(err) {
				t.Error("the file should have moved out of the source directory")
			}
			if m.opResult == "" || !strings.HasPrefix(m.opResult, "trashed ") {
				t.Errorf("opResult = %q, want a trashed message", m.opResult)
			}
		})
	}
}

func TestDeleteForceRemovesPermanently(t *testing.T) {
	m, dir := fixture(t)

	target := filepath.Join(dir, "alpha.txt")
	m = stageDelete(t, m, "alpha.txt")

	updated, cmd := m.Update(key('f'))
	m = updated.(Model)

	if m.opResult != "deleted alpha.txt" {
		t.Errorf("opResult = %q, want %q", m.opResult, "deleted alpha.txt")
	}
	requireNoOpErrors(t, runBatch(t, cmd))
	if _, err := os.Stat(target); !os.IsNotExist(err) {
		t.Error("force delete should remove the file")
	}
}

func TestDeleteCancelKeepsFile(t *testing.T) {
	m, dir := fixture(t)

	target := filepath.Join(dir, "alpha.txt")
	m = stageDelete(t, m, "alpha.txt")

	updated, cmd := m.Update(tea.KeyPressMsg{Code: tea.KeyEscape})
	m = updated.(Model)

	if cmd != nil {
		t.Error("cancelling should not dispatch a command")
	}
	if m.confirmDelete {
		t.Error("cancelling should close the prompt")
	}
	if _, err := os.Stat(target); err != nil {
		t.Errorf("cancelling must leave the file in place: %v", err)
	}
}

func TestDeletePromptLabelsOfferTrash(t *testing.T) {
	m, _ := fixture(t)
	m.width, m.height = 100, 30
	m = stageDelete(t, m, "alpha.txt")

	content := m.View().Content
	if !strings.Contains(content, "(t)rash") {
		t.Error("prompt should offer the safe trash option")
	}
	if !strings.Contains(content, "(f)orce") {
		t.Error("prompt should offer force delete")
	}
	if !strings.Contains(content, "alpha.txt") {
		t.Error("prompt should name the target")
	}
}

func TestDeleteOnDirectoryTrees(t *testing.T) {
	m, dir := fixture(t)
	t.Setenv("XDG_DATA_HOME", t.TempDir())

	m = stageDelete(t, m, "subdir")
	updated, cmd := m.Update(key('t'))
	m = updated.(Model)
	requireNoOpErrors(t, runBatch(t, cmd))

	if _, err := os.Stat(filepath.Join(dir, "subdir")); !os.IsNotExist(err) {
		t.Error("trashing a directory should move the whole tree")
	}
}
