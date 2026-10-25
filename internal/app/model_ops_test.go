package app

import (
	"archive/zip"
	"errors"
	"os"
	"path/filepath"
	"strings"
	"testing"

	"github.com/bjornramberg/omakaiju/internal/fs"

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
	updated, cmd := m.Update(tea.KeyPressMsg(tea.Key{Code: tea.KeyEnter}))
	return updated.(Model), cmd
}

func esc(t *testing.T, m Model) Model {
	t.Helper()
	updated, _ := m.Update(tea.KeyPressMsg(tea.Key{Code: tea.KeyEscape}))
	return updated.(Model)
}

func TestAddCreatesFile(t *testing.T) {
	m, dir := fixture(t)

	updated, _ := m.Update(shiftKey('a'))
	m = updated.(Model)
	if m.mode != inputAdd {
		t.Fatal("Shift+A should enter add mode")
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

	updated, _ := m.Update(shiftKey('a'))
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

	updated, _ := m.Update(shiftKey('a'))
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

	updated, _ := m.Update(shiftKey('a'))
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
		updated, _ := m.Update(arrow(tea.KeyDown))
		m = updated.(Model)
	}

	updated, _ := m.Update(shiftKey('r'))
	m = updated.(Model)
	if m.mode != inputRename {
		t.Fatal("Shift+R should enter rename mode")
	}
	if m.inputBuf != "alpha.txt" {
		t.Errorf("inputBuf = %q, want the current name pre-filled", m.inputBuf)
	}

	// Clear the pre-filled name, then type a new one.
	for i := 0; i < len("alpha.txt"); i++ {
		updated, _ = m.Update(tea.KeyPressMsg(tea.Key{Code: tea.KeyBackspace}))
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
		updated, _ := m.Update(arrow(tea.KeyDown))
		m = updated.(Model)
	}
	updated, _ := m.Update(shiftKey('r'))
	m = updated.(Model)
	for i := 0; i < len("alpha.txt"); i++ {
		updated, _ = m.Update(tea.KeyPressMsg(tea.Key{Code: tea.KeyBackspace}))
		m = updated.(Model)
	}
	m = typeString(t, m, "nope.txt")
	m = esc(t, m)

	if m.mode != inputNone {
		t.Error("esc should leave rename mode")
	}
	if m.renameTarget != "" || m.inputBuf != "" {
		t.Errorf("esc should clear the pending rename: target=%q input=%q", m.renameTarget, m.inputBuf)
	}
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
		updated, _ := m.Update(arrow(tea.KeyDown))
		m = updated.(Model)
	}
	updated, _ := m.Update(shiftKey('r'))
	m = updated.(Model)
	m, cmd := enter(t, m)

	if cmd != nil {
		t.Error("renaming to the same name should not reload")
	}
	if m.mode != inputNone {
		t.Error("a no-op rename should still leave rename mode")
	}
	if m.renameTarget != "" || m.inputBuf != "" {
		t.Errorf("a no-op rename should clear the pending rename: target=%q input=%q", m.renameTarget, m.inputBuf)
	}
	if m.opResult != "" {
		t.Errorf("a no-op rename should not report an operation: opResult = %q", m.opResult)
	}
	if _, err := os.Stat(filepath.Join(dir, "alpha.txt")); err != nil {
		t.Fatalf("file should still exist: %v", err)
	}
}

func TestAddAndRenameTargetActivePaneOnly(t *testing.T) {
	m, dir := fixture(t)

	updated, _ := m.Update(tea.KeyPressMsg(tea.Key{Code: tea.KeyTab}))
	m = updated.(Model)
	if m.activePane != 1 {
		t.Fatal("tab should activate the right pane")
	}

	updated, _ = m.Update(shiftKey('a'))
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
		if len(m.failures) > 0 {
			t.Fatalf("file op failed: %s", m.result())
		}
	}
}

// runTransfer drives a steppable transfer — a copy or a move — to completion
// the way the event loop does, and returns the model with the result applied.
//
// A transfer that runs through the progress flow does not report back in one
// message: it reports a step at a time, and each step asks for the next. The
// loop here follows that chain until the transfer says it is done, so a test
// sees the same outcome the user does.
func runTransfer(t *testing.T, m Model, cmd tea.Cmd) Model {
	t.Helper()
	for i := 0; i < 10000 && cmd != nil; i++ {
		msg := cmd()
		if batch, ok := msg.(tea.BatchMsg); ok {
			cmd = nil
			for _, c := range batch {
				if c == nil {
					continue
				}
				switch inner := c().(type) {
				case fileOpMsg:
					updated, _ := m.Update(inner)
					m = updated.(Model)
				case copyProgressMsg:
					updated, next := m.Update(inner)
					m = updated.(Model)
					cmd = next
				}
			}
			continue
		}
		switch inner := msg.(type) {
		case fileOpMsg:
			updated, _ := m.Update(inner)
			m = updated.(Model)
			cmd = nil
		case copyProgressMsg:
			updated, next := m.Update(inner)
			m = updated.(Model)
			cmd = next
		default:
			cmd = nil
		}
	}
	return m
}

