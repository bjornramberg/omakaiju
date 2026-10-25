package app

import (
	"errors"
	"fmt"
	"os"
	"path/filepath"
	"strings"
	"testing"

	tea "charm.land/bubbletea/v2"

	"github.com/bjornramberg/omakaiju/internal/fs"
)

// drainLoads runs the reload commands an operation dispatches and returns
// their results, so a test can feed them back into the model.
func drainLoads(t *testing.T, cmd tea.Cmd) []dirLoadedMsg {
	t.Helper()
	if cmd == nil {
		return nil
	}
	msg := cmd()
	batch, ok := msg.(tea.BatchMsg)
	if !ok {
		if load, ok := msg.(dirLoadedMsg); ok {
			return []dirLoadedMsg{load}
		}
		return nil
	}
	var out []dirLoadedMsg
	for _, c := range batch {
		if c == nil {
			continue
		}
		if load, ok := c().(dirLoadedMsg); ok {
			out = append(out, load)
		}
	}
	return out
}

// singleMsg runs a command that returns exactly one message.
func singleMsg(t *testing.T, cmd tea.Cmd) tea.Msg {
	t.Helper()
	if cmd == nil {
		t.Fatal("expected a command")
	}
	msg := cmd()
	if batch, ok := msg.(tea.BatchMsg); ok {
		if len(batch) != 1 || batch[0] == nil {
			t.Fatalf("expected one message, got a batch of %d", len(batch))
		}
		return batch[0]()
	}
	return msg
}

func permissionDenied(name string) *os.LinkError {
	return &os.LinkError{
		Op:  "rename",
		Old: filepath.Join("/home/a", name),
		New: filepath.Join("/mnt/share", name),
		Err: errors.New("permission denied"),
	}
}

// The bug: the failed-operation handler reloads the panes, and the successful
// reload used to clear the error before it was ever drawn. The operation error
// must outlive the reload the operation itself triggered.
func TestOperationErrorSurvivesPaneReload(t *testing.T) {
	m, _ := fixture(t)

	updated, cmd := m.Update(fileOpMsg{
		verb:     "moved",
		failures: []opFailure{{name: "photo.jpg", err: permissionDenied("photo.jpg")}},
	})
	m = updated.(Model)

	if m.opErr == nil {
		t.Fatal("a failed operation should record an error")
	}
	if !strings.Contains(m.opErr.Error(), "photo.jpg") {
		t.Errorf("opErr = %q, want the failing file named", m.opErr)
	}

	loads := drainLoads(t, cmd)
	if len(loads) == 0 {
		t.Fatal("a failed operation should still reload the panes")
	}
	for _, load := range loads {
		updated, _ = m.Update(load)
		m = updated.(Model)
	}

	if m.opErr == nil {
		t.Fatal("the pane reload erased the operation error")
	}
	if m.loadErr != nil {
		t.Errorf("loadErr = %v, want a successful listing to leave it nil", m.loadErr)
	}

	m.width, m.height = 120, 40
	view := m.View().Content
	if !strings.Contains(view, "Error:") || !strings.Contains(view, "photo.jpg") {
		t.Errorf("the failed operation is not visible in the bottom bar:\n%s", view)
	}
}

