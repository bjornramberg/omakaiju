package config

import (
	"os"
	"path/filepath"
)

type Config struct {
	ThemePath string
}

func Load() (Config, error) {
	home, err := os.UserHomeDir()
	if err != nil {
		return Config{}, err
	}

	themePath := filepath.Join(home, ".config", "omarchy", "fm.toml")

	return Config{
		ThemePath: themePath,
	}, nil
}
