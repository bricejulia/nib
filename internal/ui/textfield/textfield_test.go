package textfield

import (
	"testing"

	"github.com/bricejulia/nib/internal/layout"
)

func altKey(named string) layout.Key {
	return layout.Key{Named: named, Mods: layout.ModAlt}
}

func plainKey(named string) layout.Key {
	return layout.Key{Named: named}
}

func TestWordLeftSkipsPunctuationRuns(t *testing.T) {
	f := New("foo.bar baz")
	// caret starts at the end (11): "foo.bar baz"
	//                                0123456789 10
	var got []int
	for range 4 {
		f.WordLeft()
		got = append(got, f.Caret())
	}
	// "baz", "bar" (the space between is skipped through, not stopped at —
	// only a word/punct run is a stop, matching vim's "b"), ".", "foo".
	want := []int{8, 4, 3, 0}
	for i, w := range want {
		if got[i] != w {
			t.Fatalf("WordLeft step %d = %d, want %d (all: %v)", i, got[i], w, got)
		}
	}
}

func TestWordRightSkipsPunctuationRuns(t *testing.T) {
	f := New("foo.bar baz")
	f.caret = 0
	var got []int
	for range 4 {
		f.WordRight()
		got = append(got, f.Caret())
	}
	want := []int{3, 4, 8, 11} // end of "foo", ".", "bar", "baz"
	for i, w := range want {
		if got[i] != w {
			t.Fatalf("WordRight step %d = %d, want %d (all: %v)", i, got[i], w, got)
		}
	}
}

func TestWordLeftAtStartIsNoop(t *testing.T) {
	f := New("foo")
	f.caret = 0
	f.WordLeft()
	if f.Caret() != 0 {
		t.Fatalf("caret = %d, want 0", f.Caret())
	}
}

func TestWordRightAtEndIsNoop(t *testing.T) {
	f := New("foo")
	f.WordRight()
	if f.Caret() != 3 {
		t.Fatalf("caret = %d, want 3", f.Caret())
	}
}

func TestWordLeftOverMultipleBlankRuns(t *testing.T) {
	f := New("foo   bar")
	f.WordLeft() // from the end, lands at the start of "bar"
	if f.Caret() != 6 {
		t.Fatalf("caret = %d, want 6", f.Caret())
	}
}

func TestWordLeftRightMixedUnicode(t *testing.T) {
	// A non-ASCII letter is classified as punctuation, matching
	// editor/motion.go's own ASCII-only isIdentRune quirk — documented
	// behavior, not a bug, so app-wide word-nav stays consistent.
	f := New("café bar")
	f.WordLeft() // from the end, lands at start of "bar"
	if f.Caret() != 5 {
		t.Fatalf("caret = %d, want 5", f.Caret())
	}
	f.caret = 3 // just after "caf", before "é"
	f.WordRight()
	if f.Caret() != 5 {
		t.Fatalf("caret = %d, want 5 (the punctuation-classified é is skipped as its own run, then the blank, landing at \"bar\")", f.Caret())
	}
}

func TestDeleteWordBackwardRemovesOneWord(t *testing.T) {
	f := New("foo bar")
	f.DeleteWordBackward()
	if got := f.String(); got != "foo " {
		t.Fatalf("buffer = %q, want %q", got, "foo ")
	}
	if f.Caret() != 4 {
		t.Fatalf("caret = %d, want 4", f.Caret())
	}
}

func TestDeleteWordBackwardAtStartIsNoop(t *testing.T) {
	f := New("foo")
	f.caret = 0
	f.DeleteWordBackward()
	if got := f.String(); got != "foo" {
		t.Fatalf("buffer = %q, want unchanged %q", got, "foo")
	}
}

func TestDeleteWordBackwardOnlyWhitespace(t *testing.T) {
	f := New("   ")
	f.DeleteWordBackward()
	if got := f.String(); got != "" {
		t.Fatalf("buffer = %q, want empty (a leading blank run with nothing before it is deleted whole)", got)
	}
}

func TestHandleKeyAltLeftRightBackspaceRequireModAlt(t *testing.T) {
	f := New("foo bar")
	f.HandleKey(plainKey(layout.KeyLeft))
	if f.Caret() != 6 {
		t.Fatalf("plain Left moved by more than one rune: caret = %d, want 6", f.Caret())
	}
	f.HandleKey(plainKey(layout.KeyRight))
	if f.Caret() != 7 {
		t.Fatalf("plain Right moved by more than one rune: caret = %d, want 7", f.Caret())
	}
	f.HandleKey(plainKey(layout.KeyBackspace))
	if got := f.String(); got != "foo ba" {
		t.Fatalf("plain Backspace deleted more than one rune: buffer = %q, want %q", got, "foo ba")
	}
}

func TestHandleKeyAltLeftMovesByWord(t *testing.T) {
	f := New("foo bar")
	if !f.HandleKey(altKey(layout.KeyLeft)) {
		t.Fatal("Alt+Left was not consumed")
	}
	if f.Caret() != 4 {
		t.Fatalf("caret = %d, want 4", f.Caret())
	}
}

func TestHandleKeyAltRightMovesByWord(t *testing.T) {
	f := New("foo bar")
	f.caret = 0
	if !f.HandleKey(altKey(layout.KeyRight)) {
		t.Fatal("Alt+Right was not consumed")
	}
	if f.Caret() != 4 {
		t.Fatalf("caret = %d, want 4 (start of \"bar\" — \"w\" lands on the next word's start, not the current word's end)", f.Caret())
	}
}

func TestHandleKeyAltBackspaceDeletesWord(t *testing.T) {
	f := New("foo bar")
	if !f.HandleKey(altKey(layout.KeyBackspace)) {
		t.Fatal("Alt+Backspace was not consumed")
	}
	if got := f.String(); got != "foo " {
		t.Fatalf("buffer = %q, want %q", got, "foo ")
	}
}

func TestInsertTextAtCaret(t *testing.T) {
	f := New("foo")
	f.caret = 0
	f.InsertText("bar ")
	if got := f.String(); got != "bar foo" {
		t.Fatalf("buffer = %q, want %q", got, "bar foo")
	}
	if f.Caret() != 4 {
		t.Fatalf("caret = %d, want 4", f.Caret())
	}
}
