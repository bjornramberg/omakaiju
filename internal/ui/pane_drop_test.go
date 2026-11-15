package ui

import (
	"strings"
	"testing"

	"github.com/bjornramberg/omakaiju/internal/config"
	"github.com/bjornramberg/omakaiju/internal/fs"
)

// dropFixture builds an inactive pane with a small listing. The drop target is
// always the pane that does not have focus, so the tests exercise the state
// the app will actually set.
func dropFixture(t *testing.T) Pane {
	t.Helper()

	files := []fs.Entry{
		{Name: "alpha.txt", Path: "/tmp/alpha.txt"},
		{Name: "beta", Path: "/tmp/beta", IsDir: true},
		{Name: "gamma.txt", Path: "/tmp/gamma.txt"},
	}
	pane := NewPane(40, 12, false, config.DefaultTheme())
	pane.Path = "/home/user/projects"
	pane.Files = files
	pane.Cursor = 1
	pane.Parent = "/home/user"
	return pane
}

// A pane with DropMove draws a move cue in its header.
func TestDropMoveShowsMoveIndicator(t *testing.T) {
	theme := config.DefaultTheme()
	p := dropFixture(t)
	p.Drop = DropMove

	header := rawLineWith(p.Render(), "projects")
	if header == "" {
		t.Fatal("could not find the header row")
	}
	if !strings.Contains(stripANSI(header), "→ move") {
		t.Errorf("DropMove header %q should contain the move tag", stripANSI(header))
	}
	if strings.Contains(stripANSI(header), "copy") {
		t.Errorf("DropMove header %q should not mention copy", stripANSI(header))
	}
	if !strings.Contains(header, ansiForeground(theme.Accent)) {
		t.Error("DropMove header should use the accent colour")
	}
}

// A pane with DropCopy draws a copy cue that differs from the move cue.
func TestDropCopyShowsCopyIndicator(t *testing.T) {
	theme := config.DefaultTheme()
	p := dropFixture(t)
	p.Drop = DropCopy

	header := rawLineWith(p.Render(), "projects")
	if header == "" {
		t.Fatal("could not find the header row")
	}
	if !strings.Contains(stripANSI(header), "copy") {
		t.Errorf("DropCopy header %q should contain the copy tag", stripANSI(header))
	}
	if strings.Contains(stripANSI(header), "move") {
		t.Errorf("DropCopy header %q should not mention move", stripANSI(header))
	}
	if !strings.Contains(header, ansiForeground(theme.Accent)) {
		t.Error("DropCopy header should use the accent colour")
	}
}

// A pane with DropNone draws neither a move nor a copy cue, and keeps its
// ordinary header colour so it does not look like a target.
func TestDropNoneShowsNoIndicator(t *testing.T) {
	theme := config.DefaultTheme()
	p := dropFixture(t)
	p.Drop = DropNone

	header := rawLineWith(p.Render(), "projects")
	if header == "" {
		t.Fatal("could not find the header row")
	}
	if strings.Contains(stripANSI(header), "move") || strings.Contains(stripANSI(header), "copy") {
		t.Errorf("DropNone header %q should not contain a drop tag", stripANSI(header))
	}
	if strings.Contains(header, ansiForeground(theme.Accent)) {
		t.Error("DropNone header should not use the accent colour")
	}
}

// The drop indicator lives in the header, so the pane's height and the rows
// the listing occupies must not change. A shifted row would make the pointer
// land on the wrong entry when the button is released.
func TestDropIndicatorDoesNotShiftTheListing(t *testing.T) {
	p := dropFixture(t)

	noneLines := strings.Split(p.Render(), "\n")
	p.Drop = DropMove
	moveLines := strings.Split(p.Render(), "\n")
	p.Drop = DropCopy
	copyLines := strings.Split(p.Render(), "\n")

	if len(noneLines) != len(moveLines) || len(noneLines) != len(copyLines) {
		t.Fatalf("pane height changed: DropNone=%d DropMove=%d DropCopy=%d",
			len(noneLines), len(moveLines), len(copyLines))
	}

	window := p.Rows()
	for offset := 0; offset < window.Count; offset++ {
		idx := window.Start + offset
		if idx >= len(p.Files) {
			break
		}
		row := window.Top + offset
		if row >= len(noneLines) {
			break
		}
		want := p.Files[idx].Name
		for _, label := range []struct {
			name  string
			lines []string
		}{
			{"DropNone", noneLines},
			{"DropMove", moveLines},
			{"DropCopy", copyLines},
		} {
			if !strings.Contains(stripANSI(label.lines[row]), want) {
				t.Errorf("%s: row %d should contain %q", label.name, row, want)
			}
		}
	}
}

// The parent row sits above the listing and is not part of the scroll window,
// so it must stay put when a drop tag appears in the header.
func TestDropIndicatorDoesNotShiftParentRow(t *testing.T) {
	p := dropFixture(t)

	none := strings.Split(stripANSI(p.Render()), "\n")
	p.Drop = DropMove
	move := strings.Split(stripANSI(p.Render()), "\n")

	if len(none) != len(move) {
		t.Fatalf("pane height changed: %d vs %d", len(none), len(move))
	}
	if !strings.Contains(none[paneRowsTop], "..") {
		t.Fatalf("setup: row %d should be the parent row, got %q", paneRowsTop, none[paneRowsTop])
	}
	if none[paneRowsTop] != move[paneRowsTop] {
		t.Errorf("parent row shifted: DropNone=%q DropMove=%q", none[paneRowsTop], move[paneRowsTop])
	}
}

// The drop target border uses the accent colour, which is different from the
// primary colour that marks the focused pane. This keeps the source and target
// visually separate during a drag.
func TestDropTargetBorderUsesAccentColour(t *testing.T) {
	theme := config.DefaultTheme()
	p := dropFixture(t)
	p.Drop = DropMove

	rendered := p.Render()
	// The accent border foreground appears on the frame; the header also uses
	// the accent colour, so the test pins the border sequence to the top-left
	// corner of the rendered pane.
	lines := strings.Split(rendered, "\n")
	if len(lines) == 0 {
		t.Fatal("pane rendered no lines")
	}
	border := lines[0]
	if !strings.Contains(border, ansiForeground(theme.Accent)) {
		t.Error("DropMove pane border should use the accent colour")
	}
}
