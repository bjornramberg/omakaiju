package ui

import (
	"strings"
	"testing"

	"omakaiju/internal/config"
	"omakaiju/internal/fs"
)

func TestTruncateNameShortEnoughUnchanged(t *testing.T) {
	for _, name := range []string{"a.txt", "main.go", "short.md"} {
		if got := TruncateName(name, 40); got != name {
			t.Errorf("TruncateName(%q, 40) = %q, want unchanged", name, got)
		}
	}
}

func TestTruncateNameKeepsExtension(t *testing.T) {
	name := "a-very-long-module-name-for-testing.go"
	got := TruncateName(name, 20)

	if !strings.Contains(got, ".go") {
		t.Errorf("got %q, want the extension preserved", got)
	}
	if !strings.Contains(got, ellipsis) {
		t.Errorf("got %q, want an ellipsis", got)
	}
	if w := DisplayWidth(got); w > 20 {
		t.Errorf("width = %d, want <= 20", w)
	}
}

func TestTruncateNameRespectsBudget(t *testing.T) {
	name := "extremely-long-archive-name-that-will-not-fit.tar.gz"
	for _, budget := range []int{6, 10, 14, 18, 24, 30} {
		got := TruncateName(name, budget)
		if w := DisplayWidth(got); w > budget {
			t.Errorf("budget %d: width = %d for %q", budget, w, got)
		}
	}
}

func TestTruncateNameExactBoundary(t *testing.T) {
	name := "exact.md"
	if w := DisplayWidth(name); w != 8 {
		t.Fatalf("test setup wrong: %q is %d cells", name, w)
	}
	if got := TruncateName(name, 8); got != name {
		t.Errorf("exact fit was altered: got %q", got)
	}
}

func TestTruncateNameDotfileKeepsWholeName(t *testing.T) {
	// A dotfile has no extension, so nothing after the dot should be treated as
	// one; the leading dot must not be mistaken for an extension separator.
	got := TruncateName(".gitignore", 6)
	if strings.HasSuffix(got, ellipsis+".gitignore") {
		t.Errorf("dotfile split incorrectly: %q", got)
	}
	if w := DisplayWidth(got); w > 6 {
		t.Errorf("width = %d, want <= 6", w)
	}
}

func TestTruncateNameDirectoryKeepsSlash(t *testing.T) {
	got := TruncateName("some/very/long/directory-name/", 16)
	if !strings.HasSuffix(got, "/") {
		t.Errorf("directory should keep its trailing slash: %q", got)
	}
	if w := DisplayWidth(got); w > 16 {
		t.Errorf("width = %d, want <= 16", w)
	}
}

func TestTruncateNameWideRunes(t *testing.T) {
	name := "日本語のファイル名.txt"
	for _, budget := range []int{8, 12, 16, 20} {
		got := TruncateName(name, budget)
		if w := DisplayWidth(got); w > budget {
			t.Errorf("budget %d: width = %d for %q", budget, w, got)
		}
	}
}

func TestTruncateNameTinyBudgets(t *testing.T) {
	for _, budget := range []int{-1, 0, 1, 2, 3} {
		got := TruncateName("filename.go", budget)
		if w := DisplayWidth(got); w > max(budget, 0) {
			t.Errorf("budget %d: width = %d for %q", budget, w, got)
		}
	}
}

func TestTruncateNameNeverPanicsOnEdgeCases(t *testing.T) {
	for _, name := range []string{"", "/", ".", "..", "a", "/", "\x1b[31mred\x1b[0m"} {
		for budget := 0; budget <= 12; budget++ {
			_ = TruncateName(name, budget)
		}
	}
}

