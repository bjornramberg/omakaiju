package fs

import (
	"os"
	"path/filepath"
	"strings"
	"syscall"
	"testing"
	"time"
)

// writeFile creates a file with the given body, making the directory first.
func writeSafetyFile(t *testing.T, path, body string) {
	t.Helper()
	if err := os.MkdirAll(filepath.Dir(path), 0o755); err != nil {
		t.Fatal(err)
	}
	if err := os.WriteFile(path, []byte(body), 0o644); err != nil {
		t.Fatal(err)
	}
}

// Two files with the same basename from different directories would land on
// the same destination. Moving one would destroy the other, so the whole
// batch is refused and every source stays where it was.
func TestMoverRefusesDuplicateBasenames(t *testing.T) {
	base := t.TempDir()
	one := filepath.Join(base, "one", "photo.jpg")
	two := filepath.Join(base, "two", "photo.jpg")
	writeSafetyFile(t, one, "the first photo")
	writeSafetyFile(t, two, "the second photo")
	dst := filepath.Join(base, "dst")
	if err := os.MkdirAll(dst, 0o755); err != nil {
		t.Fatal(err)
	}

	c, err := NewMover([]string{one, two}, dst)
	if err != nil {
		t.Fatal(err)
	}

	if got := len(c.Failures()); got != 2 {
		t.Fatalf("Failures() = %v, want a failure for each colliding source", c.Failures())
	}
	for _, failure := range c.Failures() {
		msg := failure.Err.Error()
		if !strings.Contains(msg, "photo.jpg") {
			t.Errorf("the collision %q does not name the entry", msg)
		}
		if !strings.Contains(msg, one) || !strings.Contains(msg, two) {
			t.Errorf("the collision %q does not name both colliding sources", msg)
		}
	}

	if got := readFile(t, one); got != "the first photo" {
		t.Errorf("the first source was damaged: %q", got)
	}
	if got := readFile(t, two); got != "the second photo" {
		t.Errorf("the second source was damaged: %q", got)
	}
	if _, err := os.Lstat(filepath.Join(dst, "photo.jpg")); !os.IsNotExist(err) {
		t.Errorf("a refused batch must not create the destination: %v", err)
	}
	if c.Pending() != 0 {
		t.Errorf("Pending() = %d, want 0 for a refused batch", c.Pending())
	}
}

// A file and a directory with the same basename collide just as two files do:
// the collision is on the destination path, not on the type of the source.
func TestMoverRefusesDuplicateBasenameFileAndDirectory(t *testing.T) {
	base := t.TempDir()
	file := filepath.Join(base, "one", "report")
	writeSafetyFile(t, file, "a report")
	dir := filepath.Join(base, "two", "report")
	writeSafetyFile(t, filepath.Join(dir, "inside.txt"), "inside")
	dst := filepath.Join(base, "dst")
	if err := os.MkdirAll(dst, 0o755); err != nil {
		t.Fatal(err)
	}

	c, err := NewMover([]string{file, dir}, dst)
	if err != nil {
		t.Fatal(err)
	}

	if got := len(c.Failures()); got != 2 {
		t.Fatalf("Failures() = %v, want a failure for each colliding source", c.Failures())
	}
	for _, failure := range c.Failures() {
		msg := failure.Err.Error()
		if !strings.Contains(msg, file) || !strings.Contains(msg, dir) {
			t.Errorf("the collision %q does not name both colliding sources", msg)
		}
	}

	if got := readFile(t, file); got != "a report" {
		t.Errorf("the file source was damaged: %q", got)
	}
	if got := readFile(t, filepath.Join(dir, "inside.txt")); got != "inside" {
		t.Errorf("the directory source was damaged: %q", got)
	}
	if _, err := os.Lstat(filepath.Join(dst, "report")); !os.IsNotExist(err) {
		t.Errorf("a refused batch must not create the destination: %v", err)
	}
}

// Moving onto an existing regular file is refused. A rename would replace it
// atomically, destroying the file the user already had.
func TestMoveRefusesExistingRegularFile(t *testing.T) {
	base := t.TempDir()
	src := filepath.Join(base, "src.txt")
	dst := filepath.Join(base, "dst.txt")
	writeSafetyFile(t, src, "the new content")
	writeSafetyFile(t, dst, "the file that was already there")

	err := Move(src, dst)
	if err == nil {
		t.Fatal("moving onto an existing file should be refused")
	}
	if !strings.Contains(err.Error(), "already exists") {
		t.Errorf("error = %q, want it to say the destination already exists", err)
	}
	if got := readFile(t, dst); got != "the file that was already there" {
		t.Errorf("the existing file was damaged: %q", got)
	}
	if got := readFile(t, src); got != "the new content" {
		t.Errorf("the source was damaged: %q", got)
	}
}

