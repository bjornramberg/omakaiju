package fs

import (
	"errors"
	"fmt"
	"os"
	"path/filepath"
	"strings"
	"syscall"
	"testing"
	"time"
)

// shmTopdir returns /dev/shm when it is a directory on a different filesystem
// from the test's temp dir, and skips the test otherwise. /dev/shm is tmpfs on
// Linux, which is the cheapest second filesystem a test can use.
//
// The test also skips when the user's real /dev/shm trash already exists, so a
// developer's live trash is never written into or emptied by a test.
func shmTopdir(t *testing.T) string {
	t.Helper()
	const shm = "/dev/shm"

	shmInfo, err := os.Stat(shm)
	if err != nil || !shmInfo.IsDir() {
		t.Skipf("%s is not available: %v", shm, err)
	}
	tmpInfo, err := os.Stat(t.TempDir())
	if err != nil {
		t.Fatal(err)
	}
	tmpDev, ok := deviceID(tmpInfo)
	if !ok {
		t.Skip("device numbers are unavailable")
	}
	shmDev, ok := deviceID(shmInfo)
	if !ok || shmDev == tmpDev {
		t.Skipf("%s is not on a second filesystem", shm)
	}

	trash := filepath.Join(shm, fmt.Sprintf(".Trash-%d", os.Getuid()))
	if _, err := os.Lstat(trash); err == nil {
		t.Skipf("%s already exists; not touching the real trash", trash)
	} else if !errors.Is(err, os.ErrNotExist) {
		t.Fatalf("checking %s: %v", trash, err)
	}
	t.Cleanup(func() {
		if err := os.RemoveAll(trash); err != nil {
			t.Errorf("removing test trash %s: %v", trash, err)
		}
	})
	return shm
}

// topdirTrash is the trash the spec puts at the root of a filesystem.
func topdirTrash(topdir string) string {
	return filepath.Join(topdir, fmt.Sprintf(".Trash-%d", os.Getuid()))
}

// shmWorkdir makes a scratch directory on the second filesystem.
func shmWorkdir(t *testing.T, topdir string) string {
	t.Helper()
	dir, err := os.MkdirTemp(topdir, "omakaiju-trash-")
	if err != nil {
		t.Fatal(err)
	}
	t.Cleanup(func() { _ = os.RemoveAll(dir) })
	return dir
}

func writeFile(t *testing.T, path, content string) {
	t.Helper()
	if err := os.MkdirAll(filepath.Dir(path), 0o755); err != nil {
		t.Fatal(err)
	}
	if err := os.WriteFile(path, []byte(content), 0o644); err != nil {
		t.Fatal(err)
	}
}

func TestTrashUsesSecondFilesystemTrash(t *testing.T) {
	top := shmTopdir(t)
	trashEnv(t)
	work := shmWorkdir(t, top)
	file := filepath.Join(work, "movie.mkv")
	writeFile(t, file, "payload")

	dst, err := Trash(file)
	if err != nil {
		if errors.Is(err, ErrCrossDeviceTrash) {
			t.Fatalf("trashing on a second filesystem reported ErrCrossDeviceTrash")
		}
		t.Fatalf("trashing on a second filesystem: %v", err)
	}

	root := topdirTrash(top)
	if want := filepath.Join(root, "files", "movie.mkv"); dst != want {
		t.Errorf("payload landed at %q, want %q", dst, want)
	}
	if got, err := TrashDirFor(file); err != nil {
		t.Errorf("TrashDirFor: %v", err)
	} else if got != root {
		t.Errorf("TrashDirFor = %q, want %q", got, root)
	}
	if _, err := os.Stat(file); !os.IsNotExist(err) {
		t.Errorf("source should be gone after trashing: %v", err)
	}
	if _, err := os.Lstat(filepath.Join(root, "files", "movie.mkv")); err != nil {
		t.Errorf("payload missing from the topdir trash: %v", err)
	}

	info := readFile(t, filepath.Join(root, "info", "movie.mkv.trashinfo"))
	wantPath := "Path=" + filepath.Base(work) + "/movie.mkv\n"
	if !strings.Contains(info, wantPath) {
		t.Errorf("trashinfo does not record a topdir-relative path %q: %q", wantPath, info)
	}
	if !strings.Contains(info, "DeletionDate=") {
		t.Errorf("trashinfo missing DeletionDate: %q", info)
	}
}

