// Package ui is the single seam where the terminal-independent layout
// package meets the real terminal (vaxis). Nothing outside this package
// imports vaxis.
package ui

import (
	"strings"
	"time"

	"go.rockorager.dev/vaxis"

	"github.com/bricejulia/nib/internal/clipboard"
	"github.com/bricejulia/nib/internal/debuglog"
	"github.com/bricejulia/nib/internal/layout"
	"github.com/bricejulia/nib/internal/theme"
)

// vaxisWindow adapts a vaxis.Window to layout.Window.
type vaxisWindow struct{ win vaxis.Window }

func (w vaxisWindow) Size() (int, int) { return w.win.Size() }

func (w vaxisWindow) Println(row int, segs ...layout.Segment) {
	vsegs := make([]vaxis.Segment, len(segs))
	for i, s := range segs {
		vsegs[i] = vaxis.Segment{Text: s.Text, Style: translateStyle(s.Style)}
	}
	w.win.Println(row, vsegs...)
}

func (w vaxisWindow) Clear() { w.win.Clear() }

func translateStyle(s layout.Style) vaxis.Style {
	var attr vaxis.AttributeMask
	if s.Attr&layout.AttrBold != 0 {
		attr |= vaxis.AttrBold
	}
	if s.Attr&layout.AttrDim != 0 {
		attr |= vaxis.AttrDim
	}
	if s.Attr&layout.AttrReverse != 0 {
		attr |= vaxis.AttrReverse
	}
	return vaxis.Style{
		Attribute:  attr,
		Foreground: translateColor(s.Foreground),
		Background: translateColor(s.Background),
	}
}

// translateColor maps layout.Color to a vaxis indexed color by its actual
// standard-ANSI index (0-15), rather than vaxis's own named constants —
// vaxis names those constants after literal color swatches (e.g.
// vaxis.ColorRed is index 9, the *bright* red; the plain/dark red at index
// 1 is vaxis.ColorMaroon), which doesn't line up with layout.Color's
// ANSI-semantic names.
func translateColor(c layout.Color) vaxis.Color {
	if c == layout.ColorDefault {
		return vaxis.ColorDefault
	}
	return vaxis.IndexColor(uint8(c - 1))
}

func translateKey(k vaxis.Key) layout.Key {
	var mods layout.ModMask
	if k.Modifiers&vaxis.ModShift != 0 {
		mods |= layout.ModShift
	}
	if k.Modifiers&vaxis.ModAlt != 0 {
		mods |= layout.ModAlt
	}
	if k.Modifiers&vaxis.ModCtrl != 0 {
		mods |= layout.ModCtrl
	}
	if k.Modifiers&vaxis.ModSuper != 0 {
		mods |= layout.ModSuper
	}

	named := namedKey(k.Keycode)
	if named == "" {
		named = namedSpace(k)
	}
	if named == "" && k.Text != "" {
		// Shift's effect on a produced character (case for letters, the
		// shifted symbol for punctuation — e.g. "?" for Shift+/) is
		// already baked into Text itself, and terminals are inconsistent
		// about ALSO reporting ModShift alongside it: vaxis's own legacy
		// decoder only sets ModShift for uppercase letters, never for
		// shifted punctuation, so a binding keyed on "Shift+?" would
		// silently never match on some terminals while "?" alone always
		// would. Named keys (Shift+Tab, Shift+Left, ...) keep their
		// modifier: there Shift changes the key's MEANING, not its
		// printed form, so it stays significant.
		mods &^= layout.ModShift
	}

	et := layout.EventPress
	switch k.EventType {
	case vaxis.EventRepeat:
		et = layout.EventRepeat
	case vaxis.EventRelease:
		et = layout.EventRelease
	default:
		// vaxis.EventPress (already the default above), plus EventMotion/
		// EventPaste which never apply to a key event.
	}

	return layout.Key{
		Text:      k.Text,
		Codepoint: k.Keycode,
		Named:     named,
		Mods:      mods,
		EventType: et,
	}
}

// namedKey maps vaxis's special-key rune constants (values outside the
// valid unicode range) to a portable name. Ordinary printable keys return
// "".
func namedKey(keycode rune) string {
	switch keycode {
	case vaxis.KeyUp:
		return layout.KeyUp
	case vaxis.KeyDown:
		return layout.KeyDown
	case vaxis.KeyLeft:
		return layout.KeyLeft
	case vaxis.KeyRight:
		return layout.KeyRight
	case vaxis.KeyEnter:
		return layout.KeyEnter
	case vaxis.KeyTab:
		return layout.KeyTab
	case vaxis.KeyEsc:
		return layout.KeyEsc
	case vaxis.KeyPgUp:
		return layout.KeyPageUp
	case vaxis.KeyPgDown:
		return layout.KeyPageDown
	case vaxis.KeyHome:
		return layout.KeyHome
	case vaxis.KeyEnd:
		return layout.KeyEnd
	case vaxis.KeyBackspace:
		return layout.KeyBackspace
	case vaxis.KeyLeftShift, vaxis.KeyRightShift:
		return layout.KeyShift
	default:
		return ""
	}
}

