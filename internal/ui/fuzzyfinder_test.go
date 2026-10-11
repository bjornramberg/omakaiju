package ui

import (
	"fmt"
	"path/filepath"
	"strings"
	"testing"

	"github.com/bjornramberg/omakaiju/internal/config"
)

func TestFuzzyFinderShowsBasenameFirst(t *testing.T) {
	theme := config.DefaultTheme()
	f := NewFuzzyFinder(100, 30, theme)
	f.Input = "main"
	f.Results = []string{filepath.Join("/home", "user", "project", "main.go")}
	f.Cursor = 0

	plain := stripANSI(f.Render())
	if !strings.Contains(plain, "main.go") {
		t.Errorf("expected the filename in the row, got:\n%s", plain)
	}
	name := strings.Index(plain, "main.go")
	dir := strings.Index(plain, "project")
	if dir < 0 {
		t.Fatalf("expected the directory in the row, got:\n%s", plain)
	}
	if dir < name {
		t.Errorf("expected the directory after the filename, got:\n%s", plain)
	}
}

func TestFuzzyFinderShowsPosition(t *testing.T) {
	theme := config.DefaultTheme()
	f := NewFuzzyFinder(100, 30, theme)
	var results []string
	for i := 0; i < 40; i++ {
		results = append(results, filepath.Join("/tmp", fmt.Sprintf("file%02d.go", i)))
	}
	f.Results = results
	f.Cursor = 25

	plain := stripANSI(f.Render())
	if !strings.Contains(plain, "26/40") {
		t.Errorf("expected position 26/40 in the footer, got:\n%s", plain)
	}
}

func TestFuzzyFinderScrollsBothWays(t *testing.T) {
	theme := config.DefaultTheme()
	var results []string
	for i := 0; i < 40; i++ {
		results = append(results, filepath.Join("/tmp", fmt.Sprintf("file%02d.go", i)))
	}

	// Cursor near the end must still be inside the visible window.
	f := NewFuzzyFinder(100, 30, theme)
	f.Results = results
	f.Cursor = 39
	plain := stripANSI(f.Render())
	if !strings.Contains(plain, "file39.go") {
		t.Errorf("cursor result not visible when moved down, got:\n%s", plain)
	}

	// Cursor at the top likewise.
	f.Cursor = 0
	plain = stripANSI(f.Render())
	if !strings.Contains(plain, "file00.go") {
		t.Errorf("cursor result not visible when moved up, got:\n%s", plain)
	}
}

func TestFuzzyFinderShowsOverflowMarkers(t *testing.T) {
	theme := config.DefaultTheme()
	var results []string
	for i := 0; i < 40; i++ {
		results = append(results, filepath.Join("/tmp", fmt.Sprintf("file%02d.go", i)))
	}
	f := NewFuzzyFinder(100, 30, theme)
	f.Results = results
	f.Cursor = 20

	if !strings.Contains(stripANSI(f.Render()), "...") {
		t.Error("expected an overflow marker when the list does not fit")
	}
}

// The panel is overlaid, so it must not be taller than the terminal.
func TestFuzzyFinderNeverExceedsHeight(t *testing.T) {
	theme := config.DefaultTheme()
	var results []string
	for i := 0; i < 100; i++ {
		results = append(results, filepath.Join("/tmp", "nested", "dir", fmt.Sprintf("file%02d.go", i)))
	}

	for _, h := range []int{10, 12, 16, 24, 40} {
		f := NewFuzzyFinder(80, h, theme)
		f.Results = results
		rows := strings.Count(f.Render(), "\n") + 1
		if rows > h {
			t.Errorf("height %d: finder rendered %d rows", h, rows)
		}
	}
}

// A hostile filename must not be able to break the panel layout.
func TestFuzzyFinderSanitizesResultNames(t *testing.T) {
	theme := config.DefaultTheme()
	f := NewFuzzyFinder(100, 30, theme)
	f.Input = "evil"
	f.Results = []string{filepath.Join("/tmp", "evil\x1b[2Jname\nsecond.go")}
	f.Cursor = 0

	out := stripANSI(f.Render())
	if strings.Contains(out, "\x1b") || strings.Contains(out, "\x00") {
		t.Error("a control byte from a filename reached the rendered panel")
	}
}
