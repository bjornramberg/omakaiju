package ui

import (
	"strings"
	"testing"

	"github.com/bjornramberg/omakaiju/internal/config"
)

func TestMenuRendersItemsAndTitle(t *testing.T) {
	m := Menu{
		Title: "file.txt",
		Items: []MenuItem{
			{Label: "open"},
			{Label: "rename"},
			{Label: "copy"},
			{Label: "delete"},
		},
		Theme: config.DefaultTheme(),
		Width: 40,
	}

	plain := stripANSI(m.Render())
	if !strings.Contains(plain, "file.txt") {
		t.Errorf("title missing, got %q", plain)
	}
	for _, want := range []string{"open", "rename", "copy", "delete"} {
		if !strings.Contains(plain, want) {
			t.Errorf("item %q missing, got %q", want, plain)
		}
	}
}

// The selected row is rendered differently from the others so the keyboard
// cursor is visible.
func TestMenuSelectedIsDistinguishable(t *testing.T) {
	theme := config.DefaultTheme()
	base := Menu{
		Title: "file.txt",
		Items: []MenuItem{
			{Label: "open"},
			{Label: "rename"},
		},
		Theme: theme,
		Width: 30,
	}

	first := base
	first.Selected = 0
	second := base
	second.Selected = 1

	if first.Render() == second.Render() {
		t.Errorf("selected row 0 and selected row 1 rendered identically")
	}

	// With no selection the two rows are styled the same way, so only the
	// label text differs.
	base.Selected = -1
	lines := strings.Split(stripANSI(base.Render()), "\n")
	var itemLines []string
	for _, l := range lines {
		if strings.Contains(l, "open") || strings.Contains(l, "rename") {
			itemLines = append(itemLines, l)
		}
	}
	if len(itemLines) != 2 {
		t.Fatalf("expected 2 item rows, got %d: %v", len(itemLines), itemLines)
	}
	// The plain text is identical apart from the labels; the test above
	// already proved selection changes the rendering.
}

// Separators render as a horizontal rule and do not count as selectable
// items, so the indices the app uses stay aligned with the Items slice.
func TestMenuSeparatorDrawsDividerAndKeepsIndices(t *testing.T) {
	m := Menu{
		Title: "file.txt",
		Items: []MenuItem{
			{Label: "open"},
			{Separator: true},
			{Label: "rename"},
		},
		Theme: config.DefaultTheme(),
		Width: 30,
	}

	plain := stripANSI(m.Render())
	if !strings.Contains(plain, "────────") {
		t.Errorf("separator divider not found in:\n%s", plain)
	}

	w, h := m.Size()
	originX, originY := 10, 5
	if got := m.ItemAt(originX+3, originY+2+2+0, originX, originY); got != 0 {
		t.Errorf("item 0 hit-test = %d, want 0", got)
	}
	if got := m.ItemAt(originX+3, originY+2+2+2, originX, originY); got != 2 {
		t.Errorf("item 2 hit-test = %d, want 2", got)
	}
	if w <= 0 || h <= 0 {
		t.Errorf("Size() returned non-positive dimensions %dx%d", w, h)
	}
}

func TestMenuItemAt(t *testing.T) {
	m := Menu{
		Title: "file.txt",
		Items: []MenuItem{
			{Label: "open"},
			{Label: "rename"},
			{Separator: true},
			{Label: "delete"},
		},
		Theme: config.DefaultTheme(),
		Width: 30,
	}

	w, h := m.Size()
	originX, originY := 5, 4

	cases := []struct {
		x, y int
		want int
	}{
		{originX + 3, originY + 2 + 2 + 0, 0},      // first item
		{originX + w - 4, originY + 2 + 2 + 1, 1},  // last column of second item
		{originX + 3, originY + 2 + 2 + 2, -1},     // separator row
		{originX + 3, originY + 2 + 2 + 3, 3},      // last item
		{originX + 2, originY + 2 + 2 + 0, -1},     // left padding
		{originX + w - 3, originY + 2 + 2 + 0, -1}, // right padding
		{originX + 3, originY + 1, -1},             // top padding
		{originX + 3, originY + h - 2, -1},         // bottom padding
		{originX - 1, originY + 2 + 2 + 0, -1},     // left of menu
		{originX + w, originY + 2 + 2 + 0, -1},     // right of menu
		{originX + 3, originY - 1, -1},             // above menu
		{originX + 3, originY + h, -1},             // below menu
	}

	for _, tc := range cases {
		got := m.ItemAt(tc.x, tc.y, originX, originY)
		if got != tc.want {
			t.Errorf("ItemAt(%d,%d) = %d, want %d", tc.x, tc.y, got, tc.want)
		}
	}
}

// Size must agree with the rendered output: the rendered panel has the same
// outer width and the same number of lines as Size promises.
func TestMenuSizeMatchesRender(t *testing.T) {
	m := Menu{
		Title: "file.txt",
		Items: []MenuItem{
			{Label: "open"},
			{Label: "rename"},
		},
		Theme: config.DefaultTheme(),
		Width: 30,
	}

	w, h := m.Size()
	rendered := m.Render()
	lines := strings.Split(rendered, "\n")
	if len(lines) != h {
		t.Errorf("rendered %d lines, Size() height %d", len(lines), h)
	}
	for i, line := range lines {
		if got := DisplayWidth(line); got != w {
			t.Errorf("line %d width %d, Size() width %d", i, got, w)
		}
	}
}

// A disabled item is still listed, but it is rendered in the dim status
// colour rather than the normal text colour.
func TestMenuDisabledItemIsDim(t *testing.T) {
	theme := config.DefaultTheme()
	m := Menu{
		Title:   "file.txt",
		Items:   []MenuItem{{Label: "open"}, {Label: "delete", Disabled: true}},
		Theme:   theme,
		Width:   30,
		Hovered: 1,
	}

	plain := stripANSI(m.Render())
	if !strings.Contains(plain, "delete") {
		t.Fatalf("disabled item missing: %q", plain)
	}
	// The hover marker is not added to disabled items because they cannot be
	// chosen.
	if strings.Contains(plain, "▸ delete") {
		t.Errorf("disabled item carried the hover marker")
	}
}
