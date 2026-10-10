package ui

import (
	"strings"
	"testing"

	"omakaiju/internal/config"
)

// noLineExceeds reports whether every rendered line fits within max. This is
// the invariant that prevents resize artifacts: a line wider than the terminal
// wraps, which shifts everything below it.
func noLineExceeds(rendered string, max int) bool {
	for _, line := range strings.Split(rendered, "\n") {
		if DisplayWidth(line) > max {
			return false
		}
	}
	return true
}

// A range of widths crossing the minimum, including sizes where the hostname
// and filter have to be dropped.
var resizeWidths = []int{0, 1, 8, 20, 39, 40, 41, 60, 80, 120, 200}

func TestTopBarNeverExceedsWidth(t *testing.T) {
	theme := config.DefaultTheme()
	for _, w := range resizeWidths {
		bar := NewTopBar(w, theme)
		bar.Path = "/home/user/projects/omakaiju"
		if w > 0 && !noLineExceeds(bar.Render(), w) {
			t.Errorf("width %d: top bar overflowed", w)
		}
	}
}

func TestTopBarAlwaysKeepsAppName(t *testing.T) {
	theme := config.DefaultTheme()
	for _, w := range []int{13, 20, 40, 80, 200} {
		bar := NewTopBar(w, theme)
		bar.Path = "/some/path"
		out := stripANSI(bar.Render())
		if !strings.Contains(out, "omakaiju") {
			t.Errorf("width %d: app name should survive: %q", w, out)
		}
	}
}

func TestTopBarShowsRealPath(t *testing.T) {
	bar := NewTopBar(120, config.DefaultTheme())
	bar.Path = "/home/user/projects/omakaiju"
	out := stripANSI(bar.Render())
	if out == "" {
		t.Fatal("top bar rendered nothing")
	}
	if strings.Contains(out, "  /  ") {
		t.Errorf("path should not be the hardcoded root: %q", out)
	}
	if !strings.Contains(out, "/home/user/projects") {
		t.Errorf("top bar should show the active path: %q", out)
	}
}

func TestTopBarDropsHostnameWhenTight(t *testing.T) {
	theme := config.DefaultTheme()

	wide := NewTopBar(200, theme)
	wide.Path = "/tmp"
	if !strings.Contains(stripANSI(wide.Render()), "│") {
		t.Error("a wide bar should show separators")
	}

	narrow := NewTopBar(24, theme)
	narrow.Path = "/very/long/path/that/consumes/all/the/space"
	if !noLineExceeds(stripANSI(narrow.Render()), 24) {
		t.Errorf("narrow bar overflowed: %q", stripANSI(narrow.Render()))
	}
}

func TestTopBarWithFilterNeverOverflows(t *testing.T) {
	theme := config.DefaultTheme()
	for _, w := range resizeWidths {
		bar := NewTopBar(w, theme)
		bar.Path = "/home/user/projects"
		bar.Filter = "someverylongfilterexpression"
		if w > 0 && !noLineExceeds(bar.Render(), w) {
			t.Errorf("width %d: filtered top bar overflowed", w)
		}
	}
}

func TestBottomBarNeverExceedsWidth(t *testing.T) {
	theme := config.DefaultTheme()

	for _, w := range []int{1, 8, 20, 40, 80, 200} {
		bar := NewBottomBar(w, theme)
		if !noLineExceeds(bar.Render(), w) {
			t.Errorf("width %d: default bottom bar overflowed", w)
		}

		bar.Input = strings.Repeat("x", 200)
		if !noLineExceeds(bar.Render(), w) {
			t.Errorf("width %d: input bottom bar overflowed", w)
		}

		bar.Input = ""
		bar.Error = strings.Repeat("e", 200)
		if !noLineExceeds(bar.Render(), w) {
			t.Errorf("width %d: error bottom bar overflowed", w)
		}

		bar.Error = ""
		bar.OpResult = strings.Repeat("r", 200)
		if !noLineExceeds(bar.Render(), w) {
			t.Errorf("width %d: op-result bottom bar overflowed", w)
		}

		bar.OpResult = ""
		bar.FuzzyActive = true
		bar.FuzzyInput = strings.Repeat("q", 200)
		if !noLineExceeds(bar.Render(), w) {
			t.Errorf("width %d: fuzzy bottom bar overflowed", w)
		}

		bar.FuzzyActive = false
		bar.FuzzyInput = ""
		bar.Prompt = "rename"
		bar.PromptInput = strings.Repeat("p", 200)
		if !noLineExceeds(bar.Render(), w) {
			t.Errorf("width %d: prompt bottom bar overflowed", w)
		}
	}
}

