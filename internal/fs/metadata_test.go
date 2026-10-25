package fs

import (
	"errors"
	"os"
	"path/filepath"
	"strings"
	"syscall"
	"testing"
	"time"
)

// The tests set whole seconds, so the comparison does not depend on the
// filesystem's timestamp granularity.
var (
	testMtime = time.Unix(1_600_000_000, 0)
	testAtime = time.Unix(1_599_000_000, 0)
)

// setTestTimes gives path a known old pair of times.
func setTestTimes(t *testing.T, path string) {
	t.Helper()
	if err := os.Chtimes(path, testAtime, testMtime); err != nil {
		t.Fatal(err)
	}
}

// requireMtime fails unless path's modification time is want.
func requireMtime(t *testing.T, path string, want time.Time) {
	t.Helper()
	info, err := os.Lstat(path)
	if err != nil {
		t.Fatalf("stat %s: %v", path, err)
	}
	if got := info.ModTime(); !got.Equal(want) {
		t.Errorf("%s mtime = %v, want %v", path, got, want)
	}
}

// requireAtime fails unless path's access time is want. Lstat does not touch
// the access time, so reading it back is safe.
func requireAtime(t *testing.T, path string, want time.Time) {
	t.Helper()
	info, err := os.Lstat(path)
	if err != nil {
		t.Fatalf("stat %s: %v", path, err)
	}
	if got := timesOf(info).atime; !got.Equal(want) {
		t.Errorf("%s atime = %v, want %v", path, got, want)
	}
}

// A copy keeps the whole mode — the setuid bit as well as the permission bits
// — and the source's times rather than the moment the copy was made.
func TestCopierCarriesFileModeAndTimes(t *testing.T) {
	srcDir := t.TempDir()
	dstDir := t.TempDir()
	src := filepath.Join(srcDir, "helper")
	writeBytes(t, src, 64)
	if err := os.Chmod(src, 0o755|os.ModeSetuid); err != nil {
		t.Fatal(err)
	}
	setTestTimes(t, src)

	c, err := NewCopier([]string{src}, dstDir)
	if err != nil {
		t.Fatal(err)
	}
	drain(t, c)

	dst := filepath.Join(dstDir, "helper")
	info, err := os.Lstat(dst)
	if err != nil {
		t.Fatalf("the copy did not land: %v", err)
	}
	if got := info.Mode().Perm(); got != 0o755 {
		t.Errorf("mode = %04o, want 0755", got)
	}
	if info.Mode()&os.ModeSetuid == 0 {
		t.Error("the setuid bit was dropped")
	}
	requireMtime(t, dst, testMtime)
	requireAtime(t, dst, testAtime)
}

// The single-file Copy path has the same obligation as the steppable Copier:
// the whole mode and the times go with the file.
func TestCopyCarriesFileModeAndTimes(t *testing.T) {
	base := t.TempDir()
	src := filepath.Join(base, "tool")
	writeBytes(t, src, 64)
	if err := os.Chmod(src, 0o755|os.ModeSetuid); err != nil {
		t.Fatal(err)
	}
	setTestTimes(t, src)

	dst := filepath.Join(base, "copied")
	if err := Copy(src, dst); err != nil {
		t.Fatalf("Copy failed: %v", err)
	}

	info, err := os.Lstat(dst)
	if err != nil {
		t.Fatalf("the copy did not land: %v", err)
	}
	if got := info.Mode().Perm(); got != 0o755 {
		t.Errorf("mode = %04o, want 0755", got)
	}
	if info.Mode()&os.ModeSetuid == 0 {
		t.Error("the setuid bit was dropped")
	}
	requireMtime(t, dst, testMtime)
	requireAtime(t, dst, testAtime)
}

// A directory keeps its mode even under a umask that would mask it. MkdirAll
// alone leaves a 0777 source as 0755 when the process umask is 022.
func TestCopierCarriesDirectoryModeUnderUmask(t *testing.T) {
	srcBase := t.TempDir()
	dstDir := t.TempDir()
	src := filepath.Join(srcBase, "open")
	if err := os.Mkdir(src, 0o755); err != nil {
		t.Fatal(err)
	}
	writeBytes(t, filepath.Join(src, "inside.bin"), 8)
	if err := os.Chmod(src, 0o777); err != nil {
		t.Fatal(err)
	}

	// The umask is global to the process, so it is restored even if the
	// test fails.
	old := syscall.Umask(0o022)
	defer syscall.Umask(old)

	c, err := NewCopier([]string{src}, dstDir)
	if err != nil {
		t.Fatal(err)
	}
	drain(t, c)

	info, err := os.Stat(filepath.Join(dstDir, "open"))
	if err != nil {
		t.Fatalf("the directory did not land: %v", err)
	}
	if got := info.Mode().Perm(); got != 0o777 {
		t.Errorf("directory mode = %04o, want 0777 under umask 022", got)
	}
}

