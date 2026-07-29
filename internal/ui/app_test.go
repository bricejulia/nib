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

type stubView struct{}

func (stubView) Render(layout.Window)      {}
func (stubView) HandleKey(layout.Key) bool { return false }
func (stubView) Title() string             { return "" }

func twoLeafTree() layout.Node {
	return &layout.SplitNode{Dir: layout.Horizontal, Children: []layout.Child{
		{Node: &layout.LeafNode{ID: 1, View: stubView{}}, Hint: layout.Ratio(1)},
		{Node: &layout.LeafNode{ID: 2, View: stubView{}}, Hint: layout.Ratio(1)},
	}}
}

func TestFocusedLeafReflectsCurrentFocus(t *testing.T) {
	fm := &layout.FocusManager{}
	fm.Rebuild(twoLeafTree())
	a := &App{focus: fm}

	id, ok := a.FocusedLeaf()
	if !ok || id != 1 {
		t.Fatalf("got id=%v ok=%v, want 1, true", id, ok)
	}
}

// TestNotifyFocusChangeFiresOnRealChange guards the mechanism split-view
// panes rely on to track "the last editor pane that genuinely had focus"
// (needed because a file-tree row's OnOpen callback runs while focus is
// still on the file tree, not yet on the editor pane it's handing off to).
func TestNotifyFocusChangeFiresOnRealChange(t *testing.T) {
	fm := &layout.FocusManager{}
	fm.Rebuild(twoLeafTree())
	a := &App{focus: fm}

	var got []layout.LeafID
	a.SetFocusChangeHandler(func(id layout.LeafID) { got = append(got, id) })

	prev, hadPrev := fm.Focused() // leaf 1, the default
	fm.FocusAt(2)
	a.notifyFocusChange(prev, hadPrev)

	if len(got) != 1 || got[0] != 2 {
		t.Fatalf("expected exactly one callback with id=2, got %+v", got)
	}
}

func TestNotifyFocusChangeSkipsWhenUnchanged(t *testing.T) {
	fm := &layout.FocusManager{}
	fm.Rebuild(twoLeafTree())
	a := &App{focus: fm}

	called := false
	a.SetFocusChangeHandler(func(layout.LeafID) { called = true })

	prev, hadPrev := fm.Focused()
	a.notifyFocusChange(prev, hadPrev) // focus never actually moved
	if called {
		t.Error("expected no callback when focus didn't change")
	}
}
