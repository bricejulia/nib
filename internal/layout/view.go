package layout

import "strings"

// View is the contract every pane content type implements: the file tree,
// the read-only editor, and later panels (fuzzy finder, git log,
// diagnostics, DAP variables) are all Views dropped into a LeafNode. Adding
// a new panel never requires touching the window tree or focus routing.
type View interface {
	Render(w Window)
	// HandleKey returns true if it consumed the key. An unconsumed key
	// bubbles to the global keymap (see Dispatch).
	HandleKey(k Key) bool
	Title() string
}

// CursorProvider is an optional interface a View implements to request the
// terminal's native text cursor be shown at a specific cell while it holds
// focus — col/row are relative to the Window the View was last given in
// Render. Views that don't implement this (e.g. the file tree, which shows
// its selection via a highlighted row instead) get no visible cursor.
type CursorProvider interface {
	CursorPosition() (col, row int, ok bool)
}

// Unfocusable is an optional interface a View implements to opt out of
// keyboard focus entirely — e.g. a status bar that only ever displays
// information. FocusManager skips such leaves when building its Tab-cycle
// order, but they can still be looked up (e.g. for rendering) like any
// other leaf.
type Unfocusable interface {
	Unfocusable() bool
}

// Window is the drawing surface handed to a View's Render. It is a thin,
// terminal-independent abstraction over the real renderer (vaxis, in
// internal/ui/app.go) so that View implementations are unit-testable
// against an in-memory fake with no live terminal.
type Window interface {
	Size() (cols, rows int)
	// Println draws one line built by concatenating segs in order,
	// starting at column 0 of this Window, clipped to its width.
	// Out-of-range rows are ignored.
	Println(row int, segs ...Segment)
	Clear()
}

// Segment is a run of text sharing a single Style — the unit multi-color
// lines (e.g. syntax-highlighted source) are built from.
type Segment struct {
	Text  string
	Style Style
}

// AttrMask is a bitmask of boolean text attributes.
type AttrMask uint8

const (
	AttrBold AttrMask = 1 << iota
	AttrDim
	AttrReverse
)

// Color is a terminal-independent color, restricted to the standard
// 16-color ANSI palette so it renders reasonably on every terminal. The
// zero value, ColorDefault, means "the terminal's default color" (no
// override).
type Color uint8

const (
	ColorDefault Color = iota
	ColorBlack
	ColorRed
	ColorGreen
	ColorYellow
	ColorBlue
	ColorMagenta
	ColorCyan
	ColorWhite
	ColorBrightBlack
	ColorBrightRed
	ColorBrightGreen
	ColorBrightYellow
	ColorBrightBlue
	ColorBrightMagenta
	ColorBrightCyan
	ColorBrightWhite
)

// Style is the terminal-independent styling applied to a line of text.
type Style struct {
	Attr       AttrMask
	Foreground Color
}

// EventType distinguishes a key press from a repeat or release, mirroring
// what the kitty keyboard protocol reports.
type EventType int

const (
	EventPress EventType = iota
	EventRepeat
	EventRelease
)

// ModMask is a bitmask of held modifier keys.
type ModMask uint8

const (
	ModShift ModMask = 1 << iota
	ModAlt
	ModCtrl
	ModSuper
)

// Key is a terminal-independent key event. The real vaxis.Key is translated
// into this type at the single seam in internal/ui/app.go, so that View
// implementations never import vaxis.
//
// Named identifies non-printable/special keys (arrows, Enter, Tab, paging,
// Home/End, Esc) by a portable string name such as "Up" or "PageDown". It
// is empty for ordinary printable keys, which are matched via Text or
// Codepoint instead.
type Key struct {
	Text      string
	Codepoint rune
	Named     string
	Mods      ModMask
	EventType EventType
}

// Names for the special keys Step 0's Views bind against.
const (
	KeyUp        = "Up"
	KeyDown      = "Down"
	KeyLeft      = "Left"
	KeyRight     = "Right"
	KeyEnter     = "Enter"
	KeyTab       = "Tab"
	KeyEsc       = "Esc"
	KeyPageUp    = "PageUp"
	KeyPageDown  = "PageDown"
	KeyHome      = "Home"
	KeyEnd       = "End"
	KeyBackspace = "Backspace"
	// KeyShift names a bare press of the Shift key by itself (no other
	// key held down at the same time) — only reported by terminals
	// supporting the kitty keyboard protocol's "report all keys" mode.
	// Used to detect a double-tap-Shift shortcut (see ui.App's
	// double-shift handler).
	KeyShift = "Shift"
)

// String renders the key as "Mod+Mod+Key", e.g. "Ctrl+c" or "Tab". It is
// the lookup key used by the global keymap in Dispatch.
func (k Key) String() string {
	var b strings.Builder
	if k.Mods&ModCtrl != 0 {
		b.WriteString("Ctrl+")
	}
	if k.Mods&ModAlt != 0 {
		b.WriteString("Alt+")
	}
	if k.Mods&ModSuper != 0 {
		b.WriteString("Super+")
	}
	if k.Mods&ModShift != 0 {
		b.WriteString("Shift+")
	}
	switch {
	case k.Named != "":
		b.WriteString(k.Named)
	case k.Text != "":
		b.WriteString(k.Text)
	case k.Codepoint != 0:
		b.WriteRune(k.Codepoint)
	}
	return b.String()
}
