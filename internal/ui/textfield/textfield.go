// Package textfield is a small, reusable single-line text input: a rune
// buffer plus a caret index, with a HandleKey that captures typed text plus
// Backspace/Left/Right/Home/End, leaving every other named key (Tab, Enter,
// Esc, Up/Down, paging, ...) unconsumed for the caller to act on instead.
// Extracted from internal/ui/finder, which was already sharing one copy of
// this between its own search query and ReplaceView's Find/Replace fields —
// this is that same primitive, exported for panes outside finder (e.g.
// help's search box) that need an always-typeable field too.
package textfield

import (
	"unicode"

	"github.com/bricejulia/nib/internal/layout"
)

// TextField is a single-line, always-typeable text input.
type TextField struct {
	buf   []rune
	caret int
}

// New returns a TextField pre-filled with s, caret at the end — for
// callers that need to open a field with existing text (e.g. finder's
// OpenWithQuery, pre-filling the word under the cursor).
func New(s string) TextField {
	runes := []rune(s)
	return TextField{buf: runes, caret: len(runes)}
}

func (f *TextField) String() string { return string(f.buf) }

// Len is the buffer's length in runes.
func (f *TextField) Len() int { return len(f.buf) }

// Caret is the current caret position, in runes from the start of the
// buffer — what a CursorPosition measures the terminal caret's column
// from when the field's text is single-width/ASCII (see
// TextBeforeCaret for the multi-width-aware alternative).
func (f *TextField) Caret() int { return f.caret }

// TextBeforeCaret is the buffer's text up to the caret — what a
// CursorPosition measures the terminal caret's column from when it needs
// to account for the display width of wide/multi-byte characters (see
// textwidth.DisplayWidth).
func (f *TextField) TextBeforeCaret() string { return string(f.buf[:f.caret]) }

// HandleKey edits the field in place, reporting whether it consumed k.
// Named keys it doesn't itself handle (Tab, Enter, Esc, arrows-that-aren't-
// Left/Right, paging) are left unconsumed so the caller can act on them —
// exactly how each host pane reserves a few keys for its own actions while
// still typing every other character.
func (f *TextField) HandleKey(k layout.Key) bool {
	switch k.Named {
	case layout.KeyBackspace:
		if f.caret > 0 {
			f.buf = append(f.buf[:f.caret-1], f.buf[f.caret:]...)
			f.caret--
		}
		return true
	case layout.KeyLeft:
		if f.caret > 0 {
			f.caret--
		}
		return true
	case layout.KeyRight:
		if f.caret < len(f.buf) {
			f.caret++
		}
		return true
	case layout.KeyHome:
		f.caret = 0
		return true
	case layout.KeyEnd:
		f.caret = len(f.buf)
		return true
	}
	// Any other named key (Tab, Enter, Esc, Up/Down, paging) is left to the
	// caller. Space is the exception: App's translateKey promotes it to a
	// Named value while leaving Text intact, so without this a space would
	// never make it into typed text.
	if k.Named != "" && k.Named != layout.KeySpace {
		return false
	}
	if k.Text == "" || k.Mods&(layout.ModCtrl|layout.ModAlt|layout.ModSuper) != 0 {
		return false
	}
	for _, r := range k.Text {
		if !unicode.IsPrint(r) {
			continue
		}
		f.buf = append(f.buf, 0)
		copy(f.buf[f.caret+1:], f.buf[f.caret:])
		f.buf[f.caret] = r
		f.caret++
	}
	return true
}
