package fs

import (
	"errors"
	"os"
	"path/filepath"
	"strings"
	"testing"
)

// trashEnv points the trash at a temp dir for the duration of a test.
func trashEnv(t *testing.T) string {
	t.Helper()
	base := t.TempDir()
	t.Setenv("XDG_DATA_HOME", base)
	return filepath.Join(base, "Trash")
}

func TestTrashRootHonoursXDGDataHome(t *testing.T) {
	base := t.TempDir()
	t.Setenv("XDG_DATA_HOME", base)

	got, err := TrashRoot()
	if err != nil {
		t.Fatal(err)
	}
	if want := filepath.Join(base, "Trash"); got != want {
		t.Errorf("TrashRoot() = %q, want %q", got, want)
	}
}

func TestTrashRootFallsBackToHome(t *testing.T) {
	t.Setenv("XDG_DATA_HOME", "")
	t.Setenv("HOME", "/home/testuser")

	got, err := TrashRoot()
	if err != nil {
		t.Fatal(err)
	}
	if want := filepath.Join("/home/testuser", ".local", "share", "Trash"); got != want {
		t.Errorf("TrashRoot() = %q, want %q", got, want)
	}
}

func TestTrashMovesFileAndWritesInfo(t *testing.T) {
	trash := trashEnv(t)
	src := t.TempDir()
	file := filepath.Join(src, "doc.txt")
	if err := os.WriteFile(file, []byte("data"), 0o644); err != nil {
		t.Fatal(err)
	}

	dst, err := Trash(file)
	if err != nil {
		t.Fatal(err)
	}

	if _, err := os.Stat(file); !os.IsNotExist(err) {
		t.Error("source should be gone after trashing")
	}
	if _, err := os.Stat(dst); err != nil {
		t.Errorf("payload missing from trash: %v", err)
	}
	if filepath.Dir(dst) != filepath.Join(trash, "files") {
		t.Errorf("payload landed in %q, want the files directory", filepath.Dir(dst))
	}

	info, err := os.ReadFile(filepath.Join(trash, "info", "doc.txt.trashinfo"))
	if err != nil {
		t.Fatalf("trashinfo missing: %v", err)
	}
	text := string(info)
	if !strings.HasPrefix(text, "[Trash Info]\n") {
		t.Errorf("trashinfo missing header: %q", text)
	}
	if !strings.Contains(text, "Path=") {
		t.Errorf("trashinfo missing Path: %q", text)
	}
	if !strings.Contains(text, "DeletionDate=") {
		t.Errorf("trashinfo missing DeletionDate: %q", text)
	}
}

func TestTrashDirectoryTreeMovesWhole(t *testing.T) {
	trashEnv(t)
	src := t.TempDir()
	tree := filepath.Join(src, "project")
	if err := os.MkdirAll(filepath.Join(tree, "sub"), 0o755); err != nil {
		t.Fatal(err)
	}
	if err := os.WriteFile(filepath.Join(tree, "sub", "f.txt"), []byte("x"), 0o644); err != nil {
		t.Fatal(err)
	}

	dst, err := Trash(tree)
	if err != nil {
		t.Fatal(err)
	}
	if _, err := os.Stat(filepath.Join(dst, "sub", "f.txt")); err != nil {
		t.Errorf("nested file should move with the tree: %v", err)
	}
	if _, err := os.Stat(tree); !os.IsNotExist(err) {
		t.Error("source tree should be gone")
	}
}

