package fs

import (
	"os"
	"path/filepath"
	"testing"
)

// A walk of a very large tree must not exhaust the machine's memory. The
// paths are kept in memory and the matcher allocates several times that
// again, so an unbounded walk can end in a fatal error — which nothing
// recovers from, and which leaves the terminal in raw mode.
func TestWalkDirStopsAtTheLimit(t *testing.T) {
	old := walkLimit
	walkLimit = 10
	defer func() { walkLimit = old }()

	dir := t.TempDir()
	for i := 0; i < 30; i++ {
		name := filepath.Join(dir, "file"+string(rune('a'+i%26))+string(rune('0'+i/26))+".txt")
		if err := os.WriteFile(name, nil, 0o644); err != nil {
			t.Fatal(err)
		}
	}

	result, err := WalkDir(dir)
	if err != nil {
		t.Fatal(err)
	}
	if len(result.Files) != walkLimit {
		t.Errorf("collected %d files, want the limit of %d", len(result.Files), walkLimit)
	}
	if !result.Truncated {
		t.Error("a walk that stopped at the limit must say so")
	}
}

// An ordinary tree is not truncated.
func TestWalkDirDoesNotTruncateASmallTree(t *testing.T) {
	old := walkLimit
	walkLimit = 1000
	defer func() { walkLimit = old }()

	dir := t.TempDir()
	for i := 0; i < 5; i++ {
		if err := os.WriteFile(filepath.Join(dir, "f"+string(rune('0'+i))+".txt"), nil, 0o644); err != nil {
			t.Fatal(err)
		}
	}

	result, err := WalkDir(dir)
	if err != nil {
		t.Fatal(err)
	}
	if result.Truncated {
		t.Error("a small tree should not be reported as truncated")
	}
	if len(result.Files) != 5 {
		t.Errorf("collected %d files, want 5", len(result.Files))
	}
}
