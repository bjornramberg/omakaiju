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

// The mouse is on unless something says otherwise. OMAKAIJU_NO_MOUSE and the
// older OMAKAIJU_MOUSE both turn it off; a value that parses as a boolean
// follows the bool, and an unparseable value is taken as a request to turn
// the mouse off rather than a typo to be ignored.
func TestMouseDisabledFromEnv(t *testing.T) {
	cases := []struct {
		env   string
		value string
		want  bool
	}{
		{mouseEnv, "", false},
		{mouseEnv, "1", true},
		{mouseEnv, "true", true},
		{mouseEnv, "0", false},
		{mouseEnv, "false", false},
		{mouseEnv, "yes", true},
		{mouseEnv, "off", true},
		{mouseDisabledEnv, "", false},
		{mouseDisabledEnv, "1", true},
		{mouseDisabledEnv, "true", true},
		{mouseDisabledEnv, "0", false},
		{mouseDisabledEnv, "yes", true},
	}

	for _, tc := range cases {
		t.Run(tc.env+"="+tc.value, func(t *testing.T) {
			t.Setenv(mouseEnv, "")
			t.Setenv(mouseDisabledEnv, "")
			t.Setenv(tc.env, tc.value)
			if got := mouseDisabled(); got != tc.want {
				t.Errorf("mouseDisabled() with %s=%q = %v, want %v", tc.env, tc.value, got, tc.want)
			}
		})
	}
}

// Load must default the mouse on: a file manager whose mouse does nothing is
// broken for anyone who reaches for it.
func TestLoadDefaultsMouseOn(t *testing.T) {
	t.Setenv("HOME", t.TempDir())
	t.Setenv(mouseEnv, "")
	t.Setenv(mouseDisabledEnv, "")

	cfg, err := Load()
	if err != nil {
		t.Fatal(err)
	}
	if !cfg.Mouse {
		t.Error("Load must leave the mouse on when nothing asks for it off")
	}
}

// The setting has to reach the config the app is built from.
func TestLoadCarriesMouse(t *testing.T) {
	t.Setenv("HOME", t.TempDir())
	t.Setenv(mouseEnv, "")
	t.Setenv(mouseDisabledEnv, "1")

	cfg, err := Load()
	if err != nil {
		t.Fatal(err)
	}
	if cfg.Mouse {
		t.Error("Load should turn the mouse off when OMAKAIJU_NO_MOUSE says so")
	}
}
