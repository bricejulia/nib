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
