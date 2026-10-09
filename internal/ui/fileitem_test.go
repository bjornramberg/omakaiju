package ui

import (
	"strings"
	"testing"

	"omakaiju/internal/config"
	"omakaiju/internal/fs"
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
