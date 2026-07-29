// Package ui is the single seam where the terminal-independent layout
// package meets the real terminal (vaxis). Nothing outside this package
// imports vaxis.
package ui

import (
	"time"

	"go.rockorager.dev/vaxis"

	"github.com/bricejulia/kiwi/internal/layout"
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
	return vaxis.Style{Attribute: attr, Foreground: translateColor(s.Foreground)}
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

	onDoubleShift  func()
	lastShiftPress time.Time

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
	// Assumed focused until told otherwise: a terminal that doesn't
	// support focus reporting at all will simply never send FocusOut, and
	// starting out unfocused would otherwise leave a non-reporting
	// terminal's session permanently unresponsive to the double-shift
	// detector.
	return &App{vx: vx, root: root, focus: fm, global: global, focused: true}, nil
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
// real terminal to itself), then restores kiwi's own fullscreen state
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
		case vaxis.Key:
			a.handleKey(translateKey(e))
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

// wheelScrollLines is how many lines a single wheel tick moves — vaxis
// delivers one Mouse event per tick, and most terminal apps move more than
// one line per tick for it to feel responsive.
const wheelScrollLines = 3

func (a *App) handleMouse(m vaxis.Mouse) {
	id, ok := a.leafAt(m.Col, m.Row)
	if !ok {
		return
	}

	switch m.Button {
	case vaxis.MouseWheelUp, vaxis.MouseWheelDown:
		// Wheel scroll acts on whichever pane the mouse is over,
		// independent of keyboard focus — it reuses the same
		// HandleKey path as pressing Up/Down, so it inherits each
		// View's existing scroll-follow behavior for free.
		view := a.focus.ViewAt(id)
		if view == nil {
			return
		}
		key := layout.Key{Named: layout.KeyDown}
		if m.Button == vaxis.MouseWheelUp {
			key = layout.Key{Named: layout.KeyUp}
		}
		for i := 0; i < wheelScrollLines; i++ {
			view.HandleKey(key)
		}
	default:
		if m.EventType == vaxis.EventPress {
			a.focus.FocusAt(id)
		}
	}
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
	content := modalWin
	if bordered {
		content = modalWin.New(1, 1, w-2, h-2)
	}
	a.overlay.Render(vaxisWindow{content})

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

		content := leafWin
		if bordered {
			content = leafWin.New(1, 1, r.W-2, r.H-2)
		}
		v.View.Render(vaxisWindow{content})

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
		style = vaxis.Style{Attribute: vaxis.AttrBold, Foreground: translateColor(layout.ColorCyan)}
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