// namedSpace recognizes the space bar, which needs special handling because
// — unlike Enter/Tab/Esc — it has no special sentinel keycode, and because
// terminals encode it three incompatible ways depending on modifiers and
// protocol. Returns "" for anything that isn't a space.
//
// Getting this wrong is not hypothetical: a "Ctrl+Space" binding silently
// never fired on any terminal until all three forms below were handled.
func namedSpace(k vaxis.Key) string {
	switch {
	case k.Text == " ":
		// A plain (or shifted) space bar press: reported as ordinary
		// printable text. Text is deliberately left intact by the caller,
		// so an unmodified Space still inserts a literal space — only the
		// portable Named spelling is added on top.
		return layout.KeySpace
	case k.Keycode == vaxis.KeySpace:
		// Space with a modifier under the kitty keyboard protocol: a real
		// 0x20 keycode, but no Text (the modifier suppresses it).
		return layout.KeySpace
	case k.Modifiers&vaxis.ModCtrl != 0 && k.Keycode == '@' && k.Text == "":
		// Ctrl+Space on a legacy terminal, which is the common case
		// (including inside tmux). It transmits the NUL byte, and vaxis
		// decodes NUL as Ctrl+@ — the two are literally the same byte on
		// the wire, so they cannot be told apart. Reporting it as Space is
		// the useful reading: Ctrl+Space is a real binding people press,
		// while Ctrl+@ is not something nib binds at all.
		return layout.KeySpace
	default:
		return ""
	}
}

// translateMouse converts a vaxis.Mouse into layout's terminal-independent
// form, the mirror of translateKey. Col/Row are left in SCREEN space here —
// making them pane-relative needs the leaf's rect, which only handleMouse
// knows.
//
// vaxis reports coordinates 0-based already (it subtracts the 1 the SGR
// encoding uses), so there is no off-by-one to undo.
func translateMouse(m vaxis.Mouse) layout.Mouse {
	var mods layout.ModMask
	if m.Modifiers&vaxis.ModShift != 0 {
		mods |= layout.ModShift
	}
	if m.Modifiers&vaxis.ModAlt != 0 {
		mods |= layout.ModAlt
	}
	if m.Modifiers&vaxis.ModCtrl != 0 {
		mods |= layout.ModCtrl
	}

	et := layout.EventPress
	switch m.EventType {
	case vaxis.EventRelease:
		et = layout.EventRelease
	case vaxis.EventMotion:
		et = layout.EventMotion
	default:
		// vaxis.EventPress (already the default above), plus EventRepeat/
		// EventPaste which never apply to a mouse event.
	}

	return layout.Mouse{
		Col:       m.Col,
		Row:       m.Row,
		Button:    translateMouseButton(m.Button),
		EventType: et,
		Mods:      mods,
	}
}

// translateMouseButton maps vaxis's button encoding — which uses the raw
// terminal wire values, so the wheel lands at 64-67 rather than following on
// from the three real buttons — onto layout's dense enum.
func translateMouseButton(b vaxis.MouseButton) layout.MouseButton {
	switch b {
	case vaxis.MouseLeftButton:
		return layout.MouseLeft
	case vaxis.MouseMiddleButton:
		return layout.MouseMiddle
	case vaxis.MouseRightButton:
		return layout.MouseRight
	case vaxis.MouseWheelUp:
		return layout.MouseWheelUp
	case vaxis.MouseWheelDown:
		return layout.MouseWheelDown
	case vaxis.MouseWheelLeft:
		return layout.MouseWheelLeft
	case vaxis.MouseWheelRight:
		return layout.MouseWheelRight
	default:
		// vaxis.MouseNoButton, plus any button 8-11 nib doesn't bind: a
		// motion event with nothing held reports MouseNoButton, and that is
		// the reading a View needs in order to ignore bare hover.
		return layout.MouseNone
	}
}

// contentRect returns the region of leaf rect r that a View actually draws
// into: r itself when the pane is too small to be bordered, otherwise r
// inset by the 1-cell border.
//
// It exists so renderNode and handleMouse cannot disagree. renderNode used
// to inline this inset, while a.rects stores only the OUTER rect — a mouse
// handler that read a.rects directly would be off by one in both axes on
// every pane, which is exactly the kind of bug that looks like "selection is
// slightly wrong" rather than "mouse is broken". Deliberately a pure
// function of r rather than a map filled in during render, so mouse handling
// does not depend on a frame having been drawn first.
//
// The size test must stay in step with drawBorder's own bail-out.
func contentRect(r layout.Rect) layout.Rect {
	if r.W < 2 || r.H < 2 {
		return r
	}
	return layout.Rect{X: r.X + 1, Y: r.Y + 1, W: r.W - 2, H: r.H - 2}
}

// scrollbarActive reports whether v currently has a scrollbar reserved:
// true only when v implements layout.Scrollable AND presently has content
// (state.Total > 0, e.g. a file is actually open). Shared by the render
// helpers (to decide whether to narrow the View's window by one column)
// and handleMouse (to decide whether the rightmost column is chrome rather
// than pane content), so the two can never disagree about whether the
// column exists — the same discipline contentRect enforces for the border.
func scrollbarActive(v layout.View) (layout.ScrollState, bool) {
	sv, ok := v.(layout.Scrollable)
	if !ok {
		return layout.ScrollState{}, false
	}
	state := sv.ScrollState()
	if state.Total <= 0 {
		return layout.ScrollState{}, false
	}
	return state, true
}

// scrollbarRect returns the 1-cell-wide screen column a leaf's scrollbar
// occupies — the rightmost column of its content rect — or ok=false if the
// content rect is too narrow to spare one (mirroring contentRect's own
// too-small bail-out). Pure, like contentRect, for the same reason: mouse
// hit-testing must not depend on a frame having been rendered first.
func scrollbarRect(r layout.Rect) (bar layout.Rect, ok bool) {
	content := contentRect(r)
	if content.W < 2 {
		return layout.Rect{}, false
	}
	return layout.Rect{X: content.X + content.W - 1, Y: content.Y, W: 1, H: content.H}, true
}

