package ui

import (
	"fmt"
	"path/filepath"
	"strings"

	"github.com/bjornramberg/omakaiju/internal/config"
	"github.com/bjornramberg/omakaiju/internal/fuzzy"
)

type FuzzyFinder struct {
	// Focused selects the accent frame instead of the neutral one.
	Focused bool
	Width   int
	Height  int
	Theme   config.Theme
	Input   string
	Results []string
	Cursor  int
	// Loading reports that the walk is still running. Root is shown so the wait
	// is attributable to a directory rather than looking like a hang.
	Loading bool
	Root    string
}

func NewFuzzyFinder(width, height int, theme config.Theme) FuzzyFinder {
	return FuzzyFinder{Width: width, Height: height, Theme: theme}
}

// BoxWidth is the width the panel occupies, leaving a margin so the panes stay
// visible behind it. Narrow terminals still get something usable.
func (f FuzzyFinder) BoxWidth() int {
	w := f.Width - 8
	if w > 100 {
		w = 100
	}
	if w < 24 {
		w = 24
	}
	if w > f.Width {
		w = f.Width
	}
	return w
}

// contentWidth is the space inside the border and padding.
func (f FuzzyFinder) contentWidth() int {
	w := f.BoxWidth() - 2 /*border*/ - 4 /*padding*/
	if w < 1 {
		w = 1
	}
	return w
}

// maxResultRows is the vertical budget for the list, including the marker rows
// that show the list continues. The panel style draws a border and one row of
// padding above and below, and six lines sit around the list itself, so
// subtracting ten keeps the whole box inside the terminal.
// Capped rather than filling the screen so the panes stay visible behind it.
func (f FuzzyFinder) maxResultRows() int {
	rows := f.Height - 10
	if rows > 10 {
		rows = 10
	}
	if rows < 1 {
		rows = 1
	}
	return rows
}

// visibleWindow returns the slice of results shown and whether the list continues
// above or below, so the cursor stays on screen when moving in either direction.
func (f FuzzyFinder) visibleWindow() (start, end int, above, below bool) {
	rows := f.maxResultRows()
	total := len(f.Results)
	if total <= rows {
		return 0, total, false, false
	}

	// Rows are reserved for overflow markers so the rendered list never grows
	// past its budget. Very short panels cannot afford markers at all.
	markers := 0
	switch {
	case rows >= 3:
		markers = 2
	case rows == 2:
		markers = 1
	}
	capacity := rows - markers
	if capacity < 1 {
		capacity = 1
	}

	start = f.Cursor - capacity/2
	if start < 0 {
		start = 0
	}
	if start > total-capacity {
		start = total - capacity
	}
	end = start + capacity
	if end > total {
		end = total
	}

	above = start > 0
	below = end < total
	// With room for only one marker, show which way the list continues rather
	// than exceeding the budget.
	if markers == 1 && above && below {
		above = false
	}
	if markers == 0 {
		above, below = false, false
	}
	return start, end, above, below
}

