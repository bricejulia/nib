package config

import (
	"os"
	"path/filepath"
)

// Path returns nib's config file location: $XDG_CONFIG_HOME/nib/config,
// or ~/.config/nib/config if XDG_CONFIG_HOME is unset — the same
// ~/.config convention terminal tools (git, nvim, ghostty on Linux) use
// regardless of OS, rather than os.UserConfigDir's platform-specific
// (and, on macOS, GUI-app-oriented) locations.
func Path() (string, error) {
	base := os.Getenv("XDG_CONFIG_HOME")
	if base == "" {
		home, err := os.UserHomeDir()
		if err != nil {
			return "", err
		}
		base = filepath.Join(home, ".config")
	}
	return filepath.Join(base, "nib", "config"), nil
}
