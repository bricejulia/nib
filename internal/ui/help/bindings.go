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
// Hand-maintained: nib has no runtime keymap registry to introspect (the
// global keymap is a plain map[string]func() with no description slot, and
// each pane's HandleKey matches on hardcoded key literals), so this list
// must be updated by hand whenever a binding changes elsewhere.
var sections = []section{
	{
		Title: "Global",
		Bindings: []binding{
			{"Ctrl+c", "Quit (asks to confirm first)"},
			{"Tab", "Focus next pane"},
			{"Shift+Tab", "Focus previous pane"},
			{"Ctrl+p", "Open file finder (also: double-tap Shift)"},
			{"Ctrl+f", "Find references: search file contents (pre-filled with the word under the cursor, if an editor pane is focused)"},
			{"Ctrl+r", "Open find & replace in path (also: Tab twice from the file finder)"},
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
			{"r", "Redo"},
			{":<N>", "Go to line N: type a number, Enter to jump, Esc to cancel"},
			{":w", "Save the active tab"},
			{":q :q!", "Close tab (refuses if unsaved; ! discards)"},
			{":qa :qa!", "Close all tabs (refuses if any unsaved; ! discards)"},
			{":wq", "Save then close the active tab"},
			{"Ctrl+s", "Save the active tab"},
			{"Ctrl+g", "Go to parent (syntax tree)"},
			{"Ctrl+]", "Go to definition"},
			{"Ctrl+b", "Jump back"},
			{"Ctrl+Space", "Trigger autocomplete (Insert mode)"},
			{"Ctrl+a", "Show signature help for the enclosing call"},
			{"K", "Show error/warning details for this line"},
			{"I", "Show hover info (type/doc) for the symbol under the cursor"},
			{"F", "Format the document via the language server"},
			{"/", "Search in this file: type, Enter to jump, Esc to cancel"},
			{"n N", "Next / previous search match"},
		},
	},
	{
		Title: "Editor — mouse",
		Bindings: []binding{
			{"Click", "Place the cursor"},
			{"Drag", "Select text, and copy it (drag past the edge to scroll)"},
			{"Double-click", "Select the word, and copy it"},
			{"Triple-click", "Select the line, and copy it"},
			{"Shift+click", "Extend the selection (copy with y)"},
			{"y", "Copy the selection to the clipboard AND the yank register"},
			{"Esc", "Dismiss the selection (as does any cursor movement)"},
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
			{"Tab", "Cycle file / content / find & replace search"},
			{"Enter", "Open selection"},
			{"Up Down", "Move selection"},
			{"Left Right", "Peek a long line"},
			{"Esc", "Close"},
		},
	},
	{
		Title: "Find & Replace",
		Bindings: []binding{
			{"Tab Enter", "Switch between Find, Replace, and the results list"},
			{"Up Down", "Move selection (results list)"},
			{"Space", "Toggle the occurrence, or a whole file's occurrences"},
			{"Enter", "Replace just this occurrence (results list)"},
			{"a", "Replace every checked occurrence"},
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