// A batch reports one aggregate result. Failures are named, success is
// counted, and the noisy os.LinkError prefix is stripped.
func TestBatchPartialFailureReportsCountsAndNames(t *testing.T) {
	base := t.TempDir()
	srcDir := filepath.Join(base, "src")
	dstDir := filepath.Join(base, "dst")
	for _, dir := range []string{srcDir, dstDir} {
		if err := os.MkdirAll(dir, 0o755); err != nil {
			t.Fatal(err)
		}
	}

	good := filepath.Join(srcDir, "keep.txt")
	if err := os.WriteFile(good, []byte("x"), 0o644); err != nil {
		t.Fatal(err)
	}
	gone := filepath.Join(srcDir, "gone.txt")

	items := []opItem{
		{name: "keep.txt", src: good, dst: filepath.Join(dstDir, "keep.txt")},
		{name: "gone.txt", src: gone, dst: filepath.Join(dstDir, "gone.txt")},
	}

	msg, ok := singleMsg(t, batchOpCmd("moved", items, 0, nil, nil, fs.Move)).(fileOpMsg)
	if !ok {
		t.Fatal("batchOpCmd should report a fileOpMsg")
	}
	if msg.succeeded != 1 || len(msg.failures) != 1 {
		t.Fatalf("succeeded = %d, failures = %d, want 1 and 1", msg.succeeded, len(msg.failures))
	}

	m := Model{}
	updated, _ := m.Update(msg)
	m = updated.(Model)

	if m.opErr == nil {
		t.Fatal("a partial batch failure should record an error")
	}
	got := m.opErr.Error()
	if !strings.Contains(got, "moved 1 file(s)") {
		t.Errorf("opErr = %q, want the successful count", got)
	}
	if !strings.Contains(got, "failed 1: gone.txt") {
		t.Errorf("opErr = %q, want the failure named", got)
	}
	if strings.Contains(got, "rename ") || strings.Contains(got, srcDir) {
		t.Errorf("opErr = %q, want the LinkError path prefix stripped", got)
	}
	if !strings.Contains(got, "no such file or directory") {
		t.Errorf("opErr = %q, want the underlying reason", got)
	}

	if _, err := os.Stat(filepath.Join(dstDir, "keep.txt")); err != nil {
		t.Errorf("the successful entry should have moved: %v", err)
	}
}

// Two failed entries must read the same whichever order the batch processed
// them in; only the counts and the names matter, not scheduling.
func TestBatchResultIsIndependentOfOrder(t *testing.T) {
	run := func(order []string) string {
		base := t.TempDir()
		srcDir := filepath.Join(base, "src")
		dstDir := filepath.Join(base, "dst")
		for _, dir := range []string{srcDir, dstDir} {
			if err := os.MkdirAll(dir, 0o755); err != nil {
				t.Fatal(err)
			}
		}
		good := filepath.Join(srcDir, "good.txt")
		if err := os.WriteFile(good, []byte("x"), 0o644); err != nil {
			t.Fatal(err)
		}

		items := make([]opItem, 0, len(order))
		for _, name := range order {
			src := filepath.Join(srcDir, name)
			items = append(items, opItem{name: name, src: src, dst: filepath.Join(dstDir, name)})
		}

		msg, ok := singleMsg(t, batchOpCmd("moved", items, 0, nil, nil, fs.Move)).(fileOpMsg)
		if !ok {
			t.Fatal("batchOpCmd should report a fileOpMsg")
		}
		m := Model{}
		updated, _ := m.Update(msg)
		m = updated.(Model)
		if m.opErr == nil {
			t.Fatal("expected failures to be recorded")
		}
		return m.opErr.Error()
	}

	forward := run([]string{"good.txt", "a.txt", "b.txt"})
	reverse := run([]string{"b.txt", "a.txt", "good.txt"})

	if forward != reverse {
		t.Errorf("result depends on order:\n forward = %q\n reverse = %q", forward, reverse)
	}
	if !strings.Contains(forward, "moved 1 file(s), failed 2:") {
		t.Errorf("result = %q, want accurate counts", forward)
	}
	if !strings.Contains(forward, "a.txt") || !strings.Contains(forward, "b.txt") {
		t.Errorf("result = %q, want every failing file named", forward)
	}
}

// The status must not claim a result before the operations have run.
func TestPasteDoesNotClaimSuccessBeforeMovesRun(t *testing.T) {
	m, _, dst := twoPaneFixture(t)

	for _, name := range []string{"alpha.txt", "beta.txt"} {
		m = moveCursorTo(t, m, name)
		updated, _ := m.Update(shiftKey('m'))
		m = updated.(Model)
	}
	updated, _ := m.Update(tea.KeyPressMsg(tea.Key{Code: tea.KeyTab}))
	m = updated.(Model)

	updated, cmd := m.Update(shiftKey('v'))
	m = updated.(Model)

	if m.opResult != "moving 2 file(s)..." {
		t.Errorf("opResult = %q, want the in-flight progress notice", m.opResult)
	}
	if strings.Contains(m.opResult, "moved") {
		t.Errorf("opResult = %q, claims success before the moves ran", m.opResult)
	}
	if _, err := os.Stat(filepath.Join(dst, "alpha.txt")); err == nil {
		t.Error("the move had already run when the status was set")
	}

	m = runTransfer(t, m, cmd)

	if m.opResult != "moved 2 file(s)" {
		t.Errorf("opResult = %q, want %q after completion", m.opResult, "moved 2 file(s)")
	}
	if m.opErr != nil {
		t.Errorf("opErr = %v, want nil after a successful batch", m.opErr)
	}
}

