package fs

import (
	"os"
	"path/filepath"
	"strings"
	"syscall"
	"testing"
	"time"
)

// A FIFO has no end and its open waits for a writer, so a copy that streams
// one hangs for good. A special file must be refused by name, the rest of the
// batch must still be copied, and no destination may appear for it.
func TestCopierRefusesFIFO(t *testing.T) {
	base := t.TempDir()
	fifo := filepath.Join(base, "pipe")
	if err := syscall.Mkfifo(fifo, 0o600); err != nil {
		t.Skipf("mkfifo unsupported: %v", err)
	}

	// A regular file in the same batch: refusing the FIFO must not stop it.
	regular := filepath.Join(base, "keep.bin")
	writeBytes(t, regular, 32)

	dst := filepath.Join(base, "dst")
	c, err := NewCopier([]string{fifo, regular}, dst)
	if err != nil {
		t.Fatalf("NewCopier: %v", err)
	}

	// Drive the copy on its own goroutine so a hang shows up as a timeout
	// rather than freezing the whole run.
	type result struct{ err error }
	finished := make(chan result, 1)
	go func() {
		for {
			done, err := c.Step()
			if err != nil || done {
				finished <- result{err}
				return
			}
		}
	}()

	select {
	case got := <-finished:
		if got.err != nil {
			t.Fatalf("copy failed: %v", got.err)
		}
	case <-time.After(5 * time.Second):
		t.Fatal("the copy hung on a FIFO; a special file must be refused, not opened")
	}

	failures := c.Failures()
	if len(failures) != 1 {
		t.Fatalf("Failures() = %v, want one refusal for the FIFO", failures)
	}
	if failures[0].Name != "pipe" {
		t.Errorf("the refusal names %q, want %q", failures[0].Name, "pipe")
	}
	if !strings.Contains(failures[0].Err.Error(), "pipe") {
		t.Errorf("the refusal %q does not name the entry", failures[0].Err)
	}
	if _, err := os.Lstat(filepath.Join(dst, "pipe")); !os.IsNotExist(err) {
		t.Errorf("the FIFO must not be copied: %v", err)
	}
	if _, err := os.Stat(filepath.Join(dst, "keep.bin")); err != nil {
		t.Errorf("the regular file in the same batch should still be copied: %v", err)
	}
}

// Copying a symlink over a file that is already there must never remove that
// file to make room for the link. The link is staged beside the destination
// and renamed into place, so the old file is intact until the whole link
// exists, and a cancel leaves it exactly as it was.
func TestCopierSymlinkOverExistingDestinationIsSafe(t *testing.T) {
	base := t.TempDir()
	src := filepath.Join(base, "src")
	if err := os.MkdirAll(src, 0o755); err != nil {
		t.Fatal(err)
	}
	target := filepath.Join(base, "target.txt")
	const targetBody = "the link target"
	if err := os.WriteFile(target, []byte(targetBody), 0o644); err != nil {
		t.Fatal(err)
	}
	if err := os.Symlink(target, filepath.Join(src, "link")); err != nil {
		t.Skipf("symlinks unsupported: %v", err)
	}

	const old = "the file that was already at the destination"
	dst := t.TempDir()
	oldPath := filepath.Join(dst, filepath.Base(src), "link")
	if err := os.MkdirAll(filepath.Dir(oldPath), 0o755); err != nil {
		t.Fatal(err)
	}
	if err := os.WriteFile(oldPath, []byte(old), 0o644); err != nil {
		t.Fatal(err)
	}

	c, err := NewCopier([]string{src}, dst)
	if err != nil {
		t.Fatalf("NewCopier: %v", err)
	}

	// One step stages the link. The destination must not have been touched
	// to make room for it: the old file is still the file the user had.
	if _, err := c.Step(); err != nil {
		t.Fatalf("step: %v", err)
	}
	if got := readFile(t, oldPath); got != old {
		t.Fatalf("the destination was removed before the link existed: got %q, want %q", got, old)
	}

	// Drive the copy to the end: the destination is now the link, pointing
	// at the same target.
	for i := 0; ; i++ {
		done, err := c.Step()
		if err != nil {
			t.Fatalf("step: %v", err)
		}
		if done {
			break
		}
		if i > 100 {
			t.Fatal("the copy did not finish")
		}
	}
	info, err := os.Lstat(oldPath)
	if err != nil {
		t.Fatalf("the link did not arrive: %v", err)
	}
	if info.Mode()&os.ModeSymlink == 0 {
		t.Fatalf("the destination is not a symlink: mode %v", info.Mode())
	}
	gotTarget, err := os.Readlink(oldPath)
	if err != nil {
		t.Fatal(err)
	}
	if gotTarget != target {
		t.Errorf("the link points at %q, want %q", gotTarget, target)
	}

	// A cancelled copy of the same tree leaves the old file exactly as it
	// was, with its original content.
	if err := os.Remove(oldPath); err != nil {
		t.Fatal(err)
	}
	if err := os.WriteFile(oldPath, []byte(old), 0o644); err != nil {
		t.Fatal(err)
	}

	c, err = NewCopier([]string{src}, dst)
	if err != nil {
		t.Fatalf("NewCopier: %v", err)
	}
	if _, err := c.Step(); err != nil {
		t.Fatalf("step: %v", err)
	}
	if err := c.Abort(); err != nil {
		t.Fatalf("abort: %v", err)
	}
	if got := readFile(t, oldPath); got != old {
		t.Errorf("a cancelled copy must leave the destination as it was: got %q, want %q", got, old)
	}
	assertNoScratchFiles(t, filepath.Dir(oldPath))
}