// A row must never exceed the budget, which is what caused wrapping and the
// phantom empty rows.
func TestFileItemLongNameDoesNotOverflow(t *testing.T) {
	theme := config.DefaultTheme()
	entry := fs.Entry{
		Name: "an-extremely-long-file-name-that-cannot-possibly-fit-in-the-pane.go",
		Path: "/tmp/an-extremely-long-file-name-that-cannot-possibly-fit-in-the-pane.go",
	}

	for _, width := range []int{12, 20, 30, 48} {
		out := NewFileItem(entry, false, false, theme, width).Render()
		plain := stripANSI(out)
		if lines := strings.Count(plain, "\n"); lines > 0 {
			t.Errorf("width %d: row wrapped into %d lines: %q", width, lines+1, plain)
		}
		if w := DisplayWidth(plain); w > width {
			t.Errorf("width %d: rendered width = %d", width, w)
		}
	}
}

func TestFileItemNerdFontIconStillFits(t *testing.T) {
	theme := config.DefaultTheme()
	entry := fs.Entry{Name: "x.go", Path: "/tmp/x.go"}

	// A Nerd Font glyph is one cell wide; the budget must account for it.
	out := stripANSI(NewFileItem(entry, false, false, theme, 10).Render())
	if w := DisplayWidth(out); w > 10 {
		t.Errorf("rendered width = %d, want <= 10", w)
	}
}

func max(a, b int) int {
	if a > b {
		return a
	}
	return b
}

// meaningfulLines drops border, padding, and blank lines so a test can assert
// on real content rows rather than the frame Lipgloss pads to the pane height.
func meaningfulLines(rendered string) []string {
	const frame = "\u256d\u256e\u256f\u2570\u2502\u2500"
	var out []string
	for _, l := range strings.Split(stripANSI(rendered), "\n") {
		stripped := strings.Map(func(r rune) rune {
			if strings.ContainsRune(frame, r) {
				return -1
			}
			return r
		}, l)
		if trimmed := strings.TrimSpace(stripped); trimmed != "" {
			out = append(out, trimmed)
		}
	}
	return out
}

// A deep path must occupy exactly one line.
func TestPanePathHeaderDoesNotWrap(t *testing.T) {
	theme := config.DefaultTheme()
	long := "/home/user/projects/some/deeply/nested/directory/tree/with/a/long/name"

	for _, width := range []int{20, 30, 44, 60} {
		pane := NewPane(width, 12, true, theme)
		pane.Path = long
		pane.Files = []fs.Entry{{Name: "file.go", Path: "/tmp/file.go"}}

		lines := meaningfulLines(pane.Render())
		if len(lines) != 2 {
			t.Errorf("width %d: %d content lines (%v), want path + 1 row", width, len(lines), lines)
		}
		if len(lines) > 0 {
			if w := DisplayWidth(lines[0]); w > pane.ContentWidth() {
				t.Errorf("width %d: path header is %d cells, content width %d", width, w, pane.ContentWidth())
			}
		}
	}
}

// Every listed row must be exactly one line, whatever the name length, or the
// cursor index stops matching the visual row.
func TestPaneEveryRowIsOneLine(t *testing.T) {
	theme := config.DefaultTheme()
	names := []string{
		"a.go",
		"a-fairly-long-go-file-name-here.go",
		"an-extremely-long-file-name-that-cannot-possibly-fit.go",
		"\u65e5\u672c\u8a9e\u306e\u3068\u3066\u3082\u9577\u3044\u30d5\u30a1\u30a4\u30eb\u540d.txt",
	}

	var files []fs.Entry
	for _, n := range names {
		files = append(files, fs.Entry{Name: n, Path: "/tmp/" + n})
	}

	for _, width := range []int{16, 24, 40, 60} {
		pane := NewPane(width, 14, true, theme)
		pane.Path = "/tmp"
		pane.Files = files
		pane.Cursor = 2

		lines := meaningfulLines(pane.Render())
		if len(lines) != 1+len(files) {
			t.Errorf("width %d: %d content lines, want %d (a row wrapped): %v", width, len(lines), 1+len(files), lines)
		}
	}
}