// App owns the vaxis terminal, the window tree, and the focus manager. It
// is the only place a vaxis event is translated into the layout package's
// terminal-independent Key type, and the only place layout.Compute's
// output is turned into real sub-windows.
type App struct {
	vx            *vaxis.Vaxis
	root          layout.Node
	focus         *layout.FocusManager
	global        map[string]func()
	rects         map[layout.LeafID]layout.Rect
	quit          bool
	onCustomEvent func(ev interface{})

	// overlay, when non-nil, is rendered as a centered modal on top of
	// the whole tree, and captures ALL keyboard/mouse input exclusively
	// until closed — see ShowOverlay/CloseOverlay.
	overlay layout.View

	// clip writes to the system clipboard, by a mechanism resolved once in
	// NewApp — see internal/clipboard for why it isn't simply OSC 52.
	clip *clipboard.Writer

	onDoubleShift  func()
	lastShiftPress time.Time

	// mouseCapture is the leaf that owns the mouse for the duration of a
	// drag: set when a button goes down over it, cleared on release. While
	// it is set, motion and release events go there rather than to whatever
	// leafAt says is under the pointer NOW.
	//
	// Without this, dragging a selection off the bottom of the editor pane
	// would silently stop extending it the moment the pointer crossed into
	// the status bar — and worse, start feeding events to a pane the user
	// isn't interacting with. Capture is also what lets a View see an
	// out-of-bounds row and decide to auto-scroll.
	mouseCapture    layout.LeafID
	hasMouseCapture bool

	// scrollCapture is set alongside mouseCapture/hasMouseCapture for the
	// duration of a scrollbar click-or-drag (see beginScrollDrag), so
	// motion and release events for that same capture are routed to the
	// scrollbar logic in continueScrollDrag rather than to the pane's own
	// MouseHandler or the wheel fallback — a drag started on the bar must
	// never also feed the editor's text-selection drag underneath it.
	// scrollDragOffset is the row, within the thumb, where the drag was
	// grabbed (see beginScrollDrag), so subsequent motion drags the thumb
	// from that same relative point rather than snapping its top edge to
	// the pointer.
	scrollCapture    bool
	scrollDragOffset int

	// lastPress tracks the previous mouse press so consecutive presses in
	// the same cell within multiClickWindow can be reported as a double or
	// triple click (layout.Mouse.Clicks). Counted here, not per-View, so no
	// View needs wall-clock state of its own — the same split as
	// lastShiftPress above.
	lastPressAt   time.Time
	lastPressCol  int
	lastPressRow  int
	lastPressRuns int

	// onFocusChange, if set, is called whenever the focused leaf actually
	// changes (Tab-cycle, mouse click, or any FocusLeaf call) — see
	// SetFocusChangeHandler.
	onFocusChange func(id layout.LeafID)

	// focused tracks whether the terminal (not just some pane within it)
	// currently has OS-level focus, via vaxis.FocusIn/FocusOut — vaxis
	// requests this reporting from the terminal unconditionally at
	// startup. It guards the double-shift detector specifically: some
	// terminals/multiplexers (observed with Ghostty tabs) can still
	// deliver a stray bare-Shift keypress to an unfocused tab, which would
	// otherwise pop the finder open in a session the user isn't even
	// looking at.
	focused bool

	// pasteBuf accumulates the text of a bracketed paste in progress,
	// between a vaxis.PasteStartEvent and its matching PasteEndEvent — see
	// appendPasteKey. Keeping it on App (rather than e.g. a local in Run)
	// is what lets pasteCRPending survive across the many individual
	// vaxis.Key events that make up one paste.
	pasteBuf strings.Builder

	// pasteCRPending is true immediately after a CR (vaxis.KeyEnter) has
	// been appended to pasteBuf as '\n', so the very next key can tell a
	// CRLF pair's trailing LF apart from a real bare-LF line ending — both
	// arrive from vaxis identically decoded as Ctrl+j (see appendPasteKey).
	// Without this, pasted CRLF text would show up double-spaced.
	pasteCRPending bool
}

// doubleShiftWindow is the maximum gap between two bare Shift presses for
// them to count as a double-tap.
const doubleShiftWindow = 400 * time.Millisecond

// NewApp opens the terminal and constructs an App around root. global is
// consulted for any key not consumed by the focused View; callers should
// include a binding that calls (*App).Quit, e.g. global["Ctrl+c"] = app.Quit.
func NewApp(root layout.Node, global map[string]func()) (*App, error) {
	vx, err := vaxis.New(vaxis.Options{})
	if err != nil {
		return nil, err
	}
	fm := &layout.FocusManager{}
	fm.Rebuild(root)
	// Resolved once here rather than per copy, so a copy is a single exec and
	// never a PATH scan. Logged because which mechanism is in play decides
	// whether a failed copy can even be detected — see clipboard.Writer.Copy.
	clip := clipboard.New(vx.ClipboardPush)
	debuglog.Info("clipboard: using %s", clip.Mechanism())
	// Assumed focused until told otherwise: a terminal that doesn't
	// support focus reporting at all will simply never send FocusOut, and
	// starting out unfocused would otherwise leave a non-reporting
	// terminal's session permanently unresponsive to the double-shift
	// detector.
	return &App{vx: vx, root: root, focus: fm, global: global, focused: true, clip: clip}, nil
}

// Close tears down the terminal. Callers must defer this after NewApp
// succeeds.
func (a *App) Close() {
	a.vx.Close()
}

// Quit signals Run to stop once the current event finishes processing.
func (a *App) Quit() {
	a.quit = true
}