// A cancel has to leave the destination as it was even when no further Step
// follows: the partial file goes, the directories the copy created go, and
// the handles are closed. Otherwise a cancel leaks descriptors and blocks
// until a garbage collection.
func TestCopierAbortWithoutStepCleansUp(t *testing.T) {
	base := t.TempDir()
	src := filepath.Join(base, "src")
	writeBytes(t, filepath.Join(src, "sub", "big.bin"), copyChunk*4)
	dst := filepath.Join(base, "dst")

	countFDs := func() (int, bool) {
		entries, err := os.ReadDir("/proc/self/fd")
		if err != nil {
			return 0, false
		}
		return len(entries), true
	}

	// One warm-up cycle settles any descriptor the runtime opens lazily, so
	// the baseline is measured on a quiet process.
	warm, err := NewCopier([]string{src}, dst)
	if err != nil {
		t.Fatal(err)
	}
	if _, err := warm.Step(); err != nil {
		t.Fatal(err)
	}
	if err := warm.Abort(); err != nil {
		t.Fatal(err)
	}

	before, haveFDs := countFDs()

	for i := 0; i < 5; i++ {
		c, err := NewCopier([]string{src}, dst)
		if err != nil {
			t.Fatal(err)
		}
		if _, err := c.Step(); err != nil {
			t.Fatalf("cycle %d: step: %v", i, err)
		}

		// The partial file exists while the copy is in flight.
		partials, err := filepath.Glob(filepath.Join(dst, "src", "sub", tempPrefix+"*"))
		if err != nil {
			t.Fatal(err)
		}
		if len(partials) != 1 {
			t.Fatalf("cycle %d: want one partial file, found %v", i, partials)
		}

		// Abort and never step again, exactly as the application does.
		if err := c.Abort(); err != nil {
			t.Fatalf("cycle %d: abort: %v", i, err)
		}

		if _, err := os.Lstat(partials[0]); !os.IsNotExist(err) {
			t.Errorf("cycle %d: the partial file survived the cancel: %v", i, err)
		}
		// The destination directory did not exist before the copy, so the
		// copy created it and every directory under it. All of them go.
		for _, dir := range []string{
			filepath.Join(dst, "src", "sub"),
			filepath.Join(dst, "src"),
			dst,
		} {
			if _, err := os.Lstat(dir); !os.IsNotExist(err) {
				t.Errorf("cycle %d: the directory the copy created survived: %s (%v)", i, dir, err)
			}
		}
		if haveFDs {
			after, _ := countFDs()
			if after != before {
				t.Errorf("cycle %d: open descriptors = %d, want %d", i, after, before)
			}
		}
	}
}