// Moving onto an existing directory is refused just as a file is.
func TestMoveRefusesExistingDirectory(t *testing.T) {
	base := t.TempDir()
	src := filepath.Join(base, "src.txt")
	dst := filepath.Join(base, "dst")
	writeSafetyFile(t, src, "the new content")
	if err := os.MkdirAll(filepath.Join(dst, "inside"), 0o755); err != nil {
		t.Fatal(err)
	}

	if err := Move(src, dst); err == nil {
		t.Fatal("moving onto an existing directory should be refused")
	}
	info, err := os.Stat(dst)
	if err != nil {
		t.Fatalf("the destination directory is gone: %v", err)
	}
	if !info.IsDir() {
		t.Error("the destination is no longer a directory")
	}
	if got := readFile(t, src); got != "the new content" {
		t.Errorf("the source was damaged: %q", got)
	}
}

// A symlink counts as something already at the destination. It is checked
// with Lstat, so it is neither followed nor replaced.
func TestMoveRefusesExistingSymlink(t *testing.T) {
	base := t.TempDir()
	src := filepath.Join(base, "src.txt")
	target := filepath.Join(base, "target.txt")
	dst := filepath.Join(base, "dst-link")
	writeSafetyFile(t, src, "the new content")
	writeSafetyFile(t, target, "the target")
	if err := os.Symlink(target, dst); err != nil {
		t.Skipf("symlinks unsupported: %v", err)
	}

	if err := Move(src, dst); err == nil {
		t.Fatal("moving onto an existing symlink should be refused")
	}
	info, err := os.Lstat(dst)
	if err != nil {
		t.Fatalf("the destination link is gone: %v", err)
	}
	if info.Mode()&os.ModeSymlink == 0 {
		t.Fatal("the destination is no longer a symlink")
	}
	if got, err := os.Readlink(dst); err != nil || got != target {
		t.Errorf("the link points at %q, want %q (%v)", got, target, err)
	}
	if got := readFile(t, target); got != "the target" {
		t.Errorf("the link target was damaged: %q", got)
	}
	if got := readFile(t, src); got != "the new content" {
		t.Errorf("the source was damaged: %q", got)
	}
}

// Building a mover must not touch the filesystem. The renames run only when
// the mover is stepped, after any copy work has landed.
func TestNewMoverPlansWithoutMutating(t *testing.T) {
	base := t.TempDir()
	src := filepath.Join(base, "a.txt")
	dst := filepath.Join(base, "dst")
	writeSafetyFile(t, src, "payload")
	if err := os.MkdirAll(dst, 0o755); err != nil {
		t.Fatal(err)
	}

	c, err := NewMover([]string{src}, dst)
	if err != nil {
		t.Fatal(err)
	}
	if _, err := os.Stat(src); err != nil {
		t.Errorf("planning moved the source: %v", err)
	}
	if _, err := os.Lstat(filepath.Join(dst, "a.txt")); !os.IsNotExist(err) {
		t.Errorf("planning created the destination: %v", err)
	}
	if c.Pending() == 0 {
		t.Error("a mover with a rename outstanding should report pending work")
	}
	if c.Moved() != 0 {
		t.Errorf("Moved() = %d before any step, want 0", c.Moved())
	}
}

// A normal same-filesystem move still works: the source goes and the file
// arrives whole once the mover is stepped to completion.
func TestMoverSameFilesystemMoveAfterPlanning(t *testing.T) {
	base := t.TempDir()
	src := filepath.Join(base, "a.txt")
	dst := filepath.Join(base, "dst")
	writeSafetyFile(t, src, "payload")
	if err := os.MkdirAll(dst, 0o755); err != nil {
		t.Fatal(err)
	}

	c, err := NewMover([]string{src}, dst)
	if err != nil {
		t.Fatal(err)
	}
	drainMover(t, c)

	if got := readFile(t, filepath.Join(dst, "a.txt")); got != "payload" {
		t.Errorf("destination = %q, want %q", got, "payload")
	}
	if _, err := os.Stat(src); !os.IsNotExist(err) {
		t.Errorf("the source should be gone: %v", err)
	}
	if c.Moved() != 1 {
		t.Errorf("Moved() = %d, want 1", c.Moved())
	}
}

// A mixed batch carries the cross-device entry first and renames the local
// one only after the copy has landed.
func TestMoverMixedBatchMovesBoth(t *testing.T) {
	base := t.TempDir()
	other := crossDeviceDir(t, base)
	local := filepath.Join(base, "local.txt")
	remote := filepath.Join(other, "remote.txt")
	writeSafetyFile(t, local, "local payload")
	writeSafetyFile(t, remote, "remote payload")
	dst := filepath.Join(base, "dst")
	if err := os.MkdirAll(dst, 0o755); err != nil {
		t.Fatal(err)
	}

	c, err := NewMover([]string{local, remote}, dst)
	if err != nil {
		t.Fatal(err)
	}
	drainMover(t, c)

	for name, want := range map[string]string{
		"local.txt":  "local payload",
		"remote.txt": "remote payload",
	} {
		if got := readFile(t, filepath.Join(dst, name)); got != want {
			t.Errorf("%s = %q, want %q", name, got, want)
		}
	}
	for _, src := range []string{local, remote} {
		if _, err := os.Stat(src); !os.IsNotExist(err) {
			t.Errorf("the source %s should be gone: %v", src, err)
		}
	}
	if c.Moved() != 2 {
		t.Errorf("Moved() = %d, want 2", c.Moved())
	}
}

