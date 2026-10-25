package ui

import (
	"errors"
	"strings"
	"testing"

	"github.com/bjornramberg/omakaiju/internal/config"
	"github.com/bjornramberg/omakaiju/internal/fs"
)

// injection is filesystem data that a terminal would act on: a clear-screen
// sequence, an OSC title change, a NUL and a bell. Sanitisation must leave the
// text behind as replacement characters, not let the sequence through.
const injection = "/tmp/\x1b[2J\x1b]0;pwned\x07evil\x00"

// assertNoInjection fails if a rendered string still contains the escape
// sequences or control bytes from injection. The theme's own styling is
// allowed; only the injected sequence is forbidden.
func assertNoInjection(t *testing.T, surface, out string) {
	t.Helper()
	for _, bad := range []string{"\x1b[2J", "\x1b]", "\x00", "\x07"} {
		if strings.Contains(out, bad) {
			t.Errorf("%s leaked %q:\n%q", surface, bad, out)
		}
	}
	// The sanitiser replaces an escape with U+FFFD, leaving its printable
	// tail visible. Seeing that tail proves the data did reach the surface
	// and was rendered rather than silently dropped.
	if !strings.Contains(out, "[2J") {
		t.Errorf("%s did not render the filename text at all:\n%q", surface, out)
	}
}

func TestPaneSanitizesPathAndStatus(t *testing.T) {
	theme := config.DefaultTheme()

	pane := NewPane(80, 14, true, theme)
	pane.Path = injection
	pane.Status = "status " + injection
	pane.Files = []fs.Entry{{Name: "file.go", Path: "/tmp/file.go"}}

	assertNoInjection(t, "pane header/status", pane.Render())

	// The filter placeholder quotes user input too; it must not be a hole.
	empty := NewPane(80, 14, true, theme)
	empty.Path = "/tmp"
	empty.Filter = injection
	assertNoInjection(t, "pane empty-filter row", empty.Render())
}

func TestBottomBarSanitizesErrorAndOpResult(t *testing.T) {
	theme := config.DefaultTheme()

	bar := NewBottomBar(80, theme)
	bar.Error = injection
	assertNoInjection(t, "bottom bar error", bar.Render())

	bar = NewBottomBar(80, theme)
	bar.OpResult = "yanked " + injection
	assertNoInjection(t, "bottom bar op result", bar.Render())
}

func TestPreviewErrorSanitizesOperatingSystemErrors(t *testing.T) {
	theme := config.DefaultTheme()
	p := NewPreview(80, 16, theme).SetPath("/tmp/file")
	err := errors.New("stat " + injection + ": no such file or directory")

	out := p.RenderError(err)
	assertNoInjection(t, "preview error", out)

	// Route it through the content path as well, which is what the app uses.
	assertNoInjection(t, "preview error content", p.RenderContent(Content{Kind: KindError, Err: err}))
}

func TestArchiveNoticeSanitizesErrors(t *testing.T) {
	theme := config.DefaultTheme()
	p := NewPreview(80, 16, theme).SetPath("/tmp/a.zip")

	err := errors.New("open " + injection + ": permission denied")
	assertNoInjection(t, "archive notice", p.ArchiveNotice("/tmp/a.zip", "could not read archive: "+err.Error()))
}

func TestFuzzyFinderSanitizesRoot(t *testing.T) {
	theme := config.DefaultTheme()
	f := NewFuzzyFinder(100, 30, theme)
	f.Loading = true
	f.Root = injection

	assertNoInjection(t, "fuzzy root", f.Render())
}

// The preview body is raw file text. A tab in a Makefile is significant
// indentation, so the content path keeps it rather than substituting a space
// the way the name path does. The renderer then expands it to its tab stop.
func TestPreviewBodyKeepsTab(t *testing.T) {
	theme := config.DefaultTheme()
	lines := []string{"all:", "\tgo build ./..."}

	p := NewPreview(80, 16, theme).SetPath("/tmp/Makefile")
	out := p.RenderText(lines, nil)

	if !strings.Contains(out, "    go build ./...") {
		t.Errorf("the preview body lost its tab:\n%q", out)
	}
	if strings.Contains(out, replacementRune+"go build") {
		t.Errorf("the preview body replaced its tab:\n%q", out)
	}
}

// The preview body is deliberately the one surface that keeps its ANSI: it is
// produced by our highlighter, and stripping it would remove the colouring.
func TestPreviewBodyKeepsHighlighterANSI(t *testing.T) {
	theme := config.DefaultTheme()
	highlighted := Highlight("x.go", "package main\nfunc main() {}\n", OmarchyStyle(theme))
	lines := splitLines(highlighted)
	if len(lines) == 0 || !strings.Contains(highlighted, "\x1b[") {
		t.Fatalf("test setup: the highlighter produced no ANSI: %q", highlighted)
	}

	p := NewPreview(80, 16, theme).SetPath("/tmp/x.go")
	out := p.RenderText(nil, lines)

	if !strings.Contains(out, lines[0]) {
		t.Errorf("the highlighter's styled first line was altered:\nwant %q\n got %q", lines[0], out)
	}
	if !strings.Contains(out, "\x1b[") {
		t.Error("the preview body lost the highlighter's ANSI")
	}
}