// Neither error may clear the other: a listing refresh leaves the operation
// error alone, and an operation result leaves the listing error alone.
func TestOperationAndLoadErrorsDoNotOverwriteEachOther(t *testing.T) {
	m, dir := fixture(t)

	updated, _ := m.Update(fileOpMsg{
		verb:     "moved",
		failures: []opFailure{{name: "photo.jpg", err: permissionDenied("photo.jpg")}},
	})
	m = updated.(Model)

	// A listing failure arrives next and must keep both errors.
	listErr := errors.New("open " + dir + ": permission denied")
	updated, _ = m.Update(dirLoadedMsg{pane: 0, path: dir, err: listErr})
	m = updated.(Model)

	if m.opErr == nil {
		t.Fatal("the listing failure cleared the operation error")
	}
	if m.loadErr == nil {
		t.Fatal("the listing failure was not recorded")
	}

	// With both pending, the operation error wins: it is what the user just
	// caused, and the listing error stays on the model for the next reload.
	m.width, m.height = 120, 40
	view := m.View().Content
	if !strings.Contains(view, "photo.jpg") {
		t.Errorf("the bar did not show the operation error:\n%s", view)
	}
	if strings.Contains(view, "open "+dir) {
		t.Errorf("the bar showed the listing error instead of the operation error:\n%s", view)
	}

	// A successful reload clears only the listing error.
	m = loadDir(t, m, 0)
	if m.loadErr != nil {
		t.Errorf("loadErr = %v, want a successful listing to clear it", m.loadErr)
	}
	if m.opErr == nil {
		t.Fatal("a successful listing cleared the operation error")
	}

	// Now the reverse: a pending listing error must survive an operation.
	updated, _ = m.Update(dirLoadedMsg{pane: 0, path: dir, err: listErr})
	m = updated.(Model)
	updated, _ = m.Update(fileOpMsg{verb: "moved", succeeded: 1})
	m = updated.(Model)

	if m.loadErr == nil {
		t.Error("a successful operation cleared the listing error")
	}
	if m.opErr != nil {
		t.Errorf("opErr = %v, want nil after a successful operation", m.opErr)
	}
	if m.opResult != "moved 1 file(s)" {
		t.Errorf("opResult = %q, want the operation result", m.opResult)
	}

	// The operation result is retained on the model, while the bar shows the
	// listing error: an operation *error* outranks a listing error, but a
	// successful result does not.
	m.width, m.height = 120, 40
	view = m.View().Content
	if !strings.Contains(view, "Error:") {
		t.Errorf("the pending listing error is not visible:\n%s", view)
	}
	if strings.Contains(view, "moved 1 file(s)") {
		t.Errorf("the bar should prefer the listing error over a result:\n%s", view)
	}
}

// A large copy that fails mid-flight reports through the operation error and
// must not be erased by the reload that follows.
func TestFailedCopyErrorSurvivesReload(t *testing.T) {
	m, _, _ := twoPaneFixture(t)

	updated, cmd := m.Update(copyProgressMsg{
		gen:  m.copyGen,
		err:  errors.New("no space left on device"),
		name: "huge.bin",
	})
	m = updated.(Model)

	if m.opErr == nil {
		t.Fatal("a failed copy should record an error")
	}
	got := m.opErr.Error()
	if !strings.Contains(got, "huge.bin") || !strings.Contains(got, "no space left on device") {
		t.Errorf("opErr = %q, want the file and the reason", got)
	}

	for _, load := range drainLoads(t, cmd) {
		updated, _ = m.Update(load)
		m = updated.(Model)
	}
	if m.opErr == nil {
		t.Fatal("the pane reload erased the copy failure")
	}
	if m.copier != nil {
		t.Error("a failed copy should release the copier")
	}
}

