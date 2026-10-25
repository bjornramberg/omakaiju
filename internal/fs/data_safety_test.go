package fs

import (
	"errors"
	"os"
	"path/filepath"
	"strings"
	"sync"
	"syscall"
	"testing"
	"time"
)

// A cross-filesystem move goes through the copy path, and the copy path used
// to replace whatever was already at the destination. A move must never do
// that: the entry is refused, its source stays where it was, and the batch
// carries on.
func TestMoverAcrossDevicesRefusesExistingDestination(t *testing.T) {
	for _, tc := range []struct {
		name string
		// makeDestination plants the entry that is already at the
		// destination.
		makeDestination func(t *testing.T, dst string) string
	}{
		{
			name: "regular file",
			makeDestination: func(t *testing.T, dst string) string {
				writeSafetyFile(t, dst, "the file that was already there")
				return "the file that was already there"
			},
		},
		{
			name: "symlink",
			makeDestination: func(t *testing.T, dst string) string {
				target := filepath.Join(filepath.Dir(dst), "existing-target.txt")
				writeSafetyFile(t, target, "the link target")
				if err := os.Symlink(target, dst); err != nil {
					t.Skipf("symlinks unsupported: %v", err)
				}
				return target
			},
		},
	} {
		t.Run(tc.name, func(t *testing.T) {
			base := t.TempDir()
			other := crossDeviceDir(t, base)

			src := filepath.Join(other, "movie.mkv")
			const payload = "the file that is being moved"
			writeSafetyFile(t, src, payload)

			dstDir := filepath.Join(base, "dst")
			if err := os.MkdirAll(dstDir, 0o755); err != nil {
				t.Fatal(err)
			}
			dst := filepath.Join(dstDir, "movie.mkv")
			want := tc.makeDestination(t, dst)

			c, err := NewMover([]string{src}, dstDir)
			if err != nil {
				t.Fatal(err)
			}
			drainMover(t, c)

			if got := readFile(t, src); got != payload {
				t.Errorf("the source was moved even though the destination was taken: %q", got)
			}
			landed, err := os.Lstat(dst)
			if err != nil {
				t.Fatalf("the destination was removed: %v", err)
			}
			if tc.name == "symlink" {
				if landed.Mode()&os.ModeSymlink == 0 {
					t.Fatalf("the existing destination is no longer a symlink: %v", landed.Mode())
				}
				if got, err := os.Readlink(dst); err != nil || got != want {
					t.Errorf("the destination link was replaced: got %q, want %q (%v)", got, want, err)
				}
			} else if got := readFile(t, dst); got != want {
				t.Errorf("the destination was replaced: got %q, want %q", got, want)
			}

			failures := c.Failures()
			if len(failures) != 1 {
				t.Fatalf("Failures() = %v, want one refusal", failures)
			}
			if failures[0].Name != "movie.mkv" {
				t.Errorf("the refusal names %q, want %q", failures[0].Name, "movie.mkv")
			}
			if !strings.Contains(failures[0].Err.Error(), "already exists") {
				t.Errorf("the refusal %q does not say the destination exists", failures[0].Err)
			}
			if c.Moved() != 0 {
				t.Errorf("Moved() = %d, want 0: nothing moved", c.Moved())
			}
			if c.Pending() != 0 {
				t.Errorf("Pending() = %d, want 0 after the batch was refused", c.Pending())
			}
		})
	}
}

