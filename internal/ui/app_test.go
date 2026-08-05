package ui

import (
	"testing"

	"go.rockorager.dev/vaxis"

	"github.com/bricejulia/nib/internal/layout"
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
// bare-Shift keypress to a nib session running in a tab that isn't even
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

// OverlayActive is what a background event handler (e.g. the memory
// watchdog's periodic check) uses to avoid popping a modal over whatever
// the user is already looking at — see cmd/nib/main.go's memoryThresholdEvent
// handling.
func TestOverlayActiveReflectsShowAndCloseOverlay(t *testing.T) {
	a := &App{}
	if a.OverlayActive() {
		t.Fatal("expected no overlay active initially")
	}

	a.ShowOverlay(stubView{})
	if !a.OverlayActive() {
		t.Fatal("expected an overlay to be active after ShowOverlay")
	}

	a.CloseOverlay()
	if a.OverlayActive() {
		t.Fatal("expected no overlay active after CloseOverlay")
	}
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

func TestScrollbarRectIsTheRightmostContentColumn(t *testing.T) {
	got, ok := scrollbarRect(layout.Rect{X: 10, Y: 5, W: 30, H: 20})
	if !ok {
		t.Fatal("expected a scrollbar rect")
	}
	// contentRect insets to {11, 6, 28, 18}; the bar is that rect's last column.
	want := layout.Rect{X: 38, Y: 6, W: 1, H: 18}
	if got != want {
		t.Errorf("got %+v, want %+v", got, want)
	}
}

func TestScrollbarRectTooNarrowReturnsFalse(t *testing.T) {
	// contentRect of a 2-wide bordered pane is 0-wide — no room for a bar.
	if _, ok := scrollbarRect(layout.Rect{X: 0, Y: 0, W: 2, H: 20}); ok {
		t.Error("expected no scrollbar rect when the content is too narrow")
	}
}

func TestScrollbarActiveRequiresPositiveTotal(t *testing.T) {
	view := &scrollStubView{state: layout.ScrollState{Viewport: 10, Total: 0}}
	if _, ok := scrollbarActive(view); ok {
		t.Error("expected no active scrollbar when Total is 0 (e.g. no file open)")
	}
	view.state.Total = 50
	if _, ok := scrollbarActive(view); !ok {
		t.Error("expected an active scrollbar once Total > 0")
	}
}

func TestScrollbarActiveFalseForNonScrollableView(t *testing.T) {
	if _, ok := scrollbarActive(stubView{}); ok {
		t.Error("expected no active scrollbar for a View that isn't Scrollable")
	}
}

// scrollStubView is a layout.View that also implements Scrollable and
// ScrollTarget, for exercising beginScrollDrag/continueScrollDrag without a
// real pane.
type scrollStubView struct {
	stubView
	state      layout.ScrollState
	scrolledTo []int // every ScrollTo call, in order
}

func (v *scrollStubView) ScrollState() layout.ScrollState { return v.state }
func (v *scrollStubView) ScrollTo(top int) {
	v.scrolledTo = append(v.scrolledTo, top)
	v.state.Top = top
}

// mouseAppScroll is mouseApp, but for a scrollStubView — kept separate
// since mouseApp's signature takes a layout.View, which would lose the
// concrete type's ScrollTo calls need to be inspected on.
func mouseAppScroll(view *scrollStubView, area layout.Rect) *App {
	leaf := &layout.LeafNode{ID: 1, View: view}
	fm := &layout.FocusManager{}
	fm.Rebuild(leaf)
	return &App{
		root:  leaf,
		focus: fm,
		rects: map[layout.LeafID]layout.Rect{1: area},
	}
}

func TestBeginScrollDragClickOutsideBarColumnFallsThrough(t *testing.T) {
	view := &scrollStubView{state: layout.ScrollState{Viewport: 18, Total: 100}}
	a := mouseAppScroll(view, layout.Rect{X: 0, Y: 0, W: 40, H: 20})

	// Bar column is 38 (see TestScrollbarRectIsTheRightmostContentColumn's
	// math); this click is well inside the text area instead.
	a.handleMouse(vaxis.Mouse{Col: 5, Row: 2, Button: vaxis.MouseLeftButton, EventType: vaxis.EventPress})

	if a.scrollCapture {
		t.Error("a click off the scrollbar column must not start a scroll drag")
	}
	if len(view.scrolledTo) != 0 {
		t.Errorf("expected no ScrollTo calls, got %v", view.scrolledTo)
	}
}

func TestBeginScrollDragOnThumbGrabsWithoutMoving(t *testing.T) {
	state := layout.ScrollState{Top: 0, Viewport: 18, Total: 100}
	view := &scrollStubView{state: state}
	area := layout.Rect{X: 0, Y: 0, W: 40, H: 20}
	a := mouseAppScroll(view, area)

	bar, ok := scrollbarRect(area)
	if !ok {
		t.Fatal("expected a scrollbar rect")
	}
	start, _, show := layout.ThumbBounds(state, state.Viewport)
	if !show {
		t.Fatal("expected a thumb")
	}
	pressRow := bar.Y + start // the thumb's very first row

	a.handleMouse(vaxis.Mouse{Col: bar.X, Row: pressRow, Button: vaxis.MouseLeftButton, EventType: vaxis.EventPress})

	if !a.scrollCapture || !a.hasMouseCapture {
		t.Fatal("expected a scroll drag to be captured")
	}
	if len(view.scrolledTo) != 0 {
		t.Errorf("clicking directly on the thumb should not page/move it, got ScrollTo calls %v", view.scrolledTo)
	}
	if a.scrollDragOffset != 0 {
		t.Errorf("grabbing the thumb's first row should record offset 0, got %d", a.scrollDragOffset)
	}
}

func TestBeginScrollDragOnTrackPagesOnce(t *testing.T) {
	state := layout.ScrollState{Top: 0, Viewport: 18, Total: 100}
	view := &scrollStubView{state: state}
	area := layout.Rect{X: 0, Y: 0, W: 40, H: 20}
	a := mouseAppScroll(view, area)

	bar, _ := scrollbarRect(area)
	start, size, _ := layout.ThumbBounds(state, state.Viewport)
	// A press well below the thumb, still inside the track.
	pressRow := bar.Y + start + size + 2

	a.handleMouse(vaxis.Mouse{Col: bar.X, Row: pressRow, Button: vaxis.MouseLeftButton, EventType: vaxis.EventPress})

	if len(view.scrolledTo) != 1 || view.scrolledTo[0] != state.Top+state.Viewport {
		t.Fatalf("expected one page-down ScrollTo(%d), got %v", state.Top+state.Viewport, view.scrolledTo)
	}
	if !a.scrollCapture {
		t.Error("expected the drag to continue being captured after the page")
	}
}

func TestBeginScrollDragOnTrackAboveThumbPagesUp(t *testing.T) {
	state := layout.ScrollState{Top: 40, Viewport: 18, Total: 100}
	view := &scrollStubView{state: state}
	area := layout.Rect{X: 0, Y: 0, W: 40, H: 20}
	a := mouseAppScroll(view, area)

	bar, _ := scrollbarRect(area)
	start, _, show := layout.ThumbBounds(state, state.Viewport)
	if !show || start == 0 {
		t.Fatal("expected a thumb not already at the top, so there's room above it to click")
	}
	pressRow := bar.Y // the track's very first row, above the thumb

	a.handleMouse(vaxis.Mouse{Col: bar.X, Row: pressRow, Button: vaxis.MouseLeftButton, EventType: vaxis.EventPress})

	if len(view.scrolledTo) != 1 || view.scrolledTo[0] != state.Top-state.Viewport {
		t.Fatalf("expected one page-up ScrollTo(%d), got %v", state.Top-state.Viewport, view.scrolledTo)
	}
}

func TestScrollDragMotionScrollsProportionally(t *testing.T) {
	// Total = 2x Viewport, so the thumb spans exactly half the track and
	// top tracks thumbStart with a clean 2x relationship.
	state := layout.ScrollState{Top: 0, Viewport: 10, Total: 20}
	view := &scrollStubView{state: state}
	area := layout.Rect{X: 0, Y: 0, W: 40, H: 12} // content rect: H=10, matching Viewport
	a := mouseAppScroll(view, area)

	bar, ok := scrollbarRect(area)
	if !ok {
		t.Fatal("expected a scrollbar rect")
	}
	start, _, show := layout.ThumbBounds(state, state.Viewport)
	if !show {
		t.Fatal("expected a thumb")
	}

	// Grab the thumb at its first row (offset 0), then drag it down by 2
	// track rows.
	a.handleMouse(vaxis.Mouse{Col: bar.X, Row: bar.Y + start, Button: vaxis.MouseLeftButton, EventType: vaxis.EventPress})
	if len(view.scrolledTo) != 0 {
		t.Fatalf("grabbing the thumb should not itself scroll, got %v", view.scrolledTo)
	}

	a.handleMouse(vaxis.Mouse{Col: bar.X, Row: bar.Y + start + 2, Button: vaxis.MouseLeftButton, EventType: vaxis.EventMotion})

	if len(view.scrolledTo) != 1 {
		t.Fatalf("expected exactly one ScrollTo from the drag motion, got %v", view.scrolledTo)
	}
	want := layout.ScrollTopForThumbStart(state, state.Viewport, start+2)
	if view.scrolledTo[0] != want {
		t.Errorf("ScrollTo(%d), want %d", view.scrolledTo[0], want)
	}
}

func TestScrollDragReleaseEndsCaptureWithoutMovingWhereReleased(t *testing.T) {
	state := layout.ScrollState{Top: 0, Viewport: 18, Total: 100}
	view := &scrollStubView{state: state}
	area := layout.Rect{X: 0, Y: 0, W: 40, H: 20}
	a := mouseAppScroll(view, area)

	bar, _ := scrollbarRect(area)
	start, _, _ := layout.ThumbBounds(state, state.Viewport)

	a.handleMouse(vaxis.Mouse{Col: bar.X, Row: bar.Y + start, Button: vaxis.MouseLeftButton, EventType: vaxis.EventPress})
	a.handleMouse(vaxis.Mouse{Col: bar.X, Row: bar.Y + start, Button: vaxis.MouseLeftButton, EventType: vaxis.EventRelease})

	if a.scrollCapture || a.hasMouseCapture {
		t.Error("release should end both the scroll capture and the mouse capture")
	}
}

// mouseAndScrollView implements both layout.MouseHandler and
// Scrollable/ScrollTarget (via the embedded *scrollStubView, so its
// pointer-receiver ScrollTo/ScrollState methods are promoted regardless of
// how this type itself is addressed), for
// TestScrollDragNeverReachesTheViewsMouseHandler below.
type mouseAndScrollView struct {
	*scrollStubView
	got []layout.Mouse
}

func (v *mouseAndScrollView) HandleMouse(m layout.Mouse) bool {
	v.got = append(v.got, m)
	return true
}

// TestScrollDragNeverReachesTheViewsMouseHandler guards the core reason
// scrollCapture exists as a separate flag from mouseCapture: a drag begun
// on the scrollbar must never also feed the pane's own MouseHandler (e.g.
// the editor's text-selection drag) underneath it.
func TestScrollDragNeverReachesTheViewsMouseHandler(t *testing.T) {
	view := &mouseAndScrollView{
		scrollStubView: &scrollStubView{state: layout.ScrollState{Viewport: 18, Total: 100}},
	}
	area := layout.Rect{X: 0, Y: 0, W: 40, H: 20}
	leaf := &layout.LeafNode{ID: 1, View: view}
	fm := &layout.FocusManager{}
	fm.Rebuild(leaf)
	a := &App{root: leaf, focus: fm, rects: map[layout.LeafID]layout.Rect{1: area}}

	bar, ok := scrollbarRect(area)
	if !ok {
		t.Fatal("expected a scrollbar rect")
	}

	a.handleMouse(vaxis.Mouse{Col: bar.X, Row: bar.Y, Button: vaxis.MouseLeftButton, EventType: vaxis.EventPress})
	a.handleMouse(vaxis.Mouse{Col: bar.X, Row: bar.Y + 3, Button: vaxis.MouseLeftButton, EventType: vaxis.EventMotion})
	a.handleMouse(vaxis.Mouse{Col: bar.X, Row: bar.Y + 3, Button: vaxis.MouseLeftButton, EventType: vaxis.EventRelease})

	if len(view.got) != 0 {
		t.Errorf("expected the View's own MouseHandler to see nothing during a scrollbar drag, got %+v", view.got)
	}
}

// pasteRecorderView implements layout.Paster, recording each whole paste it
// receives, plus any HandleKey calls it gets alongside them.
type pasteRecorderView struct {
	stubView
	pastes []string
	keys   []layout.Key
}

func (v *pasteRecorderView) HandleKey(k layout.Key) bool {
	v.keys = append(v.keys, k)
	return true
}

func (v *pasteRecorderView) HandlePaste(s string) bool {
	v.pastes = append(v.pastes, s)
	return true
}

func newLeafApp(v layout.View) *App {
	leaf := &layout.LeafNode{ID: 1, View: v}
	fm := &layout.FocusManager{}
	fm.Rebuild(leaf)
	return &App{root: leaf, focus: fm}
}

func TestHandlePasteDeliversWholeStringToAPasterAwareView(t *testing.T) {
	v := &pasteRecorderView{}
	a := newLeafApp(v)

	a.handlePaste("line one\nline two\nline three")

	if len(v.pastes) != 1 || v.pastes[0] != "line one\nline two\nline three" {
		t.Fatalf("expected the paste delivered atomically, got %+v", v.pastes)
	}
	if len(v.keys) != 0 {
		t.Errorf("expected no HandleKey calls for a Paster-aware view, got %+v", v.keys)
	}
}

// nonPasterView is a View that deliberately does NOT implement
// layout.Paster, unlike pasteRecorderView, so handlePaste is forced onto
// its per-character HandleKey fallback.
type nonPasterView struct {
	keys []layout.Key
}

func (v *nonPasterView) Render(layout.Window) {}
func (v *nonPasterView) Title() string        { return "" }
func (v *nonPasterView) HandleKey(k layout.Key) bool {
	v.keys = append(v.keys, k)
	return true
}

func TestHandlePasteFallsBackToPerCharacterKeysWithoutPaster(t *testing.T) {
	v := &nonPasterView{}
	a := newLeafApp(v)

	a.handlePaste("ab\nc")

	want := []layout.Key{
		{Text: "a", Codepoint: 'a'},
		{Text: "b", Codepoint: 'b'},
		{Named: layout.KeyEnter},
		{Text: "c", Codepoint: 'c'},
	}
	if len(v.keys) != len(want) {
		t.Fatalf("got %d keys %+v, want %d %+v", len(v.keys), v.keys, len(want), want)
	}
	for i, k := range want {
		if v.keys[i] != k {
			t.Errorf("key %d = %+v, want %+v", i, v.keys[i], k)
		}
	}
}

// TestAppendPasteKeyHandlesLineEndings is a regression test for the actual
// reported bug: pasted multi-line text collapsing onto one line. vaxis's C0
// decoder only special-cases CR (0x0D, KeyEnter) — a bare LF (0x0A), the
// line ending virtually all non-Windows clipboard text uses, decodes
// identically to a real Ctrl+J keypress (ModCtrl, Keycode 'j', no Text).
// Within a paste that ambiguity is resolvable (see appendPasteKey's doc
// comment), and CRLF must collapse to a single '\n' rather than a blank line.
func TestAppendPasteKeyHandlesLineEndings(t *testing.T) {
	bareLF := vaxis.Key{Keycode: 'j', Modifiers: vaxis.ModCtrl, EventType: vaxis.EventPaste}
	cr := vaxis.Key{Keycode: vaxis.KeyEnter, EventType: vaxis.EventPaste}
	tab := vaxis.Key{Keycode: vaxis.KeyTab, EventType: vaxis.EventPaste}
	letter := func(r rune) vaxis.Key {
		return vaxis.Key{Text: string(r), Keycode: r, EventType: vaxis.EventPaste}
	}

	cases := []struct {
		name string
		keys []vaxis.Key
		want string
	}{
		{
			name: "bare LF line ending (Unix/macOS clipboard)",
			keys: []vaxis.Key{letter('a'), bareLF, letter('b')},
			want: "a\nb",
		},
		{
			name: "CRLF line ending collapses to one newline",
			keys: []vaxis.Key{letter('a'), cr, bareLF, letter('b')},
			want: "a\nb",
		},
		{
			name: "bare CR line ending (old Mac)",
			keys: []vaxis.Key{letter('a'), cr, letter('b')},
			want: "a\nb",
		},
		{
			name: "embedded tab",
			keys: []vaxis.Key{letter('a'), tab, letter('b')},
			want: "a\tb",
		},
	}

	for _, c := range cases {
		t.Run(c.name, func(t *testing.T) {
			a := &App{}
			for _, k := range c.keys {
				a.appendPasteKey(k)
			}
			if got := a.pasteBuf.String(); got != c.want {
				t.Errorf("got %q, want %q", got, c.want)
			}
		})
	}
}

func TestPasteStartEventResetsAnyStalePastedState(t *testing.T) {
	v := &pasteRecorderView{}
	a := newLeafApp(v)
	a.pasteBuf.WriteString("leftover")
	a.pasteCRPending = true

	a.appendPasteKey(vaxis.Key{Keycode: vaxis.KeyEnter, EventType: vaxis.EventPaste})
	a.appendPasteKey(vaxis.Key{Text: "x", Keycode: 'x', EventType: vaxis.EventPaste})

	if got := a.pasteBuf.String(); got != "leftover\nx" {
		t.Fatalf("sanity check on accumulation failed: got %q", got)
	}

	// A fresh PasteStartEvent must discard whatever was buffered before it
	// (e.g. left over from a paste that never sent its End event, or from
	// an unrelated leftover in this test) rather than prepending to it.
	a.pasteBuf.Reset()
	a.pasteCRPending = false
	a.appendPasteKey(vaxis.Key{Text: "y", Keycode: 'y', EventType: vaxis.EventPaste})
	if got := a.pasteBuf.String(); got != "y" {
		t.Errorf("got %q, want %q", got, "y")
	}
}