func TestTopdirTrashDirectoryIsPrivate(t *testing.T) {
	top := shmTopdir(t)
	trashEnv(t)
	work := shmWorkdir(t, top)
	file := filepath.Join(work, "note.txt")
	writeFile(t, file, "x")

	if _, err := Trash(file); err != nil {
		t.Fatal(err)
	}

	root := topdirTrash(top)
	for _, dir := range []string{root, filepath.Join(root, "files"), filepath.Join(root, "info")} {
		info, err := os.Stat(dir)
		if err != nil {
			t.Fatalf("%s not created: %v", dir, err)
		}
		if got := info.Mode().Perm(); got != 0o700 {
			t.Errorf("%s has mode %04o, want 0700", dir, got)
		}
	}
}

func TestTrashRefusesSymlinkedTopdirTrash(t *testing.T) {
	top := shmTopdir(t)
	trashEnv(t)
	root := topdirTrash(top)
	decoy := t.TempDir()
	if err := os.Symlink(decoy, root); err != nil {
		t.Skipf("symlinks unsupported: %v", err)
	}

	work := shmWorkdir(t, top)
	file := filepath.Join(work, "note.txt")
	writeFile(t, file, "keep me")

	dst, err := Trash(file)
	if err == nil {
		t.Fatalf("trashing through a symlinked trash succeeded at %q", dst)
	}
	if !errors.Is(err, ErrUnsafeTrash) {
		t.Errorf("error = %v, want ErrUnsafeTrash", err)
	}
	if dst != "" {
		t.Errorf("Trash returned %q with an error", dst)
	}
	if _, err := os.Stat(file); err != nil {
		t.Errorf("file must not be deleted when its trash is unsafe: %v", err)
	}
	if entries, err := os.ReadDir(decoy); err != nil {
		t.Fatal(err)
	} else if len(entries) != 0 {
		t.Errorf("something was written through the symlink: %v", entries)
	}
}

func TestTrashRefusesNonDirectoryTopdirTrash(t *testing.T) {
	top := shmTopdir(t)
	trashEnv(t)
	root := topdirTrash(top)
	if err := os.WriteFile(root, nil, 0o600); err != nil {
		t.Fatal(err)
	}

	work := shmWorkdir(t, top)
	file := filepath.Join(work, "note.txt")
	writeFile(t, file, "keep me")

	if _, err := Trash(file); !errors.Is(err, ErrUnsafeTrash) {
		t.Errorf("error = %v, want ErrUnsafeTrash", err)
	}
	if _, err := os.Stat(file); err != nil {
		t.Errorf("file must not be deleted when its trash is unsafe: %v", err)
	}
}

func TestTrashRefusesForeignOwnedTopdirTrash(t *testing.T) {
	if os.Getuid() != 0 {
		// An ordinary user cannot create a directory owned by somebody else.
		// TestCheckTrashDirRejectsUnsafeDirectories covers the check itself.
		t.Skip("needs root to create a directory owned by another user")
	}
	top := shmTopdir(t)
	trashEnv(t)
	root := topdirTrash(top)
	if err := os.Mkdir(root, 0o700); err != nil {
		t.Fatal(err)
	}
	if err := os.Chown(root, 12345, 12345); err != nil {
		t.Fatal(err)
	}

	work := shmWorkdir(t, top)
	file := filepath.Join(work, "note.txt")
	writeFile(t, file, "keep me")

	if _, err := Trash(file); !errors.Is(err, ErrUnsafeTrash) {
		t.Errorf("error = %v, want ErrUnsafeTrash", err)
	}
	if _, err := os.Stat(file); err != nil {
		t.Errorf("file must not be deleted when its trash is foreign-owned: %v", err)
	}
}

// fakeFileInfo lets the ownership check be exercised without root: a real
// directory owned by another user cannot be created as an ordinary user.
type fakeFileInfo struct {
	path string
	mode os.FileMode
	sys  any
}