// copySymlink must not replace a destination without holding it aside.
//
// The successful copy lands the link. A replacement that cannot land restores
// the old file through the strict backup instead of leaving the destination
// replaced (or gone).
func TestCopySymlinkHoldsDestinationAside(t *testing.T) {
	base := t.TempDir()
	target := filepath.Join(base, "target.txt")
	writeSafetyFile(t, target, "the link target")
	src := filepath.Join(base, "link")
	if err := os.Symlink(target, src); err != nil {
		t.Skipf("symlinks unsupported: %v", err)
	}

	t.Run("success over an existing file", func(t *testing.T) {
		dstDir := t.TempDir()
		dst := filepath.Join(dstDir, "link")
		writeSafetyFile(t, dst, "the file that was already there")

		if err := copySymlink(src, dst); err != nil {
			t.Fatalf("copySymlink: %v", err)
		}
		info, err := os.Lstat(dst)
		if err != nil {
			t.Fatalf("the link did not arrive: %v", err)
		}
		if info.Mode()&os.ModeSymlink == 0 {
			t.Fatalf("the destination is not a symlink: %v", info.Mode())
		}
		if got, err := os.Readlink(dst); err != nil || got != target {
			t.Errorf("the link points at %q, want %q (%v)", got, target, err)
		}
		assertNoScratchFiles(t, dstDir)
	})

	t.Run("replacement that cannot land restores the old file", func(t *testing.T) {
		dstDir := t.TempDir()
		dst := filepath.Join(dstDir, "link")
		const old = "the file that was already there"
		writeSafetyFile(t, dst, old)

		// A local filesystem will not make a rename fail while the old
		// destination is still there, so the failure is injected at the
		// rename the commit uses. The backup has already been taken by
		// then, which is exactly the path being checked.
		previous := renameIntoPlaceFn
		renameIntoPlaceFn = func(tmp, final string) error {
			if final == dst {
				return errors.New("simulated rename failure")
			}
			return previous(tmp, final)
		}
		defer func() { renameIntoPlaceFn = previous }()

		if err := copySymlink(src, dst); err == nil {
			t.Fatal("the replacement should have failed")
		}
		if got := readFile(t, dst); got != old {
			t.Errorf("the old file was not restored: got %q, want %q", got, old)
		}
		assertNoScratchFiles(t, dstDir)
	})

	t.Run("refused copy leaves the old file in place", func(t *testing.T) {
		dstDir := t.TempDir()
		dst := filepath.Join(dstDir, "link")
		const old = "the file that was already there"
		writeSafetyFile(t, dst, old)

		// The source link names the destination itself. Putting it there
		// would destroy the file it points at, so the copy is refused
		// before anything is staged.
		own := filepath.Join(base, "own-link")
		if err := os.Symlink(dst, own); err != nil {
			t.Skipf("symlinks unsupported: %v", err)
		}
		if err := copySymlink(own, dst); err == nil {
			t.Fatal("a link pointing at its own destination should be refused")
		}
		if got := readFile(t, dst); got != old {
			t.Errorf("the old file was touched: got %q, want %q", got, old)
		}
	})
}

// A symlink source repointed between planning and settlement is not the link
// that was copied. The move must not delete it, and the entry must be
// reported as a failure rather than counted as moved.
func TestMoverDoesNotDeleteSymlinkThatChanged(t *testing.T) {
	base := t.TempDir()
	other := crossDeviceDir(t, base)

	targetA := filepath.Join(base, "a.txt")
	targetB := filepath.Join(base, "b.txt")
	writeSafetyFile(t, targetA, "the link's original target")
	writeSafetyFile(t, targetB, "the target the link was repointed to")

	src := filepath.Join(base, "link")
	if err := os.Symlink(targetA, src); err != nil {
		t.Skipf("symlinks unsupported: %v", err)
	}

	c, err := NewMover([]string{src}, other)
	if err != nil {
		t.Fatal(err)
	}

	// Planning has read the link; repoint it now.
	if err := os.Remove(src); err != nil {
		t.Fatal(err)
	}
	if err := os.Symlink(targetB, src); err != nil {
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
		t.Fatal("a move of a repointed link should report that it did not finish")
	}
	if !strings.Contains(moveErr.Error(), "changed") {
		t.Errorf("error = %q, want it to say the source changed", moveErr)
	}

	if info, err := os.Lstat(src); err != nil {
		t.Fatalf("the source link was removed: %v", err)
	} else if info.Mode()&os.ModeSymlink == 0 {
		t.Fatal("the source is no longer a symlink")
	}
	if got, err := os.Readlink(src); err != nil || got != targetB {
		t.Errorf("the source link points at %q, want its new target %q (%v)", got, targetB, err)
	}

	failures := c.Failures()
	if len(failures) != 1 {
		t.Fatalf("Failures() = %v, want one failure for the changed link", failures)
	}
	if failures[0].Name != "link" {
		t.Errorf("the failure names %q, want %q", failures[0].Name, "link")
	}
	if c.Moved() != 0 {
		t.Errorf("Moved() = %d, want 0: the changed source never moved", c.Moved())
	}
	if c.Pending() != 0 {
		t.Errorf("Pending() = %d, want 0 after the copy stopped", c.Pending())
	}
}

