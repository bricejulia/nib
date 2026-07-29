package config

import (
	"os"
	"path/filepath"
	"testing"
)

func TestLoadMissingFileReturnsEmptyConfig(t *testing.T) {
	cfg, err := Load(filepath.Join(t.TempDir(), "does-not-exist"))
	if err != nil {
		t.Fatalf("Load: %v", err)
	}
	if len(cfg.Overrides("global")) != 0 {
		t.Errorf("expected an empty config, got overrides %v", cfg.Overrides("global"))
	}
}

func TestLoadParsesExistingFile(t *testing.T) {
	path := filepath.Join(t.TempDir(), "config")
	if err := os.WriteFile(path, []byte("keybind = ctrl+p = open_finder\n"), 0o644); err != nil {
		t.Fatal(err)
	}
	cfg, err := Load(path)
	if err != nil {
		t.Fatalf("Load: %v", err)
	}
	if got := cfg.Overrides("global")["Ctrl+p"]; got != "open_finder" {
		t.Errorf(`global["Ctrl+p"] = %q, want "open_finder"`, got)
	}
}