func TestBottomBarTailKeepsNewestInput(t *testing.T) {
	bar := NewBottomBar(20, config.DefaultTheme())
	bar.Prompt = "rename"
	bar.PromptInput = "abcdefghijklmnopqrstuvwxyz"

	out := stripANSI(bar.Render())
	if !noLineExceeds(out, 20) {
		t.Errorf("clipped prompt overflowed: %q", out)
	}
	// The prompt line, not the border or padding rows, should end with the
	// newest character.
	content := ""
	for _, l := range strings.Split(out, "\n") {
		if strings.Contains(l, "rename>") {
			content = strings.TrimRight(l, " ")
		}
	}
	if !strings.HasSuffix(content, "z") {
		t.Errorf("typing should keep the newest character visible: %q", content)
	}
}

func TestPreviewHeadersNeverExceedWidth(t *testing.T) {
	theme := config.DefaultTheme()
	long := "/tmp/an-extremely-long-file-name-that-never-ends-with-an-extension.go"

	for _, w := range []int{10, 20, 40, 80} {
		p := NewPreview(w, 16, theme)
		p = p.SetPath(long)

		if !noLineExceeds(p.RenderText([]string{"body"}, nil), w) {
			t.Errorf("width %d: text preview overflowed", w)
		}
		if !noLineExceeds(p.RenderMetadata(long), w) {
			t.Errorf("width %d: metadata preview overflowed", w)
		}
		if !noLineExceeds(p.RenderDirectory(long), w) {
			t.Errorf("width %d: directory preview overflowed", w)
		}
		if !noLineExceeds(p.ArchiveNotice(long, "unsupported"), w) {
			t.Errorf("width %d: archive notice overflowed", w)
		}
	}
}

func TestPreviewTextBodyStillBoundedWithHeader(t *testing.T) {
	theme := config.DefaultTheme()
	p := NewPreview(24, 14, theme)
	p = p.SetPath("/tmp/a-really-quite-long-filename-here.go")
	out := p.RenderText([]string{strings.Repeat("x", 300)}, nil)
	if !noLineExceeds(out, 24) {
		t.Errorf("preview overflowed: %q", out)
	}
}

func TestFuzzyFinderNeverExceedsWidth(t *testing.T) {
	theme := config.DefaultTheme()
	for _, w := range []int{20, 40, 80, 160} {
		f := NewFuzzyFinder(w, 20, theme)
		f.Input = strings.Repeat("q", 200)
		f.Results = []string{strings.Repeat("r", 300), "short.go"}
		if !noLineExceeds(f.Render(), w) {
			t.Errorf("width %d: fuzzy finder overflowed", w)
		}
	}
}

func TestLayoutClampsDegenerateDimensions(t *testing.T) {
	for _, tc := range []struct{ w, h int }{{0, 0}, {1, 1}, {5, 5}, {-10, -10}, {0, 50}, {50, 0}} {
		l := NewLayout(tc.w, tc.h, config.DefaultTheme())
		if l.MainAreaHeight() < 1 {
			t.Errorf("%dx%d: MainAreaHeight = %d, want >= 1", tc.w, tc.h, l.MainAreaHeight())
		}
		if l.PaneWidth() < 1 {
			t.Errorf("%dx%d: PaneWidth = %d, want >= 1", tc.w, tc.h, l.PaneWidth())
		}
		if l.PreviewWidth() < 1 {
			t.Errorf("%dx%d: PreviewWidth = %d, want >= 1", tc.w, tc.h, l.PreviewWidth())
		}
	}
}

