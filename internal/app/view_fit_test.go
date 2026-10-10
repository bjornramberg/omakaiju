package app

import (
	"os"
	"path/filepath"
	"strings"
	"testing"

	"omakaiju/internal/config"
	"omakaiju/internal/fs"
)

// viewFixture builds a model in dir with a realistic listing.
func viewFixture(t *testing.T) Model {
	t.Helper()
	dir := t.TempDir()

	// Enough entries to exercise the scrolled region, with names long enough to
	// force truncation at small widths.
	for i := 0; i < 40; i++ {
		name := filepath.Join(dir, "entry-with-a-deliberately-long-name-"+itoaTest(i)+".go")
		if err := os.WriteFile(name, []byte("package main\n"), 0o644); err != nil {
			t.Fatal(err)
		}
	}
	if err := os.Mkdir(filepath.Join(dir, "a-directory"), 0o755); err != nil {
		t.Fatal(err)
	}

	// A file tall enough that its preview fills the whole panel, so any extra
	// frame added around the preview is caught as overflow.
	var big strings.Builder
	for i := 0; i < 400; i++ {
		big.WriteString("func fn" + itoaTest(i) + "() { println(\"x\") }\n")
	}
	if err := os.WriteFile(filepath.Join(dir, "aaa-huge-source-file.go"), []byte(big.String()), 0o644); err != nil {
		t.Fatal(err)
	}

	files, err := fs.ReadDir(dir)
	if err != nil {
		t.Fatal(err)
	}

	m := NewModel(config.Config{ThemePath: filepath.Join(dir, "none.toml")})
	m.leftPath, m.leftFiles = dir, files

	// Put the cursor on the huge file so the preview panel renders full height.
	for i, f := range files {
		if f.Name == "aaa-huge-source-file.go" {
			m.leftCursor = i
		}
	}
	m.rightPath, m.rightFiles, m.rightCursor = dir, files, 0
	m.updatePreview()
	return m
}

func itoaTest(n int) string {
	if n == 0 {
		return "0"
	}
	var b []byte
	for n > 0 {
		b = append([]byte{byte('0' + n%10)}, b...)
		n /= 10
	}
	return string(b)
}

func visibleWidth(line string) int {
	return len([]rune(stripTestANSI(line)))
}

// stripTestANSI removes escape sequences for measurement.
func stripTestANSI(s string) string {
	var b strings.Builder
	for i := 0; i < len(s); {
		if s[i] == 0x1b && i+1 < len(s) && s[i+1] == '[' {
			i += 2
			for i < len(s) && s[i] != 'm' {
				i++
			}
			i++
			continue
		}
		b.WriteByte(s[i])
		i++
	}
	return b.String()
}

// TestViewExactlyFitsTerminal is the invariant that prevents resize artifacts:
// the rendered view must be exactly as tall as the terminal and never wider.
// Anything else makes the terminal scroll, which is what produced the artefacts
// and the jumping text.
func TestViewExactlyFitsTerminal(t *testing.T) {
	m := viewFixture(t)

	for _, w := range []int{40, 55, 80, 100, 140, 200} {
		for _, h := range []int{10, 14, 24, 40, 60} {
			m.width, m.height = w, h

			out := stripTestANSI(m.View().Content)
			lines := strings.Split(strings.TrimRight(out, "\n"), "\n")

			if len(lines) != h {
				t.Fatalf("%dx%d: rendered %d lines, want exactly %d", w, h, len(lines), h)
			}
			for i, l := range lines {
				if got := visibleWidth(l); got > w {
					t.Fatalf("%dx%d: line %d is %d cells wide: %q", w, h, i, got, l)
				}
			}
		}
	}
}

// The preview focus ring must not change the rendered size.
func TestViewFitsWhenPreviewFocused(t *testing.T) {
	m := viewFixture(t)
	m.previewFocused = true

	for _, tc := range []struct{ w, h int }{{80, 24}, {120, 40}, {200, 60}} {
		m.width, m.height = tc.w, tc.h
		out := stripTestANSI(m.View().Content)
		lines := strings.Split(strings.TrimRight(out, "\n"), "\n")
		if len(lines) != tc.h {
			t.Errorf("%dx%d: focused preview rendered %d lines, want %d", tc.w, tc.h, len(lines), tc.h)
		}
	}
}

// The fuzzy overlay must fit too.
func TestViewFitsWhenFuzzyOpen(t *testing.T) {
	m := viewFixture(t)
	m.fuzzyActive = true
	m.fuzzyInput = strings.Repeat("query", 20)

	for _, tc := range []struct{ w, h int }{{80, 24}, {120, 40}} {
		m.width, m.height = tc.w, tc.h
		out := stripTestANSI(m.View().Content)
		if !noLinesOverf(t, out, tc.w, tc.h) {
			t.Errorf("%dx%d: fuzzy overlay did not fit", tc.w, tc.h)
		}
	}
}

func noLinesOverf(t *testing.T, out string, w, h int) bool {
	t.Helper()
	lines := strings.Split(strings.TrimRight(out, "\n"), "\n")
	if len(lines) > h {
		return false
	}
	for _, l := range lines {
		if visibleWidth(l) > w {
			return false
		}
	}
	return true
}

// Every intermediate size must also fit, not just the sampled ones.
func TestViewFitsAcrossResizeSweep(t *testing.T) {
	m := viewFixture(t)

	for w := 40; w <= 200; w += 7 {
		for h := 10; h <= 50; h += 3 {
			m.width, m.height = w, h
			out := stripTestANSI(m.View().Content)
			lines := strings.Split(strings.TrimRight(out, "\n"), "\n")
			if len(lines) != h {
				t.Fatalf("%dx%d: rendered %d lines, want %d", w, h, len(lines), h)
			}
			for i, l := range lines {
				if visibleWidth(l) > w {
					t.Fatalf("%dx%d: line %d is %d cells: %q", w, h, i, visibleWidth(l), l)
				}
			}
		}
	}
}