// Render draws the finder as a bordered panel sized to fit inside the terminal.
// It is meant to be overlaid on the live layout rather than replacing it.
func (f FuzzyFinder) Render() string {
	style := f.Theme.PreviewPanel()
	if f.Focused {
		style = f.Theme.PreviewPanelFocused()
	}

	header := f.Theme.AccentText().Bold(true).Render(" fuzzy find ")
	prompt := f.Theme.AccentText().Render("> ")
	input := f.Theme.Text().Render(TruncateEnd(f.Input, f.contentWidth()-2))

	contentWidth := f.contentWidth()
	start, end, above, below := f.visibleWindow()

	var rows []string
	switch {
	case f.Loading:
		label := "scanning"
		if f.Root != "" {
			label = "scanning " + f.Root
		}
		rows = append(rows, f.Theme.StatusText().Render(TruncateEnd(label, contentWidth)))
	case len(f.Results) == 0:
		rows = append(rows, f.Theme.StatusText().Render("no results"))
	default:
		if above {
			rows = append(rows, f.Theme.StatusText().Render("  ..."))
		}
		for i := start; i < end; i++ {
			rows = append(rows, f.renderRow(f.Results[i], i == f.Cursor, contentWidth))
		}
		if below {
			rows = append(rows, f.Theme.StatusText().Render("  ..."))
		}
	}

	parts := []string{}
	if pos := f.position(); pos != "" {
		parts = append(parts, pos)
	}
	parts = append(parts, "j/k:nav", "enter:select", "esc:cancel")
	footer := f.Theme.StatusText().Render(strings.Join(parts, " | "))

	lines := []string{header, "", prompt + input, ""}
	lines = append(lines, rows...)
	lines = append(lines, "", footer)

	// Border plus padding is four rows of chrome. On a very short terminal the
	// blank separators are the only thing that can go, and dropping them keeps
	// the prompt and footer on screen instead of letting the panel run past
	// the bottom edge.
	budget := f.Height - 4
	if budget < 1 {
		budget = 1
	}
	for len(lines) > budget {
		dropped := false
		for i := len(lines) - 2; i >= 0; i-- {
			if lines[i] == "" {
				lines = append(lines[:i], lines[i+1:]...)
				dropped = true
				break
			}
		}
		if !dropped {
			break
		}
	}

	content := strings.Join(lines, "\n")

	// Width here is the panel's content width; the border adds two more cells,
	// so the outer bound is applied separately to keep the box inside the
	// terminal rather than two columns past it.
	return style.
		Width(f.BoxWidth() - 2).
		MaxWidth(f.BoxWidth() - 2).
		Render(content)
}

// position describes where the cursor sits, without implying a selection exists
// while the walk is still running.
func (f FuzzyFinder) position() string {
	if f.Loading {
		return ""
	}
	if len(f.Results) == 0 {
		return "0/0"
	}
	return fmt.Sprintf("%d/%d", f.Cursor+1, len(f.Results))
}

// renderRow shows the filename prominently with its directory dimmed, because a
// shared long prefix makes full paths hard to scan. The matched characters are
// emphasised in the filename.
func (f FuzzyFinder) renderRow(path string, active bool, width int) string {
	name := SanitizeDisplay(filepath.Base(path))
	dir := SanitizeDisplay(filepath.Dir(path))

	// One cell is reserved for the leading marker space so active and inactive
	// rows stay aligned.
	avail := width - 1
	if avail < 1 {
		avail = 1
	}

	var line string
	if dir == "." || dir == "" {
		line = f.highlight(name)
	} else {
		// The filename leads so repeated hits line up in a column, and the
		// directory trails dimmed. The directory is shortened from the left so
		// its most specific part, the part nearest the file, stays visible.
		dirW := avail - DisplayWidth(name) - 2 /*gap*/
		if dirW < 1 {
			dirW = 1
		}
		line = f.highlight(name) + "  " + f.Theme.StatusText().Render(TrimPathLeft(dir, dirW))
	}
	line = TruncateEnd(line, avail)

	// The theme adds horizontal padding, which would push the row past the
	// panel's content width and wrap it onto a second row. The marker space
	// already provides the indent.
	style := f.Theme.FileItem()
	if active {
		style = f.Theme.FileItemActive()
	}
	return style.UnsetPaddingLeft().UnsetPaddingRight().Render(" " + line)
}

func (f FuzzyFinder) Selected() string {
	if f.Cursor < len(f.Results) {
		return f.Results[f.Cursor]
	}
	return ""
}

// highlight emphasises the characters that matched the query. Names that no
// longer match are shown plainly rather than marked.
func (f FuzzyFinder) highlight(name string) string {
	if f.Input == "" {
		return name
	}
	if _, ok := fuzzy.Match(f.Input, name); !ok {
		return name
	}

	var sb strings.Builder
	pi := 0
	nameLower := strings.ToLower(name)
	inputLower := strings.ToLower(f.Input)
	for i := 0; i < len(name); i++ {
		if pi < len(inputLower) && nameLower[i] == inputLower[pi] {
			sb.WriteString(f.Theme.AccentText().Bold(true).Render(string(name[i])))
			pi++
			continue
		}
		sb.WriteString(string(name[i]))
	}
	return sb.String()
}