// SetGlobalKeymap replaces the global keymap consulted when the focused
// View does not consume a key.
func (a *App) SetGlobalKeymap(global map[string]func()) {
	a.global = global
}

// CycleFocusPrev moves focus to the previous leaf, wrapping around. It is
// intended to be bound in the global keymap (e.g. under "Shift+Tab").
func (a *App) CycleFocusPrev() {
	a.focus.Prev()
}

// CycleFocusNext moves focus to the next leaf, wrapping around. It is
// intended to be bound in the global keymap (e.g. under "Tab").
func (a *App) CycleFocusNext() {
	a.focus.Next()
}

// FocusLeaf moves focus directly to leaf id — e.g. so opening a file from
// the file tree can hand focus to the editor pane immediately.
func (a *App) FocusLeaf(id layout.LeafID) {
	a.focus.FocusAt(id)
}

// FocusedLeaf returns the currently focused leaf's ID, or ok=false if
// nothing is focused (an empty tree).
func (a *App) FocusedLeaf() (layout.LeafID, bool) {
	return a.focus.Focused()
}

// SetFocusChangeHandler registers fn to be called whenever the focused
// leaf actually changes, for any reason (Tab-cycle, mouse click, or any
// FocusLeaf call) — e.g. so a caller managing multiple interchangeable
// panes of the same kind (split editor panes) can track which one was
// last genuinely focused, since that's not always recoverable from
// FocusedLeaf alone at the moment it's needed (a callback fired from a
// file-tree row's own key handler runs while focus is still on the file
// tree, not yet on whatever editor pane it's about to hand off to).
func (a *App) SetFocusChangeHandler(fn func(id layout.LeafID)) {
	a.onFocusChange = fn
}

// notifyFocusChange calls onFocusChange iff the focused leaf differs from
// prevFocus (hadPrev is false if there was no defined "before" focus,
// e.g. an empty tree). Factored out of Run() so it's independently
// testable without a live vaxis event loop.
func (a *App) notifyFocusChange(prevFocus layout.LeafID, hadPrev bool) {
	if a.onFocusChange == nil {
		return
	}
	newFocus, ok := a.focus.Focused()
	if !ok || (hadPrev && newFocus == prevFocus) {
		return
	}
	a.onFocusChange(newFocus)
}

// SetCustomEventHandler registers fn to handle any event that isn't a
// terminal Resize/Key/Mouse — e.g. an event injected via Post from an
// external source such as a filesystem watcher. Delivering external events
// through Post rather than a separate channel keeps everything, including
// the redraw that follows, on the single event-loop goroutine.
func (a *App) SetCustomEventHandler(fn func(ev interface{})) {
	a.onCustomEvent = fn
}

// Post injects an external event into the terminal's event loop so it is
// processed on the same goroutine as terminal input, then triggers a
// redraw.
func (a *App) Post(ev interface{}) {
	a.vx.PostEvent(ev)
}

// SuspendAndRun takes the terminal out of fullscreen mode, runs fn (e.g.
// to shell out to an interactive subprocess like $EDITOR that needs the
// real terminal to itself), then restores nib's own fullscreen state
// regardless of whether fn succeeded. Run's event loop redraws
// unconditionally after the key handler that calls this returns, so no
// explicit redraw is needed here.
func (a *App) SuspendAndRun(fn func() error) error {
	if err := a.vx.Suspend(); err != nil {
		return err
	}
	defer func() { _ = a.vx.Resume() }()
	return fn()
}

// CopyToClipboard puts s on the system clipboard, by whichever mechanism
// internal/clipboard resolved at startup — a native helper (pbcopy and
// friends) when nib is local, OSC 52 when it is not. See that package for
// why neither works everywhere on its own.
//
// This used to call vaxis's ClipboardPush directly, i.e. OSC 52
// unconditionally, and that silently did nothing under tmux's default
// set-clipboard=external, which ignores the sequence when an application
// sends it. A copy that fails invisibly is the worst possible outcome, hence
// both the helper preference and the warning below.
//
// Panes reach this through a func field wired in cmd/nib/main.go, never by
// importing this package.
func (a *App) CopyToClipboard(s string) {
	if err := a.clip.Copy(s); err != nil {
		debuglog.Warn("clipboard: %v", err)
	}
}

// Rebuild recomputes focus traversal order after the tree's leaves change
// (e.g. a new panel is opened). It is a no-op for Step 0, which has a fixed
// two-pane tree, but future steps that open new panels need it.
func (a *App) Rebuild() {
	a.focus.Rebuild(a.root)
}

// ShowOverlay displays v as a centered modal on top of the whole window
// tree. While an overlay is showing, ALL keyboard and mouse input goes to
// it exclusively — normal pane focus and the global keymap are bypassed
// entirely, which is what makes it modal. v is responsible for closing
// itself (e.g. on Esc) by calling CloseOverlay.
func (a *App) ShowOverlay(v layout.View) {
	a.overlay = v
}

// CloseOverlay dismisses the current overlay, if any, restoring normal
// pane input.
func (a *App) CloseOverlay() {
	a.overlay = nil
}

// SetDoubleShiftHandler registers fn to run when a bare Shift key is
// pressed twice within doubleShiftWindow, with no overlay already open.
// This only fires on terminals that report standalone modifier keypresses
// (the kitty keyboard protocol's "report all keys" mode) — not every
// terminal does, so callers should also bind a conventional fallback key
// (e.g. Ctrl+P) to the same action via the global keymap.
func (a *App) SetDoubleShiftHandler(fn func()) {
	a.onDoubleShift = fn
}

