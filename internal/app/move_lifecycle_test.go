package app

import (
	"fmt"
	"os"
	"path/filepath"
	"strings"
	"testing"
	"time"

	"github.com/bjornramberg/omakaiju/internal/fs"

	tea "charm.land/bubbletea/v2"
)

// startMarkedMove marks the named entry in the left pane, switches to the
// destination pane, and dispatches the move the way the user does: m to mark,
// Tab, then Shift+V. It returns the model and the command the move started.
func startMarkedMove(t *testing.T, m Model, name string) (Model, tea.Cmd) {
	t.Helper()

	m = moveCursorTo(t, m, name)
	updated, _ := m.Update(shiftKey('m'))
	m = updated.(Model)

	updated, _ = m.Update(tea.KeyPressMsg(tea.Key{Code: tea.KeyTab}))
	m = updated.(Model)

	updated, cmd := m.Update(shiftKey('v'))
	return updated.(Model), cmd
}

// A same-filesystem move is a rename, so there is nothing to draw a progress
// bar for. It has to report as finished straight away, with the file already
// at the destination — the renames are driven inside the command rather than
// left to a progress step.
func TestRenameOnlyMoveReportsImmediately(t *testing.T) {
	m, src, dst := twoPaneFixture(t)

	m, cmd := startMarkedMove(t, m, "alpha.txt")
	if cmd == nil {
		t.Fatal("the move did not start")
	}

	raw := cmd()
	msg, ok := raw.(copyProgressMsg)
	if !ok {
		t.Fatalf("the move reported %T, want a copyProgressMsg", raw)
	}
	if !msg.done {
		t.Fatal("a rename-only move should report done on its first message, got done=false")
	}
	if msg.err != nil {
		t.Fatalf("the move failed: %v", msg.err)
	}

	updated, _ := m.Update(msg)
	m = updated.(Model)

	if m.opErr != nil {
		t.Fatalf("the move was reported as an error: %v", m.opErr)
	}
	if m.opResult != "moved 1 file(s)" {
		t.Errorf("opResult = %q, want %q", m.opResult, "moved 1 file(s)")
	}
	if _, err := os.Stat(filepath.Join(dst, "alpha.txt")); err != nil {
		t.Errorf("the file did not arrive at the destination: %v", err)
	}
	if _, err := os.Stat(filepath.Join(src, "alpha.txt")); !os.IsNotExist(err) {
		t.Errorf("the source should be gone after a successful move: %v", err)
	}
}

// A rename the kernel refuses — here because the destination already exists —
// is a per-entry failure. The app must name the entry, keep the source where it
// was, and never claim the move succeeded.
func TestFailedRenameMoveKeepsSourceAndReportsFailure(t *testing.T) {
	m, src, dst := twoPaneFixture(t)

	const existing = "already here"
	if err := os.WriteFile(filepath.Join(dst, "alpha.txt"), []byte(existing), 0o644); err != nil {
		t.Fatal(err)
	}

	m, cmd := startMarkedMove(t, m, "alpha.txt")
	if cmd == nil {
		t.Fatal("the move did not start")
	}

	raw := cmd()
	msg, ok := raw.(copyProgressMsg)
	if !ok {
		t.Fatalf("the move reported %T, want a copyProgressMsg", raw)
	}
	if !msg.done {
		t.Fatal("a rename-only move should report done on its first message, got done=false")
	}
	if msg.err != nil {
		t.Fatalf("a refused rename is a per-entry failure, not a step error: %v", msg.err)
	}

	updated, _ := m.Update(msg)
	m = updated.(Model)

	if m.opErr == nil {
		t.Fatal("a failed move was not reported")
	}
	if !strings.Contains(m.opErr.Error(), "alpha.txt") {
		t.Errorf("the failure does not name the entry: %v", m.opErr)
	}
	if m.opResult != "" {
		t.Errorf("opResult = %q, a failed move must not claim success", m.opResult)
	}
	if _, err := os.Stat(filepath.Join(src, "alpha.txt")); err != nil {
		t.Errorf("the source should still be there after a failed move: %v", err)
	}
	data, err := os.ReadFile(filepath.Join(dst, "alpha.txt"))
	if err != nil {
		t.Fatalf("the pre-existing destination disappeared: %v", err)
	}
	if string(data) != existing {
		t.Errorf("the destination was touched: %q", data)
	}
}

// waitForPath blocks until path exists, which is how a test knows the copy's
// step has started mutating the destination.
func waitForPath(t *testing.T, path string) {
	t.Helper()
	deadline := time.Now().Add(5 * time.Second)
	for time.Now().Before(deadline) {
		if _, err := os.Stat(path); err == nil {
			return
		}
		time.Sleep(time.Millisecond)
	}
	t.Fatalf("timed out waiting for %s", path)
}

// waitForCleanup blocks until a cancelled copy's teardown has finished: the
// directory it was creating is gone and the destination is empty again.
//
// The cancel no longer waits for this, so a test has to observe it the way a
// user would: the cleanup lands shortly after the keypress, not before the
// cancel returns.
func waitForCleanup(t *testing.T, dst string) {
	t.Helper()
	deadline := time.Now().Add(10 * time.Second)
	for {
		_, statErr := os.Stat(filepath.Join(dst, "tree"))
		entries, readErr := os.ReadDir(dst)
		if os.IsNotExist(statErr) && readErr == nil && len(entries) == 0 {
			return
		}
		if time.Now().After(deadline) {
			names := make([]string, 0, len(entries))
			for _, e := range entries {
				names = append(names, e.Name())
			}
			t.Fatalf("the cleanup never finished; the destination still holds %v (tree stat: %v)", names, statErr)
		}
		time.Sleep(time.Millisecond)
	}
}

