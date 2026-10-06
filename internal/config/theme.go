package config

import (
	"os"

	"github.com/BurntSushi/toml"
	"charm.land/lipgloss/v2"
)

type Theme struct {
	Primary     string
	Subtle      string
	SelectionBG string
	FGMain      string
	Accent      string
	BGDark      string
}

type themeFile struct {
	Omarchy struct {
		Primary     string `toml:"primary"`
		Subtle      string `toml:"subtle"`
		SelectionBG string `toml:"selection_bg"`
		FGMain      string `toml:"fg_main"`
		Accent      string `toml:"accent"`
		BGDark      string `toml:"bg_dark"`
	} `toml:"omarchy"`
}

func LoadTheme(path string) (Theme, error) {
	var tf themeFile
	if _, err := toml.DecodeFile(path, &tf); err != nil {
		return Theme{}, err
	}

	return Theme{
		Primary:     tf.Omarchy.Primary,
		Subtle:      tf.Omarchy.Subtle,
		SelectionBG: tf.Omarchy.SelectionBG,
		FGMain:      tf.Omarchy.FGMain,
		Accent:      tf.Omarchy.Accent,
		BGDark:      tf.Omarchy.BGDark,
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

func ThemePath() string {
	home, err := os.UserHomeDir()
	if err != nil {
		return ""
	}
	return home + "/.config/omarchy/fm.toml"
}
