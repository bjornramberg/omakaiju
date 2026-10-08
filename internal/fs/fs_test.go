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
