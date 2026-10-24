package fs

import (
	"os"
	"path/filepath"
	"strings"
	"testing"
)

// assertNoScratchFiles fails when a copy left a scratch file behind.
//
// A scratch file is the copy's own working file. It is removed when the
// copy lands, when it fails and when it is aborted, so one surviving in a
// destination directory means the copy left something of its own in the
// user's folder.
func assertNoScratchFiles(t *testing.T, dir string) {
	t.Helper()
	entries, err := os.ReadDir(dir)
	if err != nil {
		t.Fatalf("reading %s: %v", dir, err)
	}
	for _, e := range entries {
		if strings.HasPrefix(e.Name(), tempPrefix) {
			t.Errorf("a scratch file was left behind: %s", filepath.Join(dir, e.Name()))
		}
	}
}

// readFile returns the contents of path, failing the test if it cannot be
// read.
func readFile(t *testing.T, path string) string {
	t.Helper()
	data, err := os.ReadFile(path)
	if err != nil {
		t.Fatalf("reading %s: %v", path, err)
	}
	return string(data)
}

// crossDeviceDir returns a directory on a different filesystem from base, so
// a rename between the two has to go through a copy.
//
// The test is skipped when the machine offers only one filesystem: the
// behaviour it checks is the cross-device path, and there is nothing to
// check without a second device.
func crossDeviceDir(t *testing.T, base string) string {
	t.Helper()
	for _, candidate := range []string{"/dev/shm", "/run/shm", os.TempDir()} {
		dir, err := os.MkdirTemp(candidate, "omakaiju-xdev-")
		if err != nil {
			continue
		}
		probe := filepath.Join(dir, "probe")
		if err := os.WriteFile(probe, []byte("x"), 0o644); err != nil {
			os.RemoveAll(dir)
			continue
		}
		landed := filepath.Join(base, "omakaiju-xdev-probe")
		err = os.Rename(probe, landed)
		if err == nil {
			os.Remove(landed)
		}
		os.RemoveAll(dir)
		if err != nil && isCrossDevice(err) {
			again, err := os.MkdirTemp(candidate, "omakaiju-xdev-")
			if err != nil {
				continue
			}
			return again
		}
	}
	t.Skip("no second filesystem available for a cross-device test")
	return ""
}

// A file can be reached by more than one path. Copying it onto itself
// through the second path used to open the destination with O_TRUNC while
// the source was still open, truncating the inode before a byte of it was
// read. Both the source and the destination ended up empty, and nothing
// reported an error.
//
// This is the shape a move into an SMB share takes when the same share is
// also reachable locally: two paths, one file.
func TestCopyRefusesSameFileThroughAnotherPath(t *testing.T) {
	base := t.TempDir()
	real := filepath.Join(base, "real")
	link := filepath.Join(base, "link")
	if err := os.MkdirAll(real, 0o755); err != nil {
		t.Fatal(err)
	}
	if err := os.Symlink(real, link); err != nil {
		t.Skipf("symlinks unsupported: %v", err)
	}

	src := filepath.Join(real, "photo.jpg")
	const payload = "the only copy of this photo"
	if err := os.WriteFile(src, []byte(payload), 0o644); err != nil {
		t.Fatal(err)
	}

	// The two paths are different strings, so a string comparison would not
	// catch this. They name one file.
	dst := filepath.Join(link, "photo.jpg")
	if err := Copy(src, dst); err == nil {
		t.Fatal("copying a file onto itself through another path should be refused")
	}

	if got := readFile(t, src); got != payload {
		t.Errorf("the source was damaged: got %q, want %q", got, payload)
	}
	if got := readFile(t, dst); got != payload {
		t.Errorf("the destination was damaged: got %q, want %q", got, payload)
	}
}

