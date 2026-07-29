package ui

import (
	"testing"

	"go.rockorager.dev/vaxis"

	"github.com/bricejulia/kiwi/internal/layout"
)

func TestTranslateKeyArrowKeys(t *testing.T) {
	cases := []struct {
		name    string
		keycode rune
		want    string
	}{
		{"up", vaxis.KeyUp, layout.KeyUp},
		{"down", vaxis.KeyDown, layout.KeyDown},
		{"left", vaxis.KeyLeft, layout.KeyLeft},
		{"right", vaxis.KeyRight, layout.KeyRight},
		{"enter", vaxis.KeyEnter, layout.KeyEnter},
		{"pgup", vaxis.KeyPgUp, layout.KeyPageUp},
		{"pgdown", vaxis.KeyPgDown, layout.KeyPageDown},
	}
	for _, c := range cases {
		t.Run(c.name, func(t *testing.T) {
			got := translateKey(vaxis.Key{Keycode: c.keycode, EventType: vaxis.EventPress})
			if got.Named != c.want {
				t.Errorf("translateKey(Keycode=%v).Named = %q, want %q", c.keycode, got.Named, c.want)
			}
			if got.EventType != layout.EventPress {
				t.Errorf("EventType = %v, want EventPress", got.EventType)
			}
		})
	}
}

func TestTranslateKeyPlainLetterHasNoNamedField(t *testing.T) {
	got := translateKey(vaxis.Key{Text: "j", Keycode: 'j', EventType: vaxis.EventPress})
	if got.Named != "" {
		t.Errorf("expected Named to be empty for a plain letter key, got %q", got.Named)
	}
	if got.Text != "j" {
		t.Errorf("expected Text 'j', got %q", got.Text)
	}
}

func TestTranslateKeyEventTypes(t *testing.T) {
	cases := []struct {
		name string
		in   vaxis.EventType
		want layout.EventType
	}{
		{"press", vaxis.EventPress, layout.EventPress},
		{"repeat", vaxis.EventRepeat, layout.EventRepeat},
		{"release", vaxis.EventRelease, layout.EventRelease},
	}
	for _, c := range cases {
		t.Run(c.name, func(t *testing.T) {
			got := translateKey(vaxis.Key{Keycode: vaxis.KeyDown, EventType: c.in})
			if got.EventType != c.want {
				t.Errorf("EventType = %v, want %v", got.EventType, c.want)
			}
			if got.Named != layout.KeyDown {
				t.Errorf("Named = %q, want %q (EventType should not affect Named)", got.Named, layout.KeyDown)
			}
		})
	}
}

func TestTranslateKeyModifiers(t *testing.T) {
	k := vaxis.Key{Text: "c", Keycode: 'c', Modifiers: vaxis.ModCtrl, EventType: vaxis.EventPress}
	got := translateKey(k)
	if got.Mods&layout.ModCtrl == 0 {
		t.Errorf("expected ModCtrl to be set, got Mods=%v", got.Mods)
	}
	if got.String() != "Ctrl+c" {
		t.Errorf("got String()=%q, want %q", got.String(), "Ctrl+c")
	}
}

// TestTranslateKeyStripsShiftFromPunctuationText guards a real bug: some
// terminals report ModShift alongside a shifted punctuation character (the
// kitty protocol can, when it sends associated text) while vaxis's own
// legacy decoder never does for anything but uppercase letters — so a
// global keybinding keyed on "Shift+?" silently never matched on some
// terminals (e.g. Ghostty) even though pressing "?" is inherently a Shift
// combo on a US keyboard. Shift's effect is already baked into Text itself
// here, so it must not also survive as a modifier.
func TestTranslateKeyStripsShiftFromPunctuationText(t *testing.T) {
	got := translateKey(vaxis.Key{Text: "?", Keycode: '?', Modifiers: vaxis.ModShift, EventType: vaxis.EventPress})
	if got.Mods&layout.ModShift != 0 {
		t.Errorf("expected ModShift to be stripped for a produced punctuation character, got Mods=%v", got.Mods)
	}
	if got.String() != "?" {
		t.Errorf("got String()=%q, want %q", got.String(), "?")
	}
}

// TestDoubleShiftIgnoredWhenTerminalUnfocused guards a real bug: some
// terminals/multiplexers (observed with Ghostty tabs) can deliver a stray
// bare-Shift keypress to a kiwi session running in a tab that isn't even
// the active one, which would otherwise pop the finder open behind the
// user's back. FocusOut must disable the double-shift detector.
func TestDoubleShiftIgnoredWhenTerminalUnfocused(t *testing.T) {
	a := &App{focus: &layout.FocusManager{}, global: map[string]func(){}, focused: false}
	fired := false
	a.SetDoubleShiftHandler(func() { fired = true })

	shiftPress := layout.Key{Named: layout.KeyShift, EventType: layout.EventPress}
	a.handleKey(shiftPress)
	a.handleKey(shiftPress)
	if fired {
		t.Error("expected double-shift to be ignored while the terminal is unfocused")
	}

	a.focused = true
	a.handleKey(shiftPress)
	a.handleKey(shiftPress)
	if !fired {
		t.Error("expected double-shift to fire once the terminal regains focus")
	}
}

// TestTranslateKeyKeepsShiftForNamedKeys is the flip side: Shift+Tab,
// Shift+Left, etc. must keep ModShift, since there Shift changes the key's
// meaning rather than being absorbed into a printed character.
func TestTranslateKeyKeepsShiftForNamedKeys(t *testing.T) {
	got := translateKey(vaxis.Key{Keycode: vaxis.KeyTab, Modifiers: vaxis.ModShift, EventType: vaxis.EventPress})
	if got.Mods&layout.ModShift == 0 {
		t.Errorf("expected ModShift to be preserved for a named key, got Mods=%v", got.Mods)
	}
	if got.String() != "Shift+Tab" {
		t.Errorf("got String()=%q, want %q", got.String(), "Shift+Tab")
	}
}
