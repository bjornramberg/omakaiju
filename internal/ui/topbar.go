package ui

import (
	"os"

	"omakaiju/internal/config"
)

type TopBar struct {
	Width  int
	Path   string
	Filter string
	Theme  config.Theme
}

func NewTopBar(width int, theme config.Theme) TopBar {
	return TopBar{Width: width, Theme: theme}
}

// fit joins cells with separators, dropping trailing cells until the result
// fits budget. The last surviving cell is clipped to whatever space remains so
// name + separators + that cell can never exceed the budget, which is what
// would otherwise wrap the bar and shift the whole layout down.
func (t TopBar) fit(cells []string, sep string, budget int) string {
	if len(cells) == 0 || budget < 1 {
		return ""
	}

	used := 0
	kept := make([]string, 0, len(cells))
	for i, c := range cells {
		extra := DisplayWidth(c)
		if i > 0 {
			extra += DisplayWidth(sep)
		}
		// The first cell is always kept so the bar stays identifiable.
		if i > 0 && used+extra > budget {
			break
		}
		if i == 0 && extra > budget {
			c = TruncateEnd(c, budget)
			extra = DisplayWidth(c)
		}
		kept = append(kept, c)
		used += extra
	}
	return joinCells(kept, sep)
}

func joinCells(cells []string, sep string) string {
	out := ""
	for i, c := range cells {
		if i > 0 {
			out += sep
		}
		out += c
	}
	return out
}

func (t TopBar) Render() string {
	if t.Width <= 0 {
		return ""
	}

	hostname, _ := os.Hostname()
	if t.Path == "" {
		t.Path = "/"
	}

	sep := " │ "
	// Padding and separators are chrome, not content.
	budget := t.Width - 4
	if budget < 1 {
		budget = 1
	}

	name := t.Theme.AccentText().Bold(true).Render("omakaiju")
	path := t.Theme.PathText().Render(TrimPathLeft(t.Path, budget))
	host := t.Theme.StatusText().Render(hostname)

	cells := []string{name, path, host}
	content := t.fit(cells, sep, budget)

	if t.Filter != "" {
		filter := t.Theme.AccentText().Render("filter: " + t.Filter)
		candidate := content + sep + filter
		if DisplayWidth(candidate) <= budget {
			content = candidate
		}
	}

	// Both dimensions are pinned: the bar occupies exactly TopBarHeight rows and
	// never exceeds the terminal, so neighbouring sections stay aligned.
	return t.Theme.TopBar().
		Width(t.Width).
		MaxWidth(t.Width).
		Height(3).
		MaxHeight(3).
		Render(content)
}
