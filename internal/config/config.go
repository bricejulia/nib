// Package config parses kiwi's user config file: a flat, hand-editable
// text format (in the spirit of Ghostty's config, not TOML/YAML) whose
// only current directive is "keybind", letting the user override any of
// kiwi's default keybindings without recompiling.
//
// A line looks like:
//
//	keybind = <scope>:<trigger> = <action>
//
// scope is one of "global" (or omitted, e.g. "keybind = ctrl+p = ..."),
// "editor", "filetree", "finder", "debug", "help" — see each package's
// DefaultKeybinds for the actions available in that scope. trigger is a
// key description like "ctrl+p", "shift+left", or a bare character like
// "x"; see Normalize for exactly what's accepted. Blank lines and lines
// starting with "#" are ignored; malformed lines are skipped rather than
// erroring, so a typo in the config can't stop kiwi from starting.
package config

import (
	"bufio"
	"io"
	"strings"
)

// Binding is one trigger→action keybinding, in the canonical trigger form
// Normalize produces.
type Binding struct {
	Trigger string
	Action  string
}

// Defaults is a scope's built-in keybindings, kept in a fixed display
// order (unlike a plain map, whose iteration order Go deliberately
// randomizes) so the generated template config lists them consistently.
type Defaults []Binding

// Resolve merges these defaults with a scope's user-config overrides
// (overrides win on a matching trigger) into a trigger->action lookup
// map, ready for a View's HandleKey to index into via Key.String().
func (d Defaults) Resolve(overrides map[string]string) map[string]string {
	m := make(map[string]string, len(d)+len(overrides))
	for _, b := range d {
		m[b.Trigger] = b.Action
	}
	for trigger, action := range overrides {
		m[trigger] = action
	}
	return m
}

// Config holds parsed per-scope keybinding overrides from the user's
// config file. The zero value (and a nil *Config) is a valid empty
// config — every scope simply falls back to its built-in Defaults.
type Config struct {
	keybinds map[string]map[string]string // scope -> trigger -> action
}

// Overrides returns the parsed trigger->action overrides for scope, or
// nil if the config has none for it. Safe to call on a nil *Config.
func (c *Config) Overrides(scope string) map[string]string {
	if c == nil {
		return nil
	}
	return c.keybinds[scope]
}

// Parse reads kiwi's config format from r. It never returns an error:
// unparseable lines are silently skipped, since a broken config line
// shouldn't prevent kiwi from starting (worst case, that one override is
// just ignored).
func Parse(r io.Reader) *Config {
	cfg := &Config{keybinds: map[string]map[string]string{}}

	sc := bufio.NewScanner(r)
	for sc.Scan() {
		line := strings.TrimSpace(sc.Text())
		if line == "" || strings.HasPrefix(line, "#") {
			continue
		}

		// "keybind = <scope>:<trigger> = <action>" — a keybind line has
		// exactly two "=", so SplitN(3) either yields exactly the three
		// fields we want or the line is malformed.
		fields := strings.SplitN(line, "=", 3)
		if len(fields) != 3 || strings.TrimSpace(fields[0]) != "keybind" {
			continue
		}

		scope, trigger := splitScope(strings.TrimSpace(fields[1]))
		trigger = Normalize(trigger)
		action := strings.TrimSpace(fields[2])
		if trigger == "" || action == "" {
			continue
		}

		if cfg.keybinds[scope] == nil {
			cfg.keybinds[scope] = map[string]string{}
		}
		cfg.keybinds[scope][trigger] = action
	}

	return cfg
}

// splitScope splits "editor:ctrl+p" into ("editor", "ctrl+p"); a trigger
// with no ":" belongs to the "global" scope.
func splitScope(s string) (scope, trigger string) {
	if i := strings.IndexByte(s, ':'); i >= 0 {
		return s[:i], s[i+1:]
	}
	return "global", s
}

// namedKeys maps a case-insensitive spelling of a special key to its
// canonical name, matching the constants in internal/layout (KeyUp,
// KeyPageDown, ...) and thus what Key.String() produces.
var namedKeys = map[string]string{
	"up": "Up", "down": "Down", "left": "Left", "right": "Right",
	"enter": "Enter", "tab": "Tab",
	"esc": "Esc", "escape": "Esc",
	"pageup": "PageUp", "pgup": "PageUp",
	"pagedown": "PageDown", "pgdown": "PageDown", "pgdn": "PageDown",
	"home": "Home", "end": "End",
	"backspace": "Backspace",
	"space":     "Space", "spacebar": "Space",
	"shift": "Shift",
}

// modOrder is the order Key.String() emits held modifiers in.
var modOrder = []string{"Ctrl", "Alt", "Super", "Shift"}

// Normalize rewrites a user-typed trigger like "ctrl+P" or "CTRL+left"
// into the canonical form layout.Key.String() produces at runtime
// ("Ctrl+P", "Ctrl+Left"), so a config override matches regardless of how
// the user capitalized modifiers or named keys. The final, non-modifier
// token's case is preserved verbatim except when it names a special key
// (left, esc, pageup, ...), which is matched case-insensitively and
// rewritten to its canonical spelling.
//
// This splits the trigger on "+", so it cannot itself describe a binding
// on the literal "+" key — an accepted limitation given how rare that
// binding would be.
func Normalize(trigger string) string {
	parts := strings.Split(trigger, "+")
	key := parts[len(parts)-1]

	present := map[string]bool{}
	for _, p := range parts[:len(parts)-1] {
		switch strings.ToLower(strings.TrimSpace(p)) {
		case "ctrl", "control":
			present["Ctrl"] = true
		case "alt", "opt", "option":
			present["Alt"] = true
		case "super", "cmd", "command", "win", "windows":
			present["Super"] = true
		case "shift":
			present["Shift"] = true
		}
	}

	out := make([]string, 0, len(modOrder)+1)
	for _, m := range modOrder {
		if present[m] {
			out = append(out, m)
		}
	}

	if canon, ok := namedKeys[strings.ToLower(strings.TrimSpace(key))]; ok {
		key = canon
	}
	out = append(out, key)

	return strings.Join(out, "+")
}
