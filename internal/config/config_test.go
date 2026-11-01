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
