package main

import "testing"

// TestScopesAreWellFormed guards the keybinding registry as a whole: every
// scope has a distinct name and a help title, and within a scope every
// action is declared once, has a description (help and the action popup
// show it), and no key is claimed by two actions — Resolve would silently
// let the later one win.
func TestScopesAreWellFormed(t *testing.T) {
	seenScope := map[string]bool{}
	for _, sc := range scopes {
		if sc.Name == "" || sc.Title == "" {
			t.Errorf("scope %+v needs a Name and a Title", sc.Name)
		}
		if seenScope[sc.Name] {
			t.Errorf("scope %q listed twice", sc.Name)
		}
		seenScope[sc.Name] = true

		seenID := map[string]bool{}
		seenKey := map[string]string{}
		for _, a := range sc.Actions {
			if a.Desc == "" {
				t.Errorf("%s action %q has no description", sc.Name, a.ID)
			}
			if seenID[a.ID] {
				t.Errorf("%s action %q declared twice", sc.Name, a.ID)
			}
			seenID[a.ID] = true
			for _, k := range a.Keys {
				if other, ok := seenKey[k]; ok {
					t.Errorf("%s key %q bound to both %q and %q", sc.Name, k, other, a.ID)
				}
				seenKey[k] = a.ID
			}
		}
	}
}