// applyOps feeds operation results back into the model the way the event loop
// does, so the final status text can be asserted.
func applyOps(t *testing.T, m Model, msgs []fileOpMsg) Model {
	t.Helper()
	for _, msg := range msgs {
		updated, _ := m.Update(msg)
		m = updated.(Model)
	}
	return m
}

// moveCursorTo advances the active pane cursor onto the named entry.
func moveCursorTo(t *testing.T, m Model, name string) Model {
	t.Helper()
	idx := indexOf(m.visibleFiles(m.activePane), name)
	if idx < 0 {
		t.Fatalf("%s missing from listing", name)
	}
	for i := 0; i < idx; i++ {
		updated, _ := m.Update(arrow(tea.KeyDown))
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

// crossDevicePane points the right pane at a directory on another filesystem,
// so a move into it has to copy the file across rather than rename it.
//
// This is the shape of a move onto a network share: the destination is not
// on the same filesystem, so the kernel refuses the rename.
func crossDevicePane(t *testing.T, m Model) (Model, string) {
	t.Helper()
	for _, candidate := range []string{"/dev/shm", "/run/shm"} {
		if err := os.MkdirAll(candidate, 0o755); err != nil {
			continue
		}
		dst, err := os.MkdirTemp(candidate, "omakaiju-share-")
		if err != nil {
			continue
		}
		// The pane has to be on a genuinely different filesystem, or the
		// test would pass without exercising the copy at all.
		probe := filepath.Join(dst, "probe")
		if err := os.WriteFile(probe, []byte("x"), 0o644); err != nil {
			os.RemoveAll(dst)
			continue
		}
		if err := os.Rename(probe, filepath.Join(m.rightPath, "omakaiju-probe")); err == nil {
			os.Remove(filepath.Join(m.rightPath, "omakaiju-probe"))
			os.RemoveAll(dst)
			continue
		}
		os.Remove(probe)

		files, err := fs.ReadDir(dst)
		if err != nil {
			os.RemoveAll(dst)
			continue
		}
		m.rightPath = dst
		m.rightFiles = files
		return m, dst
	}
	t.Skip("no second filesystem available for a cross-device move test")
	return m, ""
}

// A move onto another filesystem — a share, say — cannot be a rename. The
// file has to be copied across and the original removed, and the user has to
// be told that it worked.
//
// This is the move that used to fail with "invalid cross-device link" and
// leave the user with no idea what had happened.
func TestMoveToAnotherFilesystemCopiesAndReports(t *testing.T) {
	m, src, _ := twoPaneFixture(t)
	m, dst := crossDevicePane(t, m)

	m = moveCursorTo(t, m, "alpha.txt")
	updated, _ := m.Update(shiftKey('m'))
	m = updated.(Model)

	updated, _ = m.Update(tea.KeyPressMsg(tea.Key{Code: tea.KeyTab}))
	m = updated.(Model)

	updated, cmd := m.Update(shiftKey('v'))
	m = updated.(Model)

	m = runTransfer(t, m, cmd)

	if m.opErr != nil {
		t.Fatalf("the move failed: %v", m.opErr)
	}
	if m.opResult != "moved 1 file(s)" {
		t.Errorf("opResult = %q, want %q", m.opResult, "moved 1 file(s)")
	}

	landed := filepath.Join(dst, "alpha.txt")
	data, err := os.ReadFile(landed)
	if err != nil {
		t.Fatalf("the file did not arrive on the other filesystem: %v", err)
	}
	if len(data) == 0 {
		t.Error("the file arrived empty")
	}
	if _, err := os.Stat(filepath.Join(src, "alpha.txt")); !os.IsNotExist(err) {
		t.Errorf("the original should be gone after a successful move: %v", err)
	}
}

// A move that cannot cross to the other filesystem has to leave the original
// exactly where it was, and say so.
func TestMoveToAnotherFilesystemThatFailsKeepsTheSource(t *testing.T) {
	if os.Geteuid() == 0 {
		t.Skip("root ignores directory permissions")
	}
	m, src, _ := twoPaneFixture(t)
	m, dst := crossDevicePane(t, m)

	m = moveCursorTo(t, m, "alpha.txt")
	updated, _ := m.Update(shiftKey('m'))
	m = updated.(Model)

	updated, _ = m.Update(tea.KeyPressMsg(tea.Key{Code: tea.KeyTab}))
	m = updated.(Model)

	// The destination refuses writes, so the copy cannot land.
	if err := os.Chmod(dst, 0o555); err != nil {
		t.Fatal(err)
	}
	defer os.Chmod(dst, 0o755)

	updated, cmd := m.Update(shiftKey('v'))
	m = updated.(Model)

	m = runTransfer(t, m, cmd)

	if m.opErr == nil {
		t.Fatal("a move into a directory that cannot be written to should be reported")
	}
	if _, err := os.Stat(filepath.Join(src, "alpha.txt")); err != nil {
		t.Errorf("a failed move must leave the source in place: %v", err)
	}
}

// A move into a directory the user cannot write to has to say so, name the
// file, and keep it selected so the move can be tried again once the
// permissions are fixed.
//
// It used to show nothing at all: the error was written to the same field the
// pane reload cleared a moment later.
func TestMoveWithoutPermissionIsReportedAndKeepsTheMark(t *testing.T) {
	if os.Geteuid() == 0 {
		t.Skip("root ignores directory permissions")
	}
	m, src, dst := twoPaneFixture(t)

	m = moveCursorTo(t, m, "alpha.txt")
	updated, _ := m.Update(shiftKey('m'))
	m = updated.(Model)

	updated, _ = m.Update(tea.KeyPressMsg(tea.Key{Code: tea.KeyTab}))
	m = updated.(Model)

	// The destination refuses writes.
	if err := os.Chmod(dst, 0o555); err != nil {
		t.Fatal(err)
	}
	defer os.Chmod(dst, 0o755)

	updated, cmd := m.Update(shiftKey('v'))
	m = updated.(Model)

	m = runTransfer(t, m, cmd)

	if m.opErr == nil {
		t.Fatal("a move into a directory that cannot be written to should be reported")
	}
	got := m.opErr.Error()
	if !strings.Contains(got, "alpha.txt") {
		t.Errorf("opErr = %q, want the failing file named", got)
	}
	if !strings.Contains(got, "permission denied") {
		t.Errorf("opErr = %q, want the reason the user can act on", got)
	}
	// The failure is still on screen after the panes reload.
	if m.opResult != "" {
		t.Errorf("opResult = %q, want the failure reported as an error", m.opResult)
	}

	// The entry stays selected, so the move can be retried.
	key := filepath.Join(src, "alpha.txt")
	if _, ok := m.marked[key]; !ok {
		t.Errorf("the failed move should stay marked for a retry, marked = %v", m.marked)
	}
	if _, err := os.Stat(key); err != nil {
		t.Errorf("a failed move must leave the source in place: %v", err)
	}
}

func TestMarkTogglesOnAndOff(t *testing.T) {
	m, dir := fixture(t)
	m = moveCursorTo(t, m, "alpha.txt")

	updated, _ := m.Update(shiftKey('m'))
	m = updated.(Model)
	if len(m.marked) != 1 {
		t.Fatalf("marked = %d, want 1", len(m.marked))
	}
	if _, ok := m.marked[filepath.Join(dir, "alpha.txt")]; !ok {
		t.Error("alpha.txt should be marked")
	}

	updated, _ = m.Update(shiftKey('m'))
	m = updated.(Model)
	if len(m.marked) != 0 {
		t.Fatalf("second Shift+M should unmark, marked = %d", len(m.marked))
	}
}

func TestMarksAccumulate(t *testing.T) {
	m, _ := fixture(t)

	for _, name := range []string{"alpha.txt", "beta.txt"} {
		m = moveCursorTo(t, m, name)
		updated, _ := m.Update(shiftKey('m'))
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
		updated, _ := m.Update(shiftKey('m'))
		m = updated.(Model)
	}

	// Marks land in whatever pane is highlighted at paste time, so the
	// destination is picked by tabbing over before Shift+V.
	updated, _ := m.Update(tea.KeyPressMsg(tea.Key{Code: tea.KeyTab}))
	m = updated.(Model)

	updated, cmd := m.Update(shiftKey('v'))
	m = updated.(Model)
	if cmd == nil {
		t.Fatal("paste should return move commands")
	}
	if len(m.marked) != 0 {
		t.Errorf("marks should clear after paste, got %d", len(m.marked))
	}

	// Drain the returned command so the moves actually run. A move reports
	// its outcome once every entry has been attempted.
	m = runTransfer(t, m, cmd)
	if m.opErr != nil {
		t.Fatalf("move failed: %v", m.opErr)
	}
	if m.opResult != "moved 2 file(s)" {
		t.Errorf("opResult = %q, want %q", m.opResult, "moved 2 file(s)")
	}

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
	updated, _ := m.Update(shiftKey('m'))
	m = updated.(Model)
	m = moveCursorTo(t, m, "beta.txt")
	updated, _ = m.Update(shiftKey('m'))
	m = updated.(Model)

	// Delete one marked file behind the model's back.
	if err := os.Remove(filepath.Join(src, "beta.txt")); err != nil {
		t.Fatal(err)
	}

	updated, _ = m.Update(tea.KeyPressMsg(tea.Key{Code: tea.KeyTab}))
	m = updated.(Model)

	updated, cmd := m.Update(shiftKey('v'))
	m = updated.(Model)

	// The move has been dispatched but has not run, so it must not claim a
	// result yet.
	if m.opResult != "moving 1 file(s)..." {
		t.Errorf("opResult = %q, want the in-flight progress notice", m.opResult)
	}
	m = runTransfer(t, m, cmd)
	if m.opErr != nil {
		t.Fatalf("move failed: %v", m.opErr)
	}
	if m.opResult != "moved 1 file(s), skipped 1" {
		t.Errorf("opResult = %q, want %q", m.opResult, "moved 1 file(s), skipped 1")
	}
	if _, err := os.Stat(filepath.Join(dst, "alpha.txt")); err != nil {
		t.Errorf("alpha.txt should have moved: %v", err)
	}
}
func TestPasteSkipsEntryAlreadyInTarget(t *testing.T) {
	m, _, _ := twoPaneFixture(t)

	m = moveCursorTo(t, m, "alpha.txt")
	updated, _ := m.Update(shiftKey('m'))
	m = updated.(Model)

	// The marked entry already sits in the active pane's directory, so the
	// paste has nothing to redistribute. The marks must survive: they are
	// still staged for a paste into the other pane, exactly as a yanked
	// selection survives the same case.
	updated, _ = m.Update(shiftKey('v'))
	m = updated.(Model)

	if m.opResult != "already in this directory" {
		t.Errorf("opResult = %q, want %q", m.opResult, "already in this directory")
	}
	if len(m.marked) != 1 {
		t.Errorf("marked = %d, want the mark kept for a paste elsewhere", len(m.marked))
	}
}

// seedPane writes files into a directory and refreshes the pane showing it,
// so an entry that lands there sorts between two others. Without them a
// pasted file would sit at index 0 and "the cursor follows the paste" would
// pass whether the feature worked or not.
func seedPane(t *testing.T, m Model, pane int, dir string, names ...string) Model {
	t.Helper()
	for _, name := range names {
		if err := os.WriteFile(filepath.Join(dir, name), []byte("x"), 0o644); err != nil {
			t.Fatal(err)
		}
	}
	return loadDir(t, m, pane)
}

// wantCursorOn asserts the pane's cursor sits on the named entry, and that the
// entry is not the first row — the pane used to snap back there, so a test
// that passed at index 0 would prove nothing.
func wantCursorOn(t *testing.T, m Model, pane int, name string) int {
	t.Helper()
	idx := indexOf(m.visibleFiles(pane), name)
	if idx <= 0 {
		t.Fatalf("%s is at index %d, want it sorted below the first row", name, idx)
	}
	if m.rawCursor(pane) != idx {
		t.Errorf("cursor = %d, want %d (the %s that was just pasted)", m.rawCursor(pane), idx, name)
	}
	return idx
}

// A copy ends with the destination pane pointing at the file it wrote. It used
// to reload at the top of the listing, so the result of the operation was
// nowhere on screen.
func TestPasteLandsCursorOnTheCopiedFile(t *testing.T) {
	m, _, dst := twoPaneFixture(t)
	m = seedPane(t, m, 1, dst, "aaa.txt", "zzz.txt")

	m = yankCopy(t, m, "beta.txt")
	updated, _ := m.Update(tea.KeyPressMsg(tea.Key{Code: tea.KeyTab}))
	m = updated.(Model)

	updated, cmd := m.Update(shiftKey('v'))
	m = updated.(Model)
	requireNoOpErrors(t, runBatch(t, cmd))

	m = loadDir(t, m, 1)
	wantCursorOn(t, m, 1, "beta.txt")
}

// Moving a marked set dispatches one operation per entry and every one of them
// reloads the panes, so all of those reloads have to agree on the row. Letting
// the last one decide would leave the cursor wherever the scheduling happened
// to put it.
func TestMarksMoveLandsCursorOnTheMovedSet(t *testing.T) {
	m, _, dst := twoPaneFixture(t)
	m = seedPane(t, m, 1, dst, "aaa.txt", "zzz.txt")

	// Marked back to front, so the entry the cursor lands on is not the one
	// that was marked first.
	m = moveCursorTo(t, m, "beta.txt")
	updated, _ := m.Update(shiftKey('m'))
	m = updated.(Model)
	updated, _ = m.Update(arrow(tea.KeyUp))
	m = updated.(Model)
	updated, _ = m.Update(shiftKey('m'))
	m = updated.(Model)
	if len(m.marked) != 2 {
		t.Fatalf("marked = %d, want 2", len(m.marked))
	}

	updated, _ = m.Update(tea.KeyPressMsg(tea.Key{Code: tea.KeyTab}))
	m = updated.(Model)
	updated, cmd := m.Update(shiftKey('v'))
	m = updated.(Model)
	requireNoOpErrors(t, runBatch(t, cmd))

	// The marks come out of a map, so the entry chosen has to be stable
	// rather than "whichever move reported back last".
	m = loadDir(t, m, 1)
	want := wantCursorOn(t, m, 1, "alpha.txt")

	again := loadDir(t, m, 1)
	if again.rawCursor(1) != want {
		t.Errorf("cursor = %d on the second reload, want %d", again.rawCursor(1), want)
	}
}

// The landing only holds until the user acts. A reload that arrives after the
// cursor has been moved by hand — the rest of a multi-entry paste, say — must
// not drag it back onto the entry the paste named.
func TestCursorMoveBeatsThePasteRestore(t *testing.T) {
	m, _, dst := twoPaneFixture(t)
	m = seedPane(t, m, 1, dst, "aaa.txt", "zzz.txt")

	m = yankCopy(t, m, "beta.txt")
	updated, _ := m.Update(tea.KeyPressMsg(tea.Key{Code: tea.KeyTab}))
	m = updated.(Model)
	updated, cmd := m.Update(shiftKey('v'))
	m = updated.(Model)
	requireNoOpErrors(t, runBatch(t, cmd))

	m = loadDir(t, m, 1)
	pasted := wantCursorOn(t, m, 1, "beta.txt")

	updated, _ = m.Update(arrow(tea.KeyDown))
	m = updated.(Model)
	if m.rawCursor(1) == pasted {
		t.Fatal("the cursor did not move off the pasted entry")
	}

	m = loadDir(t, m, 1)
	if m.rawCursor(1) == pasted {
		t.Error("a reload dragged the cursor back onto the pasted entry after the user moved off it")
	}
}

// The steppable copier finishes long after the key that started it, so the
// destination is only aimed at the copied file once the job reports back.
func TestLargeCopyLandsCursorOnTheCopiedFile(t *testing.T) {
	m, _, dst := twoPaneFixture(t)
	m = seedPane(t, m, 1, dst, "aaa.txt", "zzz.txt")

	// Larger than the 1 MiB threshold so the progress copier is used.
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
	updated, _ := m.Update(tea.KeyPressMsg(tea.Key{Code: tea.KeyTab}))
	m = updated.(Model)
	updated, _ = m.Update(shiftKey('v'))
	m = updated.(Model)

	copier, err := fs.NewCopier([]string{big}, dst)
	if err != nil {
		t.Fatal(err)
	}
	updated, _ = m.Update(copyProgressMsg{copier: copier, gen: m.copyGen})
	m = updated.(Model)

	for {
		msg, ok := stepCopyCmd(m.copier, m.copyGen)().(copyProgressMsg)
		if !ok {
			t.Fatal("expected progress messages while stepping")
		}
		updated, _ = m.Update(msg)
		m = updated.(Model)
		if msg.done {
			break
		}
	}

	m = loadDir(t, m, 1)
	wantCursorOn(t, m, 1, "big.bin")
}

// Pasting without leaving the pane, after navigating around, targets the
// active pane's current directory: the file the user sees is where it lands.
func TestYankPasteIntoNavigatedSamePane(t *testing.T) {
	m, src, dst := twoPaneFixture(t)

	// The fixture already provides "subdir", so the active pane's cursor can
	// be pointed at it directly, like the user having entered it.
	sub := filepath.Join(src, "subdir")

	m = yankCopy(t, m, "alpha.txt")
	m.setPath(0, sub)

	updated, cmd := m.Update(shiftKey('v'))
	m = updated.(Model)
	requireNoOpErrors(t, runBatch(t, cmd))

	if _, err := os.Stat(filepath.Join(sub, "alpha.txt")); err != nil {
		t.Errorf("copy missing from the navigated directory: %v", err)
	}
	if _, err := os.Stat(filepath.Join(src, "alpha.txt")); err != nil {
		t.Errorf("a copy must leave the original in place: %v", err)
	}
	if _, err := os.Stat(filepath.Join(dst, "alpha.txt")); err == nil {
		t.Error("the other pane must not receive anything while inactive")
	}
}

// Yanking and pasting without going anywhere is refused with the clipboard
// still armed — most importantly with the source file byte-for-byte intact,
// since a naive same-path copy would truncate it before reading.
func TestYankPasteSameDirectorySkips(t *testing.T) {
	m, src, _ := twoPaneFixture(t)

	target := filepath.Join(src, "alpha.txt")
	want, err := os.ReadFile(target)
	if err != nil {
		t.Fatal(err)
	}

	m = yankCopy(t, m, "alpha.txt")
	updated, cmd := m.Update(shiftKey('v'))
	m = updated.(Model)

	if cmd != nil {
		t.Error("nothing should be dispatched for a same-directory paste")
	}
	if m.opResult != "already in this directory" {
		t.Errorf("opResult = %q, want %q", m.opResult, "already in this directory")
	}
	if !m.clipboardOn {
		t.Error("the clipboard should stay armed so tabbing elsewhere can still paste")
	}

	got, err := os.ReadFile(target)
	if err != nil {
		t.Fatalf("source file unreadable: %v", err)
	}
	if string(got) != string(want) {
		t.Error("the source file must survive an in-place paste attempt unchanged")
	}
}

func TestYankPasteStillWorksWithoutMarks(t *testing.T) {
	m, _, dst := twoPaneFixture(t)

	m = moveCursorTo(t, m, "alpha.txt")
	updated, _ := m.Update(shiftKey('c'))
	m = updated.(Model)

	// Paste targets the highlighted pane, so tab over first.
	updated, _ = m.Update(tea.KeyPressMsg(tea.Key{Code: tea.KeyTab}))
	m = updated.(Model)

	updated, cmd := m.Update(shiftKey('v'))
	m = updated.(Model)

	if m.opResult != "copying 1 file(s)..." {
		t.Errorf("opResult = %q, want the in-flight progress notice", m.opResult)
	}
	msgs := runBatch(t, cmd)
	requireNoOpErrors(t, msgs)
	m = applyOps(t, m, msgs)
	if m.opResult != "pasted 1 file(s)" {
		t.Errorf("opResult = %q, want %q", m.opResult, "pasted 1 file(s)")
	}
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
	m = settlePreview(t, m)
	if m.previewPath != zipPath {
		t.Fatalf("previewPath = %q, want %q", m.previewPath, zipPath)
	}
	if len(m.previewContent.Archive) == 0 {
		t.Fatal("expected a cached archive listing")
	}
	if m.previewCachePath != zipPath {
		t.Errorf("previewCachePath = %q, want %q", m.previewCachePath, zipPath)
	}
	if len(m.previewContent.Archive) == 0 || m.previewContent.Archive[0].Name != "hello.txt" {
		t.Errorf("cached entries = %v, want hello.txt", m.previewContent.Archive)
	}

	// Re-running the preview update with the same selection must reuse the
	// cached listing rather than re-listing the archive.
	before := m.previewContent.Archive[0].Name
	if cmd := m.updatePreview(); cmd != nil {
		t.Error("an unchanged selection should not reload the preview")
	}
	if len(m.previewContent.Archive) == 0 || m.previewContent.Archive[0].Name != before {
		t.Error("archive listing should be reused while the selection is unchanged")
	}
	if m.previewCachePath != zipPath {
		t.Errorf("previewCachePath = %q, want %q", m.previewCachePath, zipPath)
	}

	// Rendering must not disturb the cache.
	m.width, m.height = 120, 40
	_ = m.View()
	if len(m.previewContent.Archive) == 0 || m.previewContent.Archive[0].Name != before {
		t.Error("View should not re-list the archive")
	}
}

// yankCopy stages a file and returns a model ready for paste.
func yankCopy(t *testing.T, m Model, name string) Model {
	t.Helper()
	m = moveCursorTo(t, m, name)
	updated, _ := m.Update(shiftKey('c'))
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
	updated, _ := m.Update(tea.KeyPressMsg(tea.Key{Code: tea.KeyTab}))
	m = updated.(Model)
	updated, cmd := m.Update(shiftKey('v'))
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
	updated, _ := m.Update(tea.KeyPressMsg(tea.Key{Code: tea.KeyTab}))
	m = updated.(Model)
	updated, cmd := m.Update(shiftKey('v'))
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
	updated, _ = m.Update(copyProgressMsg{copier: copier, gen: m.copyGen})
	m = updated.(Model)
	if m.copier == nil {
		t.Error("model should adopt the copier")
	}

	for {
		next := stepCopyCmd(m.copier, m.copyGen)
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
	updated, _ := m.Update(copyProgressMsg{copier: copier, gen: m.copyGen})
	m = updated.(Model)

	// Take one step so partial data exists. The copy may write it straight
	// to the destination or to a scratch file beside it, so assert on the
	// directory rather than on one implementation's path.
	msg := stepCopyCmd(m.copier, m.copyGen)().(copyProgressMsg)
	updated, _ = m.Update(msg)
	m = updated.(Model)

	partials, err := os.ReadDir(dst)
	if err != nil {
		t.Fatal(err)
	}
	if len(partials) == 0 {
		t.Fatal("expected partial copy data mid-copy")
	}

	updated, _ = m.Update(tea.KeyPressMsg(tea.Key{Code: tea.KeyEscape}))
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
	leftovers, err := os.ReadDir(dst)
	if err != nil {
		t.Fatal(err)
	}
	if len(leftovers) != 0 {
		t.Errorf("cancel left %d entr(ies) behind: %v", len(leftovers), leftovers)
	}
}

// A copy that was cancelled still has a step in flight, and its message
// arrives after the user has moved on. It must not resurrect the dead
// copier: that would block the keys again and report a copy that never
// finished.
func TestStaleCopyMessageIsDropped(t *testing.T) {
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
	gen := m.copyGen
	updated, _ := m.Update(copyProgressMsg{copier: copier, gen: gen})
	m = updated.(Model)

	// A step is in flight. The user cancels.
	inFlight := stepCopyCmd(m.copier, gen)().(copyProgressMsg)
	updated, _ = m.Update(tea.KeyPressMsg(tea.Key{Code: tea.KeyEscape}))
	m = updated.(Model)

	if m.copier != nil {
		t.Fatal("cancel should release the copier")
	}

	// The in-flight message now arrives. It belongs to a copy that no
	// longer exists.
	updated, cmd := m.Update(inFlight)
	m = updated.(Model)

	if m.copier != nil {
		t.Error("a stale message must not resurrect the cancelled copier")
	}
	if cmd != nil {
		t.Error("a stale message must not dispatch another step")
	}
	if m.opResult != "copy cancelled" {
		t.Errorf("opResult = %q, want the cancellation notice to survive", m.opResult)
	}
	if strings.Contains(m.opResult, "copied") {
		t.Errorf("the stale message reported a copy that never finished: %q", m.opResult)
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
	updated, _ = m.Update(arrow(tea.KeyDown))
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
	updated, _ := m.Update(shiftKey('d'))
	m = updated.(Model)
	if !m.confirmDelete {
		t.Fatalf("Shift+D should open the delete prompt for %s", name)
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
			msgs := runBatch(t, cmd)
			requireNoOpErrors(t, msgs)
			m = applyOps(t, m, msgs)

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

	if m.opResult != "deleting alpha.txt..." {
		t.Errorf("opResult = %q, want the in-flight progress notice", m.opResult)
	}
	msgs := runBatch(t, cmd)
	requireNoOpErrors(t, msgs)
	m = applyOps(t, m, msgs)
	if m.opResult != "deleted alpha.txt" {
		t.Errorf("opResult = %q, want %q", m.opResult, "deleted alpha.txt")
	}
	if _, err := os.Stat(target); !os.IsNotExist(err) {
		t.Error("force delete should remove the file")
	}
}

func TestDeleteCancelKeepsFile(t *testing.T) {
	m, dir := fixture(t)

	target := filepath.Join(dir, "alpha.txt")
	m = stageDelete(t, m, "alpha.txt")

	updated, cmd := m.Update(tea.KeyPressMsg(tea.Key{Code: tea.KeyEscape}))
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

	if m.confirmDelete {
		t.Error("trashing should close the delete prompt")
	}
	if m.deleteTarget != "" {
		t.Errorf("trashing should clear the delete target, got %q", m.deleteTarget)
	}
	if _, err := os.Stat(filepath.Join(dir, "subdir")); !os.IsNotExist(err) {
		t.Error("trashing a directory should move the whole tree")
	}
}

// A terminal too small to host the layout must render the placeholder rather
// than handing Lipgloss zero or negative dimensions.
func TestViewShowsPlaceholderWhenTooSmall(t *testing.T) {
	m, _ := fixture(t)

	for _, tc := range []struct{ w, h int }{{0, 0}, {10, 5}, {39, 40}, {80, 4}} {
		m.width, m.height = tc.w, tc.h
		out := m.View().Content
		// Below 20 columns the message itself cannot fit, so there is
		// nothing meaningful to assert beyond it not panicking.
		if tc.w < 20 {
			continue
		}
		if !strings.Contains(out, "too small") {
			t.Errorf("%dx%d: expected the too-small placeholder, got %q", tc.w, tc.h, out)
		}
	}
}

func TestViewRendersNormallyWhenSized(t *testing.T) {
	m, dir := fixture(t)
	m.width, m.height = 120, 40
	out := m.View().Content
	if strings.Contains(out, "too small") {
		t.Error("a normal terminal should not show the placeholder")
	}
	if !strings.Contains(out, dir) {
		t.Error("top bar should show the active path")
	}
}

// cursorOn moves the active pane cursor onto a named entry.
func cursorOn(t *testing.T, m Model, name string) Model {
	t.Helper()
	m = moveCursorTo(t, m, name)
	idx := indexOf(m.visibleFiles(m.activePane), name)
	if m.rawCursor(m.activePane) != idx {
		t.Fatalf("cursor setup failed for %s", name)
	}
	return m
}

func TestEnterOnFileDispatchesOpen(t *testing.T) {
	m, _ := fixture(t)
	m = cursorOn(t, m, "alpha.txt")

	_, cmd := m.Update(tea.KeyPressMsg(tea.Key{Code: tea.KeyEnter}))

	if cmd == nil {
		t.Fatal("enter on a file should dispatch an open command")
	}
}

// Opening must not disturb the listing or move the cursor, which is why openMsg
// is separate from fileOpMsg: that one reloads the panes.
func TestOpenResultPreservesCursorAndListing(t *testing.T) {
	m, dir := fixture(t)
	m = cursorOn(t, m, "beta.txt")
	cursorBefore := m.leftCursor
	filesBefore := len(m.leftFiles)

	updated, _ := m.Update(openMsg{name: "beta.txt"})
	m = updated.(Model)

	if m.leftCursor != cursorBefore {
		t.Errorf("leftCursor = %d, want %d preserved", m.leftCursor, cursorBefore)
	}
	if len(m.leftFiles) != filesBefore {
		t.Errorf("listing changed: %d entries, want %d", len(m.leftFiles), filesBefore)
	}
	if m.opResult != "opening beta.txt..." {
		t.Errorf("opResult = %q, want %q", m.opResult, "opening beta.txt...")
	}
	if m.loadErr != nil {
		t.Errorf("unexpected load error: %v", m.loadErr)
	}
	if m.leftPath != dir {
		t.Errorf("leftPath = %q, want %q", m.leftPath, dir)
	}
}

func TestOpenFailureSurfacesError(t *testing.T) {
	m, dir := fixture(t)
	m = cursorOn(t, m, "alpha.txt")
	cursorBefore := m.leftCursor

	updated, _ := m.Update(openMsg{name: "alpha.txt", err: errors.New("no handler")})
	m = updated.(Model)

	if m.opErr == nil {
		t.Fatal("a failed open should surface an error")
	}
	if m.loadErr != nil {
		t.Errorf("an open failure is not a listing failure: loadErr = %v", m.loadErr)
	}
	if m.opResult != "" {
		t.Errorf("opResult = %q, want cleared on failure", m.opResult)
	}
	if m.leftCursor != cursorBefore {
		t.Error("a failed open should not move the cursor")
	}
	if m.leftPath != dir {
		t.Error("a failed open should not change the directory")
	}
}

func TestEnterOnDirectoryStillNavigates(t *testing.T) {
	m, dir := fixture(t)
	m = cursorOn(t, m, "subdir")

	_, cmd := m.Update(tea.KeyPressMsg(tea.Key{Code: tea.KeyEnter}))

	if cmd == nil {
		t.Fatal("enter on a directory should still dispatch a load")
	}
	if cmd().(dirLoadedMsg).path != filepath.Join(dir, "subdir") {
		t.Error("enter on a directory should navigate into it")
	}
}

// Enter on a symlink to a directory has to navigate. It used to fall through to
// the open path, which handed the link to gio and launched a separate file
// manager window instead of descending into the directory.
func TestEnterOnSymlinkToDirectoryNavigates(t *testing.T) {
	m, dir := fixture(t)

	target := filepath.Join(dir, "target")
	if err := os.Mkdir(target, 0o755); err != nil {
		t.Fatal(err)
	}
	link := filepath.Join(dir, "linkdir")
	if err := os.Symlink(target, link); err != nil {
		t.Skipf("symlinks unavailable: %v", err)
	}

	files, err := fs.ReadDir(dir)
	if err != nil {
		t.Fatal(err)
	}
	m.leftFiles = files
	m = cursorOn(t, m, "linkdir")

	updated, cmd := m.Update(tea.KeyPressMsg(tea.Key{Code: tea.KeyEnter}))
	m = updated.(Model)

	if cmd == nil {
		t.Fatal("enter on a symlinked directory should dispatch a load")
	}
	msg, ok := cmd().(dirLoadedMsg)
	if !ok {
		t.Fatalf("enter dispatched %T, want dirLoadedMsg", cmd())
	}
	if msg.path != link {
		t.Errorf("navigated to %q, want %q", msg.path, link)
	}
}

// The right arrow enters a directory without launching anything, and has to
// treat a symlinked directory the same way.
func TestRightOnSymlinkToDirectoryEnters(t *testing.T) {
	m, dir := fixture(t)

	target := filepath.Join(dir, "target")
	if err := os.Mkdir(target, 0o755); err != nil {
		t.Fatal(err)
	}
	link := filepath.Join(dir, "linkdir")
	if err := os.Symlink(target, link); err != nil {
		t.Skipf("symlinks unavailable: %v", err)
	}

	files, err := fs.ReadDir(dir)
	if err != nil {
		t.Fatal(err)
	}
	m.leftFiles = files
	m = cursorOn(t, m, "linkdir")

	updated, cmd := m.Update(arrow(tea.KeyRight))
	m = updated.(Model)

	if cmd == nil {
		t.Fatal("right on a symlinked directory should dispatch a load")
	}
	msg, ok := cmd().(dirLoadedMsg)
	if !ok {
		t.Fatalf("right dispatched %T, want dirLoadedMsg", cmd())
	}
	if msg.path != link {
		t.Errorf("entered %q, want %q", msg.path, link)
	}
	updated, _ = m.Update(msg)
	m = updated.(Model)
	if m.leftPath != link {
		t.Errorf("leftPath = %q, want %q", m.leftPath, link)
	}
}

func TestOpenMsgDoesNotReloadPanes(t *testing.T) {
	m, _ := fixture(t)
	m = cursorOn(t, m, "alpha.txt")

	// A fileOpMsg would reload both panes; openMsg must not.
	_, cmd := m.Update(openMsg{name: "alpha.txt"})
	if cmd != nil {
		t.Error("openMsg should not dispatch a reload command")
	}
}
