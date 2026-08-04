package config

import (
	"fmt"
	"os"
	"path/filepath"
	"strings"

	"github.com/bricejulia/nib/internal/theme"
)

// Scope is one section of the generated template config file: a scope
// name (as used in "keybind = <scope>:<trigger> = <action>") paired with
// that scope's built-in keybindings.
type Scope struct {
	Name     string
	Defaults Defaults
}

// Template renders a starting-point config file: every default
// keybinding, commented out, grouped by scope, so the user can see
// exactly what's bindable and uncomment/edit a line to override it, plus
// worked examples of the "lsp" directive.
func Template(scopes []Scope) string {
	var b strings.Builder
	b.WriteString(`# nib config
#
# Uncomment and edit a line below to override that keybinding, or add a
# new "keybind" line entirely. Format:
#
#   keybind = <scope>:<trigger> = <action>
#
# "global" is the default scope and its ":" prefix may be omitted, e.g.
#
#   keybind = ctrl+p = open_finder
#
# A trigger is a "+"-joined combination of modifiers (ctrl, alt, super,
# shift) and a key — either a single character (x, ], ?) or a named key
# (up, down, left, right, enter, tab, esc, pageup, pagedown, home, end,
# backspace, space). Modifier names and named keys are case-insensitive; a
# single-character key is not (x and X are different keys).
#
# Press Ctrl+l (the "reload_config" binding below) to apply changes to
# this file without restarting nib — or just close the editor Ctrl+o
# opened it in, which reloads automatically. The one exception: a
# language server already running for an "lsp" line you just changed
# keeps using its old command until nib restarts.


# --- language servers ---
#
# Register a language server for a language:
#
#   lsp = <language> = <command args...>
#
# The language name is the one nib's grammar detection reports, shown in
# the status bar next to the file's LSP indicator:
#
#   go ●   server running
#   go ○   server configured, but not running (binary missing? see Ctrl+D)
#   go     no server configured for this language
#
# The command must be on your PATH and speak LSP over stdin/stdout. These
# merge over nib's built-in defaults, so a line here wins.
#
# PHP — pick one (nib defaults to intelephense; uncomment to change):
#   lsp = php = intelephense --stdio         # npm i -g intelephense
#   lsp = php = phpactor language-server     # fully open source alternative
#
# TypeScript/JavaScript/JSX/TSX — pick one (nib defaults to
# typescript-language-server; uncomment to change). The same command covers
# all four; set it once per language name if you want different servers:
#   lsp = typescript = typescript-language-server --stdio   # npm i -g typescript-language-server typescript
#   lsp = typescript = vtsls --stdio                        # alternative wrapper around VS Code's tsserver
#
# Some other common servers:
#   lsp = python = pyright-langserver --stdio
#   lsp = rust   = rust-analyzer
#   lsp = c      = clangd


# --- theme ---
#
# Pick a built-in color theme:
#
#   theme = <name>
#
`)
	fmt.Fprintf(&b, "# Built-in themes: %s. Falls back to\n", strings.Join(theme.BuiltinNames, ", "))
	fmt.Fprintf(&b, "# %q if this line is omitted or misspells a theme name — check the\n", theme.DefaultName)
	b.WriteString(`# debug log (Ctrl+D) for a warning if so. Colors are restricted to the 16
# standard ANSI colors nib ever draws with, so every theme renders
# reasonably on any terminal regardless of its own color scheme.
#
# Override any individual color, on top of whichever theme is active:
#
#   color = <role> = <color>
#
# <color> is one of: default black red green yellow blue magenta cyan
# white brightblack brightred brightgreen brightyellow brightblue
# brightmagenta brightcyan brightwhite (bright colors may also be spelled
# with a hyphen, e.g. bright-red).
#
# Roles, grouped by where they show up:
#   git_modified git_added git_deleted git_renamed git_conflicted git_header
#     — file tree/finder status markers, the editor gutter, diff +/- lines
#       and hunk headers, and the blame/hunk popups (B/H)
#   syntax_comment syntax_string syntax_constant syntax_keyword
#   syntax_function syntax_type
#     — source code highlighting
#   diagnostic_error diagnostic_warning diagnostic_info
#     — language-server/parse-error markers in the editor gutter
#   debug_warn debug_error
#     — the debug log (Ctrl+D)
#   filetree_prompt_error
#     — inline error text in the file tree's create/rename/delete prompt
#   editor_selection
#     — the mouse-selection highlight
#   ui_focus_border
#     — the focused pane's border and title
#
# Example — keep "default" but make added/deleted git markers brighter:
#   theme = default
#   color = git_added = brightgreen
#   color = git_deleted = brightred
`)

	for _, s := range scopes {
		b.WriteString("\n# --- ")
		b.WriteString(s.Name)
		b.WriteString(" ---\n")
		for _, bind := range s.Defaults {
			prefix := s.Name + ":"
			if s.Name == "global" {
				prefix = ""
			}
			fmt.Fprintf(&b, "# keybind = %s%s = %s\n", prefix, bind.Trigger, bind.Action)
		}
	}

	return b.String()
}

// EnsureFile writes Template(scopes) to path if no file exists there yet
// (creating any missing parent directories); it leaves an existing file
// untouched. Returns the (possibly newly created) path.
func EnsureFile(path string, scopes []Scope) error {
	if _, err := os.Stat(path); err == nil {
		return nil
	} else if !os.IsNotExist(err) {
		return err
	}

	if err := os.MkdirAll(filepath.Dir(path), 0o755); err != nil {
		return err
	}
	return os.WriteFile(path, []byte(Template(scopes)), 0o644)
}
