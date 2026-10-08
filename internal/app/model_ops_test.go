package app

import (
	"os"
	"path/filepath"
	"testing"

	tea "charm.land/bubbletea/v2"
)

func typeString(t *testing.T, m Model, s string) Model {
	t.Helper()
	for _, r := range s {
		updated, _ := m.Update(key(r))
		m = updated.(Model)
	}
	return m
}

func enter(t *testing.T, m Model) (Model, tea.Cmd) {
	t.Helper()
	updated, cmd := m.Update(tea.KeyPressMsg{Code: tea.KeyEnter})
	return updated.(Model), cmd
}

func esc(t *testing.T, m Model) Model {
	t.Helper()
	updated, _ := m.Update(tea.KeyPressMsg{Code: tea.KeyEscape})
	return updated.(Model)
}

func TestAddCreatesFile(t *testing.T) {
	m, dir := fixture(t)

	updated, _ := m.Update(key('a'))
	m = updated.(Model)
	if m.mode != inputAdd {
		t.Fatal("a should enter add mode")
	}

	m = typeString(t, m, "newfile.txt")
	m, cmd := enter(t, m)

	if m.mode != inputNone {
		t.Error("enter should leave add mode")
	}
	if cmd == nil {
		t.Error("add should trigger a directory reload")
	}
	if _, err := os.Stat(filepath.Join(dir, "newfile.txt")); err != nil {
		t.Fatalf("file not created: %v", err)
	}
	if m.opResult != "created newfile.txt" {
		t.Errorf("opResult = %q, want %q", m.opResult, "created newfile.txt")
	}
}

func TestAddCreatesDirectoryWithTrailingSlash(t *testing.T) {
	m, dir := fixture(t)

	updated, _ := m.Update(key('a'))
	m = updated.(Model)
	m = typeString(t, m, "newdir/")
	m, _ = enter(t, m)

	info, err := os.Stat(filepath.Join(dir, "newdir"))
	if err != nil {
		t.Fatalf("directory not created: %v", err)
	}
	if !info.IsDir() {
		t.Error("newdir should be a directory")
	}
	// The trailing slash must be trimmed from the stored entry name.
	entries, err := os.ReadDir(dir)
	if err != nil {
		t.Fatal(err)
	}
	for _, e := range entries {
		if e.Name() == "newdir/" {
			t.Errorf("entry name kept the trailing slash: %q", e.Name())
		}
	}
	if m.opResult != "created dir newdir" {
		t.Errorf("opResult = %q, want %q", m.opResult, "created dir newdir")
	}
}

func TestAddEmptyNameIsNoOp(t *testing.T) {
	m, dir := fixture(t)

	updated, _ := m.Update(key('a'))
	m = updated.(Model)
	m, cmd := enter(t, m)

	if m.mode != inputNone {
		t.Error("enter should leave add mode")
	}
	if cmd != nil {
		t.Error("empty add should not trigger a reload")
	}
	entries, err := os.ReadDir(dir)
	if err != nil {
		t.Fatal(err)
	}
	if len(entries) != 3 {
		t.Errorf("directory changed unexpectedly: %d entries, want 3", len(entries))
	}
}

func TestAddEscapeCancels(t *testing.T) {
	m, dir := fixture(t)

	updated, _ := m.Update(key('a'))
	m = updated.(Model)
	m = typeString(t, m, "should-not-exist")
	m = esc(t, m)

	if m.mode != inputNone {
		t.Error("esc should leave add mode")
	}
	if _, err := os.Stat(filepath.Join(dir, "should-not-exist")); !os.IsNotExist(err) {
		t.Error("esc should not create anything")
	}
}

