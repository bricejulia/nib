package config

import (
	"os"
)

// Load reads and parses the config file at path. A missing file is not an
// error — it just means no overrides, so every scope uses its built-in
// Defaults.
func Load(path string) (*Config, error) {
	f, err := os.Open(path)
	if err != nil {
		if os.IsNotExist(err) {
			return &Config{keybinds: map[string]map[string]string{}}, nil
		}
		return nil, err
	}
	defer f.Close()
	return Parse(f), nil
}