func (f fakeFileInfo) Name() string       { return filepath.Base(f.path) }
func (f fakeFileInfo) Size() int64        { return 0 }
func (f fakeFileInfo) Mode() os.FileMode  { return f.mode }
func (f fakeFileInfo) ModTime() time.Time { return time.Time{} }
func (f fakeFileInfo) IsDir() bool        { return f.mode.IsDir() }
func (f fakeFileInfo) Sys() any           { return f.sys }

func TestCheckTrashDirRejectsUnsafeDirectories(t *testing.T) {
	uid := os.Getuid()
	own := &syscall.Stat_t{Uid: uint32(uid)}
	foreign := &syscall.Stat_t{Uid: uint32(uid + 1)}

	cases := []struct {
		name string
		info fakeFileInfo
		ok   bool
	}{
		{"owned directory", fakeFileInfo{"/x", os.ModeDir | 0o700, own}, true},
		{"symlink", fakeFileInfo{"/x", os.ModeSymlink | 0o777, own}, false},
		{"regular file", fakeFileInfo{"/x", 0o600, own}, false},
		{"other owner", fakeFileInfo{"/x", os.ModeDir | 0o700, foreign}, false},
		{"unknown owner", fakeFileInfo{"/x", os.ModeDir | 0o700, nil}, false},
	}
	for _, c := range cases {
		err := checkTrashDir(c.info.path, c.info)
		if c.ok {
			if err != nil {
				t.Errorf("%s: unexpected error %v", c.name, err)
			}
			continue
		}
		if !errors.Is(err, ErrUnsafeTrash) {
			t.Errorf("%s: error = %v, want ErrUnsafeTrash", c.name, err)
		}
	}
}

func TestRestoreBringsFileBack(t *testing.T) {
	trash := trashEnv(t)
	src := t.TempDir()
	file := filepath.Join(src, "sub", "doc.txt")
	writeFile(t, file, "data")

	dst, err := Trash(file)
	if err != nil {
		t.Fatal(err)
	}
	infoPath := filepath.Join(trash, "info", "doc.txt.trashinfo")

	// The directory the file came from is gone too, so the restore has to
	// create it again.
	if err := os.RemoveAll(filepath.Join(src, "sub")); err != nil {
		t.Fatal(err)
	}

	restored, err := Restore(infoPath)
	if err != nil {
		t.Fatalf("restore: %v", err)
	}
	if restored != file {
		t.Errorf("restored to %q, want %q", restored, file)
	}
	if got := readFile(t, file); got != "data" {
		t.Errorf("restored content = %q, want %q", got, "data")
	}
	if _, err := os.Lstat(dst); !os.IsNotExist(err) {
		t.Errorf("payload should have left the trash: %v", err)
	}
	if _, err := os.Stat(infoPath); !os.IsNotExist(err) {
		t.Errorf("info file should be removed after restore: %v", err)
	}
}

func TestRestoreFromSecondFilesystemTrash(t *testing.T) {
	top := shmTopdir(t)
	trashEnv(t)
	work := shmWorkdir(t, top)
	file := filepath.Join(work, "clip.mp4")
	writeFile(t, file, "bytes")

	if _, err := Trash(file); err != nil {
		t.Fatal(err)
	}
	infoPath := filepath.Join(topdirTrash(top), "info", "clip.mp4.trashinfo")
	if _, err := os.Stat(infoPath); err != nil {
		t.Fatal(err)
	}

	restored, err := Restore(infoPath)
	if err != nil {
		t.Fatalf("restore from topdir trash: %v", err)
	}
	if restored != file {
		t.Errorf("restored to %q, want %q", restored, file)
	}
	if got := readFile(t, file); got != "bytes" {
		t.Errorf("restored content = %q, want %q", got, "bytes")
	}
	if _, err := os.Stat(infoPath); !os.IsNotExist(err) {
		t.Errorf("info file should be removed after restore: %v", err)
	}
}

func TestRestoreRefusesToReplaceExistingPath(t *testing.T) {
	trashEnv(t)
	file := filepath.Join(t.TempDir(), "doc.txt")
	writeFile(t, file, "old")
	if _, err := Trash(file); err != nil {
		t.Fatal(err)
	}
	writeFile(t, file, "new")

	if _, err := Restore(filepath.Join(trash(t), "info", "doc.txt.trashinfo")); err == nil {
		t.Fatal("restore over an existing file should error")
	}
	if got := readFile(t, file); got != "new" {
		t.Errorf("existing file was replaced: %q", got)
	}
}