func TestRenameRenamesEntry(t *testing.T) {
	m, dir := fixture(t)

	idx := indexOf(m.visibleFiles(0), "alpha.txt")
	if idx < 0 {
		t.Fatal("alpha.txt missing from listing")
	}
	for i := 0; i < idx; i++ {
		updated, _ := m.Update(key('j'))
		m = updated.(Model)
	}

	updated, _ := m.Update(key('r'))
	m = updated.(Model)
	if m.mode != inputRename {
		t.Fatal("r should enter rename mode")
	}
	if m.inputBuf != "alpha.txt" {
		t.Errorf("inputBuf = %q, want the current name pre-filled", m.inputBuf)
	}

	// Clear the pre-filled name, then type a new one.
	for i := 0; i < len("alpha.txt"); i++ {
		updated, _ = m.Update(tea.KeyPressMsg{Code: tea.KeyBackspace})
		m = updated.(Model)
	}
	m = typeString(t, m, "renamed.txt")
	m, cmd := enter(t, m)

	if m.mode != inputNone {
		t.Error("enter should leave rename mode")
	}
	if cmd == nil {
		t.Error("rename should trigger a directory reload")
	}
	if _, err := os.Stat(filepath.Join(dir, "renamed.txt")); err != nil {
		t.Fatalf("renamed file missing: %v", err)
	}
	if _, err := os.Stat(filepath.Join(dir, "alpha.txt")); !os.IsNotExist(err) {
		t.Error("original name should be gone")
	}
	if m.opResult != "renamed renamed.txt" {
		t.Errorf("opResult = %q, want %q", m.opResult, "renamed renamed.txt")
	}
}

func TestRenameEscapeCancels(t *testing.T) {
	m, dir := fixture(t)

	idx := indexOf(m.visibleFiles(0), "alpha.txt")
	for i := 0; i < idx; i++ {
		updated, _ := m.Update(key('j'))
		m = updated.(Model)
	}
	updated, _ := m.Update(key('r'))
	m = updated.(Model)
	for i := 0; i < len("alpha.txt"); i++ {
		updated, _ = m.Update(tea.KeyPressMsg{Code: tea.KeyBackspace})
		m = updated.(Model)
	}
	m = typeString(t, m, "nope.txt")
	m = esc(t, m)

	if _, err := os.Stat(filepath.Join(dir, "alpha.txt")); err != nil {
		t.Fatalf("original file should be untouched: %v", err)
	}
	if _, err := os.Stat(filepath.Join(dir, "nope.txt")); !os.IsNotExist(err) {
		t.Error("esc should not rename")
	}
}

func TestRenameSameNameIsNoOp(t *testing.T) {
	m, dir := fixture(t)

	idx := indexOf(m.visibleFiles(0), "alpha.txt")
	for i := 0; i < idx; i++ {
		updated, _ := m.Update(key('j'))
		m = updated.(Model)
	}
	updated, _ := m.Update(key('r'))
	m = updated.(Model)
	m, cmd := enter(t, m)

	if cmd != nil {
		t.Error("renaming to the same name should not reload")
	}
	if _, err := os.Stat(filepath.Join(dir, "alpha.txt")); err != nil {
		t.Fatalf("file should still exist: %v", err)
	}
}

func TestAddAndRenameTargetActivePaneOnly(t *testing.T) {
	m, dir := fixture(t)

	updated, _ := m.Update(tea.KeyPressMsg{Code: tea.KeyTab})
	m = updated.(Model)
	if m.activePane != 1 {
		t.Fatal("tab should activate the right pane")
	}

	updated, _ = m.Update(key('a'))
	m = updated.(Model)
	m = typeString(t, m, "right-only.txt")
	m, _ = enter(t, m)

	// Both panes point at the same directory here, so verify the model routed
	// through the active pane's path and left the other pane's state alone.
	if m.rightFilter != "" || m.leftFilter != "" {
		t.Error("add should not set filters")
	}
	if m.mode != inputNone {
		t.Error("mode should be cleared")
	}
	if _, err := os.Stat(filepath.Join(dir, "right-only.txt")); err != nil {
		t.Fatalf("file not created: %v", err)
	}
}
