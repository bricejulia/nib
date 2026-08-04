package theme

import (
	"testing"

	"github.com/bricejulia/nib/internal/layout"
)

func TestResolveMergesAndOverrides(t *testing.T) {
	base := Theme{
		GitAdded:   layout.ColorGreen,
		GitDeleted: layout.ColorRed,
	}
	merged := base.Resolve(map[Role]layout.Color{
		GitDeleted: layout.ColorBrightRed, // overrides a base role
		GitRenamed: layout.ColorCyan,      // adds a new role
	})

	if got := merged.Get(GitAdded); got != layout.ColorGreen {
		t.Errorf("merged[GitAdded] = %v, want ColorGreen (untouched base)", got)
	}
	if got := merged.Get(GitDeleted); got != layout.ColorBrightRed {
		t.Errorf("merged[GitDeleted] = %v, want ColorBrightRed (override wins)", got)
	}
	if got := merged.Get(GitRenamed); got != layout.ColorCyan {
		t.Errorf("merged[GitRenamed] = %v, want ColorCyan (new override)", got)
	}
}

func TestGetOnNilTheme(t *testing.T) {
	var th Theme
	if got := th.Get(GitAdded); got != layout.ColorDefault {
		t.Errorf("nil Theme.Get = %v, want ColorDefault", got)
	}
}

func TestGetMissingRoleReturnsColorDefault(t *testing.T) {
	th := Theme{GitAdded: layout.ColorGreen}
	if got := th.Get(GitDeleted); got != layout.ColorDefault {
		t.Errorf("Get(missing role) = %v, want ColorDefault", got)
	}
}

// TestDefaultThemeMatchesHardcodedColors is the regression guard: Default
// must reproduce every color that was hardcoded across the UI before
// theming existed. want is spelled out independently of Default's own
// definition, so this actually catches an accidental edit to Default
// rather than trivially agreeing with itself.
func TestDefaultThemeMatchesHardcodedColors(t *testing.T) {
	want := map[Role]layout.Color{
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

	for role, wantColor := range want {
		if got := Default.Get(role); got != wantColor {
			t.Errorf("Default.Get(%q) = %v, want %v", role, got, wantColor)
		}
	}
	if len(want) != len(AllRoles) {
		t.Fatalf("test table has %d roles, AllRoles has %d — keep them in sync", len(want), len(AllRoles))
	}
}

func TestValidRoleAcceptsKnownRejectsUnknown(t *testing.T) {
	if !ValidRole(GitAdded) {
		t.Error("ValidRole(GitAdded) = false, want true")
	}
	if ValidRole(Role("not_a_real_role")) {
		t.Error(`ValidRole("not_a_real_role") = true, want false`)
	}
}

func TestParseColorNameAcceptsAllSixteenPlusDefault(t *testing.T) {
	cases := map[string]layout.Color{
		"default":       layout.ColorDefault,
		"black":         layout.ColorBlack,
		"red":           layout.ColorRed,
		"green":         layout.ColorGreen,
		"yellow":        layout.ColorYellow,
		"blue":          layout.ColorBlue,
		"magenta":       layout.ColorMagenta,
		"cyan":          layout.ColorCyan,
		"white":         layout.ColorWhite,
		"brightblack":   layout.ColorBrightBlack,
		"brightred":     layout.ColorBrightRed,
		"brightgreen":   layout.ColorBrightGreen,
		"brightyellow":  layout.ColorBrightYellow,
		"brightblue":    layout.ColorBrightBlue,
		"brightmagenta": layout.ColorBrightMagenta,
		"brightcyan":    layout.ColorBrightCyan,
		"brightwhite":   layout.ColorBrightWhite,
	}
	for name, want := range cases {
		got, ok := ParseColorName(name)
		if !ok || got != want {
			t.Errorf("ParseColorName(%q) = (%v, %v), want (%v, true)", name, got, ok, want)
		}
	}
}

func TestParseColorNameAcceptsHyphenAndCaseVariants(t *testing.T) {
	cases := map[string]layout.Color{
		"bright-red":  layout.ColorBrightRed,
		"BrightRed":   layout.ColorBrightRed,
		"BRIGHT-BLUE": layout.ColorBrightBlue,
		"  cyan  ":    layout.ColorCyan,
		"gray":        layout.ColorBrightBlack,
		"grey":        layout.ColorBrightBlack,
	}
	for name, want := range cases {
		got, ok := ParseColorName(name)
		if !ok || got != want {
			t.Errorf("ParseColorName(%q) = (%v, %v), want (%v, true)", name, got, ok, want)
		}
	}
}

func TestParseColorNameRejectsUnknown(t *testing.T) {
	for _, name := range []string{"fluorescent", "", "reddish", "16"} {
		if _, ok := ParseColorName(name); ok {
			t.Errorf("ParseColorName(%q) = ok, want rejected", name)
		}
	}
}

func TestResolveByNameFallsBackToDefaultOnUnknownThemeName(t *testing.T) {
	got := Resolve("bogus", nil)
	want := Builtins[DefaultName].Resolve(nil)
	for _, role := range AllRoles {
		if got.Get(role) != want.Get(role) {
			t.Errorf("Resolve(%q).Get(%q) = %v, want %v (Default's)", "bogus", role, got.Get(role), want.Get(role))
		}
	}
}

func TestResolveByNameAppliesOverridesOnTopOfBuiltin(t *testing.T) {
	got := Resolve("ocean", map[Role]layout.Color{GitAdded: layout.ColorWhite})
	if got.Get(GitAdded) != layout.ColorWhite {
		t.Errorf("override GitAdded = %v, want ColorWhite", got.Get(GitAdded))
	}
	if got.Get(GitDeleted) != Ocean.Get(GitDeleted) {
		t.Errorf("untouched GitDeleted = %v, want ocean's %v", got.Get(GitDeleted), Ocean.Get(GitDeleted))
	}
}

func TestBuiltinsCoverEveryRole(t *testing.T) {
	for name, th := range Builtins {
		for _, role := range AllRoles {
			if _, ok := th[role]; !ok {
				t.Errorf("builtin theme %q has no entry for role %q", name, role)
			}
		}
	}
}