func TestEmptyTrashRemovesEntriesAndKeepsRoot(t *testing.T) {
	trash := trashEnv(t)
	src := t.TempDir()
	for _, name := range []string{"a.txt", "b.txt"} {
		writeFile(t, filepath.Join(src, name), name)
		if _, err := Trash(filepath.Join(src, name)); err != nil {
			t.Fatal(err)
		}
	}

	if err := EmptyTrash(); err != nil {
		t.Fatalf("empty trash: %v", err)
	}
	for _, sub := range []string{"files", "info"} {
		dir := filepath.Join(trash, sub)
		entries, err := os.ReadDir(dir)
		if err != nil {
			t.Fatalf("%s should still exist: %v", dir, err)
		}
		if len(entries) != 0 {
			t.Errorf("%s still holds %v", dir, entries)
		}
	}
	if _, err := os.Stat(trash); err != nil {
		t.Errorf("trash root must not be removed: %v", err)
	}
}

func TestEmptyTrashAtRemovesSecondFilesystemEntries(t *testing.T) {
	top := shmTopdir(t)
	home := trashEnv(t)
	work := shmWorkdir(t, top)

	side := filepath.Join(work, "aside.txt")
	writeFile(t, side, "x")
	if _, err := Trash(side); err != nil {
		t.Fatal(err)
	}
	keep := filepath.Join(t.TempDir(), "keep.txt")
	writeFile(t, keep, "keep")
	if _, err := Trash(keep); err != nil {
		t.Fatal(err)
	}

	root := topdirTrash(top)
	if err := EmptyTrashAt(root); err != nil {
		t.Fatalf("empty topdir trash: %v", err)
	}
	for _, sub := range []string{"files", "info"} {
		dir := filepath.Join(root, sub)
		entries, err := os.ReadDir(dir)
		if err != nil {
			t.Fatalf("%s should still exist: %v", dir, err)
		}
		if len(entries) != 0 {
			t.Errorf("%s still holds %v", dir, entries)
		}
	}
	if _, err := os.Stat(root); err != nil {
		t.Errorf("topdir trash root must not be removed: %v", err)
	}
	// The home trash holds its own entry and must be untouched.
	if entries, err := os.ReadDir(filepath.Join(home, "files")); err != nil {
		t.Fatal(err)
	} else if len(entries) != 1 {
		t.Errorf("home trash should still hold one entry, holds %v", entries)
	}
}

func TestTrashDirForReportsHomeTrash(t *testing.T) {
	trash := trashEnv(t)
	file := filepath.Join(t.TempDir(), "note.txt")
	writeFile(t, file, "x")

	got, err := TrashDirFor(file)
	if err != nil {
		t.Fatalf("TrashDirFor: %v", err)
	}
	if got != trash {
		t.Errorf("TrashDirFor = %q, want the home trash %q", got, trash)
	}
}

func TestOriginalPathHandlesBothTrashKinds(t *testing.T) {
	home := filepath.Join("/home/testuser", ".local", "share", "Trash")
	top := filepath.Join("/media/disk", ".Trash-1000")

	cases := []struct {
		name   string
		root   string
		stored string
		want   string
		bad    bool
	}{
		{"home absolute", home, "/home/testuser/doc.txt", "/home/testuser/doc.txt", false},
		{"home relative", home, "doc.txt", "", true},
		{"topdir relative", top, "sub/doc.txt", "/media/disk/sub/doc.txt", false},
		{"topdir absolute inside", top, "/media/disk/sub/doc.txt", "/media/disk/sub/doc.txt", false},
		{"topdir absolute outside", top, "/etc/passwd", "", true},
		{"topdir escape", top, "../etc/passwd", "", true},
		{"empty", home, "", "", true},
	}
	for _, c := range cases {
		got, err := originalPath(c.root, c.stored)
		if c.bad {
			if err == nil {
				t.Errorf("%s: got %q, want an error", c.name, got)
			}
			continue
		}
		if err != nil {
			t.Errorf("%s: %v", c.name, err)
			continue
		}
		if got != c.want {
			t.Errorf("%s: got %q, want %q", c.name, got, c.want)
		}
	}
}