// A cross-device copy that fails must not let the same-filesystem entries
// rename themselves. They are cheap, but moving them would complete half of
// a transfer whose other half never arrived.
func TestMoverCopyFailureDoesNotRenameSameFilesystemEntry(t *testing.T) {
	base := t.TempDir()
	other := crossDeviceDir(t, base)
	local := filepath.Join(base, "local.txt")
	remote := filepath.Join(other, "remote.txt")
	writeSafetyFile(t, local, "local payload")
	writeSafetyFile(t, remote, "remote payload")
	dst := filepath.Join(base, "dst")
	if err := os.MkdirAll(dst, 0o755); err != nil {
		t.Fatal(err)
	}

	c, err := NewMover([]string{local, remote}, dst)
	if err != nil {
		t.Fatal(err)
	}

	// The destination directory existed when the move was planned. Remove
	// it: the copy step must fail with ENOENT rather than recreating it,
	// and no rename may run.
	if err := os.RemoveAll(dst); err != nil {
		t.Fatal(err)
	}

	var moveErr error
	for i := 0; i < 100; i++ {
		done, err := c.Step()
		if err != nil {
			moveErr = err
			break
		}
		if done {
			break
		}
	}
	if moveErr == nil {
		t.Fatal("a cross-device copy into a removed destination should fail")
	}

	if got := readFile(t, local); got != "local payload" {
		t.Errorf("the same-filesystem source moved even though the copy failed: %q", got)
	}
	if _, err := os.Lstat(filepath.Join(dst, "local.txt")); !os.IsNotExist(err) {
		t.Errorf("the same-filesystem entry was renamed: %v", err)
	}
	if _, err := os.Stat(dst); !os.IsNotExist(err) {
		t.Errorf("the removed destination was recreated: %v", err)
	}
	if c.Moved() != 0 {
		t.Errorf("Moved() = %d after a failed copy, want 0", c.Moved())
	}
	failures := c.Failures()
	if len(failures) != 1 {
		t.Fatalf("Failures() = %v, want the copy failure reported", failures)
	}
	if failures[0].Name != "remote.txt" {
		t.Errorf("the failure names %q, want %q", failures[0].Name, "remote.txt")
	}
	if !strings.Contains(failures[0].Err.Error(), "no such file or directory") {
		t.Errorf("the failure = %q, want the copy's ENOENT reason", failures[0].Err)
	}
}

// Pending counts outstanding renames, so a rename-only move cannot be
// mistaken for a finished one. CopyPending says whether there are bytes to
// carry: a rename-only batch has none.
func TestMoverPendingCountsRenames(t *testing.T) {
	base := t.TempDir()
	src := filepath.Join(base, "a.txt")
	dst := filepath.Join(base, "dst")
	writeSafetyFile(t, src, "payload")
	if err := os.MkdirAll(dst, 0o755); err != nil {
		t.Fatal(err)
	}

	c, err := NewMover([]string{src}, dst)
	if err != nil {
		t.Fatal(err)
	}
	if c.Pending() == 0 {
		t.Fatal("Pending() = 0 while a rename is outstanding")
	}
	if got := c.CopyPending(); got != 0 {
		t.Errorf("CopyPending() = %d for a rename-only batch, want 0", got)
	}

	drainMover(t, c)

	if got := c.Pending(); got != 0 {
		t.Errorf("Pending() = %d after the move completed, want 0", got)
	}
	if got := c.CopyPending(); got != 0 {
		t.Errorf("CopyPending() = %d after the move completed, want 0", got)
	}
}

// A FIFO has no end, and opening it waits for a writer that may never come.
// fs.Copy must refuse it by name instead of blocking.
func TestCopyRefusesFIFOWithoutBlocking(t *testing.T) {
	base := t.TempDir()
	fifo := filepath.Join(base, "pipe")
	if err := syscall.Mkfifo(fifo, 0o600); err != nil {
		t.Skipf("mkfifo unsupported: %v", err)
	}
	dst := filepath.Join(base, "copied")

	type result struct{ err error }
	finished := make(chan result, 1)
	go func() {
		finished <- result{Copy(fifo, dst)}
	}()

	select {
	case got := <-finished:
		if got.err == nil {
			t.Fatal("copying a FIFO should be refused")
		}
		if !strings.Contains(got.err.Error(), "pipe") {
			t.Errorf("the refusal %q does not name the entry", got.err)
		}
		if !strings.Contains(got.err.Error(), "not a regular file") {
			t.Errorf("the refusal %q does not say what is wrong", got.err)
		}
	case <-time.After(5 * time.Second):
		t.Fatal("fs.Copy hung on a FIFO; a special file must be refused, not opened")
	}
	if _, err := os.Lstat(dst); !os.IsNotExist(err) {
		t.Errorf("the FIFO must not be copied: %v", err)
	}
}