// Run drives the event loop until Quit is called or the terminal closes.
func (a *App) Run() error {
	for ev := range a.vx.Events() {
		prevFocus, hadPrev := a.focus.Focused()
		switch e := ev.(type) {
		case vaxis.Resize:
			a.vx.Resize(e)
		case vaxis.PasteStartEvent:
			a.pasteBuf.Reset()
			a.pasteCRPending = false
		case vaxis.PasteEndEvent:
			if s := a.pasteBuf.String(); s != "" {
				a.handlePaste(s)
			}
			a.pasteBuf.Reset()
			a.pasteCRPending = false
		case vaxis.Key:
			if e.EventType == vaxis.EventPaste {
				a.appendPasteKey(e)
			} else {
				a.handleKey(translateKey(e))
			}
		case vaxis.Mouse:
			if a.overlay == nil {
				a.handleMouse(e)
			}
		case vaxis.FocusIn:
			a.focused = true
		case vaxis.FocusOut:
			a.focused = false
		default:
			if a.onCustomEvent != nil {
				a.onCustomEvent(ev)
			}
		}

		a.notifyFocusChange(prevFocus, hadPrev)
		a.render()

		if a.quit {
			return nil
		}
	}
	return nil
}

func (a *App) handleKey(k layout.Key) {
	if a.focused && a.overlay == nil && k.EventType == layout.EventPress && k.Named == layout.KeyShift {
		now := time.Now()
		if !a.lastShiftPress.IsZero() && now.Sub(a.lastShiftPress) <= doubleShiftWindow {
			a.lastShiftPress = time.Time{}
			if a.onDoubleShift != nil {
				a.onDoubleShift()
			}
			return
		}
		a.lastShiftPress = now
	}

	if a.overlay != nil {
		a.overlay.HandleKey(k)
		return
	}
	layout.Dispatch(k, a.focus.FocusedView(), a.global)
}

// appendPasteKey accumulates one key of an in-progress bracketed paste (see
// the vaxis.PasteStartEvent/PasteEndEvent cases in Run) into a.pasteBuf.
//
// It exists because a pasted line ending cannot simply be read off k.Text:
// vaxis's C0 decoder only special-cases CR (0x0D) as KeyEnter — a bare LF
// (0x0A), the line ending virtually all non-Windows clipboard text uses,
// falls through its default branch and decodes as Ctrl+j (ModCtrl set,
// Keycode 'j', Text ""), identically to how a real Ctrl+J keypress would.
// That ambiguity is harmless here: actual keypresses never arrive tagged
// EventPaste, so within a paste, "Ctrl+j with no text" can only mean a
// pasted bare LF.
func (a *App) appendPasteKey(k vaxis.Key) {
	if k.Keycode == vaxis.KeyEnter {
		a.pasteBuf.WriteByte('\n')
		a.pasteCRPending = true
		return
	}

	wasCR := a.pasteCRPending
	a.pasteCRPending = false

	isBareLF := k.Text == "" && k.Modifiers&vaxis.ModCtrl != 0 && k.Keycode == 'j'
	switch {
	case isBareLF && wasCR:
		// The LF half of a CRLF pair already written as '\n' above.
	case isBareLF:
		a.pasteBuf.WriteByte('\n')
	case k.Keycode == vaxis.KeyTab:
		a.pasteBuf.WriteByte('\t')
	case k.Text != "":
		a.pasteBuf.WriteString(k.Text)
	}
}

// handlePaste delivers a fully-accumulated paste to whatever currently owns
// input — the overlay if one is open, otherwise the focused pane — via
// layout.Paster when it implements that (an atomic multi-line insert),
// falling back to feeding it through HandleKey one character at a time
// (embedded newlines becoming a synthetic Enter) for anything that doesn't,
// which is exactly how the paste would have arrived before this existed.
func (a *App) handlePaste(s string) {
	var target layout.View
	if a.overlay != nil {
		target = a.overlay
	} else {
		target = a.focus.FocusedView()
	}
	if target == nil {
		return
	}
	if p, ok := target.(layout.Paster); ok {
		p.HandlePaste(s)
		return
	}
	feedPasteAsKeystrokes(target, s)
}

// feedPasteAsKeystrokes is handlePaste's fallback for a View that doesn't
// implement layout.Paster: it replays s as the sequence of keys typing it
// would have produced, so a paste into e.g. the file tree behaves the same
// as it always has.
func feedPasteAsKeystrokes(v layout.View, s string) {
	for _, r := range s {
		switch r {
		case '\n':
			v.HandleKey(layout.Key{Named: layout.KeyEnter})
		case '\t':
			v.HandleKey(layout.Key{Named: layout.KeyTab})
		default:
			v.HandleKey(layout.Key{Text: string(r), Codepoint: r})
		}
	}
}

// wheelScrollLines is how many lines a single wheel tick moves — vaxis
// delivers one Mouse event per tick, and most terminal apps move more than
// one line per tick for it to feel responsive.
const wheelScrollLines = 3

// multiClickWindow is the maximum gap between two presses in the same cell
// for them to count as a double (then triple) click. Same duration as
// doubleShiftWindow, for the same reason: long enough not to punish a
// deliberate slow double-click, short enough that two unrelated clicks in
// one spot don't merge.
const multiClickWindow = 400 * time.Millisecond