// A cancel that lands after the last abort check but before the rename must
// not commit the new file. The hook lands the cancel exactly in that window;
// the late check has to see it and put the scratch away.
func TestCopierCancelBetweenAbortCheckAndCommit(t *testing.T) {
	srcDir := t.TempDir()
	src := filepath.Join(srcDir, "file.bin")
	if err := os.WriteFile(src, []byte("the new content that must not land"), 0o644); err != nil {
		t.Fatal(err)
	}

	dstDir := t.TempDir()
	dst := filepath.Join(dstDir, "file.bin")
	const old = "the file that was already there"
	if err := os.WriteFile(dst, []byte(old), 0o644); err != nil {
		t.Fatal(err)
	}

	c, err := NewCopier([]string{src}, dstDir)
	if err != nil {
		t.Fatal(err)
	}

	previous := beforeCommitHook
	var once sync.Once
	beforeCommitHook = func() {
		once.Do(func() { c.aborted.Store(true) })
	}
	defer func() { beforeCommitHook = previous }()

	for i := 0; i < 100; i++ {
		done, err := c.Step()
		if err != nil {
			t.Fatalf("step: %v", err)
		}
		if done {
			break
		}
	}

	if got := readFile(t, dst); got != old {
		t.Errorf("the cancelled copy replaced the destination: got %q, want %q", got, old)
	}
	assertNoScratchFiles(t, dstDir)
	if c.Pending() != 0 {
		t.Errorf("Pending() = %d, want 0 after the cancel", c.Pending())
	}
}

// A source that disappears between planning and opening fails the step, and
// the directories the copy created for it are taken away again. Without the
// unwind the user is left with an empty tree at the destination.
func TestCopierMissingSourceRemovesCreatedDirs(t *testing.T) {
	srcBase := t.TempDir()
	tree := filepath.Join(srcBase, "tree")
	gone := filepath.Join(tree, "sub", "gone.bin")
	writeBytes(t, gone, 16)

	dstParent := t.TempDir()
	dst := filepath.Join(dstParent, "dst")

	c, err := NewCopier([]string{tree}, dst)
	if err != nil {
		t.Fatal(err)
	}

	// The job was planned; the file leaves before the step opens it.
	if err := os.Remove(gone); err != nil {
		t.Fatal(err)
	}

	var stepErr error
	for i := 0; i < 100; i++ {
		done, err := c.Step()
		if err != nil {
			stepErr = err
			break
		}
		if done {
			break
		}
	}
	if stepErr == nil {
		t.Fatal("a source that vanished should fail the step")
	}

	for _, dir := range []string{dst, filepath.Join(dst, "tree"), filepath.Join(dst, "tree", "sub")} {
		if _, err := os.Lstat(dir); !os.IsNotExist(err) {
			t.Errorf("the directory the copy created survived the failure: %s (%v)", dir, err)
		}
	}
	if c.Pending() != 0 {
		t.Errorf("Pending() = %d, want 0 after the copy stopped", c.Pending())
	}
}

// The UI reads FilesDone and FilesTotal on every frame while the move runs on
// another goroutine. Run with -race, this is the test that finds a counter
// read directly from a field the step goroutine is writing.
func TestMoverCountersAreSafeWhileMoving(t *testing.T) {
	base := t.TempDir()
	other := crossDeviceDir(t, base)

	local := filepath.Join(base, "local.txt")
	remote := filepath.Join(other, "remote.txt")
	writeSafetyFile(t, local, "the local payload")
	writeSafetyFile(t, remote, "the remote payload")
	dst := filepath.Join(base, "dst")
	if err := os.MkdirAll(dst, 0o755); err != nil {
		t.Fatal(err)
	}

	c, err := NewMover([]string{local, remote}, dst)
	if err != nil {
		t.Fatal(err)
	}

	stepDone := make(chan struct{})
	var stepErr error
	var wg sync.WaitGroup
	wg.Add(1)
	go func() {
		defer wg.Done()
		defer close(stepDone)
		for {
			done, err := c.Step()
			if err != nil {
				stepErr = err
				return
			}
			if done {
				return
			}
		}
	}()

	// Read the counters the progress bar reads, as fast as possible, until
	// the step goroutine is finished. The timer is only a safety net.
	wg.Add(1)
	go func() {
		defer wg.Done()
		deadline := time.Now().Add(10 * time.Second)
		for {
			select {
			case <-stepDone:
				return
			default:
			}
			_ = c.FilesDone()
			_ = c.FilesTotal()
			_ = c.Pending()
			_ = c.Moved()
			_ = c.Renamed()
			_ = c.Total()
			_ = c.Copied()
			_ = c.Percent()
			if time.Now().After(deadline) {
				return
			}
		}
	}()
	wg.Wait()

	if stepErr != nil {
		t.Fatalf("move failed: %v", stepErr)
	}
	if c.Pending() != 0 {
		t.Errorf("Pending() = %d after the move finished, want 0", c.Pending())
	}
	if c.Moved() != 2 {
		t.Errorf("Moved() = %d, want 2", c.Moved())
	}
}