func TestTrashSuffixesOnNameCollision(t *testing.T) {
	trash := trashEnv(t)
	dir := t.TempDir()

	first := filepath.Join(dir, "a", "note.txt")
	second := filepath.Join(dir, "b", "note.txt")
	for _, f := range []string{first, second} {
		if err := os.MkdirAll(filepath.Dir(f), 0o755); err != nil {
			t.Fatal(err)
		}
		if err := os.WriteFile(f, []byte("x"), 0o644); err != nil {
			t.Fatal(err)
		}
	}

	dst1, err := Trash(first)
	if err != nil {
		t.Fatal(err)
	}
	dst2, err := Trash(second)
	if err != nil {
		t.Fatal(err)
	}

	if dst1 == dst2 {
		t.Fatalf("both landed on %q; collision was not handled", dst1)
	}
	if filepath.Base(dst2) != "note.2.txt" {
		t.Errorf("second payload = %q, want note.2.txt", filepath.Base(dst2))
	}
	// Both entries need their own info file.
	for _, n := range []string{"note.txt", "note.2.txt"} {
		if _, err := os.Stat(filepath.Join(trash, "info", n+".trashinfo")); err != nil {
			t.Errorf("trashinfo for %s missing: %v", n, err)
		}
	}
}

func TestTrashSymlinkIsMovedNotFollowed(t *testing.T) {
	trashEnv(t)
	dir := t.TempDir()
	target := filepath.Join(dir, "target.txt")
	if err := os.WriteFile(target, []byte("payload"), 0o644); err != nil {
		t.Fatal(err)
	}
	link := filepath.Join(dir, "link.txt")
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
	if _, err := os.Stat(target); err != nil {
		t.Errorf("symlink target should be untouched: %v", err)
	}
}

func TestTrashMissingSourceErrors(t *testing.T) {
	trashEnv(t)
	if _, err := Trash(filepath.Join(t.TempDir(), "nope.txt")); err == nil {
		t.Error("trashing a missing file should error")
	}
}

func TestTrashCreatesDirectories(t *testing.T) {
	trash := trashEnv(t)
	src := t.TempDir()
	file := filepath.Join(src, "x.txt")
	if err := os.WriteFile(file, nil, 0o644); err != nil {
		t.Fatal(err)
	}

	// The trash root does not exist yet.
	if _, err := os.Stat(trash); !os.IsNotExist(err) {
		t.Fatal("setup: trash root should not exist yet")
	}
	if _, err := Trash(file); err != nil {
		t.Fatal(err)
	}
	for _, dir := range []string{"files", "info"} {
		if _, err := os.Stat(filepath.Join(trash, dir)); err != nil {
			t.Errorf("trash %s directory not created: %v", dir, err)
		}
	}
}

func TestTrashInfoEncodesPath(t *testing.T) {
	trashEnv(t)
	// A directory name with a space and a percent must be encoded.
	src := filepath.Join(t.TempDir(), "dir with space %sign")
	if err := os.MkdirAll(src, 0o755); err != nil {
		t.Fatal(err)
	}
	if _, err := Trash(src); err != nil {
		t.Fatal(err)
	}
	info, err := os.ReadFile(filepath.Join(trash(t), "info", "dir with space %sign.trashinfo"))
	if err != nil {
		t.Fatal(err)
	}
	if !strings.Contains(string(info), "%20") {
		t.Errorf("path not percent-encoded: %q", info)
	}
}

// trash re-reads the trash root, which the env may have changed since setup.
func trash(t *testing.T) string {
	t.Helper()
	root, err := TrashRoot()
	if err != nil {
		t.Fatal(err)
	}
	return root
}

func TestUniqueTrashPathReturnsFirstFreeName(t *testing.T) {
	dir := t.TempDir()
	got, err := uniqueTrashPath(dir, "free.txt")
	if err != nil {
		t.Fatal(err)
	}
	if got != filepath.Join(dir, "free.txt") {
		t.Errorf("got %q, want the unmodified name", got)
	}
}

func TestIsCrossDeviceRecognisesLinkError(t *testing.T) {
	le := &os.LinkError{Op: "rename", Err: errors.New("invalid cross-device link")}
	if !isCrossDevice(le) {
		t.Error("cross-device link errors should be detected")
	}
	if isCrossDevice(errors.New("permission denied")) {
		t.Error("unrelated errors should not be treated as cross-device")
	}
}