func TestLayoutTooSmallDetection(t *testing.T) {
	theme := config.DefaultTheme()
	if !NewLayout(10, 40, theme).TooSmall() {
		t.Error("a narrow terminal should be too small")
	}
	if !NewLayout(80, 4, theme).TooSmall() {
		t.Error("a short terminal should be too small")
	}
	if NewLayout(0, 0, theme).TooSmall() != true {
		t.Error("an unsized terminal should be too small")
	}
	if NewLayout(120, 40, theme).TooSmall() {
		t.Error("a normal terminal should not be too small")
	}
}

func TestTooSmallPlaceholderNeverOverflows(t *testing.T) {
	theme := config.DefaultTheme()
	for _, w := range []int{1, 5, 20, 40, 80} {
		l := NewLayout(w, 5, theme)
		out := l.TooSmallPlaceholder()
		if out == "" {
			continue // zero width renders nothing, which is correct
		}
		if !noLineExceeds(out, w) {
			t.Errorf("width %d: placeholder overflowed: %q", w, out)
		}
	}
}

func TestTrimPathLeftKeepsTail(t *testing.T) {
	long := "/home/user/projects/omakaiju/internal/ui"
	got := TrimPathLeft(long, 20)
	if DisplayWidth(got) > 20 {
		t.Errorf("trimmed path is %d cells: %q", DisplayWidth(got), got)
	}
	if !strings.HasSuffix(got, "internal/ui") && !strings.Contains(got, "ui") {
		t.Errorf("trimmed path should keep the meaningful tail: %q", got)
	}
	if !strings.HasPrefix(got, "/") {
		t.Errorf("trimmed path should still look absolute: %q", got)
	}
}

func TestTrimPathLeftShortEnoughUnchanged(t *testing.T) {
	p := "/tmp/x"
	if got := TrimPathLeft(p, 40); got != p {
		t.Errorf("short path altered: %q", got)
	}
}

// TestSweepAllSurfacesAtEveryWidth is the invariant behind the resize fixes:
// nothing the app renders may ever be wider than the terminal, at any size.
func TestSweepAllSurfacesAtEveryWidth(t *testing.T) {
	theme := config.DefaultTheme()

	for w := 0; w <= 160; w++ {
		layout := NewLayout(w, 30, theme)
		if layout.TooSmall() {
			continue
		}

		bar := NewTopBar(w, theme)
		bar.Path = "/home/user/projects/omakaiju/internal/ui/preview.go"
		bar.Filter = "filterstring"
		if !noLineExceeds(bar.Render(), w) {
			t.Fatalf("width %d: top bar overflowed", w)
		}

		bottom := NewBottomBar(w, theme)
		bottom.Input = strings.Repeat("i", 300)
		if !noLineExceeds(bottom.Render(), w) {
			t.Fatalf("width %d: bottom bar overflowed", w)
		}

		pane := NewPane(layout.PaneWidth(), layout.MainAreaHeight(), true, theme)
		pane.Path = strings.Repeat("/deep", 40)
		if !noLineExceeds(pane.Render(), layout.PaneWidth()) {
			t.Fatalf("width %d: pane overflowed", w)
		}

		p := NewPreview(layout.PreviewWidth(), layout.MainAreaHeight(), theme)
		p = p.SetPath("/tmp/" + strings.Repeat("long", 40) + ".go")
		if !noLineExceeds(p.RenderText([]string{strings.Repeat("w", 300)}, nil), layout.PreviewWidth()) {
			t.Fatalf("width %d: preview overflowed", w)
		}
	}
}