// The status text formats are part of the contract: counts for a clean batch,
// a named reason for a partial one, and a list when several fail.
func TestOperationResultText(t *testing.T) {
	cases := []struct {
		name string
		msg  fileOpMsg
		want string
	}{
		{
			name: "moved all",
			msg:  fileOpMsg{verb: "moved", succeeded: 2},
			want: "moved 2 file(s)",
		},
		{
			name: "copied all",
			msg:  fileOpMsg{verb: "copied", succeeded: 2},
			want: "copied 2 file(s)",
		},
		{
			name: "pasted all",
			msg:  fileOpMsg{verb: "pasted", succeeded: 2},
			want: "pasted 2 file(s)",
		},
		{
			name: "partial failure",
			msg: fileOpMsg{
				verb:      "moved",
				succeeded: 1,
				failures:  []opFailure{{name: "photo.jpg", err: permissionDenied("photo.jpg")}},
			},
			want: "moved 1 file(s), failed 1: photo.jpg (permission denied)",
		},
		{
			name: "several failures",
			msg: fileOpMsg{
				verb: "moved",
				failures: []opFailure{
					{name: "b.txt", err: errors.New("no space left on device")},
					{name: "a.txt", err: permissionDenied("a.txt")},
				},
			},
			want: "failed 2: a.txt (permission denied), b.txt (no space left on device)",
		},
		{
			name: "skipped",
			msg:  fileOpMsg{verb: "moved", succeeded: 1, skipped: 1},
			want: "moved 1 file(s), skipped 1",
		},
		{
			name: "single success",
			msg:  fileOpMsg{verb: "deleted", name: "alpha.txt", succeeded: 1},
			want: "deleted alpha.txt",
		},
		{
			name: "single failure",
			msg: fileOpMsg{
				verb:     "deleted",
				failures: []opFailure{{name: "alpha.txt", err: permissionDenied("alpha.txt")}},
			},
			want: "failed 1: alpha.txt (permission denied)",
		},
	}

	for _, tc := range cases {
		t.Run(tc.name, func(t *testing.T) {
			if got := tc.msg.result(); got != tc.want {
				t.Errorf("result = %q, want %q", got, tc.want)
			}
		})
	}
}

// A refused replacement has to keep the reason it was refused. fs reports one
// as a *fs.RefusedReplacement naming the destination that could not be kept;
// unwrapping that to the cause would leave only "operation not supported", so
// the user cannot tell a replacement refused because the old file could not be
// kept from a copy that failed some other way.
func TestRefusedReplacementKeepsItsContext(t *testing.T) {
	dst := filepath.Join("/mnt/share", "photo.jpg")
	reason := errors.New("operation not supported")
	// Wrapped the way an outer layer might hand it on: the type still has to
	// be found, and its message is what the user sees.
	err := fmt.Errorf("copy: %w", &fs.RefusedReplacement{
		Path: dst,
		Err: &os.LinkError{
			Op:  "link",
			Old: dst,
			New: filepath.Join("/mnt/share", ".kaiju-123"),
			Err: reason,
		},
	})

	var refused *fs.RefusedReplacement
	if !errors.As(err, &refused) {
		t.Fatalf("error = %T (%v), want a *fs.RefusedReplacement to be reachable", err, err)
	}

	got := readableOpError(err)
	if got != refused.Error() {
		t.Errorf("readableOpError = %q, want the refusal's own message %q", got, refused.Error())
	}
	if !strings.Contains(got, dst) {
		t.Errorf("readableOpError = %q, want the refused path named", got)
	}
	if !strings.Contains(got, reason.Error()) {
		t.Errorf("readableOpError = %q, want the underlying reason", got)
	}
	if strings.HasPrefix(got, "copy: ") {
		t.Errorf("readableOpError = %q, want the operation prefix trimmed", got)
	}

	// The same context has to reach the line the bottom bar builds.
	line := fileOpMsg{
		verb:     "copied",
		failures: []opFailure{{name: "photo.jpg", err: err}},
	}.result()
	if !strings.Contains(line, refused.Error()) || !strings.Contains(line, dst) || !strings.Contains(line, reason.Error()) {
		t.Errorf("result = %q, want the refusal, its path and its reason", line)
	}
}