func (a *App) handleMouse(m vaxis.Mouse) {
	ev := translateMouse(m)

	// A drag belongs to whoever the button went down on, even once the
	// pointer has wandered off that pane — see mouseCapture.
	id, ok := a.mouseCapture, a.hasMouseCapture
	if !ok {
		id, ok = a.leafAt(m.Col, m.Row)
		if !ok {
			return
		}
	}

	// A scrollbar drag in progress claims every event for its own capture,
	// same as a text-selection drag would, but routed to different logic
	// (see scrollCapture's doc comment).
	if a.scrollCapture {
		a.continueScrollDrag(id, m, ev)
		return
	}
	if ev.EventType == layout.EventPress && ev.Button == layout.MouseLeft && a.beginScrollDrag(id, m) {
		return
	}

	switch ev.EventType {
	case layout.EventPress:
		if ev.Button == layout.MouseLeft {
			a.mouseCapture, a.hasMouseCapture = id, true
		}
		ev.Clicks = a.countPress(m.Col, m.Row)
		// Clicking a pane focuses it, as it always has — done before the
		// View sees the event so a handler can rely on being focused.
		if !isWheel(ev.Button) {
			a.focus.FocusAt(id)
		}
	case layout.EventRelease:
		a.mouseCapture, a.hasMouseCapture = 0, false
	default:
		// EventMotion (hover/drag) and EventRepeat need none of the
		// press/release bookkeeping above; they fall straight through to
		// the generic handling below.
	}

	view := a.focus.ViewAt(id)
	if view == nil {
		return
	}

	// Offer the event to the View first, in ITS coordinate space. An
	// unconsumed event falls through to the generic handling below, so a
	// View that only cares about clicks keeps wheel scrolling for free.
	if mh, ok := view.(layout.MouseHandler); ok {
		local := ev
		content := contentRect(a.rects[id])
		local.Col -= content.X
		local.Row -= content.Y
		if mh.HandleMouse(local) {
			return
		}
	}

	if ev.Button == layout.MouseWheelUp || ev.Button == layout.MouseWheelDown {
		// Wheel scroll acts on whichever pane the mouse is over,
		// independent of keyboard focus — it reuses the same
		// HandleKey path as pressing Up/Down, so it inherits each
		// View's existing scroll-follow behavior for free.
		key := layout.Key{Named: layout.KeyDown}
		if ev.Button == layout.MouseWheelUp {
			key = layout.Key{Named: layout.KeyUp}
		}
		for i := 0; i < wheelScrollLines; i++ {
			view.HandleKey(key)
		}
	}
}

// beginScrollDrag checks whether a left press at screen (m.Col, m.Row)
// lands on leaf id's scrollbar column, and if so starts a drag: a press on
// the track outside the thumb pages once, then the drag is anchored at
// whatever offset within the thumb the press ended up at (recomputed after
// paging, if it paged) — unifying "click to page" and "then keep dragging"
// into one mental model, since every subsequent motion is relative to that
// anchor. Returns false, having done nothing, if id isn't both Scrollable
// and ScrollTarget, has no scrollbar reserved right now, or the press
// missed the bar.
func (a *App) beginScrollDrag(id layout.LeafID, m vaxis.Mouse) bool {
	view := a.focus.ViewAt(id)
	if view == nil {
		return false
	}
	target, ok := view.(layout.ScrollTarget)
	if !ok {
		return false
	}
	state, hasBar := scrollbarActive(view)
	if !hasBar {
		return false
	}
	bar, ok := scrollbarRect(a.rects[id])
	if !ok || m.Col != bar.X || m.Row < bar.Y || m.Row >= bar.Y+bar.H {
		return false
	}

	track := state.Viewport
	pressRow := m.Row - bar.Y - state.RowOffset
	if pressRow < 0 || pressRow >= track {
		// The press landed on the bar's own column but over the pane's
		// header rows (e.g. above the editor's tab-bar row) — not part of
		// the scrollable track at all.
		return false
	}

	start, size, show := layout.ThumbBounds(state, track)
	if show && (pressRow < start || pressRow >= start+size) {
		// Track click: page once toward it, then re-derive the thumb so the
		// drag anchor below reflects where it actually ended up.
		if pressRow < start {
			target.ScrollTo(state.Top - state.Viewport)
		} else {
			target.ScrollTo(state.Top + state.Viewport)
		}
		state, _ = scrollbarActive(view)
		start, size, show = layout.ThumbBounds(state, track)
	}

	offset := 0
	if show {
		offset = pressRow - start
		if offset < 0 {
			offset = 0
		}
		if offset >= size {
			offset = size - 1
		}
	}

	// Claimed regardless of `show`: a click on the bar is chrome, not pane
	// content, so it must not fall through to the view underneath it even
	// when there's nothing to scroll yet.
	a.mouseCapture, a.hasMouseCapture, a.scrollCapture = id, true, true
	a.scrollDragOffset = offset
	return true
}

// continueScrollDrag routes a motion/release belonging to an in-progress
// scrollbar drag (see beginScrollDrag). Release just ends it; motion
// recomputes the thumb's desired top row from the drag offset captured at
// press time and asks the pane to scroll there.
func (a *App) continueScrollDrag(id layout.LeafID, m vaxis.Mouse, ev layout.Mouse) {
	if ev.EventType == layout.EventRelease {
		a.mouseCapture, a.hasMouseCapture, a.scrollCapture = 0, false, false
		return
	}
	if ev.EventType != layout.EventMotion {
		return
	}

	view := a.focus.ViewAt(id)
	if view == nil {
		return
	}
	target, ok := view.(layout.ScrollTarget)
	if !ok {
		return
	}
	state, hasBar := scrollbarActive(view)
	if !hasBar {
		return
	}
	bar, ok := scrollbarRect(a.rects[id])
	if !ok {
		return
	}

	track := state.Viewport
	_, size, show := layout.ThumbBounds(state, track)
	if !show {
		return
	}
	thumbStart := m.Row - bar.Y - state.RowOffset - a.scrollDragOffset
	if maxStart := track - size; thumbStart > maxStart {
		thumbStart = maxStart
	}
	if thumbStart < 0 {
		thumbStart = 0
	}
	target.ScrollTo(layout.ScrollTopForThumbStart(state, track, thumbStart))
}

