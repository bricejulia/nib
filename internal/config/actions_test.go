package config

import (
	"slices"
	"strings"
	"testing"
)

func TestKeysForListsDefaultsInOrderThenAddedKeysSorted(t *testing.T) {
	actions := Actions{
		{ID: "move_down", Keys: []string{"j", "Down"}},
		{ID: "move_up", Keys: []string{"k", "Up"}},
	}
	keymap := actions.Resolve(map[string]string{
		"k":      "move_down", // takes k away from move_up
		"Ctrl+n": "move_down",
	})

	keys := actions.KeysFor(keymap)

	if got, want := keys["move_down"], []string{"j", "Down", "Ctrl+n", "k"}; !slices.Equal(got, want) {
		t.Errorf("move_down keys = %v, want %v", got, want)
	}
	if got, want := keys["move_up"], []string{"Up"}; !slices.Equal(got, want) {
		t.Errorf("move_up keys = %v, want %v (k was rebound)", got, want)
	}
}

func TestUnknownActionsReportsTyposInEveryScope(t *testing.T) {
	cfg := Parse(strings.NewReader(`
keybind = ctrl+p = open_findr
keybind = editor:x = delete_char_forward
keybind = editor:z = delet_char
keybind = nosuch:x = y
`))
	scopes := []Scope{
		{Name: "global", Actions: Actions{{ID: "open_finder"}}},
		{Name: "editor", Actions: Actions{{ID: "delete_char_forward"}}},
	}

	got := cfg.UnknownActions(scopes)

	want := []string{
		`unknown editor action "delet_char" bound to "z"`,
		`unknown global action "open_findr" bound to "Ctrl+p"`,
		`unknown keybind scope "nosuch"`,
	}
	if !slices.Equal(got, want) {
		t.Errorf("UnknownActions =\n%q\nwant\n%q", got, want)
	}
}
