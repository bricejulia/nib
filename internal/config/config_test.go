package config

import (
	"strings"
	"testing"
)

func TestNormalizeCanonicalizesModifiersAndNamedKeys(t *testing.T) {
	cases := map[string]string{
		"ctrl+p":         "Ctrl+p",
		"CTRL+P":         "Ctrl+P",
		"control+left":   "Ctrl+Left",
		"shift+left":     "Shift+Left",
		"ctrl+shift+p":   "Ctrl+Shift+p",
		"cmd+q":          "Super+q",
		"esc":            "Esc",
		"ESCAPE":         "Esc",
		"pgdown":         "PageDown",
		"x":              "x",
		"X":              "X",
		"]":              "]",
		"?":              "?",
	}
	for in, want := range cases {
		if got := Normalize(in); got != want {
			t.Errorf("Normalize(%q) = %q, want %q", in, got, want)
		}
	}
}

func TestDefaultsResolveMergesAndOverrides(t *testing.T) {
	defaults := Defaults{
		{Trigger: "j", Action: "move_down"},
		{Trigger: "Down", Action: "move_down"},
		{Trigger: "k", Action: "move_up"},
	}
	merged := defaults.Resolve(map[string]string{
		"j":      "custom_action", // overrides a default trigger
		"Ctrl+n": "move_down",     // adds a new trigger
	})

	if merged["j"] != "custom_action" {
		t.Errorf(`merged["j"] = %q, want "custom_action"`, merged["j"])
	}
	if merged["Down"] != "move_down" {
		t.Errorf(`merged["Down"] = %q, want "move_down" (untouched default)`, merged["Down"])
	}
	if merged["Ctrl+n"] != "move_down" {
		t.Errorf(`merged["Ctrl+n"] = %q, want "move_down" (new override)`, merged["Ctrl+n"])
	}
	if merged["k"] != "move_up" {
		t.Errorf(`merged["k"] = %q, want "move_up"`, merged["k"])
	}
}

func TestParseKeybindLines(t *testing.T) {
	src := `
# a comment
	  # an indented comment

keybind = ctrl+p = open_finder
keybind = editor:x = close_tab
keybind =editor:CTRL+shift+k=custom_kill
`
	cfg := Parse(strings.NewReader(src))

	if got := cfg.Overrides("global")["Ctrl+p"]; got != "open_finder" {
		t.Errorf(`global["Ctrl+p"] = %q, want "open_finder"`, got)
	}
	if got := cfg.Overrides("editor")["x"]; got != "close_tab" {
		t.Errorf(`editor["x"] = %q, want "close_tab"`, got)
	}
	if got := cfg.Overrides("editor")["Ctrl+Shift+k"]; got != "custom_kill" {
		t.Errorf(`editor["Ctrl+Shift+k"] = %q, want "custom_kill"`, got)
	}
}

func TestParseSkipsMalformedLines(t *testing.T) {
	src := `
not a keybind line at all
keybind = onlyonefield
keybind = missing action =
keybind = = missing trigger
somekey = value
`
	cfg := Parse(strings.NewReader(src))
	if len(cfg.Overrides("global")) != 0 {
		t.Errorf("expected no global overrides from malformed input, got %v", cfg.Overrides("global"))
	}
}

func TestOverridesOnNilConfig(t *testing.T) {
	var cfg *Config
	if got := cfg.Overrides("global"); got != nil {
		t.Errorf("expected nil overrides from a nil *Config, got %v", got)
	}
}
