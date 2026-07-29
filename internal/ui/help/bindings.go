package help

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

// sections is the full keybinding reference shown in the help overlay.
// Hand-maintained: kiwi has no runtime keymap registry to introspect (the
// global keymap is a plain map[string]func() with no description slot, and
// each pane's HandleKey matches on hardcoded key literals), so this list
// must be updated by hand whenever a binding changes elsewhere.
var sections = []section{
	{
		Title: "Global",
		Bindings: []binding{
			{"Ctrl+c", "Quit"},
			{"Tab", "Focus next pane"},
			{"Shift+Tab", "Focus previous pane"},
			{"Ctrl+p", "Open file finder (also: double-tap Shift)"},
			{"Ctrl+d", "Open debug log"},
			{"?", "Open this help"},
			{"Ctrl+o", "Open config file"},
		},
	},
	{
		Title: "Splits",
		Bindings: []binding{
			{"Ctrl+w", "Split the focused editor pane (side-by-side, right)"},
			{"Ctrl+e", "Split the focused editor pane (stacked, below)"},
			{"Ctrl+x", "Close the focused editor pane"},
		},
	},
	{
		Title: "Editor",
		Bindings: []binding{
			{"] [", "Next / previous tab"},
			{"x X", "Close tab / close all tabs"},
			{"j k h l, arrows", "Move cursor (Normal mode)"},
			{"PageUp PageDown", "Move by a page"},
			{"Home End", "Start / end of line"},
			{"g G", "First / last line"},
			{"i", "Enter Insert mode"},
			{"a", "Enter Insert mode, after the cursor"},
			{"Esc", "Return to Normal mode"},
			{"Enter", "Insert a newline (Insert mode)"},
			{"Backspace", "Delete before cursor (Insert mode)"},
			{"arrows", "Move cursor (also works in Insert mode)"},
			{"u", "Undo the last Insert session"},
			{"Ctrl+r", "Redo"},
			{":", "Go to line: type a number, Enter to jump, Esc to cancel"},
			{"Ctrl+s", "Save the active tab"},
		},
	},
	{
		Title: "File Tree",
		Bindings: []binding{
			{"j k, arrows", "Move cursor"},
			{"Enter, l, Right", "Open file / expand directory"},
			{"h, Left", "Collapse directory (or its parent)"},
			{"Shift+Left Shift+Right", "Peek a long name"},
		},
	},
	{
		Title: "Finder",
		Bindings: []binding{
			{"Tab", "Switch file / content search"},
			{"Enter", "Open selection"},
			{"Up Down", "Move selection"},
			{"Left Right", "Peek a long line"},
			{"Esc", "Close"},
		},
	},
	{
		Title: "Debug Log",
		Bindings: []binding{
			{"Tab", "Cycle minimum severity filter"},
			{"Up Down PageUp PageDown", "Scroll"},
			{"Home End", "Oldest / newest entry"},
			{"Esc", "Close"},
		},
	},
	{
		Title: "Help",
		Bindings: []binding{
			{"Up Down PageUp PageDown", "Scroll"},
			{"Esc", "Close"},
		},
	},
}
