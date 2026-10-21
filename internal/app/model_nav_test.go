package app

import (
	"os"
	"path/filepath"
	"strings"
	"testing"

	"github.com/bjornramberg/omakaiju/internal/config"
	"github.com/bjornramberg/omakaiju/internal/fs"

	tea "charm.land/bubbletea/v2"
)

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

	updated, _ := m.Update(arrow(tea.KeyDown))
	m = updated.(Model)
	if m.leftCursor != 1 {
		t.Fatalf("after down: leftCursor = %d, want 1", m.leftCursor)
	}

	updated, _ = m.Update(arrow(tea.KeyDown))
	m = updated.(Model)
	if m.leftCursor != 2 {
		t.Fatalf("after down down: leftCursor = %d, want 2", m.leftCursor)
	}

	updated, _ = m.Update(arrow(tea.KeyUp))
	m = updated.(Model)
	if m.leftCursor != 1 {
		t.Fatalf("after up: leftCursor = %d, want 1", m.leftCursor)
	}
}

func TestEnteringDirectoryChangesPath(t *testing.T) {
	m, dir := fixture(t)

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
		updated, _ := m.Update(arrow(tea.KeyDown))
		m = updated.(Model)
	}
	if got := m.visibleFiles(0)[m.leftCursor].Name; got != "subdir" {
		t.Fatalf("cursor not on subdir, got %q", got)
	}

	updated, cmd := m.Update(tea.KeyPressMsg(tea.Key{Code: tea.KeyEnter}))
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

	updated, cmd := m.Update(arrow(tea.KeyLeft))
	m = updated.(Model)
	if m.leftPath != dir {
		t.Fatalf("after left: leftPath = %q, want %q", m.leftPath, dir)
	}
	if cmd == nil {
		t.Fatal("left should return a load command")
	}
}

func TestTabSwitchesActivePane(t *testing.T) {
	m, _ := fixture(t)

	updated, _ := m.Update(tea.KeyPressMsg(tea.Key{Code: tea.KeyTab}))
	m = updated.(Model)
	if m.activePane != 1 {
		t.Fatalf("after tab: activePane = %d, want 1", m.activePane)
	}

	updated, _ = m.Update(arrow(tea.KeyDown))
	m = updated.(Model)
	if m.rightCursor != 1 {
		t.Fatalf("after tab+down: rightCursor = %d, want 1", m.rightCursor)
	}
	if m.leftCursor != 0 {
		t.Fatalf("after tab+down: leftCursor = %d, want 0", m.leftCursor)
	}
}