// A failed rename is an operation failure, not a listing failure, and names
// the target the user typed.
func TestRenameFailureRoutesThroughOperationError(t *testing.T) {
	m, dir := fixture(t)

	m = moveCursorTo(t, m, "alpha.txt")
	updated, _ := m.Update(shiftKey('r'))
	m = updated.(Model)
	for i := 0; i < len("alpha.txt"); i++ {
		updated, _ = m.Update(tea.KeyPressMsg(tea.Key{Code: tea.KeyBackspace}))
		m = updated.(Model)
	}
	// A name containing a separator sends the rename into a directory that
	// does not exist, so os.Rename fails with a LinkError.
	m = typeString(t, m, "missing/renamed.txt")
	m, _ = enter(t, m)

	if m.opErr == nil {
		t.Fatal("a failed rename should record an operation error")
	}
	if m.loadErr != nil {
		t.Errorf("loadErr = %v, want a rename failure not to be a listing failure", m.loadErr)
	}
	got := m.opErr.Error()
	if !strings.Contains(got, "failed 1") || !strings.Contains(got, "renamed.txt") {
		t.Errorf("opErr = %q, want the failed rename named", got)
	}
	if strings.Contains(got, "rename ") || strings.Contains(got, dir) {
		t.Errorf("opErr = %q, want the LinkError prefix and paths stripped", got)
	}
}

// symlinkedPane points the right pane at a symlink for the left pane's
// directory. The two paths name the same files without being string-equal, so
// paste dispatches the operation rather than skipping it, and the filesystem
// reports that the source and destination are one file.
func symlinkedPane(t *testing.T, m Model, src string) Model {
	t.Helper()
	alias := filepath.Join(filepath.Dir(src), filepath.Base(src)+"-alias")
	if err := os.Symlink(src, alias); err != nil {
		t.Skipf("symlinks unavailable: %v", err)
	}
	files, err := fs.ReadDir(alias)
	if err != nil {
		t.Fatal(err)
	}
	m.rightPath = alias
	m.rightFiles = files
	return m
}

// A move onto the same file through another path must report the filesystem's
// error, and the mark must survive so the user can fix the destination and try
// again. It used to look like a silent success.
func TestFailedSameFileMoveKeepsMarkAndReportsError(t *testing.T) {
	m, src := fixture(t)
	m = symlinkedPane(t, m, src)

	m = moveCursorTo(t, m, "alpha.txt")
	updated, _ := m.Update(shiftKey('m'))
	m = updated.(Model)

	updated, _ = m.Update(tea.KeyPressMsg(tea.Key{Code: tea.KeyTab}))
	m = updated.(Model)

	updated, cmd := m.Update(shiftKey('v'))
	m = updated.(Model)

	// The selection is consumed while the batch runs, and comes back on
	// failure rather than being lost.
	if len(m.marked) != 0 {
		t.Fatalf("marks = %d, want them consumed while the move runs", len(m.marked))
	}

	m = runTransfer(t, m, cmd)

	if m.opErr == nil {
		t.Fatal("moving a file onto itself should be reported")
	}
	got := m.opErr.Error()
	if !strings.Contains(got, "same file") {
		t.Errorf("opErr = %q, want the filesystem's same-file error", got)
	}
	if !strings.Contains(got, "alpha.txt") {
		t.Errorf("opErr = %q, want the failing file named", got)
	}

	key := filepath.Join(src, "alpha.txt")
	if _, ok := m.marked[key]; !ok {
		t.Errorf("the failed move should stay marked for a retry, marked = %v", m.marked)
	}
	if _, err := os.Stat(key); err != nil {
		t.Errorf("a failed move must leave the source in place: %v", err)
	}
}

