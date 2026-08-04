package theme

import "github.com/bricejulia/nib/internal/layout"

// Default reproduces, role for role, every color that was hardcoded across
// the UI before theming existed — installing it must change nothing about
// how nib looks out of the box.
var Default = Theme{
	GitModified:   layout.ColorYellow,
	GitAdded:      layout.ColorGreen,
	GitDeleted:    layout.ColorRed,
	GitRenamed:    layout.ColorCyan,
	GitConflicted: layout.ColorBrightRed,
	GitHeader:     layout.ColorCyan,

	SyntaxComment:  layout.ColorBrightBlack,
	SyntaxString:   layout.ColorGreen,
	SyntaxConstant: layout.ColorMagenta,
	SyntaxKeyword:  layout.ColorYellow,
	SyntaxFunction: layout.ColorBlue,
	SyntaxType:     layout.ColorCyan,

	DiagnosticError:   layout.ColorRed,
	DiagnosticWarning: layout.ColorYellow,
	DiagnosticInfo:    layout.ColorBlue,

	DebugWarn:  layout.ColorYellow,
	DebugError: layout.ColorBrightRed,

	FiletreePromptError: layout.ColorRed,

	EditorSelection: layout.ColorBrightBlack,

	UIFocusBorder: layout.ColorCyan,
}

// Ocean is a cooler, blue/cyan-leaning palette: mostly the "bright" ANSI
// variants of Default's colors, plus a blue-tinted selection and border.
var Ocean = Theme{
	GitModified:   layout.ColorBrightYellow,
	GitAdded:      layout.ColorBrightGreen,
	GitDeleted:    layout.ColorBrightRed,
	GitRenamed:    layout.ColorBrightCyan,
	GitConflicted: layout.ColorBrightMagenta,
	GitHeader:     layout.ColorBrightBlue,

	SyntaxComment:  layout.ColorBrightBlack,
	SyntaxString:   layout.ColorBrightGreen,
	SyntaxConstant: layout.ColorBrightMagenta,
	SyntaxKeyword:  layout.ColorBrightBlue,
	SyntaxFunction: layout.ColorBrightCyan,
	SyntaxType:     layout.ColorBlue,

	DiagnosticError:   layout.ColorBrightRed,
	DiagnosticWarning: layout.ColorBrightYellow,
	DiagnosticInfo:    layout.ColorBrightCyan,

	DebugWarn:  layout.ColorBrightYellow,
	DebugError: layout.ColorBrightRed,

	FiletreePromptError: layout.ColorBrightRed,

	EditorSelection: layout.ColorBlue,

	UIFocusBorder: layout.ColorBrightBlue,
}

// Mono is a low-color, high-legibility palette: color is reserved almost
// entirely for git/diagnostic signal (red=danger, green=good); everything
// else is white/bright-white/bright-black so it reads the same regardless
// of the terminal's own light/dark color scheme.
var Mono = Theme{
	GitModified:   layout.ColorWhite,
	GitAdded:      layout.ColorGreen,
	GitDeleted:    layout.ColorRed,
	GitRenamed:    layout.ColorWhite,
	GitConflicted: layout.ColorBrightRed,
	GitHeader:     layout.ColorWhite,

	SyntaxComment:  layout.ColorBrightBlack,
	SyntaxString:   layout.ColorWhite,
	SyntaxConstant: layout.ColorWhite,
	SyntaxKeyword:  layout.ColorBrightWhite,
	SyntaxFunction: layout.ColorBrightWhite,
	SyntaxType:     layout.ColorWhite,

	DiagnosticError:   layout.ColorRed,
	DiagnosticWarning: layout.ColorYellow,
	DiagnosticInfo:    layout.ColorWhite,

	DebugWarn:  layout.ColorYellow,
	DebugError: layout.ColorBrightRed,

	FiletreePromptError: layout.ColorRed,

	EditorSelection: layout.ColorBrightBlack,

	UIFocusBorder: layout.ColorWhite,
}

// Amber is a warm, retro-terminal palette leaning on yellow/bright-white.
var Amber = Theme{
	GitModified:   layout.ColorYellow,
	GitAdded:      layout.ColorBrightYellow,
	GitDeleted:    layout.ColorBrightRed,
	GitRenamed:    layout.ColorYellow,
	GitConflicted: layout.ColorBrightRed,
	GitHeader:     layout.ColorYellow,

	SyntaxComment:  layout.ColorBrightBlack,
	SyntaxString:   layout.ColorBrightYellow,
	SyntaxConstant: layout.ColorYellow,
	SyntaxKeyword:  layout.ColorBrightWhite,
	SyntaxFunction: layout.ColorBrightYellow,
	SyntaxType:     layout.ColorYellow,

	DiagnosticError:   layout.ColorBrightRed,
	DiagnosticWarning: layout.ColorYellow,
	DiagnosticInfo:    layout.ColorBrightWhite,

	DebugWarn:  layout.ColorYellow,
	DebugError: layout.ColorBrightRed,

	FiletreePromptError: layout.ColorBrightRed,

	EditorSelection: layout.ColorBrightBlack,

	UIFocusBorder: layout.ColorBrightYellow,
}

// DefaultName is the built-in theme Resolve falls back to when a
// config-file "theme = <name>" line is absent or names an unknown theme.
const DefaultName = "default"

// BuiltinNames lists every built-in theme's name, in the fixed order
// template.go's generated docs list them.
var BuiltinNames = []string{"default", "ocean", "mono", "amber"}

// Builtins maps a theme name (as typed in a config file's "theme = <name>"
// line) to its Theme.
var Builtins = map[string]Theme{
	"default": Default,
	"ocean":   Ocean,
	"mono":    Mono,
	"amber":   Amber,
}

// Resolve looks up name in Builtins, falling back to Default on a miss
// (never unknown-name errors — the same "malformed config never breaks
// startup" spirit as config.Parse), then merges overrides on top.
func Resolve(name string, overrides map[Role]layout.Color) Theme {
	base, ok := Builtins[name]
	if !ok {
		base = Builtins[DefaultName]
	}
	return base.Resolve(overrides)
}
