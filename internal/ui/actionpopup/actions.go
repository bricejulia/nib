package actionpopup

// entry is one command the action popup can search for and run. Scope says
// which registry executes it: "global" for cmd/nib/main.go's own
// map[string]func(), "editor" for editor.View.ExecuteAction.
//
// Hand-maintained, like internal/ui/help/bindings.go: nib has no runtime
// command registry to introspect (see that file's own doc comment), so
// this list must be updated by hand whenever an action is added, renamed,
// or removed elsewhere. Deliberately narrower than the full keybinding
// reference in internal/ui/help — movement, vim operators, and mode-entry
// keys (insert_mode, next_tab, delete_char_forward, ...) aren't included:
// they aren't "commands" in the find-action sense, they're meaningless
// without an actual keypress driving them.
type entry struct {
	ID, Desc, Scope string
}

var entries = []entry{
	{"open_finder", "Open file finder", "global"},
	{"open_find_references", "Find references: search file contents", "global"},
	{"open_replace", "Open find & replace in path", "global"},
	{"open_debug", "Open debug log", "global"},
	{"open_help", "Open help", "global"},
	{"open_config", "Open config file", "global"},
	{"reload_config", "Reload config file", "global"},
	{"reveal_in_tree", "Locate the active file in the file tree", "global"},
	{"split_right", "Split the focused editor pane (side-by-side)", "global"},
	{"split_down", "Split the focused editor pane (stacked)", "global"},
	{"close_pane", "Close the focused editor pane", "global"},

	{"save", "Save the active tab", "editor"},
	{"undo", "Undo the last change", "editor"},
	{"redo", "Redo", "editor"},
	{"go_to_definition", "Go to definition", "editor"},
	{"go_to_parent", "Go to parent (syntax tree)", "editor"},
	{"jump_back", "Jump back to before the last jump", "editor"},
	{"search_next", "Next search match", "editor"},
	{"search_prev", "Previous search match", "editor"},
	{"toggle_tab_mode", "Toggle this file between spaces and tabs", "editor"},
	{"show_hover", "Show hover info for the symbol under the cursor", "editor"},
	{"trigger_signature_help", "Show signature help for the enclosing call", "editor"},
	{"format_document", "Format the document via the language server", "editor"},
	{"show_blame", "Blame: who last changed this line", "editor"},
	{"show_line_diff", "Show the diff hunk this line belongs to", "editor"},
	{"show_file_diff", "Show this file's full diff against HEAD", "editor"},
}
