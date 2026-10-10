package config

import (
	"os"
	"path/filepath"
	"testing"
)

func writeFile(t *testing.T, dir, name, body string) string {
	t.Helper()
	if err := os.MkdirAll(dir, 0o755); err != nil {
		t.Fatal(err)
	}
	p := filepath.Join(dir, name)
	if err := os.WriteFile(p, []byte(body), 0o644); err != nil {
		t.Fatal(err)
	}
	return p
}

const flatPalette = `mode = "dark"
accent = "#2b5e8f"
selection = "#1c3b5c"
muted = "#294857"
background = "#0b1b2b"
dark_background = "#09141b"
foreground = "#b9b6a7"
dark_foreground = "#667286"
bright_foreground = "#d4d1c2"
cyan = "#638f92"
bright_cyan = "#7aa8ab"
`

func TestLoadThemeReadsFlatPalette(t *testing.T) {
	p := writeFile(t, t.TempDir(), "colors.toml", flatPalette)

	theme, err := LoadTheme(p)
	if err != nil {
		t.Fatal(err)
	}

	if theme.BGDark != "#0b1b2b" {
		t.Errorf("BGDark = %q, want the theme background, not black", theme.BGDark)
	}
	if theme.FGMain != "#b9b6a7" {
		t.Errorf("FGMain = %q, want #b9b6a7", theme.FGMain)
	}
	if theme.Primary != "#2b5e8f" {
		t.Errorf("Primary = %q, want the accent", theme.Primary)
	}
	if theme.Subtle != "#667286" {
		t.Errorf("Subtle = %q, want the dark foreground", theme.Subtle)
	}
	if theme.SelectionBG != "#1c3b5c" {
		t.Errorf("SelectionBG = %q, want the selection colour", theme.SelectionBG)
	}
	// Accent text must differ from the primary border colour.
	if theme.Accent == theme.Primary {
		t.Errorf("Accent and Primary are both %q; they should differ", theme.Accent)
	}
}

func TestLoadThemeBackgroundIsNeverHardcodedBlack(t *testing.T) {
	p := writeFile(t, t.TempDir(), "colors.toml", flatPalette)
	theme, err := LoadTheme(p)
	if err != nil {
		t.Fatal(err)
	}
	if theme.BGDark == DefaultTheme().BGDark {
		t.Error("BGDark fell back to the hardcoded default instead of the theme")
	}
}

func TestLoadThemeAddsMissingHash(t *testing.T) {
	p := writeFile(t, t.TempDir(), "colors.toml", "background = \"0b1b2b\"\n")
	theme, err := LoadTheme(p)
	if err != nil {
		t.Fatal(err)
	}
	if theme.BGDark != "#0b1b2b" {
		t.Errorf("BGDark = %q, want a # prefix added", theme.BGDark)
	}
}

func TestLoadThemePartialPaletteFallsBackPerField(t *testing.T) {
	p := writeFile(t, t.TempDir(), "colors.toml", "background = \"#101820\"\n")

	theme, err := LoadTheme(p)
	if err != nil {
		t.Fatal(err)
	}
	if theme.BGDark != "#101820" {
		t.Errorf("BGDark = %q, want the provided value", theme.BGDark)
	}
	if theme.FGMain == "" || theme.Primary == "" {
		t.Error("missing fields should fall back, not be empty")
	}
}

func TestLoadThemeLegacyNestedPalette(t *testing.T) {
	p := writeFile(t, t.TempDir(), "fm.toml", `[omarchy]
primary = "#aabbcc"
subtle = "#445566"
selection_bg = "#223344"
fg_main = "#ddeeff"
accent = "#ff00ff"
bg_dark = "#0a0a14"
`)

	theme, err := LoadTheme(p)
	if err != nil {
		t.Fatal(err)
	}
	if theme.Primary != "#aabbcc" || theme.BGDark != "#0a0a14" || theme.Accent != "#ff00ff" {
		t.Errorf("legacy palette not honoured: %+v", theme)
	}
}

func TestLoadThemeMissingFileErrors(t *testing.T) {
	if _, err := LoadTheme(filepath.Join(t.TempDir(), "nope.toml")); err == nil {
		t.Error("a missing palette should error so the caller can fall back")
	}
}

func TestLoadPrefersOmarchyColorsFile(t *testing.T) {
	home := t.TempDir()
	state := filepath.Join(home, ".local", "state", "omarchy", "current", "theme")
	writeFile(t, state, "colors.toml", flatPalette)

	// A stale legacy file must not win when the canonical one exists.
	writeFile(t, filepath.Join(home, ".config", "omarchy"), "fm.toml", "[omarchy]\nprimary = \"#000000\"\n")

	t.Setenv("HOME", home)
	t.Setenv("OMARCHY_COLORS", "")
	os.Unsetenv("OMARCHY_COLORS")

	cfg, err := Load()
	if err != nil {
		t.Fatal(err)
	}
	if filepath.Base(cfg.ThemePath) != "colors.toml" {
		t.Errorf("ThemePath = %q, want colors.toml", cfg.ThemePath)
	}
	theme, err := LoadTheme(cfg.ThemePath)
	if err != nil {
		t.Fatal(err)
	}
	if theme.BGDark != "#0b1b2b" {
		t.Errorf("BGDark = %q, want the live theme background", theme.BGDark)
	}
}

func TestLoadFallsBackToLegacyFile(t *testing.T) {
	home := t.TempDir()
	writeFile(t, filepath.Join(home, ".config", "omarchy"), "fm.toml",
		"[omarchy]\nbg_dark = \"#0a0a14\"\n")

	t.Setenv("HOME", home)

	cfg, err := Load()
	if err != nil {
		t.Fatal(err)
	}
	if filepath.Base(cfg.ThemePath) != "fm.toml" {
		t.Errorf("ThemePath = %q, want the legacy fm.toml", cfg.ThemePath)
	}
}

func TestWatchDirsIncludeThemeAndState(t *testing.T) {
	home := t.TempDir()
	state := filepath.Join(home, ".local", "state", "omarchy", "current", "theme")
	writeFile(t, state, "colors.toml", flatPalette)
	t.Setenv("HOME", home)

	cfg, err := Load()
	if err != nil {
		t.Fatal(err)
	}
	if len(cfg.ThemeWatchDirs) == 0 {
		t.Fatal("no watch directories resolved")
	}
	// The directory holding the palette must always be watched.
	found := false
	for _, d := range cfg.ThemeWatchDirs {
		if d == state {
			found = true
		}
	}
	if !found {
		t.Errorf("watch dirs %v do not include the palette directory", cfg.ThemeWatchDirs)
	}
}
