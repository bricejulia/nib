package config

import (
	"fmt"
	"slices"
	"sort"
)

// Action is one named command a scope's keymap can bind: what the View
// behind that scope does when one of its keys is pressed. Every reader of
// "what can be bound, to what, and what it does" — the View's own keymap,
// the help overlay, the action popup, the generated template config, and
// the check for unknown action names in the user's config — works from
// these declarations, so they can't drift apart.
//
// Running an action stays with its View (a `switch` on the ID): the
// editor's operators, counts and modes don't fit a flat handler table.
type Action struct {
	ID string
	// Keys are the default triggers, in Normalize's canonical form, in
	// the order help and the action popup show them. An action may have
	// none (bindable only from the config file).
	Keys []string
	// Desc is one line, shown in help and the action popup.
	Desc string
	// Section is the help section the action is listed under; "" means
	// the scope's own Title.
	Section string
	// InPopup lists the action in the action popup (Ctrl+j), which runs it
	// without a key press — so only actions that make sense on their own:
	// not movements, operators, or keys that only mean something mid-mode.
	InPopup bool
}

// Actions is a scope's actions, in display order.
type Actions []Action

// Resolve merges the default keys with a scope's user-config overrides
// (overrides win on a matching trigger) into a trigger->action lookup
// map, ready for a View's HandleKey to index into via Key.String().
func (a Actions) Resolve(overrides map[string]string) map[string]string {
	m := make(map[string]string, len(a)+len(overrides))
	for _, act := range a {
		for _, key := range act.Keys {
			m[key] = act.ID
		}
	}
	for trigger, action := range overrides {
		m[trigger] = action
	}
	return m
}

// Has reports whether id is one of these actions.
func (a Actions) Has(id string) bool {
	return slices.ContainsFunc(a, func(act Action) bool { return act.ID == id })
}

// KeysFor inverts a keymap Resolve built from these actions: each action's
// ID mapped to every trigger bound to it — its default keys that are still
// bound to it first, in declared order, then any added by the user's
// config, sorted. Deterministic, unlike ranging over the keymap itself, so
// the first key is a stable choice for a one-key hint.
func (a Actions) KeysFor(keymap map[string]string) map[string][]string {
	out := map[string][]string{}
	defaultOwner := map[string]string{}
	for _, act := range a {
		for _, key := range act.Keys {
			defaultOwner[key] = act.ID
			if keymap[key] == act.ID {
				out[act.ID] = append(out[act.ID], key)
			}
		}
	}
	// Everything else: keys the config added, or took from another action.
	var extra []string
	for trigger, id := range keymap {
		if defaultOwner[trigger] != id {
			extra = append(extra, trigger)
		}
	}
	sort.Strings(extra)
	for _, trigger := range extra {
		id := keymap[trigger]
		out[id] = append(out[id], trigger)
	}
	return out
}

// Scope is one keymap: a name as used in "keybind = <scope>:<trigger> =
// <action>", a Title for help, and the actions its View runs.
type Scope struct {
	Name    string
	Title   string
	Actions Actions
}

// UnknownActions describes every keybinding in c that names a scope not in
// scopes, or an action its scope doesn't have — almost always a typo,
// which would otherwise silently do nothing. Safe on a nil *Config.
func (c *Config) UnknownActions(scopes []Scope) []string {
	if c == nil {
		return nil
	}
	byName := map[string]Scope{}
	for _, s := range scopes {
		byName[s.Name] = s
	}
	var out []string
	for scope, binds := range c.keybinds {
		s, ok := byName[scope]
		if !ok {
			out = append(out, fmt.Sprintf("unknown keybind scope %q", scope))
			continue
		}
		for trigger, action := range binds {
			if !s.Actions.Has(action) {
				out = append(out, fmt.Sprintf("unknown %s action %q bound to %q", scope, action, trigger))
			}
		}
	}
	sort.Strings(out)
	return out
}
