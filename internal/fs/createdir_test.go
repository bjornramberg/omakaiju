package fs

import (
	"os"
	"path/filepath"
	"testing"
)

// CreateDir makes exactly the directory it was asked for. It is not
// MkdirAll: a missing parent is not created, and a name that is already
// taken is refused rather than silently reported as a creation.
func TestCreateDirCreatesOneDirectoryAndRefusesExisting(t *testing.T) {
	dir := t.TempDir()
	path := filepath.Join(dir, "one")

	if err := CreateDir(path); err != nil {
		t.Fatalf("CreateDir(%q) = %v, want it to create the directory", path, err)
	}
	info, err := os.Stat(path)
	if err != nil {
		t.Fatalf("directory missing after CreateDir: %v", err)
	}
	if !info.IsDir() {
		t.Error("CreateDir should create a directory")
	}

	if err := CreateDir(path); err == nil {
		t.Fatal("CreateDir should refuse a name that is already taken")
	}

	nested := filepath.Join(dir, "missing", "child")
	if err := CreateDir(nested); err == nil {
		t.Fatal("CreateDir should not create missing parents")
	}
	if _, err := os.Stat(filepath.Join(dir, "missing")); !os.IsNotExist(err) {
		t.Error("CreateDir must not leave a parent directory behind")
	}
}
