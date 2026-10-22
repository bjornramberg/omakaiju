package config

import (
	"fmt"
	"math"
	"os"
	"strconv"
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

// dimAmount is how far the unfocused pane's text is dimmed toward the
// theme's subtle foreground.
const dimAmount = 0.55

// accentDimAmount is how far the unfocused pane's directory accent is dimmed
// toward the dimmed text colour. The accent keeps a hint of its hue so the
// listing stays readable, but it stops shouting.
const accentDimAmount = 0.45

// blend mixes two hex colours, t being the weight of b. It reports false
// when either colour is not a hex value, so the caller can fall back to the
// unblended colour instead of guessing at a mix.
func blend(a, b string, t float64) (string, bool) {
	ar, ag, ab, ok := parseHex(a)
	if !ok {
		return "", false
	}
	br, bg, bb, ok := parseHex(b)
	if !ok {
		return "", false
	}
	mix := func(x, y uint8) uint8 {
		return uint8(math.Round(float64(x)*(1-t) + float64(y)*t))
	}
	return fmt.Sprintf("#%02x%02x%02x", mix(ar, br), mix(ag, bg), mix(ab, bb)), true
}

// blendColor blends two colours and falls back to def when the palette
// carries a colour the mixer cannot read.
func blendColor(a, b string, t float64, def string) string {
	if c, ok := blend(a, b, t); ok {
		return c
	}
	return def
}

// parseHex reads #rgb or #rrggbb into its three channels.
func parseHex(c string) (r, g, b uint8, ok bool) {
	c = strings.TrimSpace(c)
	if !strings.HasPrefix(c, "#") {
		return 0, 0, 0, false
	}
	h := c[1:]
	switch len(h) {
	case 3:
		// #abc is #aabbcc.
		h = string([]byte{h[0], h[0], h[1], h[1], h[2], h[2]})
	case 6:
	default:
		return 0, 0, 0, false
	}
	v, err := strconv.ParseUint(h, 16, 32)
	if err != nil {
		return 0, 0, 0, false
	}
	return uint8(v >> 16), uint8(v >> 8), uint8(v), true
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

// DimmedColor is the unfocused pane's text colour: the main foreground
// dimmed toward the theme's subtle foreground, so the unfocused pane recedes
// without its listings becoming unreadable.
//
// It is derived from the palette rather than being a colour of its own, so a
// theme switch re-derives it like every other colour.
func (t Theme) DimmedColor() string {
	return blendColor(t.FGMain, t.Subtle, dimAmount, t.Subtle)
}

// dimmedAccentColor is the unfocused pane's directory accent.
func (t Theme) dimmedAccentColor() string {
	return blendColor(t.Accent, t.DimmedColor(), accentDimAmount, t.Accent)
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

// PaneMarker marks the focused pane's header. It is a shape cue first and a
// colour cue second: the glyph itself says which pane has focus, so the
// marker still reads where two colours are too close to tell apart.
func (t Theme) PaneMarker() lipgloss.Style {
	return lipgloss.NewStyle().
		Foreground(lipgloss.Color(t.Primary)).
		Bold(true)
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

// FileItemDimmed is a row in the unfocused pane.
func (t Theme) FileItemDimmed() lipgloss.Style {
	return lipgloss.NewStyle().
		Foreground(lipgloss.Color(t.DimmedColor())).
		Padding(0, 1)
}

// FileItemDirDimmed is a directory row in the unfocused pane.
func (t Theme) FileItemDirDimmed() lipgloss.Style {
	return lipgloss.NewStyle().
		Foreground(lipgloss.Color(t.dimmedAccentColor())).
		Padding(0, 1)
}

// FileItemCursorUnfocused is the cursor row of the unfocused pane. It keeps
// the cursor findable but drops the selection bar, which otherwise out-shouts
// the focused pane's border.
func (t Theme) FileItemCursorUnfocused() lipgloss.Style {
	return lipgloss.NewStyle().
		Foreground(lipgloss.Color(t.FGMain)).
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

// PathTextUnfocused is the unfocused pane's header path.
func (t Theme) PathTextUnfocused() lipgloss.Style {
	return lipgloss.NewStyle().
		Foreground(lipgloss.Color(t.DimmedColor()))
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
