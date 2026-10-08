package app

import (
	"os"
	"path/filepath"
	"strings"
	"testing"

	"omakaiju/internal/config"
	"omakaiju/internal/fs"

	tea "charm.land/bubbletea/v2"
)

func key(r rune) tea.KeyMsg {
	return tea.KeyPressMsg{Code: r}
}

func fixture(t *testing.T) (Model, string) {
	t.Helper()
	dir := t.TempDir()
	if err := os.WriteFile(filepath.Join(dir, "alpha.txt"), []byte("a"), 0o644); err != nil {
		t.Fatal(err)
	}
	if err := os.WriteFile(filepath.Join(dir, "beta.txt"), []byte("b"), 0o644); err != nil {
		t.Fatal(err)
	}
	if err := os.Mkdir(filepath.Join(dir, "subdir"), 0o755); err != nil {
		t.Fatal(err)
	}

	files, err := fs.ReadDir(dir)
	if err != nil {
		t.Fatal(err)
	}

	m := NewModel(config.Config{ThemePath: filepath.Join(dir, "nonexistent.toml")})
	m.leftPath, m.leftFiles = dir, files
	m.rightPath, m.rightFiles = dir, files
	m.updatePreview()
	return m, dir
}

func TestCursorNavigationMovesCursor(t *testing.T) {
	m, _ := fixture(t)

	updated, _ := m.Update(key('j'))
	m = updated.(Model)
	if m.leftCursor != 1 {
		t.Fatalf("after j: leftCursor = %d, want 1", m.leftCursor)
	}

	updated, _ = m.Update(key('j'))
	m = updated.(Model)
	if m.leftCursor != 2 {
		t.Fatalf("after jj: leftCursor = %d, want 2", m.leftCursor)
	}

	updated, _ = m.Update(key('k'))
	m = updated.(Model)
	if m.leftCursor != 1 {
		t.Fatalf("after k: leftCursor = %d, want 1", m.leftCursor)
	}
}

func TestEnteringDirectoryChangesPath(t *testing.T) {
	m, dir := fixture(t)

	// ReadDir returns raw directory order, so locate "subdir" rather than
	// assuming an index.
	files := m.visibleFiles(0)
	target := -1
	for i, f := range files {
		if f.Name == "subdir" {
			target = i
		}
	}
	if target < 0 {
		t.Fatal("subdir missing from listing")
	}

	for i := 0; i < target; i++ {
		updated, _ := m.Update(key('j'))
		m = updated.(Model)
	}
	if got := m.visibleFiles(0)[m.leftCursor].Name; got != "subdir" {
		t.Fatalf("cursor not on subdir, got %q", got)
	}

	updated, cmd := m.Update(tea.KeyPressMsg{Code: tea.KeyEnter})
	m = updated.(Model)
	if want := filepath.Join(dir, "subdir"); m.leftPath != want {
		t.Fatalf("after enter: leftPath = %q, want %q", m.leftPath, want)
	}
	if cmd == nil {
		t.Fatal("enter should return a load command")
	}
}

func TestParentDirectoryNavigation(t *testing.T) {
	m, dir := fixture(t)
	m.leftPath = filepath.Join(dir, "subdir")

	updated, cmd := m.Update(tea.KeyPressMsg{Code: 'h'})
	m = updated.(Model)
	if m.leftPath != dir {
		t.Fatalf("after h: leftPath = %q, want %q", m.leftPath, dir)
	}
	if cmd == nil {
		t.Fatal("h should return a load command")
	}
}

func TestTabSwitchesActivePane(t *testing.T) {
	m, _ := fixture(t)

	updated, _ := m.Update(tea.KeyPressMsg{Code: tea.KeyTab})
	m = updated.(Model)
	if m.activePane != 1 {
		t.Fatalf("after tab: activePane = %d, want 1", m.activePane)
	}

	updated, _ = m.Update(key('j'))
	m = updated.(Model)
	if m.rightCursor != 1 {
		t.Fatalf("after tab+j: rightCursor = %d, want 1", m.rightCursor)
	}
	if m.leftCursor != 0 {
		t.Fatalf("after tab+j: leftCursor = %d, want 0", m.leftCursor)
	}
}