// The same refusal has to hold for the recursive copy and for the large-copy
// path, or the hole is only closed for one of the three.
func TestEveryCopyPathRefusesSameFileThroughAnotherPath(t *testing.T) {
	base := t.TempDir()
	real := filepath.Join(base, "real")
	link := filepath.Join(base, "link")
	if err := os.MkdirAll(real, 0o755); err != nil {
		t.Fatal(err)
	}
	if err := os.Symlink(real, link); err != nil {
		t.Skipf("symlinks unsupported: %v", err)
	}

	src := filepath.Join(real, "photo.jpg")
	const payload = "the only copy of this photo"
	if err := os.WriteFile(src, []byte(payload), 0o644); err != nil {
		t.Fatal(err)
	}
	other := filepath.Join(link, "photo.jpg")

	for _, op := range []struct {
		name string
		run  func() error
	}{
		{"CopyRecursive", func() error { return CopyRecursive(src, other) }},
		{"Copy", func() error { return Copy(src, other) }},
		{"Copier", func() error {
			c, err := NewCopier([]string{src}, link)
			if err != nil {
				return err
			}
			_, err = c.Step()
			return err
		}},
	} {
		t.Run(op.name, func(t *testing.T) {
			if err := op.run(); err == nil {
				t.Error("copying a file onto itself through another path should be refused")
			}
			if got := readFile(t, src); got != payload {
				t.Errorf("the source was damaged: got %q, want %q", got, payload)
			}
		})
	}
}

// A copy must never leave a half-written file under the name the user
// expects. The bytes go to a scratch file and the destination only takes
// the name once the copy is whole.
func TestCopyDoesNotExposePartialDestination(t *testing.T) {
	src := t.TempDir()
	dst := t.TempDir()
	writeBytes(t, filepath.Join(src, "big.bin"), copyChunk*4)

	c, err := NewCopier([]string{filepath.Join(src, "big.bin")}, dst)
	if err != nil {
		t.Fatal(err)
	}
	if _, err := c.Step(); err != nil {
		t.Fatal(err)
	}
	if _, err := os.Stat(filepath.Join(dst, "big.bin")); !os.IsNotExist(err) {
		t.Errorf("the destination must not exist while the copy is partial: %v", err)
	}
}

// A failed copy must leave the destination exactly as it was. A file that
// was already there is not the copy's to destroy.
func TestFailedCopyLeavesTheDestinationAlone(t *testing.T) {
	if os.Geteuid() == 0 {
		t.Skip("root ignores directory permissions")
	}
	base := t.TempDir()
	src := filepath.Join(base, "src")
	if err := os.MkdirAll(src, 0o755); err != nil {
		t.Fatal(err)
	}
	writeBytes(t, filepath.Join(src, "a.bin"), 100)

	// A destination that already holds a file the user cares about.
	dst := filepath.Join(base, "dst")
	if err := os.MkdirAll(dst, 0o755); err != nil {
		t.Fatal(err)
	}
	const existing = "the file that was already there"
	if err := os.WriteFile(filepath.Join(dst, "a.bin"), []byte(existing), 0o644); err != nil {
		t.Fatal(err)
	}

	// A read-only directory refuses the scratch file the copy needs.
	if err := os.Chmod(dst, 0o555); err != nil {
		t.Fatal(err)
	}
	defer os.Chmod(dst, 0o755)

	if err := Copy(filepath.Join(src, "a.bin"), filepath.Join(dst, "a.bin")); err == nil {
		t.Fatal("a copy into a read-only directory should fail")
	}
	if got := readFile(t, filepath.Join(dst, "a.bin")); got != existing {
		t.Errorf("the existing destination was damaged: got %q, want %q", got, existing)
	}
}

// A rename cannot cross a filesystem, so a move onto a share has to copy the
// file across and remove the original. The copy has to land first: the
// source is only removed once the file is whole and in place.
func TestMoveAcrossDevicesCopiesThenRemoves(t *testing.T) {
	base := t.TempDir()
	other := crossDeviceDir(t, base)

	src := filepath.Join(base, "movie.mkv")
	const payload = "a long movie that has to travel to the share"
	if err := os.WriteFile(src, []byte(payload), 0o644); err != nil {
		t.Fatal(err)
	}

	dst := filepath.Join(other, "movie.mkv")
	if err := Move(src, dst); err != nil {
		t.Fatalf("move across devices failed: %v", err)
	}

	if got := readFile(t, dst); got != payload {
		t.Errorf("the moved file is wrong: got %q, want %q", got, payload)
	}
	if _, err := os.Stat(src); !os.IsNotExist(err) {
		t.Errorf("the source should be gone after a successful move: %v", err)
	}
}

