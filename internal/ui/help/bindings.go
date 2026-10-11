package help

import (
	"strings"

	"github.com/bricejulia/nib/internal/config"
)

// section groups related keybindings under a heading shown in the help
// overlay.
type section struct {
	Title    string
	Bindings []binding
}

type binding struct {
	Key  string
	Desc string
}

// extra is help content that isn't an action, so no scope declares it:
// ":" commands, vim's composite gestures, mouse use, keys a View handles
// structurally rather than through its keymap. Added to the end of the
// section titled Section; a section with no actions at all is created
// right after the one titled After.
type extra struct {
	Section string
	After   string
	Rows    []binding
}

var extras = []extra{
	{Section: "Global", Rows: []binding{
		{"Shift Shift", "Open file finder (double-tap Shift)"},
	}},
	{Section: "Editor", Rows: []binding{
		{"<count><motion>", "Repeat a motion: 3j, 5w, 2G"},
		{"<op><motion>", "Apply d/y/c over a motion: dw, d$, c0, y3w"},
		{"<op>iw <op>aw", "Apply d/y/c to the word under the cursor (aw: with its spacing): diw, caw"},
		{":<N>", "Go to line N: type a number, Enter to jump, Esc to cancel"},
		{":w", "Save the active tab"},
		{":q :q!", "Close tab (refuses if unsaved; ! discards)"},
		{":qa :qa!", "Close all tabs (refuses if any unsaved; ! discards)"},
		{":wq", "Save then close the active tab"},
	}},
	{Section: "Editor — mouse", After: "Editor", Rows: []binding{
		{"Click", "Place the cursor"},
		{"Drag", "Select text, and copy it (drag past the edge to scroll)"},
		{"Double-click", "Select the word, and copy it"},
		{"Triple-click", "Select the line, and copy it"},
		{"Shift+click", "Extend the selection (copy with y)"},
		{"Wheel", "Scroll the pane under the pointer"},
		{"Right-click tab", "Menu: close, split or move the tab to another pane"},
	}},
	{Section: "File Tree", Rows: []binding{
		{"Esc", "Cancel the prompt"},
		{"Right-click", "Menu: copy path / relative path, rename / move, delete"},
	}},
	{Section: "Finder", Rows: []binding{
		{"Left Right Home End", "Move the caret in the query (e.g. to add a prefix to a pre-filled word)"},
	}},
	{Section: "Find & Replace", Rows: []binding{
		{"Tab Enter", "Switch between Find, Replace, and the results list"},
		{"Esc", "Close"},
	}},
	{Section: "Help", Rows: []binding{
		{"<type>", "Filter the list by key or description"},
	}},
}

// buildSections lists every scope's actions with the keys currently bound
// to them (keymap gives each scope's resolved trigger->action map), grouped
// by help section in order of first appearance, then folds in extras.
// An action with no key bound is left out: there's nothing to press.
func buildSections(scopes []config.Scope, keymap func(scope string) map[string]string) []section {
	var out []section
	index := map[string]int{}
	add := func(title string, rows ...binding) {
		i, ok := index[title]
		if !ok {
			i = len(out)
			index[title] = i
			out = append(out, section{Title: title})
		}
		out[i].Bindings = append(out[i].Bindings, rows...)
	}

	for _, s := range scopes {
		keys := s.Actions.KeysFor(keymap(s.Name))
		for _, a := range s.Actions {
			if len(keys[a.ID]) == 0 {
				continue
			}
			title := a.Section
			if title == "" {
				title = s.Title
			}
			add(title, binding{Key: strings.Join(keys[a.ID], " "), Desc: a.Desc})
		}
	}

	for _, e := range extras {
		if _, ok := index[e.Section]; ok {
			add(e.Section, e.Rows...)
			continue
		}
		at := len(out)
		if i, ok := index[e.After]; ok {
			at = i + 1
		}
		out = append(out[:at], append([]section{{Title: e.Section, Bindings: e.Rows}}, out[at:]...)...)
		for title, i := range index {
			if i >= at {
				index[title] = i + 1
			}
		}
		index[e.Section] = at
	}
	return out
}