func TestTrashSameNameKeepsBothPayloads(t *testing.T) {
	trash := trashEnv(t)
	dir := t.TempDir()

	first := filepath.Join(dir, "a", "note.txt")
	second := filepath.Join(dir, "b", "note.txt")
	writeFile(t, first, "first")
	writeFile(t, second, "second")

	dst1, err := Trash(first)
	if err != nil {
		t.Fatal(err)
	}
	dst2, err := Trash(second)
	if err != nil {
		t.Fatal(err)
	}
	if dst1 == dst2 {
		t.Fatalf("both landed on %q; a payload was replaced", dst1)
	}
	if got := readFile(t, dst1); got != "first" {
		t.Errorf("first payload = %q, want %q", got, "first")
	}
	if got := readFile(t, dst2); got != "second" {
		t.Errorf("second payload = %q, want %q", got, "second")
	}
	for _, name := range []string{"note.txt", "note.2.txt"} {
		if _, err := os.Stat(filepath.Join(trash, "info", name+".trashinfo")); err != nil {
			t.Errorf("trashinfo for %s missing: %v", name, err)
		}
	}
	if entries, err := os.ReadDir(filepath.Join(trash, "files")); err != nil {
		t.Fatal(err)
	} else if len(entries) != 2 {
		t.Errorf("trash holds %d payloads, want 2: %v", len(entries), entries)
	}
}

func TestTrashDoesNotTakeAReservedName(t *testing.T) {
	trash := trashEnv(t)
	infoDir := filepath.Join(trash, "info")
	if err := os.MkdirAll(infoDir, 0o700); err != nil {
		t.Fatal(err)
	}
	// Another trasher has reserved the name: its info file is there, its
	// payload has not been renamed into place yet. The old code looked only
	// at the payloads directory, so it took the name and overwrote the other
	// trasher's record.
	other := filepath.Join(infoDir, "note.txt.trashinfo")
	writeFile(t, other, "[Trash Info]\nPath=/somewhere/else/note.txt\nDeletionDate=2026-01-02T03:04:05\n")

	file := filepath.Join(t.TempDir(), "note.txt")
	writeFile(t, file, "mine")
	dst, err := Trash(file)
	if err != nil {
		t.Fatal(err)
	}
	if got := filepath.Base(dst); got != "note.2.txt" {
		t.Errorf("payload landed on the reserved name %q", got)
	}
	if got := readFile(t, other); !strings.Contains(got, "/somewhere/else/note.txt") {
		t.Errorf("the other trasher's info file was overwritten: %q", got)
	}
}

func TestEmptyTrashDoesNotFollowPayloadSymlink(t *testing.T) {
	trash := trashEnv(t)
	outside := t.TempDir()
	target := filepath.Join(outside, "keep.txt")
	writeFile(t, target, "keep")
	filesDir := filepath.Join(trash, "files")
	if err := os.MkdirAll(filesDir, 0o700); err != nil {
		t.Fatal(err)
	}
	if err := os.Symlink(target, filepath.Join(filesDir, "link.txt")); err != nil {
		t.Skipf("symlinks unsupported: %v", err)
	}

	if err := EmptyTrash(); err != nil {
		t.Fatalf("empty trash: %v", err)
	}
	if got := readFile(t, target); got != "keep" {
		t.Errorf("empty trash followed a payload symlink: %q", got)
	}
	if _, err := os.Lstat(filepath.Join(filesDir, "link.txt")); !os.IsNotExist(err) {
		t.Errorf("payload symlink should have been removed: %v", err)
	}
}