// A move across devices that cannot complete must leave the source exactly
// where it was. Losing the file because the destination was unreachable is
// the one outcome a move may never produce.
func TestMoveAcrossDevicesThatFailsKeepsTheSource(t *testing.T) {
	if os.Geteuid() == 0 {
		t.Skip("root ignores directory permissions")
	}
	base := t.TempDir()
	other := crossDeviceDir(t, base)

	src := filepath.Join(base, "movie.mkv")
	const payload = "a long movie that has to travel to the share"
	if err := os.WriteFile(src, []byte(payload), 0o644); err != nil {
		t.Fatal(err)
	}

	// The destination directory refuses writes, so the copy cannot land.
	if err := os.Chmod(other, 0o555); err != nil {
		t.Fatal(err)
	}
	defer os.Chmod(other, 0o755)

	if err := Move(src, filepath.Join(other, "movie.mkv")); err == nil {
		t.Fatal("a move into a read-only directory should fail")
	}
	if got := readFile(t, src); got != payload {
		t.Errorf("the source was damaged: got %q, want %q", got, payload)
	}
}

// A directory moved across devices arrives whole, and the original is only
// removed once every file in it has.
func TestMoveDirectoryAcrossDevices(t *testing.T) {
	base := t.TempDir()
	other := crossDeviceDir(t, base)

	src := filepath.Join(base, "album")
	if err := os.MkdirAll(filepath.Join(src, "inner"), 0o755); err != nil {
		t.Fatal(err)
	}
	if err := os.WriteFile(filepath.Join(src, "a.txt"), []byte("alpha"), 0o644); err != nil {
		t.Fatal(err)
	}
	if err := os.WriteFile(filepath.Join(src, "inner", "b.txt"), []byte("beta"), 0o644); err != nil {
		t.Fatal(err)
	}

	dst := filepath.Join(other, "album")
	if err := Move(src, dst); err != nil {
		t.Fatalf("move across devices failed: %v", err)
	}
	if got := readFile(t, filepath.Join(dst, "a.txt")); got != "alpha" {
		t.Errorf("a.txt = %q, want %q", got, "alpha")
	}
	if got := readFile(t, filepath.Join(dst, "inner", "b.txt")); got != "beta" {
		t.Errorf("inner/b.txt = %q, want %q", got, "beta")
	}
	if _, err := os.Stat(src); !os.IsNotExist(err) {
		t.Errorf("the source directory should be gone after a successful move: %v", err)
	}
}

// A move inside one filesystem is still a rename: it must not turn into a
// copy, and it must not lose the file.
func TestMoveWithinOneFilesystemIsARename(t *testing.T) {
	base := t.TempDir()
	src := filepath.Join(base, "a.txt")
	if err := os.WriteFile(src, []byte("payload"), 0o644); err != nil {
		t.Fatal(err)
	}
	dst := filepath.Join(base, "b.txt")
	if err := Move(src, dst); err != nil {
		t.Fatal(err)
	}
	if got := readFile(t, dst); got != "payload" {
		t.Errorf("b.txt = %q, want %q", got, "payload")
	}
	if _, err := os.Stat(src); !os.IsNotExist(err) {
		t.Errorf("the source should be gone: %v", err)
	}
}