// Only the entries that failed come back into the marked set; the ones that
// landed stay moved.
func TestPartialBatchRestoresOnlyFailedMarks(t *testing.T) {
	base := t.TempDir()
	srcDir := filepath.Join(base, "src")
	dstDir := filepath.Join(base, "dst")
	for _, dir := range []string{srcDir, dstDir} {
		if err := os.MkdirAll(dir, 0o755); err != nil {
			t.Fatal(err)
		}
	}

	good := filepath.Join(srcDir, "keep.txt")
	if err := os.WriteFile(good, []byte("x"), 0o644); err != nil {
		t.Fatal(err)
	}
	gone := filepath.Join(srcDir, "gone.txt")

	marks := map[string]fs.Entry{
		good: {Name: "keep.txt", Path: good},
		gone: {Name: "gone.txt", Path: gone},
	}
	items := []opItem{
		{name: "keep.txt", src: good, dst: filepath.Join(dstDir, "keep.txt")},
		{name: "gone.txt", src: gone, dst: filepath.Join(dstDir, "gone.txt")},
	}

	msg, ok := singleMsg(t, batchOpCmd("moved", items, 0, marks, nil, fs.Move)).(fileOpMsg)
	if !ok {
		t.Fatal("batchOpCmd should report a fileOpMsg")
	}
	m := Model{}
	updated, _ := m.Update(msg)
	m = updated.(Model)

	if _, ok := m.marked[good]; ok {
		t.Error("the entry that moved should not be marked again")
	}
	if _, ok := m.marked[gone]; !ok {
		t.Error("the entry that failed should be marked again for a retry")
	}
}

// A failed paste must leave the clipboard armed so the user can retry it.
func TestFailedPasteReArmsClipboard(t *testing.T) {
	m, src := fixture(t)
	m = symlinkedPane(t, m, src)

	m = yankCopy(t, m, "alpha.txt")
	updated, _ := m.Update(tea.KeyPressMsg(tea.Key{Code: tea.KeyTab}))
	m = updated.(Model)

	updated, cmd := m.Update(shiftKey('v'))
	m = updated.(Model)

	if m.clipboardOn {
		t.Fatal("the clipboard should be disarmed while the paste runs")
	}

	msgs := runBatch(t, cmd)
	if len(msgs) != 1 {
		t.Fatalf("expected one aggregate result, got %d", len(msgs))
	}
	m = applyOps(t, m, msgs)

	if m.opErr == nil {
		t.Fatal("pasting a file onto itself should report an error")
	}
	if !m.clipboardOn {
		t.Error("a failed paste should re-arm the clipboard")
	}
	if len(m.clipboard.Files) != 1 || m.clipboard.Files[0].Name != "alpha.txt" {
		t.Errorf("clipboard = %+v, want the failed selection restored", m.clipboard.Files)
	}
}

// The large-copy path holds its selection on the model rather than on the
// message, so its failure branch has to restore it too.
func TestFailedLargeCopyReArmsClipboard(t *testing.T) {
	m, _, _ := twoPaneFixture(t)

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
	if m.clipboardOn {
		t.Fatal("the clipboard should be disarmed while the copy runs")
	}

	progress, ok := cmd().(copyProgressMsg)
	if !ok {
		t.Fatal("expected the copy to start")
	}
	updated, _ = m.Update(progress)
	m = updated.(Model)
	if m.copyClipboard == nil {
		t.Fatal("the in-flight copy should hold the selection for a retry")
	}

	updated, _ = m.Update(copyProgressMsg{
		copier: m.copier,
		gen:    m.copyGen,
		err:    errors.New("no space left on device"),
		name:   "big.bin",
	})
	m = updated.(Model)

	if m.opErr == nil {
		t.Fatal("a failed copy should report an error")
	}
	if !m.clipboardOn {
		t.Error("a failed copy should re-arm the clipboard")
	}
	if len(m.clipboard.Files) != 1 || m.clipboard.Files[0].Name != "big.bin" {
		t.Errorf("clipboard = %+v, want the failed selection restored", m.clipboard.Files)
	}
}

