package ui

import (
	"os"
	"path/filepath"
	"strings"
	"testing"

	"github.com/bjornramberg/omakaiju/internal/fs"
)

// ReadDir must carry permission bits through to the entry.
func TestReadDirCarriesMode(t *testing.T) {
	dir := t.TempDir()
	path := filepath.Join(dir, "script.sh")
	if err := os.WriteFile(path, []byte("echo hi\n"), 0o755); err != nil {
		t.Fatal(err)
	}
	sub := filepath.Join(dir, "sub")
	if err := os.Mkdir(sub, 0o755); err != nil {
		t.Fatal(err)
	}

	entries, err := fs.ReadDir(dir)
	if err != nil {
		t.Fatal(err)
	}

	for _, e := range entries {
		switch e.Name {
		case "script.sh":
			if got := e.Mode.String(); !strings.Contains(got, "rwxr-xr-x") {
				t.Errorf("script.sh mode = %q, want executable bits", got)
			}
		case "sub":
			if !e.IsDir || e.Mode.String() != "drwxr-xr-x" {
				t.Errorf("sub mode = %q, want drwxr-xr-x", e.Mode.String())
			}
		}
	}
}