// A symlink copied over the file it points at used to remove that file and
// leave a link pointing at itself. The file the link names is the user's,
// and it has to survive.
func TestCopySymlinkOverItsOwnTargetIsRefused(t *testing.T) {
	base := t.TempDir()
	target := filepath.Join(base, "photo.jpg")
	if err := os.WriteFile(target, []byte("the photo"), 0o644); err != nil {
		t.Fatal(err)
	}
	link := filepath.Join(base, "link.jpg")
	if err := os.Symlink(target, link); err != nil {
		t.Skipf("symlinks unsupported: %v", err)
	}

	if err := Copy(link, target); err == nil {
		t.Fatal("copying a symlink over its own target should be refused")
	}
	if got := readFile(t, target); got != "the photo" {
		t.Errorf("the target was damaged: got %q, want %q", got, "the photo")
	}
}

// The large-copy path must refuse the same thing, or the hole is only
// closed for the small copies.
func TestCopierRefusesSymlinkOverItsOwnTarget(t *testing.T) {
	base := t.TempDir()
	targetDir := filepath.Join(base, "target")
	srcDir := filepath.Join(base, "src")
	if err := os.MkdirAll(targetDir, 0o755); err != nil {
		t.Fatal(err)
	}
	if err := os.MkdirAll(srcDir, 0o755); err != nil {
		t.Fatal(err)
	}

	// The file the link names.
	target := filepath.Join(targetDir, "photo.jpg")
	if err := os.WriteFile(target, []byte("the photo"), 0o644); err != nil {
		t.Fatal(err)
	}
	// A link with the same name, pointing at it.
	link := filepath.Join(srcDir, "photo.jpg")
	if err := os.Symlink(target, link); err != nil {
		t.Skipf("symlinks unsupported: %v", err)
	}

	// Copying the link into the target directory would put it where the
	// file it names already is. The refusal may come when the copy is
	// queued or when it runs; either is fine, as long as it happens
	// before anything is written.
	c, err := NewCopier([]string{link}, targetDir)
	if err == nil {
		if _, err := c.Step(); err == nil {
			t.Error("copying a symlink over its own target should be refused")
		}
	}
	if got := readFile(t, target); got != "the photo" {
		t.Errorf("the target was damaged: got %q, want %q", got, "the photo")
	}
}

// A relative link target is read against the directory holding the link, so
// the same guard has to hold for it.
func TestCopySymlinkWithRelativeTargetOverItsOwnDestination(t *testing.T) {
	base := t.TempDir()
	dir := filepath.Join(base, "dir")
	if err := os.MkdirAll(dir, 0o755); err != nil {
		t.Fatal(err)
	}
	target := filepath.Join(dir, "photo.jpg")
	if err := os.WriteFile(target, []byte("the photo"), 0o644); err != nil {
		t.Fatal(err)
	}
	link := filepath.Join(dir, "link.jpg")
	if err := os.Symlink("photo.jpg", link); err != nil {
		t.Skipf("symlinks unsupported: %v", err)
	}

	if err := Copy(link, target); err == nil {
		t.Fatal("copying a symlink over its own target should be refused")
	}
	if got := readFile(t, target); got != "the photo" {
		t.Errorf("the target was damaged: got %q, want %q", got, "the photo")
	}
}

// A directory copy that fails part way through must not leave a half-filled
// directory at the destination. The tree is built beside the destination and
// renamed into place, so the destination either holds everything or is not
// there at all.
func TestFailedDirectoryCopyLeavesNoHalfTree(t *testing.T) {
	if os.Geteuid() == 0 {
		t.Skip("root ignores directory permissions")
	}
	base := t.TempDir()
	src := filepath.Join(base, "src")
	if err := os.MkdirAll(src, 0o755); err != nil {
		t.Fatal(err)
	}
	writeBytes(t, filepath.Join(src, "a.bin"), 10)

	// A destination whose parent refuses writes, so the scratch directory
	// cannot be created.
	dstParent := filepath.Join(base, "locked")
	if err := os.MkdirAll(dstParent, 0o755); err != nil {
		t.Fatal(err)
	}
	if err := os.Chmod(dstParent, 0o555); err != nil {
		t.Fatal(err)
	}
	defer os.Chmod(dstParent, 0o755)

	dst := filepath.Join(dstParent, "tree")
	if err := CopyRecursive(src, dst); err == nil {
		t.Fatal("a copy into a read-only directory should fail")
	}
	if _, err := os.Stat(dst); !os.IsNotExist(err) {
		t.Errorf("a failed copy must not leave a half-copied directory: %v", err)
	}
	// Nothing may be left over in the parent either.
	entries, err := os.ReadDir(dstParent)
	if err != nil {
		t.Fatal(err)
	}
	for _, e := range entries {
		t.Errorf("a failed copy left %s behind", e.Name())
	}
}

