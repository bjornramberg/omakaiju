package ui

import (
	"os"
	"strings"

	"github.com/bjornramberg/omakaiju/internal/config"
	"github.com/bjornramberg/omakaiju/internal/icons"
)

type TopBar struct {
	Width  int
	Path   string
	Filter string
	// Host overrides the machine name, mainly so tests can pin the display.
	// Empty means os.Hostname().
	Host  string
	Theme config.Theme
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

// pinRight pushes host against the right edge of the content area, padding the
// gap to the left of it. The host is dropped when it cannot fit with at least
// one separating space, so a long path keeps priority over it.
func (t TopBar) pinRight(content, host string, budget int) string {
	gap := budget - DisplayWidth(content) - DisplayWidth(host)
	if host == "" || gap < 1 {
		return content
	}
	return content + strings.Repeat(" ", gap) + host
}

// appName renders the app name, prefixed with a Nerd Font glyph and a space.
// The glyph is decorative, so it is only shown when the bar has room for the
// whole name plus the icon; a bar too narrow for that keeps the readable name
// and drops the icon rather than clipping the name.
func (t TopBar) appName(budget int) string {
	name := "omakaiju"
	prefixed := name
	// With the icons off there is no glyph and no space after it, so the name
	// itself starts at the bar's left edge.
	if icon := icons.Glyph(icons.AppIcon); icon != "" {
		prefixed = icon + " " + name
	}
	if budget < DisplayWidth(prefixed) {
		return t.Theme.AccentText().Bold(true).Render(name)
	}
	return t.Theme.AccentText().Bold(true).Render(prefixed)
}

func (t TopBar) Render() string {
	if t.Width <= 0 {
		return ""
	}

	hostname := t.Host
	if hostname == "" {
		hostname, _ = os.Hostname()
	}
	if t.Path == "" {
		t.Path = "/"
	}

	// The separator is a decorative glyph; without it the cells keep a plain
	// two-space gap so they stay visually distinct.
	sep := "  "
	if glyph := icons.Glyph(icons.Separator); glyph != "" {
		sep = " " + glyph + " "
	}
	// The left padding is gone so the title starts on the pane frame's column;
	// only the right padding remains chrome.
	budget := t.Width - 2
	if budget < 1 {
		budget = 1
	}

	name := t.appName(budget)
	path := t.Theme.PathText().Render(TrimPathLeft(SanitizeName(t.Path), budget))
	host := t.Theme.StatusText().Render(hostname)

	// Path stays ahead of the hostname: when the bar is too narrow, the host is
	// the cell that gives way because it is the least specific of the three.
	content := t.fit([]string{name, path}, sep, budget)

	if t.Filter != "" {
		filter := t.Theme.AccentText().Render("filter: " + t.Filter)
		candidate := content + sep + filter
		// The filter may use spare space, but it must not push off a host that
		// would otherwise fit on the right.
		if DisplayWidth(candidate) <= budget &&
			(DisplayWidth(content)+DisplayWidth(host)+1 > budget ||
				DisplayWidth(candidate)+DisplayWidth(host)+1 <= budget) {
			content = candidate
		}
	}

	content = t.pinRight(content, host, budget)

	// Both dimensions are pinned: the bar occupies exactly TopBarHeight rows and
	// never exceeds the terminal, so neighbouring sections stay aligned.
	return t.Theme.TopBar().
		Width(t.Width).
		MaxWidth(t.Width).
		Height(3).
		MaxHeight(3).
		Render(content)
}