// A copied directory's time is the source's, even though the files written
// into it moved its time forward again. The time can only be set once the
// directory holds everything.
func TestCopierCarriesDirectoryModTime(t *testing.T) {
	srcBase := t.TempDir()
	dstDir := t.TempDir()
	src := filepath.Join(srcBase, "old-tree")
	if err := os.Mkdir(src, 0o755); err != nil {
		t.Fatal(err)
	}
	writeBytes(t, filepath.Join(src, "inside.bin"), 8)
	sub := filepath.Join(src, "sub")
	if err := os.Mkdir(sub, 0o755); err != nil {
		t.Fatal(err)
	}
	writeBytes(t, filepath.Join(sub, "nested.bin"), 8)
	subMtime := testMtime.Add(-time.Hour)
	if err := os.Chtimes(sub, testAtime.Add(-time.Hour), subMtime); err != nil {
		t.Fatal(err)
	}
	setTestTimes(t, src)

	c, err := NewCopier([]string{src}, dstDir)
	if err != nil {
		t.Fatal(err)
	}
	drain(t, c)

	requireMtime(t, filepath.Join(dstDir, "old-tree"), testMtime)
	requireAtime(t, filepath.Join(dstDir, "old-tree"), testAtime)
	// A nested directory has its own time, and it has to survive the copy
	// just as the root's does.
	requireMtime(t, filepath.Join(dstDir, "old-tree", "sub"), subMtime)
	requireAtime(t, filepath.Join(dstDir, "old-tree", "sub"), testAtime.Add(-time.Hour))
}

// The recursive copy path builds its directory in a scratch tree and renames
// it into place; the mode (sticky bit included) and times must survive that.
func TestCopyRecursiveCarriesDirectoryModeAndTimes(t *testing.T) {
	base := t.TempDir()
	src := filepath.Join(base, "tree")
	if err := os.Mkdir(src, 0o755); err != nil {
		t.Fatal(err)
	}
	writeBytes(t, filepath.Join(src, "file.txt"), 16)
	if err := os.Chmod(src, 0o777|os.ModeSticky); err != nil {
		t.Fatal(err)
	}
	setTestTimes(t, src)

	dst := filepath.Join(base, "copy")
	if err := CopyRecursive(src, dst); err != nil {
		t.Fatalf("CopyRecursive failed: %v", err)
	}

	info, err := os.Lstat(dst)
	if err != nil {
		t.Fatalf("the copy did not land: %v", err)
	}
	if got := info.Mode().Perm(); got != 0o777 {
		t.Errorf("directory mode = %04o, want 0777", got)
	}
	if info.Mode()&os.ModeSticky == 0 {
		t.Error("the sticky bit was dropped")
	}
	requireMtime(t, dst, testMtime)
}

// A cross-device move goes through the copy path, so it carries the times the
// same way a copy does.
func TestMoveAcrossDevicesCarriesFileTimes(t *testing.T) {
	base := t.TempDir()
	other := crossDeviceDir(t, base)
	t.Cleanup(func() { os.RemoveAll(other) })

	src := filepath.Join(base, "movie.mkv")
	writeBytes(t, src, 4096)
	setTestTimes(t, src)

	c, err := NewMover([]string{src}, other)
	if err != nil {
		t.Fatal(err)
	}
	drainMover(t, c)

	dst := filepath.Join(other, "movie.mkv")
	requireMtime(t, dst, testMtime)
	requireAtime(t, dst, testAtime)

	if _, err := os.Lstat(src); !os.IsNotExist(err) {
		t.Errorf("the source should be gone after the move: %v", err)
	}
}

// A copy that cannot apply the mode must report the failure and leave the
// destination exactly as it was. A local filesystem will not refuse a chmod on
// a file the process owns, so the mode change is made to fail through the
// package's test seam. The failure happens after the scratch file is whole and
// before the rename, which is the window that must leave the destination
// untouched.
func TestFailedFileModeLeavesDestinationUntouched(t *testing.T) {
	base := t.TempDir()
	src := filepath.Join(base, "src", "keep.bin")
	writeBytes(t, src, 64)

	dstDir := filepath.Join(base, "dst")
	if err := os.MkdirAll(dstDir, 0o755); err != nil {
		t.Fatal(err)
	}
	dst := filepath.Join(dstDir, "keep.bin")
	const payload = "the file that was already there"
	if err := os.WriteFile(dst, []byte(payload), 0o644); err != nil {
		t.Fatal(err)
	}

	realChmod := chmodFn
	chmodFn = func(path string, mode os.FileMode) error {
		if strings.HasPrefix(filepath.Base(path), tempPrefix) {
			return errors.New("chmod refused for the test")
		}
		return realChmod(path, mode)
	}
	defer func() { chmodFn = realChmod }()

	c, err := NewCopier([]string{src}, dstDir)
	if err != nil {
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
		t.Fatal("a copy whose mode cannot be set should fail")
	}
	if !strings.Contains(stepErr.Error(), "chmod refused") {
		t.Errorf("error = %q, want it to name the refused mode change", stepErr)
	}

	// The destination is the file it was: not the empty scratch file, not a
	// half-written one, and not the source's content without its mode.
	if got := readFile(t, dst); got != payload {
		t.Errorf("the destination = %q, want %q", got, payload)
	}
	info, err := os.Lstat(dst)
	if err != nil {
		t.Fatalf("the destination is gone: %v", err)
	}
	if got := info.Mode().Perm(); got != 0o644 {
		t.Errorf("destination mode = %04o, want 0644", got)
	}
	assertNoScratchFiles(t, dstDir)
}