// A directory copy that succeeds leaves the tree whole, with the source's
// permissions, and no scratch file behind.
func TestDirectoryCopyIsWholeAndLeavesNoScratchFiles(t *testing.T) {
	base := t.TempDir()
	src := filepath.Join(base, "src")
	if err := os.MkdirAll(filepath.Join(src, "sub"), 0o755); err != nil {
		t.Fatal(err)
	}
	writeBytes(t, filepath.Join(src, "a.bin"), 10)
	writeBytes(t, filepath.Join(src, "sub", "b.bin"), 20)

	dst := filepath.Join(base, "dst")
	if err := CopyRecursive(src, dst); err != nil {
		t.Fatal(err)
	}
	if got := readFile(t, filepath.Join(dst, "a.bin")); len(got) != 10 {
		t.Errorf("a.bin has %d bytes, want 10", len(got))
	}
	if got := readFile(t, filepath.Join(dst, "sub", "b.bin")); len(got) != 20 {
		t.Errorf("sub/b.bin has %d bytes, want 20", len(got))
	}
	assertNoScratchFiles(t, base)
}

// Creating a file must not empty one that is already there. "New file" is
// a request for a file that does not exist, not for an empty one.
func TestCreateFileRefusesToEmptyAnExistingFile(t *testing.T) {
	base := t.TempDir()
	path := filepath.Join(base, "keep.txt")
	const payload = "the file that was already there"
	if err := os.WriteFile(path, []byte(payload), 0o644); err != nil {
		t.Fatal(err)
	}

	if err := CreateFile(path); err == nil {
		t.Fatal("creating a file over an existing one should be refused")
	}
	if got := readFile(t, path); got != payload {
		t.Errorf("the existing file was damaged: got %q, want %q", got, payload)
	}
}

// A copy of a symlink that cannot be made must leave the destination alone.
// The link used to be made by removing the destination first, so a link that
// failed to be created took the user's file with it.
func TestFailedSymlinkCopyLeavesTheDestinationAlone(t *testing.T) {
	if os.Geteuid() == 0 {
		t.Skip("root ignores directory permissions")
	}
	base := t.TempDir()
	src := filepath.Join(base, "link")
	if err := os.Symlink("target", src); err != nil {
		t.Skipf("symlinks unsupported: %v", err)
	}

	dstDir := filepath.Join(base, "dst")
	if err := os.MkdirAll(dstDir, 0o755); err != nil {
		t.Fatal(err)
	}
	dst := filepath.Join(dstDir, "link")
	const payload = "the file that was already there"
	if err := os.WriteFile(dst, []byte(payload), 0o644); err != nil {
		t.Fatal(err)
	}

	// The destination directory refuses the new link.
	if err := os.Chmod(dstDir, 0o555); err != nil {
		t.Fatal(err)
	}
	defer os.Chmod(dstDir, 0o755)

	if err := Copy(src, dst); err == nil {
		t.Fatal("a symlink copy into a read-only directory should fail")
	}
	if got := readFile(t, dst); got != payload {
		t.Errorf("the destination was damaged: got %q, want %q", got, payload)
	}
}

