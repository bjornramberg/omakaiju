package config

import (
	"os"
	"strings"

	"charm.land/lipgloss/v2"
	"github.com/BurntSushi/toml"
)

// Theme holds the six palette values the UI renders with.
type Theme struct {
	Primary     string
	Subtle      string
	SelectionBG string
	FGMain      string
	Accent      string
	BGDark      string
}

// omarchyColors is the flat palette written by omarchy into each theme's
// colors.toml.
type omarchyColors struct {
	Mode             string `toml:"mode"`
	Accent           string `toml:"accent"`
	Selection        string `toml:"selection"`
	Muted            string `toml:"muted"`
	Background       string `toml:"background"`
	DarkBackground   string `toml:"dark_background"`
	DarkerBackground string `toml:"darker_background"`
	Foreground       string `toml:"foreground"`
	DarkForeground   string `toml:"dark_foreground"`
	BrightForeground string `toml:"bright_foreground"`
	Cyan             string `toml:"cyan"`
	BrightCyan       string `toml:"bright_cyan"`
	Orange           string `toml:"orange"`
	Blue             string `toml:"blue"`
}

// legacyThemeFile supports the older per-application template, which nested the
// values under an [omarchy] table.
type legacyThemeFile struct {
	Omarchy struct {
		Primary     string `toml:"primary"`
		Subtle      string `toml:"subtle"`
		SelectionBG string `toml:"selection_bg"`
		FGMain      string `toml:"fg_main"`
		Accent      string `toml:"accent"`
		BGDark      string `toml:"bg_dark"`
	} `toml:"omarchy"`
}

// first returns the first non-empty candidate, falling back to def.
func first(def string, candidates ...string) string {
	for _, c := range candidates {
		if strings.TrimSpace(c) != "" {
			return strings.TrimSpace(c)
		}
	}
	return def
}

func norm(v string) string {
	v = strings.TrimSpace(v)
	if v == "" {
		return ""
	}
	if !strings.HasPrefix(v, "#") {
		return "#" + v
	}
	return v
}

// LoadTheme reads an omarchy palette and maps it onto the UI's palette. Missing
// values fall back to sensible relatives so a partial palette still renders
// coherently rather than falling back wholesale to the defaults.
func LoadTheme(path string) (Theme, error) {
	data, err := os.ReadFile(path)
	if err != nil {
		return Theme{}, err
	}

	defaults := DefaultTheme()

	// The legacy template nests values under [omarchy]; try it first and fall
	// through to the flat palette.
	var legacy legacyThemeFile
	if _, err := toml.Decode(string(data), &legacy); err == nil && legacy.Omarchy.Primary != "" {
		o := legacy.Omarchy
		return Theme{
			Primary:     norm(first(defaults.Primary, o.Primary)),
			Subtle:      norm(first(defaults.Subtle, o.Subtle)),
			SelectionBG: norm(first(defaults.SelectionBG, o.SelectionBG)),
			FGMain:      norm(first(defaults.FGMain, o.FGMain)),
			Accent:      norm(first(defaults.Accent, o.Accent)),
			BGDark:      norm(first(defaults.BGDark, o.BGDark)),
		}, nil
	}

	var c omarchyColors
	if _, err := toml.Decode(string(data), &c); err != nil {
		return Theme{}, err
	}

	accent := first(defaults.Primary, c.Accent, c.Blue, c.Cyan)
	background := first(defaults.BGDark, c.Background, c.DarkBackground)

	return Theme{
		// Active pane border: the theme's accent.
		Primary: norm(accent),
		// Inactive borders: the dimmed foreground, so the focused pane stands out.
		Subtle:      norm(first(defaults.Subtle, c.DarkForeground, c.Muted)),
		SelectionBG: norm(first(defaults.SelectionBG, c.Selection, c.Muted)),
		// Default text.
		FGMain: norm(first(defaults.FGMain, c.Foreground, c.BrightForeground)),
		// Executable and link text: a distinct hue from the accent.
		Accent: norm(first(defaults.Accent, c.BrightCyan, c.Cyan, c.Orange, accent)),
		// Status bars follow the theme background rather than a fixed black.
		BGDark: norm(background),
	}, nil
}

