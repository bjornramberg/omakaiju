package app

import (
	"path/filepath"
	"testing"

	"github.com/bjornramberg/omakaiju/internal/config"
	"github.com/bjornramberg/omakaiju/internal/icons"
)

// The icons package is a leaf, so NewModel has to pass the startup setting on
// to it. Without this the environment variable would be read but never reach
// the rows.
func TestNewModelAppliesNoIcons(t *testing.T) {
	t.Cleanup(func() { icons.SetEnabled(true) })
	icons.SetEnabled(true)

	NewModel(config.Config{
		ThemePath: filepath.Join(t.TempDir(), "none.toml"),
		NoIcons:   true,
	})

	if icons.Enabled() {
		t.Error("NewModel should turn the icons off when the config says NoIcons")
	}
}

// The default config keeps the icons on, even after something else turned them
// off earlier.
func TestNewModelKeepsIconsByDefault(t *testing.T) {
	t.Cleanup(func() { icons.SetEnabled(true) })
	icons.SetEnabled(false)

	NewModel(config.Config{ThemePath: filepath.Join(t.TempDir(), "none.toml")})

	if !icons.Enabled() {
		t.Error("NewModel should keep the icons on by default")
	}
}