// A directory entry's Size is the inode, not its payload. The pre-copy
// estimate has to walk inside it or a large folder never gets a progress bar.
func TestCopyTotalBytesWalksDirectoryContents(t *testing.T) {
	dir := t.TempDir()
	nested := filepath.Join(dir, "nested")
	if err := os.MkdirAll(nested, 0o755); err != nil {
		t.Fatal(err)
	}
	payload := filepath.Join(nested, "big.bin")
	if err := os.WriteFile(payload, make([]byte, 2<<20), 0o644); err != nil {
		t.Fatal(err)
	}

	files := []fs.Entry{
		// The inode size is deliberately wrong-looking: it must not be
		// what the estimate uses.
		{Name: "payload", Path: dir, IsDir: true, Size: 4096},
		{Name: "small.txt", Path: filepath.Join(dir, "small.txt"), Size: 10},
	}

	got := copyTotalBytes(files)
	want := int64(2<<20) + 10
	if got != want {
		t.Errorf("copyTotalBytes = %d, want %d (directory contents plus the file)", got, want)
	}
}

// A directory big enough to matter must take the progress path, and it must
// land as a directory under its own name rather than scattering its contents
// into the destination root.
func TestLargeDirectoryPasteUsesProgressAndLandsUnderItsName(t *testing.T) {
	m, src, dst := twoPaneFixture(t)

	payload := filepath.Join(src, "payload")
	if err := os.MkdirAll(filepath.Join(payload, "nested"), 0o755); err != nil {
		t.Fatal(err)
	}
	if err := os.WriteFile(filepath.Join(payload, "nested", "big.bin"), make([]byte, 2<<20), 0o644); err != nil {
		t.Fatal(err)
	}
	m = loadDir(t, m, 0)

	m = yankCopy(t, m, "payload")
	updated, _ := m.Update(tea.KeyPressMsg(tea.Key{Code: tea.KeyTab}))
	m = updated.(Model)

	updated, cmd := m.Update(shiftKey('v'))
	m = updated.(Model)

	if m.copyFiles != 1 {
		t.Fatalf("copyFiles = %d, want 1; a 2 MiB directory should use the progress copier", m.copyFiles)
	}
	start, ok := cmd().(copyProgressMsg)
	if !ok {
		t.Fatalf("a large directory should start the progress copier, got %T", cmd())
	}
	updated, _ = m.Update(start)
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

	info, err := os.Stat(filepath.Join(dst, "payload"))
	if err != nil {
		t.Fatalf("copied directory missing: %v", err)
	}
	if !info.IsDir() {
		t.Errorf("%s is not a directory", filepath.Join(dst, "payload"))
	}
	if _, err := os.Stat(filepath.Join(dst, "payload", "nested", "big.bin")); err != nil {
		t.Errorf("directory contents are not under the copied directory: %v", err)
	}
	if _, err := os.Stat(filepath.Join(dst, "big.bin")); !os.IsNotExist(err) {
		t.Error("directory contents must not be scattered into the destination root")
	}
}

// Cancelling a transfer must put the selection back. A move's sources are
// removed only once the whole transfer lands, so anything still on disk can be
// retried; anything already renamed away did move and must not return.
func TestCancelRestoresMarksStillOnDisk(t *testing.T) {
	m, _, dst := twoPaneFixture(t)

	keep := filepath.Join(m.leftPath, "keep.bin")
	gone := filepath.Join(m.leftPath, "gone.bin")
	for _, path := range []string{keep, gone} {
		if err := os.WriteFile(path, make([]byte, 2<<20), 0o644); err != nil {
			t.Fatal(err)
		}
	}

	copier, err := fs.NewCopier([]string{keep, gone}, dst)
	if err != nil {
		t.Fatal(err)
	}
	updated, _ := m.Update(copyProgressMsg{copier: copier, gen: m.copyGen})
	m = updated.(Model)

	m.copyMarks = map[string]fs.Entry{
		keep: {Name: "keep.bin", Path: keep},
		gone: {Name: "gone.bin", Path: gone},
	}
	// This one already moved: it is no longer at its source.
	if err := os.Remove(gone); err != nil {
		t.Fatal(err)
	}

	updated, _ = m.Update(tea.KeyPressMsg(tea.Key{Code: tea.KeyEscape}))
	m = updated.(Model)

	if m.opResult != "copy cancelled" {
		t.Errorf("opResult = %q, want %q", m.opResult, "copy cancelled")
	}
	if _, ok := m.marked[keep]; !ok {
		t.Error("an entry still at its source should stay marked after a cancel")
	}
	if _, ok := m.marked[gone]; ok {
		t.Error("an entry already renamed away did move and must not be marked again")
	}
}

