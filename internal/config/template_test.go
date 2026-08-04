package config

import (
	"os"
	"path/filepath"
	"strings"
	"testing"

	"github.com/bricejulia/nib/internal/theme"
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

func TestTemplateDocumentsThemeAndColorDirectives(t *testing.T) {
	out := Template(testScopes)
	if !strings.Contains(out, "theme = <name>") {
		t.Errorf("expected theme directive syntax, got:\n%s", out)
	}
	if !strings.Contains(out, "color = <role> = <color>") {
		t.Errorf("expected color directive syntax, got:\n%s", out)
	}
}

// TestTemplateListsEveryBuiltinThemeAndRole guards against the hand-written
// theme doc block in Template drifting from internal/theme's actual role
// and built-in-theme lists — a future role/theme rename that forgets to
// update the doc block fails this test.
func TestTemplateListsEveryBuiltinThemeAndRole(t *testing.T) {
	out := Template(testScopes)
	for _, name := range theme.BuiltinNames {
		if !strings.Contains(out, name) {
			t.Errorf("expected built-in theme name %q in template, got:\n%s", name, out)
		}
	}
	for _, role := range theme.AllRoles {
		if !strings.Contains(out, string(role)) {
			t.Errorf("expected role %q in template, got:\n%s", role, out)
		}
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