// A copy that is cancelled must not leave an empty tree behind in the
// destination.
func TestCancelledCopyLeavesNoEmptyTree(t *testing.T) {
	base := t.TempDir()
	src := filepath.Join(base, "src", "sub")
	if err := os.MkdirAll(src, 0o755); err != nil {
		t.Fatal(err)
	}
	writeBytes(t, filepath.Join(src, "big.bin"), copyChunk*3)

	dst := filepath.Join(base, "dst")
	c, err := NewCopier([]string{filepath.Join(base, "src")}, dst)
	if err != nil {
		t.Fatal(err)
	}
	if _, err := c.Step(); err != nil {
		t.Fatal(err)
	}
	if err := c.Abort(); err != nil {
		t.Fatal(err)
	}
	// The abort is noticed at the next chunk boundary, which is where the
	// copy tidies up after itself.
	if _, err := c.Step(); err != nil {
		t.Fatal(err)
	}

	// The destination directory may exist, but the tree the copy made for
	// itself must not be left behind empty.
	entries, err := os.ReadDir(dst)
	if err != nil {
		if os.IsNotExist(err) {
			return
		}
		t.Fatal(err)
	}
	for _, e := range entries {
		t.Errorf("a cancelled copy left %s behind", e.Name())
	}
}

// A copy over a file that is already there replaces it, and leaves nothing
// of its own behind. The old file is held aside while the replacement is
// checked, so it can be put back if the replacement does not land.
func TestCopyOverAnExistingFileReplacesItAndLeavesNothingBehind(t *testing.T) {
	base := t.TempDir()
	src := filepath.Join(base, "src.txt")
	if err := os.WriteFile(src, []byte("the new content"), 0o644); err != nil {
		t.Fatal(err)
	}
	dst := filepath.Join(base, "dst.txt")
	const old = "the old content"
	if err := os.WriteFile(dst, []byte(old), 0o644); err != nil {
		t.Fatal(err)
	}

	if err := Copy(src, dst); err != nil {
		t.Fatal(err)
	}
	if got := readFile(t, dst); got != "the new content" {
		t.Errorf("dst = %q, want the new content", got)
	}
	// Nothing of the copy's own may be left in the folder.
	assertNoScratchFiles(t, base)
}

// SameFile has to recognise one file reached by two paths, and must not
// claim two different files are one.
func TestSameFileComparesByIdentity(t *testing.T) {
	base := t.TempDir()
	real := filepath.Join(base, "real")
	link := filepath.Join(base, "link")
	if err := os.MkdirAll(real, 0o755); err != nil {
		t.Fatal(err)
	}
	if err := os.Symlink(real, link); err != nil {
		t.Skipf("symlinks unsupported: %v", err)
	}

	a := filepath.Join(real, "a.txt")
	b := filepath.Join(link, "a.txt")
	c := filepath.Join(real, "c.txt")
	if err := os.WriteFile(a, []byte("a"), 0o644); err != nil {
		t.Fatal(err)
	}
	if err := os.WriteFile(c, []byte("c"), 0o644); err != nil {
		t.Fatal(err)
	}

	same, err := SameFile(a, b)
	if err != nil {
		t.Fatal(err)
	}
	if !same {
		t.Error("two paths for one file should be recognised as the same file")
	}

	same, err = SameFile(a, c)
	if err != nil {
		t.Fatal(err)
	}
	if same {
		t.Error("two different files should not be recognised as the same file")
	}

	// A destination that does not exist yet is not the source.
	same, err = SameFile(a, filepath.Join(real, "missing.txt"))
	if err != nil {
		t.Fatal(err)
	}
	if same {
		t.Error("a missing destination is not the source")
	}
}

// isCrossDevice has to recognise the error Go raises for a rename that
// crosses a filesystem, and leave other errors alone.
func TestIsCrossDevice(t *testing.T) {
	base := t.TempDir()
	other := crossDeviceDir(t, base)

	probe := filepath.Join(base, "probe")
	if err := os.WriteFile(probe, []byte("x"), 0o644); err != nil {
		t.Fatal(err)
	}
	err := os.Rename(probe, filepath.Join(other, "probe"))
	if err == nil {
		t.Skip("the probe did not cross a filesystem")
	}
	if !isCrossDevice(err) {
		t.Errorf("a cross-device rename should be recognised: %v", err)
	}
	if isCrossDevice(os.ErrNotExist) {
		t.Error("an unrelated error should not be treated as cross-device")
	}
}
