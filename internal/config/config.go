// Package config parses nib's user config file: a flat, hand-editable
// text format (in the spirit of Ghostty's config, not TOML/YAML), letting
// the user override nib's defaults without recompiling.
//
// Four directives share one three-field shape:
//
//	keybind = <scope>:<trigger>   = <action>
//	lsp     = <language>         = <command args...>
//	color   = <role>             = <color name>
//	tabmode = <language|default> = <spaces|tabs>[:<width>]
//
// plus two two-field directives:
//
//	theme      = <name>
//	whitespace = true
//
// The "lsp" directive registers a language server, e.g.
//
//	lsp = php = intelephense --stdio
//
// which is how a language nib doesn't ship a default for gets one. See
// internal/lsp.DefaultServers for the built-ins these merge over.
//
// The "theme"/"color" directives pick a built-in color theme and override
// individual semantic colors on top of it — see internal/theme for the
// built-in themes, the role vocabulary, and the color-name vocabulary.
//
// The "tabmode" directive picks whether Tab inserts spaces or a literal
// tab character, and the indent width, for files of a given language
// (matched by the same language name internal/ui/editor uses for syntax
// highlighting and the "lsp" directive), e.g.
//
//	tabmode = yaml    = spaces:2
//	tabmode = default = tabs:4
//
// "default" is the fallback applied to any language without its own
// "tabmode" entry. The width suffix is optional; omitting it keeps
// whatever width nib would otherwise use.
//
// The "whitespace" directive turns on rendering of spaces and tab-fill as
// visible glyphs in the editor, e.g. "whitespace = true". Any other value
// (or omitting the directive) leaves it off.
//
// scope is one of "global" (or omitted, e.g. "keybind = ctrl+p = ..."),
// "editor", "filetree", "finder", "debug", "help" — see each package's
// DefaultKeybinds for the actions available in that scope. trigger is a
// key description like "ctrl+p", "shift+left", or a bare character like
// "x"; see Normalize for exactly what's accepted. Blank lines and lines
// starting with "#" are ignored; malformed lines are skipped rather than
// erroring, so a typo in the config can't stop nib from starting.
package config

