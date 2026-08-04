// Package theme maps nib's semantic UI/syntax roles (e.g. "a diagnostic
// error", "an added git line") onto layout.Color values, restricted to the
// 16 standard ANSI colors like layout.Color itself — a theme reassigns
// which of those 16 colors each role uses, it never introduces new ones, so
// every theme renders reasonably on any terminal.
package theme

import (
	"maps"

	"github.com/bricejulia/nib/internal/layout"
)

// Role names one themeable role. A map key (map[Role]layout.Color) rather
// than a struct field, because "color = <role> = <value>" config lines
// arrive as arbitrary strings from config.Parse — a map merges those
// string-keyed overrides directly, the same shape config.Defaults.Resolve
// already uses for trigger->action overrides.
type Role string

// Roles, grouped by where they render. Each maps 1:1 onto a color that was
// hardcoded somewhere in the UI before theming existed — see Default in
// builtins.go, which reproduces those exact colors so the out-of-the-box
// look is unchanged.
const (
	// Git status: shared by the file tree and finder's status markers, the
	// editor gutter's diff markers, diffview's +/- lines, and the editor's
	// blame/hunk popups — the same "this line/file was added/deleted/etc."
	// concept everywhere it renders.
	GitModified   Role = "git_modified"
	GitAdded      Role = "git_added"
	GitDeleted    Role = "git_deleted"
	GitRenamed    Role = "git_renamed"
	GitConflicted Role = "git_conflicted"
	// GitHeader is a line of git metadata rather than content: diffview's
	// "@@ ... @@" hunk header, and the editor's hunk-summary and
	// blame-header popup lines — all three are the same color today.
	GitHeader Role = "git_header"

	// Source-code highlighting, shared by the real tree-sitter highlighter
	// and highlight.go's heuristic stopgap.
	SyntaxComment  Role = "syntax_comment"
	SyntaxString   Role = "syntax_string"
	SyntaxConstant Role = "syntax_constant" // number, constant, boolean, escape
	SyntaxKeyword  Role = "syntax_keyword"  // keyword, operator, label, tag
	SyntaxFunction Role = "syntax_function" // function, constructor
	SyntaxType     Role = "syntax_type"     // type, variable.builtin, property, attribute, namespace, module

	// Editor gutter diagnostics (language-server + parse errors).
	DiagnosticError   Role = "diagnostic_error"
	DiagnosticWarning Role = "diagnostic_warning"
	DiagnosticInfo    Role = "diagnostic_info"

	// The debug log (Ctrl+D).
	DebugWarn  Role = "debug_warn"
	DebugError Role = "debug_error"

	// The file tree's create/rename/delete prompt.
	FiletreePromptError Role = "filetree_prompt_error"

	// The editor's mouse-selection highlight (a Background color).
	EditorSelection Role = "editor_selection"

	// The space/tab glyphs drawn when "whitespace = true" is set.
	EditorWhitespace Role = "editor_whitespace"

	// The focused pane's border and title.
	UIFocusBorder Role = "ui_focus_border"
)

// AllRoles lists every role in a fixed order, for template.go's generated
// docs and for tests that check every built-in theme covers every role.
var AllRoles = []Role{
	GitModified, GitAdded, GitDeleted, GitRenamed, GitConflicted, GitHeader,
	SyntaxComment, SyntaxString, SyntaxConstant, SyntaxKeyword, SyntaxFunction, SyntaxType,
	DiagnosticError, DiagnosticWarning, DiagnosticInfo,
	DebugWarn, DebugError,
	FiletreePromptError,
	EditorSelection,
	EditorWhitespace,
	UIFocusBorder,
}

// validRoles backs ValidRole; built once from AllRoles rather than
// maintained as a second literal that could drift from it.
var validRoles = func() map[Role]bool {
	m := make(map[Role]bool, len(AllRoles))
	for _, r := range AllRoles {
		m[r] = true
	}
	return m
}()

// ValidRole reports whether r is one of the roles nib actually themes.
func ValidRole(r Role) bool { return validRoles[r] }

// Theme maps every role to one of the 16 standard ANSI colors. A nil Theme
// is valid: Get answers layout.ColorDefault for every role.
type Theme map[Role]layout.Color

// Get returns t's color for role, or layout.ColorDefault if t has no entry
// for it (including when t is nil) — mirrors (*config.Config).Overrides's
// nil-safety.
func (t Theme) Get(role Role) layout.Color {
	if t == nil {
		return layout.ColorDefault
	}
	return t[role]
}

// Resolve merges base with overrides (overrides win on a matching role)
// into a new Theme — the same shape as config.Defaults.Resolve, just keyed
// by Role/layout.Color instead of trigger/action strings.
func (base Theme) Resolve(overrides map[Role]layout.Color) Theme {
	merged := make(Theme, len(base)+len(overrides))
	maps.Copy(merged, base)
	maps.Copy(merged, overrides)
	return merged
}

// Active is the theme every color-bearing View reads from via Get. Set at
// startup by cmd/nib's run() (see SetActive), before app.Run() starts
// rendering, and set again whenever the user reloads the config live
// (cmd/nib's reload_config action). It's a plain package var, not behind
// a mutex: nib's event loop and rendering are single-threaded (see
// internal/ui.App — cross-goroutine work like LSP responses marshals
// back through App.Post specifically to land on that goroutine), so a
// later SetActive call from reload_config never races a concurrent read.
var Active Theme = Default

// SetActive installs t as the theme Get subsequently reads. Called once
// at startup, and again by cmd/nib's reload_config action on every live
// config reload — see the doc on Active for why both are safe with no
// synchronization.
func SetActive(t Theme) { Active = t }

// Get is shorthand for Active.Get(role) — what every themed Style literal
// in the UI packages calls.
func Get(role Role) layout.Color { return Active.Get(role) }