func TestFilterNarrowsVisibleFiles(t *testing.T) {
	m, _ := fixture(t)

	updated, _ := m.Update(key('/'))
	m = updated.(Model)
	if m.mode != inputFilter {
		t.Fatal("/ should enter filter mode")
	}

	for _, r := range "beta" {
		updated, _ = m.Update(key(r))
		m = updated.(Model)
	}

	if m.leftFilter != "beta" {
		t.Fatalf("leftFilter = %q, want %q", m.leftFilter, "beta")
	}
	if got := len(m.visibleFiles(0)); got != 1 {
		t.Fatalf("visible files = %d, want 1", got)
	}

	updated, _ = m.Update(tea.KeyPressMsg{Code: tea.KeyEnter})
	m = updated.(Model)
	if m.mode == inputFilter {
		t.Fatal("enter should leave filter mode")
	}
	if len(m.visibleFiles(0)) != 1 {
		t.Fatal("filter should persist after enter")
	}

	updated, _ = m.Update(tea.KeyPressMsg{Code: tea.KeyEscape})
	m = updated.(Model)
	if m.leftFilter != "" {
		t.Fatalf("esc should clear filter, got %q", m.leftFilter)
	}
	if got := len(m.visibleFiles(0)); got != 3 {
		t.Fatalf("after esc: visible files = %d, want 3", got)
	}
}

func TestFilterIsPerPane(t *testing.T) {
	m, _ := fixture(t)

	updated, _ := m.Update(key('/'))
	m = updated.(Model)
	updated, _ = m.Update(key('a'))
	m = updated.(Model)
	updated, _ = m.Update(tea.KeyPressMsg{Code: tea.KeyEnter})
	m = updated.(Model)

	if m.leftFilter == "" {
		t.Fatal("left filter should be set")
	}
	if m.rightFilter != "" {
		t.Fatalf("right filter should stay empty, got %q", m.rightFilter)
	}
}

func TestPreviewTracksCursor(t *testing.T) {
	m, dir := fixture(t)

	// Directories sort first, then alpha/beta; walk to beta.txt explicitly so
	// the assertion does not depend on listing order.
	idx := indexOf(m.visibleFiles(0), "beta.txt")
	if idx < 0 {
		t.Fatal("beta.txt missing from listing")
	}
	for i := 0; i < idx; i++ {
		updated, _ := m.Update(key('j'))
		m = updated.(Model)
	}

	want := filepath.Join(dir, "beta.txt")
	if m.previewPath != want {
		t.Fatalf("previewPath = %q, want %q", m.previewPath, want)
	}
	if m.previewCachePath != want {
		t.Fatalf("previewCachePath = %q, want %q", m.previewCachePath, want)
	}
}

func indexOf(files []fs.Entry, name string) int {
	for i, f := range files {
		if f.Name == name {
			return i
		}
	}
	return -1
}

func TestPreviewIsHighlightedForSourceFile(t *testing.T) {
	m, dir := fixture(t)

	goFile := filepath.Join(dir, "main.go")
	src := "package main\n\n// comment\nfunc main() {\n\tprintln(\"hi\")\n}\n"
	if err := os.WriteFile(goFile, []byte(src), 0o644); err != nil {
		t.Fatal(err)
	}

	files, err := fs.ReadDir(dir)
	if err != nil {
		t.Fatal(err)
	}
	m.leftFiles = files
	m.leftPath = dir

	// Directories sort first, so walk the cursor onto main.go.
	idx := indexOf(m.visibleFiles(0), "main.go")
	if idx < 0 {
		t.Fatal("main.go missing from listing")
	}
	for i := 0; i < idx; i++ {
		updated, _ := m.Update(key('j'))
		m = updated.(Model)
	}

	if m.previewPath != goFile {
		t.Fatalf("previewPath = %q, want %q", m.previewPath, goFile)
	}
	if len(m.previewCache) == 0 {
		t.Fatal("expected raw preview cache to be populated")
	}
	if len(m.previewHL) != len(m.previewCache) {
		t.Fatalf("highlighted lines = %d, want %d", len(m.previewHL), len(m.previewCache))
	}
	if !strings.Contains(strings.Join(m.previewHL, "\n"), "\x1b[") {
		t.Error("expected ANSI sequences in highlighted preview")
	}
}