import (
	"bufio"
	"io"
	"strconv"
	"strings"

	"github.com/bricejulia/nib/internal/layout"
	"github.com/bricejulia/nib/internal/theme"
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

// TabMode is a language's indent style: whether Tab inserts spaces or a
// literal tab character, and how wide an indent level is.
type TabMode struct {
	UseSpaces bool
	Width     int
}

// Config holds the parsed user config: per-scope keybinding overrides,
// language-server commands, theme selection/color overrides, and
// indent/whitespace-display settings. The zero value (and a nil *Config)
// is a valid empty config — every scope falls back to its built-in
// Defaults, no extra language servers are registered, theming falls back
// to internal/theme's default theme, and whitespace display is off.
type Config struct {
	keybinds   map[string]map[string]string // scope -> trigger -> action
	servers    map[string][]string          // language -> argv
	themeName  string                       // "" means unset, falls back to theme.DefaultName
	colors     map[string]layout.Color      // role name (as typed, lowercased) -> validated color
	tabModes   map[string]TabMode           // language (or "default") -> indent style
	whitespace bool                         // "whitespace = true" was set
}

// Servers returns the language->command entries parsed from "lsp" lines,
// for the caller to merge over internal/lsp's built-in registry. Safe to
// call on a nil *Config.
func (c *Config) Servers() map[string][]string {
	if c == nil {
		return nil
	}
	return c.servers
}

// Overrides returns the parsed trigger->action overrides for scope, or
// nil if the config has none for it. Safe to call on a nil *Config.
func (c *Config) Overrides(scope string) map[string]string {
	if c == nil {
		return nil
	}
	return c.keybinds[scope]
}

// ThemeName returns the user-configured "theme = <name>" value, or "" if
// unset. Safe to call on a nil *Config.
func (c *Config) ThemeName() string {
	if c == nil {
		return ""
	}
	return c.themeName
}

// ColorOverrides returns the per-role color overrides parsed from "color"
// lines, keyed by role name exactly as typed (lowercased). Values are
// already validated color names; whether the ROLE name is one
// internal/theme actually knows about is for the caller to check (see
// theme.ValidRole), the same division Overrides/Servers already draw for
// scope and language names. Safe to call on a nil *Config.
func (c *Config) ColorOverrides() map[string]layout.Color {
	if c == nil {
		return nil
	}
	return c.colors
}

// TabModes returns the language->indent-style entries parsed from
// "tabmode" lines, keyed by language name (or "default" for the
// fallback), for the caller to merge over its own hardcoded default.
// Safe to call on a nil *Config.
func (c *Config) TabModes() map[string]TabMode {
	if c == nil {
		return nil
	}
	return c.tabModes
}

// ShowWhitespace returns whether "whitespace = true" was set. Safe to
// call on a nil *Config.
func (c *Config) ShowWhitespace() bool {
	if c == nil {
		return false
	}
	return c.whitespace
}

// Parse reads nib's config format from r. It never returns an error:
// unparseable lines are silently skipped, since a broken config line
// shouldn't prevent nib from starting (worst case, that one override is
// just ignored).
func Parse(r io.Reader) *Config {
	cfg := &Config{
		keybinds: map[string]map[string]string{},
		servers:  map[string][]string{},
		colors:   map[string]layout.Color{},
		tabModes: map[string]TabMode{},
	}

	sc := bufio.NewScanner(r)
	for sc.Scan() {
		line := strings.TrimSpace(sc.Text())
		if line == "" || strings.HasPrefix(line, "#") {
			continue
		}

		fields := strings.SplitN(line, "=", 3)

		// "theme = <name>" and "whitespace = true" are the two directives
		// with a single value rather than this format's usual
		// <directive> = <key> = <value> triplet, so they're handled before
		// the three-field shape below.
		if len(fields) == 2 {
			switch strings.TrimSpace(fields[0]) {
			case "theme":
				if name := strings.TrimSpace(fields[1]); name != "" {
					cfg.themeName = name
				}
			case "whitespace":
				cfg.whitespace = strings.TrimSpace(fields[1]) == "true"
			}
			continue
		}

		// keybind/lsp/color/tabmode share the same three-field shape:
		//   keybind = <scope>:<trigger>   = <action>
		//   lsp     = <language>         = <command args...>
		//   color   = <role>             = <color name>
		//   tabmode = <language|default> = <spaces|tabs>[:<width>]
		// so SplitN(3) either yields exactly the three fields wanted or the
		// line is malformed.
		if len(fields) != 3 {
			continue
		}
		directive := strings.TrimSpace(fields[0])
		key := strings.TrimSpace(fields[1])
		value := strings.TrimSpace(fields[2])
		if key == "" || value == "" {
			continue
		}

		switch directive {
		case "keybind":
			scope, trigger := splitScope(key)
			if trigger = Normalize(trigger); trigger == "" {
				continue
			}
			if cfg.keybinds[scope] == nil {
				cfg.keybinds[scope] = map[string]string{}
			}
			cfg.keybinds[scope][trigger] = value

		case "lsp":
			// Split the command on whitespace into argv. Quoting isn't
			// supported — a path with spaces needs a wrapper script — which
			// keeps this consistent with the rest of the format's
			// deliberately unclever parsing.
			argv := strings.Fields(value)
			if len(argv) == 0 {
				continue
			}
			cfg.servers[key] = argv

		case "color":
			color, ok := theme.ParseColorName(value)
			if !ok {
				continue
			}
			cfg.colors[strings.ToLower(key)] = color

		case "tabmode":
			mode, ok := parseTabMode(value)
			if !ok {
				continue
			}
			cfg.tabModes[key] = mode
		}
	}

	return cfg
}

// parseTabMode parses a "tabmode" directive's value, "<spaces|tabs>" or
// "<spaces|tabs>:<width>". A missing or non-positive width leaves Width
// at 0, meaning "unspecified" to the caller.
func parseTabMode(value string) (TabMode, bool) {
	style, widthStr, _ := strings.Cut(value, ":")
	var mode TabMode
	switch style {
	case "spaces":
		mode.UseSpaces = true
	case "tabs":
		mode.UseSpaces = false
	default:
		return TabMode{}, false
	}
	if widthStr != "" {
		width, err := strconv.Atoi(widthStr)
		if err != nil || width <= 0 {
			return TabMode{}, false
		}
		mode.Width = width
	}
	return mode, true
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
