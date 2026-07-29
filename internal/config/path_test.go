package config

import (
	"path/filepath"
	"testing"
)

func TestPathUsesXDGConfigHome(t *testing.T) {
	t.Setenv("XDG_CONFIG_HOME", "/xdg-home")
	got, err := Path()
	if err != nil {
		t.Fatalf("Path: %v", err)
	}
	want := filepath.Join("/xdg-home", "kiwi", "config")
	if got != want {
		t.Errorf("got %q, want %q", got, want)
	}
}

func TestPathFallsBackToDotConfig(t *testing.T) {
	t.Setenv("XDG_CONFIG_HOME", "")
	t.Setenv("HOME", "/home/testuser")
	got, err := Path()
	if err != nil {
		t.Fatalf("Path: %v", err)
	}
	want := filepath.Join("/home/testuser", ".config", "kiwi", "config")
	if got != want {
		t.Errorf("got %q, want %q", got, want)
	}
}