// Same for the clipboard: a cancelled paste can be retried without yanking the
// file again.
func TestCancelReArmsClipboard(t *testing.T) {
	m, _, _ := twoPaneFixture(t)

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
	start, ok := cmd().(copyProgressMsg)
	if !ok {
		t.Fatal("a large file should start the progress copier")
	}
	updated, _ = m.Update(start)
	m = updated.(Model)

	if m.clipboardOn {
		t.Fatal("the clipboard should be disarmed while the copy runs")
	}

	updated, _ = m.Update(tea.KeyPressMsg(tea.Key{Code: tea.KeyEscape}))
	m = updated.(Model)

	if m.opResult != "copy cancelled" {
		t.Errorf("opResult = %q, want %q", m.opResult, "copy cancelled")
	}
	if !m.clipboardOn {
		t.Error("a cancelled paste should re-arm the clipboard")
	}
	if len(m.clipboard.Files) != 1 || m.clipboard.Files[0].Name != "big.bin" {
		t.Errorf("clipboard = %+v, want the selection restored", m.clipboard.Files)
	}
}

// When the payload reaches the trash but the metadata write fails, the status
// has to say where the file went: the user's file is no longer where it was.
func TestTrashMetadataFailureReportsDestination(t *testing.T) {
	m, _ := fixture(t)
	dataHome := t.TempDir()
	t.Setenv("XDG_DATA_HOME", dataHome)

	// Put a directory where the .trashinfo file must go, so the rename
	// succeeds and only the metadata write fails.
	infoDir := filepath.Join(dataHome, "Trash", "info")
	if err := os.MkdirAll(infoDir, 0o755); err != nil {
		t.Fatal(err)
	}
	if err := os.Mkdir(filepath.Join(infoDir, "alpha.txt.trashinfo"), 0o755); err != nil {
		t.Fatal(err)
	}

	m = stageDelete(t, m, "alpha.txt")
	updated, cmd := m.Update(key('t'))
	m = updated.(Model)
	m = applyOps(t, m, runBatch(t, cmd))

	payload := filepath.Join(dataHome, "Trash", "files", "alpha.txt")
	if _, err := os.Stat(payload); err != nil {
		t.Fatalf("the payload should be in the trash: %v", err)
	}
	if m.opErr == nil {
		t.Fatal("the metadata failure should be reported")
	}
	got := m.opErr.Error()
	if !strings.Contains(got, "trashed alpha.txt to "+payload) {
		t.Errorf("opErr = %q, want the payload's destination", got)
	}
	if !strings.Contains(got, "write failed") {
		t.Errorf("opErr = %q, want the metadata failure named", got)
	}
}

// A trash that fails before the rename must say the file did not move.
func TestTrashBeforeMoveSaysFileDidNotMove(t *testing.T) {
	m, _ := fixture(t)
	t.Setenv("XDG_DATA_HOME", t.TempDir())

	m = stageDelete(t, m, "alpha.txt")
	if err := os.Remove(m.deleteTarget); err != nil {
		t.Fatal(err)
	}

	updated, cmd := m.Update(key('t'))
	m = updated.(Model)
	m = applyOps(t, m, runBatch(t, cmd))

	if m.opErr == nil {
		t.Fatal("a failed trash should be reported")
	}
	got := m.opErr.Error()
	if !strings.Contains(got, "did not move") {
		t.Errorf("opErr = %q, want it to say the file did not move", got)
	}
	if strings.Contains(got, " to ") {
		t.Errorf("opErr = %q, must not claim a destination", got)
	}
}
