package fs

import (
	"os"
	"path/filepath"
	"testing"
)

func TestReadDirSortsDirsFirstThenAlpha(t *testing.T) {
	dir := t.TempDir()

	// Create in deliberately non-alphabetical order.
	for _, name := range []string{"zeta.txt", "Alpha.txt", "beta.txt", "alpha.txt"} {
		if err := os.WriteFile(filepath.Join(dir, name), nil, 0o644); err != nil {
			t.Fatal(err)
		}
	}
	for _, name := range []string{"zoo", "api"} {
		if err := os.Mkdir(filepath.Join(dir, name), 0o755); err != nil {
			t.Fatal(err)
		}
	}

	entries, err := ReadDir(dir)
	if err != nil {
		t.Fatal(err)
	}

	want := []string{"api", "zoo", "Alpha.txt", "alpha.txt", "beta.txt", "zeta.txt"}
	if len(entries) != len(want) {
		t.Fatalf("got %d entries, want %d", len(entries), len(want))
	}
	for i, w := range want {
		if entries[i].Name != w {
			got := make([]string, len(entries))
			for j, e := range entries {
				got[j] = e.Name
			}
			t.Fatalf("order = %v, want %v", got, want)
		}
	}
	if !entries[0].IsDir || !entries[1].IsDir {
		t.Error("directories should sort before files")
	}
	if entries[2].IsDir {
		t.Error("files should sort after directories")
	}
}

func TestReadDirIsDeterministic(t *testing.T) {
	dir := t.TempDir()
	for _, name := range []string{"c", "a", "b", "e", "d"} {
		if err := os.WriteFile(filepath.Join(dir, name), nil, 0o644); err != nil {
			t.Fatal(err)
		}
	}

	first, err := ReadDir(dir)
	if err != nil {
		t.Fatal(err)
	}
	for i := 0; i < 5; i++ {
		next, err := ReadDir(dir)
		if err != nil {
			t.Fatal(err)
		}
		for j := range first {
			if first[j].Name != next[j].Name {
				t.Fatalf("unstable order at %d: %q vs %q", j, first[j].Name, next[j].Name)
			}
		}
	}
}

// A symlink to a directory must be reported as a directory. DirEntry.IsDir
// reports the raw dirent, which is not a directory for a link, and treating it
// as a file sent Enter to the desktop opener instead of navigating.
func TestReadDirResolvesSymlinksToDirectories(t *testing.T) {
	dir := t.TempDir()
	target := filepath.Join(dir, "target")
	if err := os.Mkdir(target, 0o755); err != nil {
		t.Fatal(err)
	}
	if err := os.Symlink(target, filepath.Join(dir, "link")); err != nil {
		t.Skipf("symlinks unavailable: %v", err)
	}
	if err := os.WriteFile(filepath.Join(dir, "file.txt"), nil, 0o644); err != nil {
		t.Fatal(err)
	}

	entries, err := ReadDir(dir)
	if err != nil {
		t.Fatal(err)
	}

	var link Entry
	for _, e := range entries {
		if e.Name == "link" {
			link = e
		}
	}
	if link.Name == "" {
		t.Fatal("link missing from listing")
	}
	if !link.IsDir {
		t.Error("a symlink to a directory should be reported as a directory")
	}
	if !link.IsLink {
		t.Error("a symlink should be flagged as a link")
	}
	if link.Mode&os.ModeSymlink == 0 {
		t.Errorf("mode should keep the symlink bit so the permissions column shows l, got %v", link.Mode)
	}

	// Directories sort first, so the link must sit ahead of the plain file.
	if entries[0].Name != "link" || entries[1].Name != "target" || entries[2].Name != "file.txt" {
		names := make([]string, len(entries))
		for i, e := range entries {
			names[i] = e.Name
		}
		t.Errorf("order = %v, want link and target before file.txt", names)
	}
}

func TestReadDirSymlinkToFileStaysFile(t *testing.T) {
	dir := t.TempDir()
	target := filepath.Join(dir, "target.txt")
	if err := os.WriteFile(target, []byte("hello"), 0o644); err != nil {
		t.Fatal(err)
	}
	if err := os.Symlink(target, filepath.Join(dir, "link.txt")); err != nil {
		t.Skipf("symlinks unavailable: %v", err)
	}

	entries, err := ReadDir(dir)
	if err != nil {
		t.Fatal(err)
	}

	for _, e := range entries {
		if e.Name != "link.txt" {
			continue
		}
		if e.IsDir {
			t.Error("a symlink to a file should not be reported as a directory")
		}
		if !e.IsLink {
			t.Error("a symlink should be flagged as a link")
		}
		// The link's own size is the length of the stored path, which is
		// meaningless in the listing; the target's size is what belongs there.
		if e.Size != int64(len("hello")) {
			t.Errorf("size = %d, want the target's %d", e.Size, len("hello"))
		}
	}
}

// A dangling link has no target to resolve, so it stays a file. Enter then
// hands it to the desktop opener, which reports the broken target, rather
// than the pane refusing to open it.
func TestReadDirBrokenSymlinkStaysFile(t *testing.T) {
	dir := t.TempDir()
	if err := os.Symlink(filepath.Join(dir, "missing"), filepath.Join(dir, "dangling")); err != nil {
		t.Skipf("symlinks unavailable: %v", err)
	}

	entries, err := ReadDir(dir)
	if err != nil {
		t.Fatal(err)
	}

	if len(entries) != 1 {
		t.Fatalf("got %d entries, want 1", len(entries))
	}
	if entries[0].IsDir {
		t.Error("a broken symlink should not be reported as a directory")
	}
	if !entries[0].IsLink {
		t.Error("a broken symlink should still be flagged as a link")
	}
}

func TestWalkDirIsSorted(t *testing.T) {
	dir := t.TempDir()
	for _, name := range []string{"z.txt", "a.txt", "m.txt"} {
		if err := os.WriteFile(filepath.Join(dir, name), nil, 0o644); err != nil {
			t.Fatal(err)
		}
	}
	nested := filepath.Join(dir, "nested")
	if err := os.Mkdir(nested, 0o755); err != nil {
		t.Fatal(err)
	}
	if err := os.WriteFile(filepath.Join(nested, "inner.txt"), nil, 0o644); err != nil {
		t.Fatal(err)
	}

	files, err := WalkDir(dir)
	if err != nil {
		t.Fatal(err)
	}

	want := []string{
		filepath.Join(dir, "a.txt"),
		filepath.Join(dir, "m.txt"),
		filepath.Join(nested, "inner.txt"),
		filepath.Join(dir, "z.txt"),
	}
	if len(files) != len(want) {
		t.Fatalf("got %v, want %v", files, want)
	}
	for i := range want {
		if files[i] != want[i] {
			t.Fatalf("got %v, want %v", files, want)
		}
	}
}