// isWheel reports whether b is a wheel direction rather than a real button.
// A wheel tick arrives as a press, but scrolling over a pane must not steal
// focus from it — that's the whole point of routing the wheel by hover.
func isWheel(b layout.MouseButton) bool {
	switch b {
	case layout.MouseWheelUp, layout.MouseWheelDown, layout.MouseWheelLeft, layout.MouseWheelRight:
		return true
	default:
		return false
	}
}

// countPress returns 1, 2 or 3 for a press that continues a multi-click run
// in the same cell, and resets to 1 whenever the pointer moved or the run
// timed out. Caps at 3: nothing binds a quadruple click, and wrapping back
// to 1 on the fourth press is what most editors do.
func (a *App) countPress(col, row int) int {
	now := time.Now()
	sameCell := col == a.lastPressCol && row == a.lastPressRow
	inWindow := !a.lastPressAt.IsZero() && now.Sub(a.lastPressAt) <= multiClickWindow

	if sameCell && inWindow && a.lastPressRuns < 3 {
		a.lastPressRuns++
	} else {
		a.lastPressRuns = 1
	}
	a.lastPressAt, a.lastPressCol, a.lastPressRow = now, col, row
	return a.lastPressRuns
}

// leafAt returns the leaf whose last-computed Rect contains (col, row).
func (a *App) leafAt(col, row int) (layout.LeafID, bool) {
	for id, r := range a.rects {
		if col >= r.X && col < r.X+r.W && row >= r.Y && row < r.Y+r.H {
			return id, true
		}
	}
	return 0, false
}

func (a *App) render() {
	full := a.vx.Window()
	cols, rows := full.Size()
	a.rects = layout.Compute(a.root, layout.Rect{W: cols, H: rows})

	full.Clear()
	cursorShown := a.renderNode(a.root, full)

	if a.overlay != nil {
		if a.renderOverlay(full, cols, rows) {
			cursorShown = true
		}
	}

	if !cursorShown {
		a.vx.HideCursor()
	}

	a.vx.Render()
}

// overlayWidthFrac/overlayHeightFrac size the modal relative to the
// terminal; overlayMinWidth/overlayMinHeight keep it usable on tiny
// terminals (clamped to whatever actually fits).
const (
	overlayWidthFrac  = 0.6
	overlayHeightFrac = 0.6
	overlayMinWidth   = 20
	overlayMinHeight  = 6
)

// renderOverlay draws a.overlay as a centered modal on top of full, which
// has already had the normal tree rendered into it — Fill (via drawBorder
// and the View's own Clear) overwrites those same terminal cells, which is
// what makes the modal appear "in front". It returns true if the overlay
// showed the native cursor.
func (a *App) renderOverlay(full vaxis.Window, cols, rows int) bool {
	w := min(max(int(float64(cols)*overlayWidthFrac), overlayMinWidth), cols)
	h := min(max(int(float64(rows)*overlayHeightFrac), overlayMinHeight), rows)
	x := (cols - w) / 2
	y := (rows - h) / 2

	modalWin := full.New(x, y, w, h)
	modalWin.Fill(vaxis.Cell{Character: vaxis.Character{Grapheme: " ", Width: 1}})

	bordered := drawBorder(modalWin, true, a.overlay.Title())
	localX, localY, cw, ch := 0, 0, w, h
	if bordered {
		localX, localY, cw, ch = 1, 1, w-2, h-2
	}
	// A scrollbar renders here too (an overlay pane can implement
	// Scrollable), but it is never clickable: overlays receive no mouse
	// events at all today (see the vaxis.Mouse case in Run), and wiring
	// that up is a separate, larger change than adding the bar itself.
	content := renderScrollable(modalWin, localX, localY, cw, ch, a.overlay)

	if cp, ok := a.overlay.(layout.CursorProvider); ok {
		if col, row, show := cp.CursorPosition(); show {
			content.ShowCursor(col, row, vaxis.CursorBlock)
			return true
		}
	}
	return false
}

// renderNode walks the (small, fixed-shape) window tree once per frame,
// handing each leaf a sub-window scoped to its computed Rect. This is
// distinct from the "never recurse during render" constraint on
// filetree.View.Render, which is about not walking a potentially huge file
// tree per frame — walking the window tree itself is cheap and expected.
// It returns true if some leaf showed the terminal's native cursor.
func (a *App) renderNode(n layout.Node, full vaxis.Window) bool {
	switch v := n.(type) {
	case *layout.LeafNode:
		r := a.rects[v.ID]
		leafWin := full.New(r.X, r.Y, r.W, r.H)

		focusedID, _ := a.focus.Focused()
		focused := v.ID == focusedID
		bordered := drawBorder(leafWin, focused, v.View.Title())

		// The inset comes from contentRect rather than being written out
		// again here, so that the region drawn into and the region mouse
		// coordinates are measured against can never drift apart. cr is in
		// screen space; leafWin.New wants an offset within the leaf.
		cr := r
		localX, localY := 0, 0
		if bordered {
			cr = contentRect(r)
			localX, localY = cr.X-r.X, cr.Y-r.Y
		}
		content := renderScrollable(leafWin, localX, localY, cr.W, cr.H, v.View)

		// Only the focused pane's cursor is shown — there is only one
		// hardware cursor for the whole terminal, and showing it for
		// an unfocused pane would be misleading.
		if focused {
			if cp, ok := v.View.(layout.CursorProvider); ok {
				if col, row, show := cp.CursorPosition(); show {
					content.ShowCursor(col, row, vaxis.CursorBlock)
					return true
				}
			}
		}
		return false
	case *layout.SplitNode:
		shown := false
		for _, c := range v.Children {
			if a.renderNode(c.Node, full) {
				shown = true
			}
		}
		return shown
	}
	return false
}

