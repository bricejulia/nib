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
		if k.Mods&layout.ModAlt != 0 {
			f.DeleteWordBackward()
		} else if f.caret > 0 {
			f.buf = append(f.buf[:f.caret-1], f.buf[f.caret:]...)
			f.caret--
		}
		return true
	case layout.KeyLeft:
		if k.Mods&layout.ModAlt != 0 {
			f.WordLeft()
		} else if f.caret > 0 {
			f.caret--
		}
		return true
	case layout.KeyRight:
		if k.Mods&layout.ModAlt != 0 {
			f.WordRight()
		} else if f.caret < len(f.buf) {
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
	f.InsertText(k.Text)
	return true
}

// InsertText splices s into the buffer at the caret, filtering to printable
// runes exactly like a typed character, leaving the caret positioned after
// the inserted text. For callers that receive a whole block of text at once
// (e.g. a paste) instead of one HandleKey call per rune.
func (f *TextField) InsertText(s string) {
	for _, r := range s {
		if !unicode.IsPrint(r) {
			continue
		}
		f.buf = append(f.buf, 0)
		copy(f.buf[f.caret+1:], f.buf[f.caret:])
		f.buf[f.caret] = r
		f.caret++
	}
}

// wordClass mirrors internal/ui/editor/motion.go's runeClass three-way split
// (word run / punctuation run / blank run), duplicated here rather than
// imported: this leaf package has no dependency on editor (and importing it
// would cycle back, since editor's own command/search prompts are built on
// TextField too). Small and stable, unlike the caret/buffer logic textfield
// itself was extracted to stop duplicating — if the classification rule
// ever changes, update both copies.
type wordClass int

const (
	classBlank wordClass = iota
	classPunct
	classWord
)

func classifyRune(r rune) wordClass {
	switch {
	case r == ' ' || r == '\t':
		return classBlank
	case r == '_' || (r >= 'a' && r <= 'z') || (r >= 'A' && r <= 'Z') || (r >= '0' && r <= '9'):
		return classWord
	default:
		return classPunct
	}
}

// wordLeftIndex returns the caret position WordLeft would land on: the
// start of the previous word-or-punctuation run, mirroring
// editor/motion.go's wordBackwardOnce minus line-crossing (a TextField is
// always one flat line). Clamps (never negative) at 0.
func wordLeftIndex(buf []rune, pos int) int {
	if pos <= 0 {
		return 0
	}
	pos--
	for pos > 0 && classifyRune(buf[pos]) == classBlank {
		pos--
	}
	if classifyRune(buf[pos]) == classBlank {
		return 0 // ran out of buffer through nothing but blanks
	}
	class := classifyRune(buf[pos])
	for pos > 0 && classifyRune(buf[pos-1]) == class {
		pos--
	}
	return pos
}

// wordRightIndex returns the caret position WordRight would land on: the
// start of the next word-or-punctuation run, mirroring wordForwardOnce.
// Clamps at len(buf).
func wordRightIndex(buf []rune, pos int) int {
	n := len(buf)
	if pos >= n {
		return n
	}
	if class := classifyRune(buf[pos]); class != classBlank {
		for pos < n && classifyRune(buf[pos]) == class {
			pos++
		}
	}
	for pos < n && classifyRune(buf[pos]) == classBlank {
		pos++
	}
	return pos
}

// WordLeft moves the caret to the previous word-motion boundary (vim's "b",
// adapted to a flat single-line buffer). A no-op at caret 0.
func (f *TextField) WordLeft() { f.caret = wordLeftIndex(f.buf, f.caret) }

// WordRight moves the caret to the next word-motion boundary (vim's "w"). A
// no-op at the end of the buffer.
func (f *TextField) WordRight() { f.caret = wordRightIndex(f.buf, f.caret) }

// DeleteWordBackward removes the run between the previous word-motion
// boundary and the caret — vim's "db" equivalent. A no-op at caret 0.
func (f *TextField) DeleteWordBackward() {
	start := wordLeftIndex(f.buf, f.caret)
	if start == f.caret {
		return
	}
	f.buf = append(f.buf[:start], f.buf[f.caret:]...)
	f.caret = start
}