func TestEmptyTrashRefusesSymlinkedFilesDir(t *testing.T) {
	trash := trashEnv(t)
	outside := t.TempDir()
	keep := filepath.Join(outside, "keep.txt")
	writeFile(t, keep, "keep")
	if err := os.MkdirAll(filepath.Join(trash, "info"), 0o700); err != nil {
		t.Fatal(err)
	}
	if err := os.Symlink(outside, filepath.Join(trash, "files")); err != nil {
		t.Skipf("symlinks unsupported: %v", err)
	}

	if err := EmptyTrash(); !errors.Is(err, ErrUnsafeTrash) {
		t.Errorf("error = %v, want ErrUnsafeTrash", err)
	}
	if got := readFile(t, keep); got != "keep" {
		t.Errorf("empty trash walked through a symlinked files directory: %q", got)
	}
}

func TestTrashDoesNotReplaceUnrecordedPayload(t *testing.T) {
	trash := trashEnv(t)
	filesDir := filepath.Join(trash, "files")
	if err := os.MkdirAll(filesDir, 0o700); err != nil {
		t.Fatal(err)
	}
	orphan := filepath.Join(filesDir, "note.txt")
	writeFile(t, orphan, "orphan")

	file := filepath.Join(t.TempDir(), "note.txt")
	writeFile(t, file, "mine")
	dst, err := Trash(file)
	if err != nil {
		t.Fatal(err)
	}
	if filepath.Base(dst) != "note.2.txt" {
		t.Errorf("payload landed at %q, want note.2.txt", filepath.Base(dst))
	}
	if got := readFile(t, orphan); got != "orphan" {
		t.Errorf("orphan payload was replaced: %q", got)
	}
}

func TestListTrashReportsOriginalPathAndDate(t *testing.T) {
	trashEnv(t)
	before := time.Now().Add(-time.Second)
	file := filepath.Join(t.TempDir(), "dir with space", "report.pdf")
	writeFile(t, file, "pdf")
	if _, err := Trash(file); err != nil {
		t.Fatal(err)
	}
	after := time.Now().Add(time.Second)

	entries, err := ListTrash()
	if err != nil {
		t.Fatalf("list trash: %v", err)
	}
	if len(entries) != 1 {
		t.Fatalf("got %d entries, want 1: %+v", len(entries), entries)
	}
	entry := entries[0]
	if entry.Name != "report.pdf" {
		t.Errorf("Name = %q, want report.pdf", entry.Name)
	}
	if entry.Path != file {
		t.Errorf("Path = %q, want %q", entry.Path, file)
	}
	if entry.DeletedAt.Before(before) || entry.DeletedAt.After(after) {
		t.Errorf("DeletedAt = %v, want between %v and %v", entry.DeletedAt, before, after)
	}
	if entry.InfoPath != filepath.Join(trash(t), "info", "report.pdf.trashinfo") {
		t.Errorf("InfoPath = %q", entry.InfoPath)
	}
	if _, err := os.Stat(entry.PayloadPath); err != nil {
		t.Errorf("PayloadPath is not usable: %v", err)
	}

	if err := EmptyTrash(); err != nil {
		t.Fatal(err)
	}
	entries, err = ListTrash()
	if err != nil {
		t.Fatal(err)
	}
	if len(entries) != 0 {
		t.Errorf("emptied trash lists %d entries", len(entries))
	}
}

func TestListTrashOnMissingTrashIsEmpty(t *testing.T) {
	trashEnv(t)
	entries, err := ListTrash()
	if err != nil {
		t.Fatalf("listing a trash that does not exist: %v", err)
	}
	if len(entries) != 0 {
		t.Errorf("got %d entries from a missing trash", len(entries))
	}
}

func TestTrashSymlinkOnSecondFilesystemStaysLink(t *testing.T) {
	top := shmTopdir(t)
	trashEnv(t)
	work := shmWorkdir(t, top)
	target := filepath.Join(work, "target.txt")
	writeFile(t, target, "payload")
	link := filepath.Join(work, "link.txt")
	if err := os.Symlink(target, link); err != nil {
		t.Skipf("symlinks unsupported: %v", err)
	}

	dst, err := Trash(link)
	if err != nil {
		t.Fatal(err)
	}
	info, err := os.Lstat(dst)
	if err != nil {
		t.Fatal(err)
	}
	if info.Mode()&os.ModeSymlink == 0 {
		t.Error("trashed entry should remain a symlink, not be followed")
	}
	if got := readFile(t, target); got != "payload" {
		t.Errorf("symlink target was touched: %q", got)
	}
}