// renderScrollable renders v into a content area (w by h cells, at
// (localX, localY) within win) and, if v implements layout.Scrollable and
// currently has content, reserves and draws its rightmost column as a
// scrollbar instead of handing that column to v. Shared by renderNode (a
// tree leaf) and renderOverlay (a modal), which differ only in how their
// outer geometry is computed — both need the identical "narrow before
// Render, draw after" sequencing so a pane's own width math (gutters,
// popup clamps, tab-bar clipping) is never computed against a width wider
// than what it actually got to draw into.
//
// Returns the window v was actually given, for the caller's own
// CursorProvider handling — narrower than win when a scrollbar was drawn,
// which is why this can't be split into "compute rect" then "render"
// separately without the caller re-deriving the same width twice.
func renderScrollable(win vaxis.Window, localX, localY, w, h int, v layout.View) vaxis.Window {
	state, hasBar := scrollbarActive(v)
	textWidth := w
	if hasBar && w >= 2 {
		textWidth = w - 1
	} else {
		hasBar = false
	}

	content := win.New(localX, localY, textWidth, h)
	v.Render(vaxisWindow{content})

	if hasBar {
		drawScrollbar(win, localX+textWidth, localY, h, v, state)
	}
	return content
}

// scrollbarTrackStyle/scrollbarThumbStyle are the bar's bare-track and
// thumb appearance. The thumb reuses AttrReverse, the same "this is the
// selected/current thing" convention already used for the tab bar, the
// file tree's selected row, and popup selections — so it reads as
// consistent chrome rather than a new visual language.
var (
	scrollbarTrackStyle = vaxis.Style{Attribute: vaxis.AttrDim}
	scrollbarThumbStyle = vaxis.Style{Attribute: vaxis.AttrReverse}
)

// drawScrollbar paints v's scrollbar into the 1-cell-wide, h-tall column at
// (localCol, localRow) within win. state.RowOffset rows at the top of that
// column are left untouched (e.g. the editor's tab-bar row), so the track
// lines up with the scrollable text it represents rather than the pane's
// header.
//
// A track cell defaults to a dim vertical line, is overridden by a
// ScrollMarker's mark color where BucketMarks places one, and is finally
// overridden by the thumb wherever the two overlap: a single column has no
// room to show both without one obscuring the other, and losing track of
// "what changed here" for the couple of rows currently under the thumb is
// a smaller loss than losing "where am I" would be.
func drawScrollbar(win vaxis.Window, localCol, localRow, h int, v layout.View, state layout.ScrollState) {
	track := state.Viewport
	barRows := h - state.RowOffset
	if barRows > track {
		barRows = track
	}
	if barRows <= 0 {
		return
	}

	var marks map[int]layout.ScrollMark
	if mk, ok := v.(layout.ScrollMarker); ok {
		marks = layout.BucketMarks(mk.ScrollMarks(), state.Total, track)
	}
	start, size, showThumb := layout.ThumbBounds(state, track)

	for row := 0; row < barRows; row++ {
		style, glyph := scrollbarTrackStyle, "│"
		if m, ok := marks[row]; ok {
			style, glyph = translateStyle(m.Style), "┃"
		}
		if showThumb && row >= start && row < start+size {
			style, glyph = scrollbarThumbStyle, "█"
		}
		win.SetCell(localCol, localRow+state.RowOffset+row, vaxis.Cell{
			Character: vaxis.Character{Grapheme: glyph, Width: 1},
			Style:     style,
		})
	}
}

// drawBorder draws a one-cell box around win with title in the top edge,
// bold and cyan when focused, dim (default color) otherwise, so the
// focused pane stands out at a glance rather than by weight alone. It is a
// no-op (returning false) if win is too small to fit a border — e.g. a
// Fixed(1) status-bar leaf, which then gets its full area as content with
// no inset. This is App-level chrome, not part of any View — keeping it
// here means Views never need to know about focus or borders.
func drawBorder(win vaxis.Window, focused bool, title string) bool {
	cols, rows := win.Size()
	if cols < 2 || rows < 2 {
		return false
	}

	style := vaxis.Style{Attribute: vaxis.AttrDim}
	if focused {
		style = vaxis.Style{Attribute: vaxis.AttrBold, Foreground: translateColor(theme.Get(theme.UIFocusBorder))}
	}
	cell := func(ch string) vaxis.Cell {
		return vaxis.Cell{Character: vaxis.Character{Grapheme: ch, Width: 1}, Style: style}
	}

	win.SetCell(0, 0, cell("┌"))
	win.SetCell(cols-1, 0, cell("┐"))
	win.SetCell(0, rows-1, cell("└"))
	win.SetCell(cols-1, rows-1, cell("┘"))
	for x := 1; x < cols-1; x++ {
		win.SetCell(x, 0, cell("─"))
		win.SetCell(x, rows-1, cell("─"))
	}
	for y := 1; y < rows-1; y++ {
		win.SetCell(0, y, cell("│"))
		win.SetCell(cols-1, y, cell("│"))
	}

	if title != "" && cols > 4 {
		label := " " + title + " "
		titleWin := win.New(2, 0, cols-4, 1)
		titleWin.Println(0, vaxis.Segment{Text: label, Style: style})
	}

	return true
}
