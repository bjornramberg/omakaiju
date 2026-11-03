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

// The panel's height must not depend on how long the results are.
//
// A row that filled its width used to push its tail onto a second line: the
// finder built the rows for a wider panel than the one lipgloss wrapped
// them in, so the last couple of cells fell over the edge. The continuation
// carried only the panel's padding, not the row's marker space, so it also
// started a cell further left than the row above it.
func TestFuzzyFinderHeightDoesNotDependOnResultLength(t *testing.T) {
	theme := config.DefaultTheme()

	for _, w := range []int{40, 80, 100, 120, 200} {
		short := NewFuzzyFinder(w, 40, theme)
		short.Results = []string{"/tmp/a.go"}
		want := strings.Count(stripANSI(short.Render()), "\n") + 1

		for n := 20; n <= 90; n += 2 {
			long := NewFuzzyFinder(w, 40, theme)
			long.Results = []string{"/tmp/" + strings.Repeat("d", n) + "/file.lua"}

			got := strings.Count(stripANSI(long.Render()), "\n") + 1
			if got != want {
				t.Errorf("width %d, path of %d cells: panel has %d lines, want %d",
					w, n, got, want)
			}
		}
	}
}

// The panel must be exactly as wide as it says it is, and every row of it the
// same width. A panel that is wider than its border is a panel whose border
// has been cut off.
func TestFuzzyFinderPanelIsExactlyBoxWidth(t *testing.T) {
	theme := config.DefaultTheme()

	for _, w := range []int{24, 30, 40, 60, 100, 200} {
		f := NewFuzzyFinder(w, 30, theme)
		f.Results = []string{"/tmp/a.go"}

		for i, line := range strings.Split(stripANSI(f.Render()), "\n") {
			if got := DisplayWidth(line); got != f.BoxWidth() {
				t.Errorf("width %d: row %d is %d cells, want %d", w, i, got, f.BoxWidth())
			}
		}
	}
}

// The footer must not wrap. A wrapped footer pushes the results around, so a
// narrow panel gives up hints instead of taking another line.
func TestFuzzyFinderFooterNeverWraps(t *testing.T) {
	theme := config.DefaultTheme()

	for _, w := range []int{24, 30, 40, 60, 100} {
		f := NewFuzzyFinder(w, 40, theme)
		f.Results = []string{"/tmp/a.go"}

		lines := strings.Split(stripANSI(f.Render()), "\n")
		// One border, one header, one prompt, one footer, one result, and
		// the padding between and around them.
		if len(lines) != 11 {
			t.Errorf("width %d: panel has %d lines, want 11:\n%s", w, len(lines), lines)
		}
	}
}

// The footer must name the keys that actually move the cursor. j and k are
// part of the query now, so a hint that names them promises something the
// finder does not do.
func TestFuzzyFinderFooterNamesTheRealKeys(t *testing.T) {
	theme := config.DefaultTheme()
	f := NewFuzzyFinder(100, 30, theme)
	f.Results = []string{"/tmp/a.go"}

	out := stripANSI(f.Render())
	if !strings.Contains(out, "up/down:nav") {
		t.Errorf("footer should name the arrow keys, got:\n%s", out)
	}
	if strings.Contains(out, "j/k") {
		t.Errorf("footer still names j/k, which type into the query now:\n%s", out)
	}
}
