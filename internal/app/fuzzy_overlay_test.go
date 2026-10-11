package app

import (
	"os"
	"path/filepath"
	"regexp"
	"strings"
	"testing"

	"github.com/bjornramberg/omakaiju/internal/config"
	"github.com/bjornramberg/omakaiju/internal/fs"
	"github.com/bjornramberg/omakaiju/internal/fuzzy"
	"github.com/bjornramberg/omakaiju/internal/ui"

	tea "charm.land/bubbletea/v2"
)

var escSeq = regexp.MustCompile(`\x1b\[[0-9;?]*[A-Za-z]|\x1b\][^\a]*\a|\x1b[@-Z\\-_]`)

// fuzzyModel builds a model with the fuzzy finder open over a populated
// directory, ready to render.
func fuzzyModel(t *testing.T, w, h int, query string) Model {
	t.Helper()

	dir := t.TempDir()
	for _, name := range []string{"alpha.go", "beta.go", "gamma.txt"} {
		if err := os.WriteFile(filepath.Join(dir, name), []byte("x\n"), 0o644); err != nil {
			t.Fatal(err)
		}
	}
	if err := os.Mkdir(filepath.Join(dir, "subdir"), 0o755); err != nil {
		t.Fatal(err)
	}

	files, err := fs.ReadDir(dir)
	if err != nil {
		t.Fatal(err)
	}

	m := NewModel(config.Config{ThemePath: filepath.Join(dir, "nonexistent.toml")})
	m.width, m.height = w, h
	m.leftPath, m.leftFiles = dir, files
	m.rightPath, m.rightFiles = dir, files
	m.updatePreview()

	m.fuzzyActive = true
	m.fuzzyInput = query
	m.fuzzyAllFiles, _ = fs.WalkDir(dir)
	m.fuzzyResults = fuzzy.Filter(query, m.fuzzyAllFiles)
	return m
}

// The overlay is spliced into the finished layout, so the row count must still
// match the terminal exactly or the view will scroll.
func TestFuzzyOverlayKeepsExactFit(t *testing.T) {
	for _, tc := range []struct{ w, h int }{
		{40, 10}, {46, 16}, {80, 24}, {100, 30}, {160, 50}, {200, 12},
	} {
		m := fuzzyModel(t, tc.w, tc.h, "a")
		view := m.View().Content

		lines := strings.Split(view, "\n")
		if len(lines) != tc.h {
			t.Errorf("%dx%d: rendered %d rows, want %d", tc.w, tc.h, len(lines), tc.h)
		}
		for i, line := range lines {
			if got := ui.DisplayWidth(line); got != tc.w {
				t.Errorf("%dx%d: row %d width %d, want %d", tc.w, tc.h, i, got, tc.w)
			}
		}
	}
}

// The finder must sit over the panes rather than replacing them, so the rest of
// the interface stays visible behind it.
func TestFuzzyOverlayKeepsPanesVisible(t *testing.T) {
	m := fuzzyModel(t, 100, 30, "a")
	view := escSeq.ReplaceAllString(m.View().Content, "")

	for _, want := range []string{"fuzzy find", "subdir"} {
		if !strings.Contains(view, want) {
			t.Errorf("expected %q on screen with the finder open, got:\n%s", want, view)
		}
	}
}

// Without the finder the layout is unchanged; the overlay must not leak into
// the normal view.
func TestFuzzyClosedShowsPlainLayout(t *testing.T) {
	m := fuzzyModel(t, 100, 30, "a")
	m.fuzzyActive = false

	view := escSeq.ReplaceAllString(m.View().Content, "")
	if strings.Contains(view, "fuzzy find") {
		t.Error("finder still drawn after closing it")
	}
	if !strings.Contains(view, "subdir") {
		t.Error("expected the file listing after closing the finder")
	}
}

// Escape must dismiss the finder and leave the layout intact.
func TestFuzzyEscapeRestoresLayout(t *testing.T) {
	m := fuzzyModel(t, 100, 30, "a")

	updated, _ := m.Update(tea.KeyPressMsg{Code: tea.KeyEscape})
	m = updated.(Model)

	if m.fuzzyActive {
		t.Error("escape should close the finder")
	}
	view := escSeq.ReplaceAllString(m.View().Content, "")
	if strings.Contains(view, "fuzzy find") {
		t.Error("finder still drawn after escape")
	}
}

// A hostile filename anywhere in the result set must not corrupt the view.
func TestFuzzyOverlayIgnoresControlBytesInPaths(t *testing.T) {
	m := fuzzyModel(t, 100, 30, "evil")
	m.fuzzyResults = append(m.fuzzyResults, filepath.Join("/tmp", "evil\x1b[2Jname\nsecond.go"))
	m.fuzzyCursor = len(m.fuzzyResults) - 1

	view := m.View().Content
	plain := escSeq.ReplaceAllString(view, "")

	for _, r := range plain {
		if r < 0x20 && r != '\n' && r != '\t' {
			t.Fatalf("control byte %U from a filename reached the view", r)
		}
	}
	// The row count must still be exact despite the embedded newline.
	if got := len(strings.Split(view, "\n")); got != m.height {
		t.Errorf("rendered %d rows, want %d", got, m.height)
	}
}
