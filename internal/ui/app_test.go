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

func TestTranslateKeySpaceIsPromotedToNamedButKeepsText(t *testing.T) {
	got := translateKey(vaxis.Key{Text: " ", Keycode: ' ', EventType: vaxis.EventPress})
	if got.Named != layout.KeySpace {
		t.Errorf("Named = %q, want %q", got.Named, layout.KeySpace)
	}
	if got.Text != " " {
		t.Errorf("expected Text to stay %q so a bare Space still inserts a literal space, got %q", " ", got.Text)
	}
}

// TestTranslateKeyCtrlSpaceAcrossTerminalEncodings is a regression test for a
// real bug: "Ctrl+Space" was registered as an editor binding but silently
// never fired, because terminals encode it in ways that produced "Ctrl+@" or
// "Ctrl+ " instead. Every form a terminal actually sends has to normalize to
// the one trigger string bindings are written against.
func TestTranslateKeyCtrlSpaceAcrossTerminalEncodings(t *testing.T) {
	cases := []struct {
		name string
		in   vaxis.Key
	}{
		{
			// Legacy terminals (including inside tmux) send the NUL byte,
			// which vaxis decodes as Ctrl+@. This is the common case, and
			// the one that was broken.
			name: "legacy NUL byte",
			in:   vaxis.Key{Keycode: '@', Modifiers: vaxis.ModCtrl},
		},
		{
			name: "kitty protocol, keycode only",
			in:   vaxis.Key{Keycode: vaxis.KeySpace, Modifiers: vaxis.ModCtrl},
		},
		{
			name: "keycode with text",
			in:   vaxis.Key{Keycode: vaxis.KeySpace, Text: " ", Modifiers: vaxis.ModCtrl},
		},
	}
	for _, c := range cases {
		t.Run(c.name, func(t *testing.T) {
			c.in.EventType = vaxis.EventPress
			if got := translateKey(c.in).String(); got != "Ctrl+Space" {
				t.Errorf("String() = %q, want %q", got, "Ctrl+Space")
			}
		})
	}
}

