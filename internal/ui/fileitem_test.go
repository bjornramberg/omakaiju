package ui

import (
	"strings"
	"testing"

	"github.com/bjornramberg/omakaiju/internal/config"
	"github.com/bjornramberg/omakaiju/internal/fs"
	"github.com/bjornramberg/omakaiju/internal/icons"
)

func TestFileItemShowsMarkerColumn(t *testing.T) {
	theme := config.DefaultTheme()
	entry := fs.Entry{Name: "alpha.txt", Path: "/tmp/alpha.txt"}

	marked := NewFileItem(entry, false, true, theme, 20).Render()
	unmarked := NewFileItem(entry, false, false, theme, 20).Render()

	if !strings.Contains(stripANSI(marked), "*") {
		t.Error("marked item should render a marker")
	}
	if strings.Contains(stripANSI(unmarked), "*") {
		t.Error("unmarked item should not render a marker")
	}
}

// With the icons turned off, the icon cell is gone rather than blank, so a
// name starts right after the one-cell marker column and no glyph is drawn.
func TestFileItemWithoutIconsHasNoIconOrGap(t *testing.T) {
	icons.SetEnabled(false)
	t.Cleanup(func() { icons.SetEnabled(true) })

	theme := config.DefaultTheme()
	const width = 40
	entries := []fs.Entry{
		{Name: "notes.txt", Path: "/tmp/notes.txt"},
		{Name: "subdir", Path: "/tmp/subdir", IsDir: true},
	}

	for _, entry := range entries {
		out := stripANSI(NewFileItem(entry, false, false, theme, width).Render())
		name := entry.Name
		if entry.IsDir {
			name += "/"
		}
		// The marker and the space after it stay; the name takes the cell
		// where the icon used to be, so no blank icon column remains.
		if got := strings.Index(out, name); got != 2 {
			t.Errorf("%s: name starts at %d, want 2 (marker and its space, then the name): %q",
				entry.Name, got, out)
		}
		for _, glyph := range []string{icons.FileIcon, icons.TextIcon, icons.UnknownIcon, icons.DirIcon} {
			if strings.Contains(out, glyph) {
				t.Errorf("%s: glyph %q reached the row with the icons off: %q", entry.Name, glyph, out)
			}
		}
	}
}

func TestFileItemMarkerKeepsAlignment(t *testing.T) {
	theme := config.DefaultTheme()
	entry := fs.Entry{Name: "alpha.txt", Path: "/tmp/alpha.txt"}

	marked := NewFileItem(entry, false, true, theme, 24).Render()
	unmarked := NewFileItem(entry, false, false, theme, 24).Render()

	// Both render the name at the same column so rows stay aligned.
	mIdx := strings.Index(stripANSI(marked), "alpha.txt")
	uIdx := strings.Index(stripANSI(unmarked), "alpha.txt")
	if mIdx != uIdx {
		t.Errorf("name column differs: marked=%d unmarked=%d", mIdx, uIdx)
	}
}

// A tab in a filename is data, not an instruction to move the cursor. The row
// must stay one line at the width it was given, with its details where a plain
// name puts them, rather than letting the terminal expand the tab and wrap.
func TestFileItemTabInNameKeepsRowOnOneLine(t *testing.T) {
	theme := config.DefaultTheme()
	tabbed := fs.Entry{Name: "ab\tcd.txt", Path: "/tmp/ab\tcd.txt", Size: 12}
	spaced := fs.Entry{Name: "ab cd.txt", Path: "/tmp/ab cd.txt", Size: 12}

	const width = 40
	got := NewFileItem(tabbed, false, false, theme, width).Render()

	if n := strings.Count(got, "\n"); n != 0 {
		t.Errorf("row rendered on %d lines, want 1:\n%q", n+1, got)
	}
	if w := DisplayWidth(got); w != width {
		t.Errorf("row is %d cells wide, want %d:\n%q", w, width, got)
	}

	// A name with a tab must render exactly like the same name with the space
	// that replaces it, so the detail columns line up across rows.
	want := NewFileItem(spaced, false, false, theme, width).Render()
	if got != want {
		t.Errorf("tabbed row differs from the spaced row:\n got %q\nwant %q", got, want)
	}
}

// The filename on screen shows a space where the tab was.
func TestFileItemShowsTabAsSpace(t *testing.T) {
	theme := config.DefaultTheme()
	entry := fs.Entry{Name: "ab\tcd.txt", Path: "/tmp/ab\tcd.txt"}

	out := stripANSI(NewFileItem(entry, false, false, theme, 40).Render())
	if !strings.Contains(out, "ab cd.txt") {
		t.Errorf("tab was not rendered as a space: %q", out)
	}
	if strings.ContainsRune(out, '\t') {
		t.Errorf("a tab reached the rendered row: %q", out)
	}
}

func TestPaneMarksSelectedRows(t *testing.T) {
	theme := config.DefaultTheme()
	entry := fs.Entry{Name: "alpha.txt", Path: "/tmp/alpha.txt"}

	pane := NewPane(30, 12, true, theme)
	pane.Path = "/tmp"
	pane.Files = []fs.Entry{entry}
	pane.Marked = map[string]bool{entry.Path: true}

	out := stripANSI(pane.Render())
	if !strings.Contains(out, "*") {
		t.Error("pane should render the marker for a marked entry")
	}
}

func TestPaneEmptyMarkedMapRendersNoMarker(t *testing.T) {
	theme := config.DefaultTheme()
	entry := fs.Entry{Name: "alpha.txt", Path: "/tmp/alpha.txt"}

	pane := NewPane(30, 12, true, theme)
	pane.Path = "/tmp"
	pane.Files = []fs.Entry{entry}

	if out := stripANSI(pane.Render()); strings.Contains(out, "*") {
		t.Error("pane with no marks should not render markers")
	}
}
