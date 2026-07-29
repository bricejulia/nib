package config

import (
	"os"
	"path/filepath"
	"strings"
	"testing"
)

var testScopes = []Scope{
	{Name: "global", Defaults: Defaults{{Trigger: "Ctrl+p", Action: "open_finder"}}},
	{Name: "editor", Defaults: Defaults{{Trigger: "x", Action: "close_tab"}}},
}

func TestTemplateCommentsOutEveryDefaultByScope(t *testing.T) {
	out := Template(testScopes)

	if !strings.Contains(out, "# --- global ---") || !strings.Contains(out, "# --- editor ---") {
		t.Fatalf("expected both scope headers, got:\n%s", out)
	}
	if !strings.Contains(out, "# keybind = Ctrl+p = open_finder") {
		t.Errorf("expected global's trigger with no scope prefix, got:\n%s", out)
	}
	if !strings.Contains(out, "# keybind = editor:x = close_tab") {
		t.Errorf("expected editor's trigger prefixed with its scope, got:\n%s", out)
	}
}

func TestEnsureFileCreatesOnlyWhenMissing(t *testing.T) {
	dir := t.TempDir()
	path := filepath.Join(dir, "nested", "config")

	if err := EnsureFile(path, testScopes); err != nil {
		t.Fatalf("EnsureFile: %v", err)
	}
	first, err := os.ReadFile(path)
	if err != nil {
		t.Fatalf("expected file to be created: %v", err)
	}
	if !strings.Contains(string(first), "open_finder") {
		t.Errorf("expected template content, got:\n%s", first)
	}

	if err := os.WriteFile(path, []byte("custom content, untouched"), 0o644); err != nil {
		t.Fatal(err)
	}
	if err := EnsureFile(path, testScopes); err != nil {
		t.Fatalf("EnsureFile (existing file): %v", err)
	}
	second, err := os.ReadFile(path)
	if err != nil {
		t.Fatal(err)
	}
	if string(second) != "custom content, untouched" {
		t.Errorf("EnsureFile must not overwrite an existing file, got:\n%s", second)
	}
}