func TestTranslateKeyPlainSpaceIsNotConfusedWithCtrlSpace(t *testing.T) {
	// A bare space must stay insertable text and must NOT look like the
	// Ctrl+Space trigger.
	got := translateKey(vaxis.Key{Text: " ", Keycode: ' ', EventType: vaxis.EventPress})
	if got.String() == "Ctrl+Space" {
		t.Fatal("a bare Space must not match the Ctrl+Space trigger")
	}
	if got.Text != " " {
		t.Errorf("Text = %q, want a literal space to remain insertable", got.Text)
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

func TestContentRectInsetsByTheBorder(t *testing.T) {
	// A View draws inside the 1-cell border, so mouse coordinates must be
	// measured against the inset region — a.rects holds the OUTER rect.
	got := contentRect(layout.Rect{X: 10, Y: 5, W: 30, H: 20})
	want := layout.Rect{X: 11, Y: 6, W: 28, H: 18}
	if got != want {
		t.Errorf("got %+v, want %+v", got, want)
	}
}

func TestContentRectOnAPaneTooSmallToBorderIsUnchanged(t *testing.T) {
	// Must stay in step with drawBorder's own bail-out, or a tiny pane's
	// clicks land one cell off.
	for _, r := range []layout.Rect{
		{X: 3, Y: 4, W: 1, H: 20},
		{X: 3, Y: 4, W: 20, H: 1},
		{X: 0, Y: 0, W: 0, H: 0},
	} {
		if got := contentRect(r); got != r {
			t.Errorf("contentRect(%+v) = %+v, want it unchanged", r, got)
		}
	}
}

func TestTranslateMouseCoordinatesAndButtons(t *testing.T) {
	cases := []struct {
		name string
		in   vaxis.MouseButton
		want layout.MouseButton
	}{
		{"left", vaxis.MouseLeftButton, layout.MouseLeft},
		{"middle", vaxis.MouseMiddleButton, layout.MouseMiddle},
		{"right", vaxis.MouseRightButton, layout.MouseRight},
		// A bare motion event reports no button; that's the reading a View
		// needs in order to ignore hover.
		{"none", vaxis.MouseNoButton, layout.MouseNone},
		{"wheel up", vaxis.MouseWheelUp, layout.MouseWheelUp},
		{"wheel down", vaxis.MouseWheelDown, layout.MouseWheelDown},
	}
	for _, c := range cases {
		t.Run(c.name, func(t *testing.T) {
			got := translateMouse(vaxis.Mouse{Col: 7, Row: 3, Button: c.in})
			if got.Button != c.want {
				t.Errorf("Button = %v, want %v", got.Button, c.want)
			}
			// vaxis already reports 0-based cells, so there is no
			// off-by-one to undo here.
			if got.Col != 7 || got.Row != 3 {
				t.Errorf("Col,Row = %d,%d, want 7,3", got.Col, got.Row)
			}
		})
	}
}

func TestTranslateMouseEventTypes(t *testing.T) {
	cases := []struct {
		in   vaxis.EventType
		want layout.EventType
	}{
		{vaxis.EventPress, layout.EventPress},
		{vaxis.EventRelease, layout.EventRelease},
		{vaxis.EventMotion, layout.EventMotion},
	}
	for _, c := range cases {
		if got := translateMouse(vaxis.Mouse{EventType: c.in}); got.EventType != c.want {
			t.Errorf("EventType %v translated to %v, want %v", c.in, got.EventType, c.want)
		}
	}
}

func TestTranslateMouseModifiers(t *testing.T) {
	got := translateMouse(vaxis.Mouse{Modifiers: vaxis.ModShift | vaxis.ModCtrl})
	if got.Mods&layout.ModShift == 0 {
		t.Error("expected ModShift")
	}
	if got.Mods&layout.ModCtrl == 0 {
		t.Error("expected ModCtrl")
	}
	if got.Mods&layout.ModAlt != 0 {
		t.Error("did not expect ModAlt")
	}
}

func TestCountPressReportsSingleDoubleThenTripleClick(t *testing.T) {
	a := &App{}
	for i, want := range []int{1, 2, 3} {
		if got := a.countPress(4, 9); got != want {
			t.Errorf("press %d: got %d clicks, want %d", i+1, got, want)
		}
	}
}

func TestCountPressCapsAtTripleClick(t *testing.T) {
	// Nothing binds a quadruple click; wrapping back to 1 is what most
	// editors do.
	a := &App{}
	a.countPress(4, 9)
	a.countPress(4, 9)
	a.countPress(4, 9)
	if got := a.countPress(4, 9); got != 1 {
		t.Errorf("fourth press got %d clicks, want the run to restart at 1", got)
	}
}

func TestCountPressRestartsWhenThePointerMoves(t *testing.T) {
	// Two clicks in different cells are two single clicks, however fast.
	a := &App{}
	a.countPress(4, 9)
	if got := a.countPress(5, 9); got != 1 {
		t.Errorf("got %d clicks after moving one cell, want 1", got)
	}
}

func TestCountPressRestartsAfterTheMultiClickWindow(t *testing.T) {
	a := &App{}
	a.countPress(4, 9)
	// Backdate the previous press past the window rather than sleeping.
	a.lastPressAt = a.lastPressAt.Add(-2 * multiClickWindow)
	if got := a.countPress(4, 9); got != 1 {
		t.Errorf("got %d clicks after the window lapsed, want 1", got)
	}
}

// mouseStubView records the mouse events it is given and reports whether it
// consumed them, so routing can be asserted without a live terminal.
type mouseStubView struct {
	stubView
	got      []layout.Mouse
	keys     []layout.Key
	consumes bool
}

func (v *mouseStubView) HandleKey(k layout.Key) bool {
	v.keys = append(v.keys, k)
	return false
}

func (v *mouseStubView) HandleMouse(m layout.Mouse) bool {
	v.got = append(v.got, m)
	return v.consumes
}

// mouseApp builds an App around a single full-screen leaf holding view, with
// rects already computed as a frame would have left them.
func mouseApp(view layout.View, area layout.Rect) *App {
	leaf := &layout.LeafNode{ID: 1, View: view}
	fm := &layout.FocusManager{}
	fm.Rebuild(leaf)
	return &App{
		root:  leaf,
		focus: fm,
		rects: map[layout.LeafID]layout.Rect{1: area},
	}
}

func TestHandleMouseDeliversPaneRelativeCoordinates(t *testing.T) {
	// The pane is offset AND bordered, so both corrections have to apply.
	view := &mouseStubView{consumes: true}
	a := mouseApp(view, layout.Rect{X: 10, Y: 5, W: 30, H: 20})

	a.handleMouse(vaxis.Mouse{Col: 15, Row: 8, Button: vaxis.MouseLeftButton, EventType: vaxis.EventPress})

	if len(view.got) != 1 {
		t.Fatalf("got %d events, want 1", len(view.got))
	}
	// X 10 + 1 border = content starts at 11, so col 15 is pane column 4.
	if view.got[0].Col != 4 || view.got[0].Row != 2 {
		t.Errorf("Col,Row = %d,%d, want 4,2", view.got[0].Col, view.got[0].Row)
	}
}

func TestHandleMouseUnconsumedWheelStillScrolls(t *testing.T) {
	// Regression guard on the behaviour that existed before Views could see
	// the mouse at all: a wheel tick the View declines becomes Up/Down keys.
	view := &mouseStubView{consumes: false}
	a := mouseApp(view, layout.Rect{X: 0, Y: 0, W: 40, H: 20})

	a.handleMouse(vaxis.Mouse{Col: 5, Row: 5, Button: vaxis.MouseWheelDown, EventType: vaxis.EventPress})

	if len(view.keys) != wheelScrollLines {
		t.Fatalf("got %d key presses, want %d", len(view.keys), wheelScrollLines)
	}
	for _, k := range view.keys {
		if k.Named != layout.KeyDown {
			t.Errorf("got key %q, want %q", k.Named, layout.KeyDown)
		}
	}
}

func TestHandleMouseConsumedWheelDoesNotAlsoScroll(t *testing.T) {
	view := &mouseStubView{consumes: true}
	a := mouseApp(view, layout.Rect{X: 0, Y: 0, W: 40, H: 20})

	a.handleMouse(vaxis.Mouse{Col: 5, Row: 5, Button: vaxis.MouseWheelDown, EventType: vaxis.EventPress})

	if len(view.keys) != 0 {
		t.Errorf("got %d key presses, want 0 — a consumed wheel must not scroll twice", len(view.keys))
	}
}

func TestHandleMouseCaptureKeepsDeliveringOutsideThePane(t *testing.T) {
	// Dragging a selection off the pane must keep extending it rather than
	// stopping the moment the pointer crosses out — see App.mouseCapture.
	view := &mouseStubView{consumes: true}
	a := mouseApp(view, layout.Rect{X: 0, Y: 0, W: 40, H: 10})

	a.handleMouse(vaxis.Mouse{Col: 5, Row: 5, Button: vaxis.MouseLeftButton, EventType: vaxis.EventPress})
	// Row 50 is outside every rect, so without capture leafAt would find
	// nothing and the event would be dropped.
	a.handleMouse(vaxis.Mouse{Col: 5, Row: 50, Button: vaxis.MouseLeftButton, EventType: vaxis.EventMotion})

	if len(view.got) != 2 {
		t.Fatalf("got %d events, want 2 — the drag should still be delivered", len(view.got))
	}
	// The out-of-bounds row is passed through, not clamped: that is the
	// signal a View uses to auto-scroll.
	if view.got[1].Row != 49 {
		t.Errorf("Row = %d, want 49 (50 minus the 1-cell border)", view.got[1].Row)
	}
}

func TestHandleMouseReleaseEndsTheCapture(t *testing.T) {
	view := &mouseStubView{consumes: true}
	a := mouseApp(view, layout.Rect{X: 0, Y: 0, W: 40, H: 10})

	a.handleMouse(vaxis.Mouse{Col: 5, Row: 5, Button: vaxis.MouseLeftButton, EventType: vaxis.EventPress})
	a.handleMouse(vaxis.Mouse{Col: 5, Row: 5, Button: vaxis.MouseLeftButton, EventType: vaxis.EventRelease})

	if a.hasMouseCapture {
		t.Error("release should end the capture")
	}
}

func TestHandleMouseOutsideEveryPaneIsDropped(t *testing.T) {
	view := &mouseStubView{consumes: true}
	a := mouseApp(view, layout.Rect{X: 0, Y: 0, W: 10, H: 10})

	a.handleMouse(vaxis.Mouse{Col: 99, Row: 99, Button: vaxis.MouseLeftButton, EventType: vaxis.EventPress})

	if len(view.got) != 0 {
		t.Errorf("got %d events, want 0 for a click on no pane", len(view.got))
	}
}

func TestHandleMousePassesTheClickCountThrough(t *testing.T) {
	view := &mouseStubView{consumes: true}
	a := mouseApp(view, layout.Rect{X: 0, Y: 0, W: 40, H: 20})

	press := vaxis.Mouse{Col: 5, Row: 5, Button: vaxis.MouseLeftButton, EventType: vaxis.EventPress}
	a.handleMouse(press)
	a.handleMouse(press)

	if len(view.got) != 2 {
		t.Fatalf("got %d events, want 2", len(view.got))
	}
	if view.got[0].Clicks != 1 || view.got[1].Clicks != 2 {
		t.Errorf("Clicks = %d then %d, want 1 then 2", view.got[0].Clicks, view.got[1].Clicks)
	}
}

func TestTranslateStyleCarriesTheBackgroundThrough(t *testing.T) {
	// translateStyle used to drop the background entirely, which is what
	// forced search highlighting onto reverse video.
	got := translateStyle(layout.Style{Background: layout.ColorBrightBlack})
	if got.Background != translateColor(layout.ColorBrightBlack) {
		t.Errorf("Background = %v, want the translated bright black", got.Background)
	}
}

func TestTranslateStyleDefaultBackgroundStaysDefault(t *testing.T) {
	got := translateStyle(layout.Style{Foreground: layout.ColorRed})
	if got.Background != vaxis.ColorDefault {
		t.Errorf("Background = %v, want ColorDefault", got.Background)
	}
}