// A move only counts an entry as moved when its source is really gone. A
// source that refuses to be removed is reported, but it is not counted: the
// file is still where it was. The settle path is driven directly because a
// local filesystem will not refuse an unlink once the copy has landed, and
// the test must not depend on directory permissions or on running unprivileged.
func TestMoverDoesNotCountUnremovedSourceAsMoved(t *testing.T) {
	base := t.TempDir()

	// A non-empty directory cannot be removed with os.Remove, so it stands
	// in for a source the move cannot delete.
	src := filepath.Join(base, "stuck")
	writeSafetyFile(t, filepath.Join(src, "child"), "this keeps the directory non-empty")

	dst := filepath.Join(base, "landed.bin")
	writeSafetyFile(t, dst, "")

	srcInfo, err := os.Lstat(src)
	if err != nil {
		t.Fatal(err)
	}
	// The settle check compares the destination's size with the source's.
	if err := os.Truncate(dst, srcInfo.Size()); err != nil {
		t.Fatal(err)
	}

	c := &Copier{move: true}
	c.jobs = []copyJob{{
		src:     src,
		dst:     dst,
		size:    srcInfo.Size(),
		modTime: srcInfo.ModTime().UnixNano(),
	}}
	c.copyPending.Store(1)
	// The copy landed, which is what the old counter used to report as
	// moved even when the source stayed.
	c.filesDone = 1
	c.publish()

	if err := c.settleMove(); err == nil {
		t.Fatal("a source that could not be removed should be reported")
	} else if !strings.Contains(err.Error(), "could not be removed") {
		t.Errorf("error = %q, want it to say the source could not be removed", err)
	}
	if got := c.Moved(); got != 0 {
		t.Errorf("Moved() = %d, want 0: the source is still there", got)
	}
	if _, err := os.Lstat(src); err != nil {
		t.Fatalf("the source was removed after all: %v", err)
	}

	// A stopped copy must not report work that will never run.
	c.stop()
	if got := c.Pending(); got != 0 {
		t.Errorf("Pending() = %d after the copy stopped, want 0", got)
	}
}

// A FIFO has no end and its open waits for a writer that may never come. Every
// reader in the package has to refuse one by name before opening it, so the
// preview worker cannot hang forever.
func TestReadersRefuseFIFO(t *testing.T) {
	dir := t.TempDir()
	textFIFO := filepath.Join(dir, "text.txt")
	archiveFIFO := filepath.Join(dir, "blob.zip")
	unknownFIFO := filepath.Join(dir, "unknown")
	for _, path := range []string{textFIFO, archiveFIFO, unknownFIFO} {
		if err := syscall.Mkfifo(path, 0o600); err != nil {
			t.Skipf("mkfifo unsupported: %v", err)
		}
	}

	mustReturn := func(t *testing.T, what string, fn func() error) {
		t.Helper()
		done := make(chan error, 1)
		go func() { done <- fn() }()
		select {
		case err := <-done:
			if err == nil {
				t.Errorf("%s accepted a FIFO", what)
			}
		case <-time.After(5 * time.Second):
			t.Fatalf("%s blocked on a FIFO; it must refuse it before opening", what)
		}
	}

	mustReturn(t, "ReadFileHead", func() error {
		_, err := ReadFileHead(textFIFO, 10)
		return err
	})
	mustReturn(t, "CountLines", func() error {
		_, err := CountLines(textFIFO)
		return err
	})
	mustReturn(t, "ListArchive", func() error {
		_, _, err := ListArchive(archiveFIFO)
		return err
	})

	// DetectFileType must not open an unknown extension to sniff it.
	sniffed := make(chan FileType, 1)
	go func() { sniffed <- DetectFileType(unknownFIFO) }()
	select {
	case kind := <-sniffed:
		if kind != FileTypeBinary {
			t.Errorf("DetectFileType = %v for a FIFO, want FileTypeBinary", kind)
		}
	case <-time.After(5 * time.Second):
		t.Fatal("DetectFileType blocked on a FIFO; it must not open it")
	}
}
