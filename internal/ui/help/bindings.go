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
			{"j k h l, arrows", "Move cursor (Normal mode)"},
			{"PageUp PageDown", "Move by a page"},
			{"Home End, 0 $", "Start / end of line"},
			{"g G", "First / last line"},
			{"i", "Enter Insert mode"},
			{"a", "Enter Insert mode, after the cursor"},
			{"o", "Open a new line below and enter Insert mode"},
			{"x", "Delete character under cursor"},
			{"X", "Delete character before cursor"},
			{"dd", "Delete (cut) this line"},
			{"yy", "Yank (copy) this line"},
			{"p", "Put (paste) after this line"},
			{"y", "Copy the mouse selection (to the clipboard too)"},
			{"Esc", "Return to Normal mode"},
			{"Enter", "Insert a newline (Insert mode)"},
			{"Backspace", "Delete before cursor (Insert mode)"},
			{"Tab", "Insert a tab character (Insert mode)"},
			{"arrows", "Move cursor (also works in Insert mode)"},
			{"u", "Undo the last change"},
			{"Ctrl+r", "Redo"},
			{":<N>", "Go to line N: type a number, Enter to jump, Esc to cancel"},
			{":w", "Save the active tab"},
			{":q :q!", "Close tab (refuses if unsaved; ! discards)"},
			{":qa :qa!", "Close all tabs (refuses if any unsaved; ! discards)"},
			{":wq", "Save then close the active tab"},
			{"Ctrl+s", "Save the active tab"},
			{"Ctrl+g", "Go to parent (syntax tree)"},
			{"Ctrl+]", "Go to definition"},
			{"Ctrl+b", "Jump back"},
			{"Ctrl+f", "Find references (opens the finder)"},
			{"Ctrl+Space", "Trigger autocomplete (Insert mode)"},
			{"K", "Show error/warning details for this line"},
			{"/", "Search in this file: type, Enter to jump, Esc to cancel"},
			{"n N", "Next / previous search match"},
		},
	},
	{
		Title: "Editor — mouse",
		Bindings: []binding{
			{"Click", "Place the cursor"},
			{"Drag", "Select text (drag past the edge to scroll)"},
			{"Double-click", "Select the word"},
			{"Triple-click", "Select the line"},
			{"Shift+click", "Extend the selection"},
			{"y", "Copy the selection, then Esc or any move to dismiss"},
			{"Wheel", "Scroll the pane under the pointer"},
		},
	},
	{
		Title: "Editor — git",
		Bindings: []binding{
			{"B", "Blame: who last changed this line"},
			{"H", "Show the diff hunk this line belongs to"},
			{"D", "Show this file's full diff against HEAD"},
		},
	},
	{
		Title: "File Tree",
		Bindings: []binding{
			{"j k, arrows", "Move cursor"},
			{"Enter, l, Right", "Open file / expand directory"},
			{"h, Left", "Collapse directory (or its parent)"},
			{"Shift+Left Shift+Right", "Peek a long name"},
			{"a", "New file (end with \"/\" for a directory)"},
			{"r", "Rename / move: edit the path, Enter"},
			{"d", "Delete (confirm y/N)"},
			{"Esc", "Cancel the prompt"},
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
		Title: "Diff",
		Bindings: []binding{
			{"j k, arrows", "Scroll"},
			{"PageUp PageDown", "Scroll by a page"},
			{"Home End", "First / last line"},
			{"Left Right", "Peek a long line"},
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