func (t Theme) PaneActive() lipgloss.Style {
	return lipgloss.NewStyle().
		Border(lipgloss.RoundedBorder()).
		BorderForeground(lipgloss.Color(t.Primary)).
		BorderBackground(lipgloss.Color(t.BGDark)).
		Padding(1, 2)
}

func (t Theme) PaneInactive() lipgloss.Style {
	return lipgloss.NewStyle().
		Border(lipgloss.RoundedBorder()).
		BorderForeground(lipgloss.Color(t.Subtle)).
		BorderBackground(lipgloss.Color(t.BGDark)).
		Padding(1, 2)
}

func (t Theme) TopBar() lipgloss.Style {
	return lipgloss.NewStyle().
		Border(lipgloss.RoundedBorder(), false, false, true, false).
		BorderForeground(lipgloss.Color(t.Subtle)).
		Background(lipgloss.Color(t.BGDark)).
		Padding(0, 2)
}

func (t Theme) BottomBar() lipgloss.Style {
	return lipgloss.NewStyle().
		Border(lipgloss.RoundedBorder(), true, false, false, false).
		BorderForeground(lipgloss.Color(t.Subtle)).
		Background(lipgloss.Color(t.BGDark)).
		Padding(0, 2)
}

func (t Theme) PreviewPanel() lipgloss.Style {
	return lipgloss.NewStyle().
		Border(lipgloss.RoundedBorder()).
		BorderForeground(lipgloss.Color(t.Subtle)).
		BorderBackground(lipgloss.Color(t.BGDark)).
		Padding(1, 2)
}

func (t Theme) PreviewPanelFocused() lipgloss.Style {
	return lipgloss.NewStyle().
		Border(lipgloss.RoundedBorder()).
		BorderForeground(lipgloss.Color(t.Primary)).
		BorderBackground(lipgloss.Color(t.BGDark)).
		Padding(1, 2)
}

func (t Theme) FileItem() lipgloss.Style {
	return lipgloss.NewStyle().
		Foreground(lipgloss.Color(t.FGMain)).
		Padding(0, 1)
}

func (t Theme) FileItemActive() lipgloss.Style {
	return lipgloss.NewStyle().
		Foreground(lipgloss.Color(t.FGMain)).
		Background(lipgloss.Color(t.SelectionBG)).
		Bold(true).
		Padding(0, 1)
}

func (t Theme) FileItemDir() lipgloss.Style {
	return lipgloss.NewStyle().
		Foreground(lipgloss.Color(t.Accent)).
		Bold(true).
		Padding(0, 1)
}

func (t Theme) FileItemExec() lipgloss.Style {
	return lipgloss.NewStyle().
		Foreground(lipgloss.Color(t.Accent)).
		Padding(0, 1)
}

func (t Theme) Text() lipgloss.Style {
	return lipgloss.NewStyle().
		Foreground(lipgloss.Color(t.FGMain))
}

func (t Theme) StatusText() lipgloss.Style {
	return lipgloss.NewStyle().
		Foreground(lipgloss.Color(t.Subtle))
}

func (t Theme) PathText() lipgloss.Style {
	return lipgloss.NewStyle().
		Foreground(lipgloss.Color(t.FGMain)).
		Bold(true)
}

func (t Theme) AccentText() lipgloss.Style {
	return lipgloss.NewStyle().
		Foreground(lipgloss.Color(t.Accent))
}

func (t Theme) ErrorText() lipgloss.Style {
	return lipgloss.NewStyle().
		Foreground(lipgloss.Color("#FF0000")).
		Bold(true)
}

// DefaultTheme is used only when no omarchy palette can be read.
func DefaultTheme() Theme {
	return Theme{
		Primary:     "#7D56F4",
		Subtle:      "#555555",
		SelectionBG: "#333333",
		FGMain:      "#FAFAFA",
		Accent:      "#FF6B6B",
		BGDark:      "#1A1A1A",
	}
}

// ThemePath returns the palette file the app expects to read.
func ThemePath() string {
	home, err := os.UserHomeDir()
	if err != nil {
		return ""
	}
	return home + "/.local/state/omarchy/current/theme/colors.toml"
}
