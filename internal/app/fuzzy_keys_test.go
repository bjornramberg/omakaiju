package app

import (
	"os"
	"path/filepath"
	"testing"

	"github.com/bjornramberg/omakaiju/internal/config"
	"github.com/bjornramberg/omakaiju/internal/fs"

	tea "charm.land/bubbletea/v2"
)

// fuzzyCtrlKey builds a Ctrl+key press the way a terminal delivers it: the
// base key in Code and ctrl in Mod.
func fuzzyCtrlKey(r rune) tea.KeyMsg {
	return tea.KeyPressMsg(tea.Key{Code: r, Mod: tea.ModCtrl})
}

// fuzzyKeysModel opens the finder over a small, populated directory with two
// results already in place, so both typing and navigation can be observed.
func fuzzyKeysModel(t *testing.T) Model {
	t.Helper()

	dir := t.TempDir()
	for _, name := range []string{"json.txt", "kube.txt", "other.txt"} {
		if err := os.WriteFile(filepath.Join(dir, name), []byte("x"), 0o644); err != nil {
			t.Fatal(err)
		}
	}
	files, err := fs.ReadDir(dir)
	if err != nil {
		t.Fatal(err)
	}

	m := NewModel(config.Config{ThemePath: filepath.Join(dir, "nonexistent.toml")})
	m.leftPath, m.leftFiles = dir, files
	m.rightPath, m.rightFiles = dir, files
	m.fuzzyActive = true
	m.fuzzyAllFiles = []string{
		filepath.Join(dir, "json.txt"),
		filepath.Join(dir, "kube.txt"),
	}
	m.fuzzyResults = append([]string(nil), m.fuzzyAllFiles...)
	return m
}

// j and k are letters before they are navigation. Typing them must extend the
// query, or a path containing them can never be found: "json" used to search
// for "son".
func TestFuzzyFinderTypesJAndK(t *testing.T) {
	for _, typed := range []string{"j", "k", "json", "kube"} {
		m := fuzzyKeysModel(t)
		m.fuzzyInput = ""

		for _, r := range typed {
			updated, _ := m.Update(key(r))
			m = updated.(Model)
		}

		if m.fuzzyInput != typed {
			t.Errorf("typing %q left the query as %q", typed, m.fuzzyInput)
		}
	}
}

// Navigation moves to ctrl+j and ctrl+k as well as the arrows, and must not
// put those letters into the query.
func TestFuzzyFinderCtrlJAndCtrlKNavigate(t *testing.T) {
	m := fuzzyKeysModel(t)
	m.fuzzyCursor = 0

	updated, _ := m.Update(fuzzyCtrlKey('j'))
	m = updated.(Model)
	if m.fuzzyCursor != 1 {
		t.Errorf("ctrl+j: fuzzyCursor = %d, want 1", m.fuzzyCursor)
	}

	updated, _ = m.Update(fuzzyCtrlKey('k'))
	m = updated.(Model)
	if m.fuzzyCursor != 0 {
		t.Errorf("ctrl+k: fuzzyCursor = %d, want 0", m.fuzzyCursor)
	}

	// The arrows still navigate as they always did.
	updated, _ = m.Update(arrow(tea.KeyDown))
	m = updated.(Model)
	if m.fuzzyCursor != 1 {
		t.Errorf("down: fuzzyCursor = %d, want 1", m.fuzzyCursor)
	}
	updated, _ = m.Update(arrow(tea.KeyUp))
	m = updated.(Model)
	if m.fuzzyCursor != 0 {
		t.Errorf("up: fuzzyCursor = %d, want 0", m.fuzzyCursor)
	}

	if m.fuzzyInput != "" {
		t.Errorf("navigation typed into the query: %q", m.fuzzyInput)
	}
}

// Enter jumps to the selected result and leaves the cursor on it, rather than
// on the top of the directory it lives in.
func TestFuzzyEnterLandsCursorOnTheMatch(t *testing.T) {
	dir := t.TempDir()
	sub := filepath.Join(dir, "sub")
	if err := os.Mkdir(sub, 0o755); err != nil {
		t.Fatal(err)
	}
	// Two entries, so landing on the second is visibly different from the
	// top of the listing.
	for _, name := range []string{"aaa.txt", "target.txt"} {
		if err := os.WriteFile(filepath.Join(sub, name), []byte("x"), 0o644); err != nil {
			t.Fatal(err)
		}
	}
	if err := os.WriteFile(filepath.Join(dir, "root.txt"), []byte("x"), 0o644); err != nil {
		t.Fatal(err)
	}
	files, err := fs.ReadDir(dir)
	if err != nil {
		t.Fatal(err)
	}

	m := NewModel(config.Config{ThemePath: filepath.Join(dir, "nonexistent.toml")})
	m.leftPath, m.leftFiles = dir, files
	m.rightPath, m.rightFiles = dir, files

	matched := filepath.Join(sub, "target.txt")
	m.fuzzyActive = true
	m.fuzzyAllFiles = []string{matched}
	m.fuzzyResults = []string{matched}
	m.fuzzyCursor = 0

	updated, cmd := m.Update(tea.KeyPressMsg(tea.Key{Code: tea.KeyEnter}))
	m = updated.(Model)

	if m.fuzzyActive {
		t.Fatal("enter should close the finder")
	}
	if cmd == nil {
		t.Fatal("enter should load the matched result's directory")
	}
	m = loadDir(t, m, 0)

	if m.leftPath != sub {
		t.Fatalf("leftPath = %q, want the result's directory %q", m.leftPath, sub)
	}
	visible := m.visibleFiles(0)
	if m.leftCursor >= len(visible) {
		t.Fatalf("cursor %d is outside the %d-row listing", m.leftCursor, len(visible))
	}
	if got := visible[m.leftCursor].Name; got != "target.txt" {
		t.Errorf("cursor landed on %q at row %d, want target.txt", got, m.leftCursor)
	}
	if m.leftCursor == 0 {
		t.Error("cursor is still at the top of the listing, not on the match")
	}
}
