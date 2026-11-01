package config

import (
	"os"
	"path/filepath"
	"strconv"
	"strings"
)

type Config struct {
	// ThemePath is the palette file the app renders its colours from.
	ThemePath string
	// NoIcons turns off the Nerd Font glyphs in the file rows, the top bar,
	// and the archive listing. The font is optional, so a terminal without
	// it can read clean rows instead of a tofu box in every one. It is read
	// from OMAKAIJU_NO_ICONS and is off by default, which keeps the icons
	// that the people who have the font expect.
	NoIcons bool
	// ThemeWatchDirs are the directories to watch for palette changes. A theme
	// switch swaps a symlink rather than rewriting the file, so both the
	// resolved theme directory and its parent are watched.
	ThemeWatchDirs []string
	// OmarchyStateDir is the root whose changes signal a theme switch.
	OmarchyStateDir string
}

func Load() (Config, error) {
	home, err := os.UserHomeDir()
	if err != nil {
		return Config{}, err
	}

	// Omarchy renders each theme into ~/.local/state/omarchy/current/theme, and
	// `current` is a symlink re-pointed on every theme switch. This is the same
	// path omarchy's own omarchy-theme-color helper reads.
	colorsFile := filepath.Join(home, ".local", "state", "omarchy", "current", "theme", "colors.toml")
	if env := os.Getenv("OMARCHY_COLORS"); env != "" {
		colorsFile = env
	}

	// Older setups render a per-application template instead.
	if _, err := os.Stat(colorsFile); err != nil {
		legacy := filepath.Join(home, ".config", "omarchy", "fm.toml")
		if _, lerr := os.Stat(legacy); lerr == nil {
			colorsFile = legacy
		}
	}

	state := filepath.Join(home, ".local", "state", "omarchy")
	cfg := Config{
		ThemePath:       colorsFile,
		OmarchyStateDir: state,
		NoIcons:         iconsDisabled(),
	}
	cfg.ThemeWatchDirs = watchDirs(home, colorsFile)
	return cfg, nil
}

// noIconsEnv is the environment variable that turns the icons off. It follows
// the same pattern as OMAKAIJU_COLORS and XDG_DATA_HOME rather than adding a
// settings file: it is one setting, and the environment is the lightest way to
// carry it.
const noIconsEnv = "OMAKAIJU_NO_ICONS"

// iconsDisabled reads OMAKAIJU_NO_ICONS. An unset or empty value keeps the
// icons on. A value that parses as a boolean follows the bool; anything else
// is treated as a request to turn them off, so `OMAKAIJU_NO_ICONS=yes` works
// as a reader would expect.
func iconsDisabled() bool {
	v := strings.TrimSpace(os.Getenv(noIconsEnv))
	if v == "" {
		return false
	}
	off, err := strconv.ParseBool(v)
	if err != nil {
		return true
	}
	return off
}

// watchDirs returns the directories whose contents can signal a palette change.
func watchDirs(home, colorsFile string) []string {
	seen := map[string]bool{}
	var dirs []string

	add := func(dir string) {
		if dir == "" || dir == "." || seen[dir] {
			return
		}
		if st, err := os.Stat(dir); err != nil || !st.IsDir() {
			return
		}
		seen[dir] = true
		dirs = append(dirs, dir)
	}

	// The directory holding the palette file catches edits in place.
	add(filepath.Dir(colorsFile))

	// `current` is a symlink into the theme store; watching its parent catches
	// the symlink being re-pointed when the theme changes.
	current := filepath.Join(home, ".local", "state", "omarchy", "current")
	if filepath.Dir(colorsFile) != current {
		add(current)
		add(filepath.Dir(current))
	}
	return dirs
}
