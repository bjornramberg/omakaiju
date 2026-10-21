package app

import (
	"os"
	"path/filepath"
	"testing"

	tea "charm.land/bubbletea/v2"
	"github.com/bjornramberg/omakaiju/internal/config"
	"github.com/bjornramberg/omakaiju/internal/fs"
)

// The preview used to be built on the UI thread: the file was read and
// syntax-coloured inline, so every cursor move waited for the disk and the
// highlighter. This measures the cost of the keypress itself.
func BenchmarkCursorMoveWithPreview(b *testing.B) {
	dir := b.TempDir()

	// A realistic source file: a thousand lines is what the preview reads.
	src := "package main\n\nimport \"fmt\"\n\nfunc main() {\n"
	for i := 0; i < 1000; i++ {
		src += "\tfmt.Println(\"hello\", 42)\n"
	}
	src += "}\n"
	if err := os.WriteFile(filepath.Join(dir, "main.go"), []byte(src), 0o644); err != nil {
		b.Fatal(err)
	}

	files, err := fs.ReadDir(dir)
	if err != nil {
		b.Fatal(err)
	}

	m := NewModel(config.Config{ThemePath: filepath.Join(dir, "nonexistent.toml")})
	m.width, m.height = 120, 40
	m.leftPath, m.leftFiles = dir, files
	m.rightPath, m.rightFiles = dir, files

	b.ResetTimer()
	for i := 0; i < b.N; i++ {
		updated, _ := m.Update(arrow(tea.KeyDown))
		m = updated.(Model)
	}
}

// The render path must not touch the disk at all. It draws what it is
// given, and the reading happens in the background.
func BenchmarkRenderWithPreview(b *testing.B) {
	dir := b.TempDir()
	src := "package main\n\nimport \"fmt\"\n\nfunc main() {\n"
	for i := 0; i < 1000; i++ {
		src += "\tfmt.Println(\"hello\", 42)\n"
	}
	src += "}\n"
	if err := os.WriteFile(filepath.Join(dir, "main.go"), []byte(src), 0o644); err != nil {
		b.Fatal(err)
	}
	files, err := fs.ReadDir(dir)
	if err != nil {
		b.Fatal(err)
	}

	m := NewModel(config.Config{ThemePath: filepath.Join(dir, "nonexistent.toml")})
	m.width, m.height = 120, 40
	m.leftPath, m.leftFiles = dir, files
	m.rightPath, m.rightFiles = dir, files
	m = settlePreviewB(b, m)

	b.ResetTimer()
	for i := 0; i < b.N; i++ {
		_ = m.View()
	}
}

// settlePreviewB is the benchmark twin of settlePreview.
func settlePreviewB(b *testing.B, m Model) Model {
	b.Helper()
	cmd := m.updatePreview()
	if cmd == nil {
		return m
	}
	updated, _ := m.Update(cmd())
	return updated.(Model)
}
