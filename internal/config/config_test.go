package config

import "testing"

// OMAKAIJU_NO_ICONS is the opt-in switch for a terminal without a Nerd Font.
// An absent or false value keeps the icons; any true value turns them off, and
// an unparseable value is treated as a request to turn them off.
func TestIconsDisabledFromEnv(t *testing.T) {
	cases := []struct {
		value string
		want  bool
	}{
		{"", false},
		{"1", true},
		{"true", true},
		{"TRUE", true},
		{"0", false},
		{"false", false},
		{"off", true},
		{"yes", true},
	}

	for _, tc := range cases {
		t.Run(tc.value, func(t *testing.T) {
			t.Setenv(noIconsEnv, tc.value)
			if got := iconsDisabled(); got != tc.want {
				t.Errorf("iconsDisabled() with %s=%q = %v, want %v", noIconsEnv, tc.value, got, tc.want)
			}
		})
	}
}

// Load has to carry the environment setting into the config the app reads at
// startup.
func TestLoadCarriesNoIcons(t *testing.T) {
	t.Setenv("HOME", t.TempDir())
	t.Setenv(noIconsEnv, "1")

	cfg, err := Load()
	if err != nil {
		t.Fatal(err)
	}
	if !cfg.NoIcons {
		t.Error("Load should set NoIcons when OMAKAIJU_NO_ICONS is on")
	}
}

// OMAKAIJU_MOUSE is opt-in: absent or false leaves the terminal's own text
// selection alone. A true value follows the bool, and an unparseable value is
// taken as a request for the mouse rather than a typo to be ignored. That
// includes words like "off": only an actual boolean (or an empty value) can
// turn it back off, matching OMAKAIJU_NO_ICONS.
func TestMouseEnabledFromEnv(t *testing.T) {
	cases := []struct {
		value string
		want  bool
	}{
		{"", false},
		{"1", true},
		{"true", true},
		{"TRUE", true},
		{"0", false},
		{"false", false},
		{"off", true},
		{"yes", true},
		{"on", true},
	}

	for _, tc := range cases {
		t.Run(tc.value, func(t *testing.T) {
			t.Setenv(mouseEnv, tc.value)
			if got := mouseEnabled(); got != tc.want {
				t.Errorf("mouseEnabled() with %s=%q = %v, want %v", mouseEnv, tc.value, got, tc.want)
			}
		})
	}
}

// Load must default the mouse off, since the environment is where the choice
// comes from and no variable means no mouse.
func TestLoadDefaultsMouseOff(t *testing.T) {
	t.Setenv("HOME", t.TempDir())

	cfg, err := Load()
	if err != nil {
		t.Fatal(err)
	}
	if cfg.Mouse {
		t.Error("Load must leave the mouse off when OMAKAIJU_MOUSE is unset")
	}
}

// The setting has to reach the config the app is built from.
func TestLoadCarriesMouse(t *testing.T) {
	t.Setenv("HOME", t.TempDir())
	t.Setenv(mouseEnv, "1")

	cfg, err := Load()
	if err != nil {
		t.Fatal(err)
	}
	if !cfg.Mouse {
		t.Error("Load should set Mouse when OMAKAIJU_MOUSE is on")
	}
}