func TestFilterNarrowsVisibleFiles(t *testing.T) {
	m, _ := fixture(t)

	updated, _ := m.Update(shiftKey('f'))
	m = updated.(Model)
	if m.mode != inputFilter {
		t.Fatal("Shift+/ should enter filter mode")
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

	updated, _ = m.Update(tea.KeyPressMsg(tea.Key{Code: tea.KeyEnter}))
	m = updated.(Model)
	if m.mode == inputFilter {
		t.Fatal("enter should leave filter mode")
	}
	if len(m.visibleFiles(0)) != 1 {
		t.Fatal("filter should persist after enter")
	}

	updated, _ = m.Update(tea.KeyPressMsg(tea.Key{Code: tea.KeyEscape}))
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

	updated, _ := m.Update(shiftKey('f'))
	m = updated.(Model)
	updated, _ = m.Update(key('a'))
	m = updated.(Model)
	updated, _ = m.Update(tea.KeyPressMsg(tea.Key{Code: tea.KeyEnter}))
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

	idx := indexOf(m.visibleFiles(0), "beta.txt")
	if idx < 0 {
		t.Fatal("beta.txt missing from listing")
	}
	for i := 0; i < idx; i++ {
		updated, _ := m.Update(arrow(tea.KeyDown))
		m = updated.(Model)
	}
	m = settlePreview(t, m)

	want := filepath.Join(dir, "beta.txt")
	if m.previewPath != want {
		t.Fatalf("previewPath = %q, want %q", m.previewPath, want)
	}
	if m.previewCachePath != want {
		t.Fatalf("previewCachePath = %q, want %q", m.previewCachePath, want)
	}
}

// settlePreview runs the pending preview load and feeds the result back, so
// a test can assert on a preview that is now gathered in the background.
func settlePreview(t *testing.T, m Model) Model {
	t.Helper()
	cmd := m.updatePreview()
	if cmd == nil {
		return m
	}
	updated, _ := m.Update(cmd())
	return updated.(Model)
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

	idx := indexOf(m.visibleFiles(0), "main.go")
	if idx < 0 {
		t.Fatal("main.go missing from listing")
	}
	for i := 0; i < idx; i++ {
		updated, _ := m.Update(arrow(tea.KeyDown))
		m = updated.(Model)
	}
	m = settlePreview(t, m)

	if m.previewPath != goFile {
		t.Fatalf("previewPath = %q, want %q", m.previewPath, goFile)
	}
	if len(m.previewContent.Text) == 0 {
		t.Fatal("expected raw preview content to be populated")
	}
	if len(m.previewContent.Highlighted) != len(m.previewContent.Text) {
		t.Fatalf("highlighted lines = %d, want %d", len(m.previewContent.Highlighted), len(m.previewContent.Text))
	}
	if !strings.Contains(strings.Join(m.previewContent.Highlighted, "\n"), "\x1b[") {
		t.Error("expected ANSI sequences in highlighted preview")
	}
}

func loadDir(t *testing.T, m Model, pane int) Model {
	t.Helper()
	path := m.rawPath(pane)
	files, err := fs.ReadDir(path)
	if err != nil {
		t.Fatal(err)
	}
	updated, _ := m.Update(dirLoadedMsg{pane: pane, path: path, files: files})
	return updated.(Model)
}

func twoDirFixture(t *testing.T) (Model, string) {
	t.Helper()
	dir := t.TempDir()
	for _, name := range []string{"aaa", "zzz"} {
		if err := os.Mkdir(filepath.Join(dir, name), 0o755); err != nil {
			t.Fatal(err)
		}
	}
	if err := os.WriteFile(filepath.Join(dir, "file.txt"), []byte("x"), 0o644); err != nil {
		t.Fatal(err)
	}

	files, err := fs.ReadDir(dir)
	if err != nil {
		t.Fatal(err)
	}
	m := NewModel(config.Config{ThemePath: filepath.Join(dir, "none.toml")})
	m.leftPath, m.leftFiles = dir, files
	m.rightPath, m.rightFiles = dir, files
	m.updatePreview()
	return m, dir
}

func TestGoingUpRestoresCursorOnEnteredFolder(t *testing.T) {
	m, dir := twoDirFixture(t)

	m = moveCursorTo(t, m, "zzz")
	if idx := indexOf(m.visibleFiles(0), "zzz"); idx != 1 {
		t.Fatalf("setup: zzz is at index %d, want 1", idx)
	}

	updated, _ := m.Update(tea.KeyPressMsg(tea.Key{Code: tea.KeyEnter}))
	m = updated.(Model)
	m = loadDir(t, m, 0)
	if m.leftPath != filepath.Join(dir, "zzz") {
		t.Fatalf("leftPath = %q, want the zzz subdir", m.leftPath)
	}

	updated, _ = m.Update(arrow(tea.KeyLeft))
	m = updated.(Model)
	m = loadDir(t, m, 0)

	if m.leftPath != dir {
		t.Fatalf("leftPath = %q, want %q", m.leftPath, dir)
	}
	if m.leftCursor != 1 {
		t.Errorf("leftCursor = %d, want 1 (the directory we came from)", m.leftCursor)
	}
}

func TestRestoreFallsBackToTopWhenFolderGone(t *testing.T) {
	m, dir := fixture(t)

	m = moveCursorTo(t, m, "subdir")
	updated, _ := m.Update(tea.KeyPressMsg(tea.Key{Code: tea.KeyEnter}))
	m = updated.(Model)
	m = loadDir(t, m, 0)

	if err := os.Remove(filepath.Join(dir, "subdir")); err != nil {
		t.Fatal(err)
	}

	updated, _ = m.Update(arrow(tea.KeyLeft))
	m = updated.(Model)
	m = loadDir(t, m, 0)

	if m.leftCursor != 0 {
		t.Errorf("leftCursor = %d, want 0 when the target vanished", m.leftCursor)
	}
}

func TestPlainReloadStillStartsAtTop(t *testing.T) {
	m, _ := fixture(t)
	m = moveCursorTo(t, m, "beta.txt")

	updated, _ := m.Update(shiftKey('e'))
	m = updated.(Model)
	m = loadDir(t, m, 0)

	if m.leftCursor != 0 {
		t.Errorf("leftCursor = %d, want 0 after a refresh", m.leftCursor)
	}
}

func TestRestoreIsPerPane(t *testing.T) {
	m, dir := fixture(t)

	m = moveCursorTo(t, m, "subdir")
	updated, _ := m.Update(tea.KeyPressMsg(tea.Key{Code: tea.KeyEnter}))
	m = updated.(Model)
	m = loadDir(t, m, 0)

	updated, _ = m.Update(arrow(tea.KeyLeft))
	m = updated.(Model)
	m = loadDir(t, m, 0)

	if m.rightRestore != "" {
		t.Errorf("rightRestore = %q, want empty", m.rightRestore)
	}
	if m.leftPath != dir {
		t.Errorf("leftPath = %q, want %q", m.leftPath, dir)
	}
}