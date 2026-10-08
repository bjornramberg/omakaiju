package ui

import (
	"strings"
	"testing"

	"github.com/alecthomas/chroma/v2"

	"omakaiju/internal/config"
)

func testTheme() config.Theme {
	return config.DefaultTheme()
}

func TestHighlightProducesANSISequences(t *testing.T) {
	src := "package main\n\nfunc main() {\n\tprintln(\"hi\")\n}\n"

	out := Highlight("main.go", src, OmarchyStyle(testTheme()))

	if out == src {
		t.Fatal("expected highlighted output to differ from source")
	}
	if !strings.Contains(out, "\x1b[") {
		t.Error("expected ANSI escape sequences in highlighted Go source")
	}
	// Content must survive highlighting.
	if !strings.Contains(stripANSI(out), "package main") {
		t.Error("highlighted output lost source text")
	}
}

func TestHighlightUnknownExtensionFallsBack(t *testing.T) {
	src := "some plain content\nwith no known lexer\n"
	// A .qqq extension has no lexer, and the content will not analyse.
	out := Highlight("data.qqq", src, OmarchyStyle(testTheme()))
	if out != src {
		t.Errorf("unknown lexer should return source unchanged, got %q", out)
	}
}

func TestHighlightEmptyInput(t *testing.T) {
	for _, src := range []string{"", "   ", "\n\n"} {
		if out := Highlight("main.go", src, OmarchyStyle(testTheme())); out != src {
			t.Errorf("empty input: got %q, want %q", out, src)
		}
	}
}

func TestHighlightNilStyleStillWorks(t *testing.T) {
	src := "package main\n"
	out := Highlight("main.go", src, nil)
	if out == "" {
		t.Error("nil style should still produce output")
	}
	if !strings.Contains(stripANSI(out), "package main") {
		t.Error("nil style output lost source text")
	}
}

func TestOmarchyStyleUsesThemeColours(t *testing.T) {
	theme := testTheme()
	style := OmarchyStyle(theme)
	if style == nil {
		t.Fatal("style should not be nil")
	}
	// The keyword entry should resolve to the theme primary colour.
	if got := style.Get(chroma.Keyword).Colour; !strings.EqualFold(got.String(), theme.Primary) {
		t.Errorf("keyword colour = %v, want %v", got.String(), theme.Primary)
	}
	if got := style.Get(chroma.Text).Colour; !strings.EqualFold(got.String(), theme.FGMain) {
		t.Errorf("text colour = %v, want %v", got.String(), theme.FGMain)
	}
	if got := style.Get(chroma.Comment).Colour; !strings.EqualFold(got.String(), theme.Subtle) {
		t.Errorf("comment colour = %v, want %v", got.String(), theme.Subtle)
	}
}

func TestHighlightLinesReturnsNilWhenUnhighlightable(t *testing.T) {
	lines := []string{"opaque data", "more opaque data"}
	if got := HighlightLines("data.qqq", lines, testTheme()); got != nil {
		t.Errorf("expected nil for unhighlightable content, got %v", got)
	}
}

func TestHighlightLinesEmpty(t *testing.T) {
	if got := HighlightLines("main.go", nil, testTheme()); got != nil {
		t.Errorf("expected nil for no lines, got %v", got)
	}
}

func TestHighlightLinesPreservesLineCount(t *testing.T) {
	lines := []string{"package main", "", "// comment", "func main() {}"}
	got := HighlightLines("main.go", lines, testTheme())
	if len(got) != len(lines) {
		t.Fatalf("line count = %d, want %d", len(got), len(lines))
	}
	if plain := stripANSI(strings.Join(got, "\n")); plain != strings.Join(lines, "\n") {
		t.Error("highlighting altered the source text")
	}
}

func TestSplitLines(t *testing.T) {
	if got := splitLines(""); got != nil {
		t.Errorf("empty string should split to nil, got %v", got)
	}
	if got := splitLines("a\nb"); len(got) != 2 {
		t.Errorf("got %d lines, want 2", len(got))
	}
}

func stripANSI(s string) string {
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
