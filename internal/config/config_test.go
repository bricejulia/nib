package config

import (
	"strings"
	"testing"

	"github.com/bricejulia/nib/internal/layout"
)

func TestNormalizeCanonicalizesModifiersAndNamedKeys(t *testing.T) {
	cases := map[string]string{
		"ctrl+p":        "Ctrl+p",
		"CTRL+P":        "Ctrl+P",
		"control+left":  "Ctrl+Left",
		"shift+left":    "Shift+Left",
		"ctrl+shift+p":  "Ctrl+Shift+p",
		"cmd+q":         "Super+q",
		"esc":           "Esc",
		"ESCAPE":        "Esc",
		"pgdown":        "PageDown",
		"x":             "x",
		"X":             "X",
		"]":             "]",
		"?":             "?",
		"ctrl+space":    "Ctrl+Space",
		"ctrl+spacebar": "Ctrl+Space",
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

func TestParseLSPDirective(t *testing.T) {
	src := `
lsp = php = intelephense --stdio
lsp = rust = rust-analyzer
keybind = editor:ctrl+t = go_to_definition
`
	cfg := Parse(strings.NewReader(src))
	servers := cfg.Servers()

	php, ok := servers["php"]
	if !ok {
		t.Fatalf("no php entry in %v", servers)
	}
	if len(php) != 2 || php[0] != "intelephense" || php[1] != "--stdio" {
		t.Errorf("php = %q, want [intelephense --stdio] (args split into argv)", php)
	}
	if rust := servers["rust"]; len(rust) != 1 || rust[0] != "rust-analyzer" {
		t.Errorf("rust = %q, want [rust-analyzer]", rust)
	}
	// The two directives share a parser; neither may swallow the other.
	if got := cfg.Overrides("editor")["Ctrl+t"]; got != "go_to_definition" {
		t.Errorf("keybind parsing broke alongside lsp: got %q", got)
	}
}

func TestParseLSPSkipsMalformedLines(t *testing.T) {
	src := `
lsp = php
lsp = = intelephense
lsp = python =
`
	if servers := Parse(strings.NewReader(src)).Servers(); len(servers) != 0 {
		t.Errorf("expected no servers from malformed input, got %v", servers)
	}
}

func TestServersOnNilConfig(t *testing.T) {
	var cfg *Config
	if got := cfg.Servers(); got != nil {
		t.Errorf("expected nil servers from a nil *Config, got %v", got)
	}
}

func TestParseThemeDirective(t *testing.T) {
	src := `
theme = ocean
keybind = editor:ctrl+t = go_to_definition
`
	cfg := Parse(strings.NewReader(src))
	if got := cfg.ThemeName(); got != "ocean" {
		t.Errorf(`ThemeName() = %q, want "ocean"`, got)
	}
	// The two-field directive must not swallow the surrounding three-field one.
	if got := cfg.Overrides("editor")["Ctrl+t"]; got != "go_to_definition" {
		t.Errorf("keybind parsing broke alongside theme: got %q", got)
	}
}

func TestParseThemeDirectiveSkipsBlankValue(t *testing.T) {
	if got := Parse(strings.NewReader("theme =\n")).ThemeName(); got != "" {
		t.Errorf(`ThemeName() = %q, want ""`, got)
	}
}

func TestParseColorDirective(t *testing.T) {
	src := `
color = git_added = brightgreen
color = GIT_DELETED = Red
`
	cfg := Parse(strings.NewReader(src)).ColorOverrides()
	if got := cfg["git_added"]; got != layout.ColorBrightGreen {
		t.Errorf(`colors["git_added"] = %v, want ColorBrightGreen`, got)
	}
	if got := cfg["git_deleted"]; got != layout.ColorRed {
		t.Errorf(`colors["git_deleted"] = %v, want ColorRed (role key lowercased)`, got)
	}
}

func TestParseColorDirectiveSkipsUnknownColorName(t *testing.T) {
	if got := Parse(strings.NewReader("color = git_added = fluorescent\n")).ColorOverrides(); len(got) != 0 {
		t.Errorf("expected no color overrides from an unknown color name, got %v", got)
	}
}

func TestParseThemeAndColorCoexistWithKeybindAndLSP(t *testing.T) {
	src := `
theme = mono
color = git_added = brightgreen
keybind = editor:ctrl+t = go_to_definition
lsp = rust = rust-analyzer
`
	cfg := Parse(strings.NewReader(src))
	if got := cfg.ThemeName(); got != "mono" {
		t.Errorf(`ThemeName() = %q, want "mono"`, got)
	}
	if got := cfg.ColorOverrides()["git_added"]; got != layout.ColorBrightGreen {
		t.Errorf(`colors["git_added"] = %v, want ColorBrightGreen`, got)
	}
	if got := cfg.Overrides("editor")["Ctrl+t"]; got != "go_to_definition" {
		t.Errorf("keybind parsing broke: got %q", got)
	}
	if rust := cfg.Servers()["rust"]; len(rust) != 1 || rust[0] != "rust-analyzer" {
		t.Errorf("lsp parsing broke: got %q", rust)
	}
}

func TestThemeNameOnNilConfig(t *testing.T) {
	var cfg *Config
	if got := cfg.ThemeName(); got != "" {
		t.Errorf(`expected "" theme name from a nil *Config, got %q`, got)
	}
}

func TestColorOverridesOnNilConfig(t *testing.T) {
	var cfg *Config
	if got := cfg.ColorOverrides(); got != nil {
		t.Errorf("expected nil color overrides from a nil *Config, got %v", got)
	}
}

func TestParseTabModeDirective(t *testing.T) {
	src := `
tabmode = yaml = spaces:2
tabmode = default = tabs:4
tabmode = python = spaces
keybind = editor:ctrl+t = go_to_definition
`
	cfg := Parse(strings.NewReader(src))
	modes := cfg.TabModes()

	if got, want := modes["yaml"], (TabMode{UseSpaces: true, Width: 2}); got != want {
		t.Errorf("modes[yaml] = %+v, want %+v", got, want)
	}
	if got, want := modes["default"], (TabMode{UseSpaces: false, Width: 4}); got != want {
		t.Errorf("modes[default] = %+v, want %+v", got, want)
	}
	if got, want := modes["python"], (TabMode{UseSpaces: true, Width: 0}); got != want {
		t.Errorf("modes[python] = %+v, want %+v (no width suffix)", got, want)
	}
	// The three-field directive must not swallow the surrounding keybind.
	if got := cfg.Overrides("editor")["Ctrl+t"]; got != "go_to_definition" {
		t.Errorf("keybind parsing broke alongside tabmode: got %q", got)
	}
}

func TestParseTabModeSkipsMalformedLines(t *testing.T) {
	src := `
tabmode = yaml
tabmode = yaml = sideways
tabmode = yaml = spaces:zero
tabmode = yaml = spaces:0
tabmode = yaml = spaces:-2
`
	if modes := Parse(strings.NewReader(src)).TabModes(); len(modes) != 0 {
		t.Errorf("expected no tab modes from malformed input, got %v", modes)
	}
}

func TestTabModesOnNilConfig(t *testing.T) {
	var cfg *Config
	if got := cfg.TabModes(); got != nil {
		t.Errorf("expected nil tab modes from a nil *Config, got %v", got)
	}
}

func TestParseWhitespaceDirective(t *testing.T) {
	if got := Parse(strings.NewReader("whitespace = true\n")).ShowWhitespace(); !got {
		t.Errorf("ShowWhitespace() = %v, want true", got)
	}
	if got := Parse(strings.NewReader("whitespace = yes\n")).ShowWhitespace(); got {
		t.Errorf(`ShowWhitespace() = %v, want false for "yes"`, got)
	}
	if got := Parse(strings.NewReader("")).ShowWhitespace(); got {
		t.Errorf("ShowWhitespace() = %v, want false when unset", got)
	}
}

func TestShowWhitespaceOnNilConfig(t *testing.T) {
	var cfg *Config
	if got := cfg.ShowWhitespace(); got {
		t.Errorf("expected false from a nil *Config, got %v", got)
	}
}