// Cancelling must not make the UI wait for a step that is in flight: a step
// can be stuck in a read or a sync on a stalled filesystem, and the cancel runs
// on the event loop.
//
// A step cannot be blocked at will from a test, so this asserts the property
// that matters instead: the cancel returns while the step is still running.
// One Step creates every queued directory before it moves on to a file, so a
// tree of many directories keeps the step busy across the short time the
// cancel takes. Against the old synchronous Close the assertion is exact:
// Close could not return until the step had finished, so the step was always
// done by the time the cancel returned.
func TestCancelCopyReturnsWhileAStepIsStillRunning(t *testing.T) {
	m, src, dst := twoPaneFixture(t)

	tree := filepath.Join(src, "tree")
	if err := os.MkdirAll(tree, 0o755); err != nil {
		t.Fatal(err)
	}
	// Enough directories that the step is still creating them when the
	// cancel lands. The payload keeps the step from treating the tree as a
	// complete copy once the directories are done.
	const dirs = 5000
	for i := 0; i < dirs; i++ {
		if err := os.Mkdir(filepath.Join(tree, fmt.Sprintf("d%04d", i)), 0o755); err != nil {
			t.Fatal(err)
		}
	}
	if err := os.WriteFile(filepath.Join(tree, "payload.bin"), make([]byte, 1<<20), 0o644); err != nil {
		t.Fatal(err)
	}

	copier, err := fs.NewCopier([]string{tree}, dst)
	if err != nil {
		t.Fatal(err)
	}

	m.copier = copier

	stepDone := make(chan struct{})
	go func() {
		defer close(stepDone)
		_, _ = copier.Step()
	}()
	defer func() { <-stepDone }()

	// The step creates the tree, then its children. Waiting for the first
	// child guarantees the step owns the copier, with the rest of the tree
	// still to create.
	waitForPath(t, filepath.Join(dst, "tree", "d0000"))

	updated, _ := m.Update(tea.KeyPressMsg(tea.Key{Code: tea.KeyEscape}))
	m = updated.(Model)

	select {
	case <-stepDone:
		t.Fatal("the cancel waited for the step in flight; a stalled filesystem would freeze the UI")
	default:
	}

	if m.opResult != "copy cancelled" {
		t.Errorf("opResult = %q, want %q", m.opResult, "copy cancelled")
	}
	if m.copier != nil {
		t.Error("the cancelled copier is still attached to the model")
	}
}

// Cancelling an in-flight copy returns promptly, and the cleanup completes
// shortly afterwards. The copy is arrested mid-step on a tree large enough
// that the step is still creating directories, so the teardown is left to the
// step and to the goroutine that closed the copier.
func TestCancelCopyCleansUpShortlyAfterReturning(t *testing.T) {
	m, src, dst := twoPaneFixture(t)

	tree := filepath.Join(src, "tree")
	if err := os.MkdirAll(tree, 0o755); err != nil {
		t.Fatal(err)
	}
	// Enough directories that the single step below is still running when the
	// cancel lands. The file job after them means the step reaches the abort
	// check and unwinds rather than treating the tree as complete.
	const dirs = 2000
	for i := 0; i < dirs; i++ {
		if err := os.Mkdir(filepath.Join(tree, fmt.Sprintf("d%04d", i)), 0o755); err != nil {
			t.Fatal(err)
		}
	}
	payload := make([]byte, 3<<20)
	if err := os.WriteFile(filepath.Join(tree, "payload.bin"), payload, 0o644); err != nil {
		t.Fatal(err)
	}

	copier, err := fs.NewCopier([]string{tree}, dst)
	if err != nil {
		t.Fatal(err)
	}

	m.copier = copier
	m.copyGen = 7

	stepDone := make(chan struct{})
	go func() {
		defer close(stepDone)
		_, _ = copier.Step()
	}()
	defer func() { <-stepDone }()

	// The step creates the tree, then its children. Waiting for the first
	// child guarantees the step owns the copier, with the rest of the tree
	// still to create.
	waitForPath(t, filepath.Join(dst, "tree", "d0000"))

	updated, _ := m.Update(tea.KeyPressMsg(tea.Key{Code: tea.KeyEscape}))
	m = updated.(Model)

	// The cancel must not have waited for the step to unwind: the cleanup
	// is the step's job once the abort is signalled.
	select {
	case <-stepDone:
		t.Fatal("the cancel waited for the step in flight; the cleanup should run after it returns")
	default:
	}

	// The cancel has returned; what the user sees is already correct.
	if m.opResult != "copy cancelled" {
		t.Errorf("opResult = %q, want %q", m.opResult, "copy cancelled")
	}
	if m.copier != nil {
		t.Error("the cancelled copier is still attached to the model")
	}

	// Close's guarantee still holds, just not by the time the cancel
	// returns: the partial file and the directories the copy made are taken
	// away once the in-flight step observes the abort.
	waitForCleanup(t, dst)
}
