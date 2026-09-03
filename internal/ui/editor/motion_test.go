package editor

import (
	"strings"
	"testing"

	"github.com/bricejulia/nib/internal/layout"
)

func TestWordForwardOnceStopsAtNextWordAcrossWhitespace(t *testing.T) {
	buf := &Buffer{Lines: []string{"foo bar"}}
	ln, col := wordForwardOnce(buf, 0, 0)
	if ln != 0 || col != 4 {
		t.Fatalf("got (%d,%d), want (0,4) — start of \"bar\"", ln, col)
	}
}

func TestWordForwardOnceTreatsPunctuationAsItsOwnWord(t *testing.T) {
	buf := &Buffer{Lines: []string{"foo.bar"}}
	ln, col := wordForwardOnce(buf, 0, 0)
	if ln != 0 || col != 3 {
		t.Fatalf("got (%d,%d), want (0,3) — the \".\"", ln, col)
	}
	ln, col = wordForwardOnce(buf, ln, col)
	if ln != 0 || col != 4 {
		t.Fatalf("got (%d,%d), want (0,4) — start of \"bar\"", ln, col)
	}
}

func TestWordForwardOnceCrossesLinesSkippingLeadingWhitespace(t *testing.T) {
	buf := &Buffer{Lines: []string{"foo", "   bar"}}
	ln, col := wordForwardOnce(buf, 0, 0)
	if ln != 1 || col != 3 {
		t.Fatalf("got (%d,%d), want (1,3) — start of \"bar\" on the next line", ln, col)
	}
}

func TestWordForwardOnceStopsOnAnEmptyLine(t *testing.T) {
	buf := &Buffer{Lines: []string{"foo", "", "bar"}}
	ln, col := wordForwardOnce(buf, 0, 0)
	if ln != 1 || col != 0 {
		t.Fatalf("got (%d,%d), want (1,0) — the empty line is its own stop", ln, col)
	}
	ln, col = wordForwardOnce(buf, ln, col)
	if ln != 2 || col != 0 {
		t.Fatalf("got (%d,%d), want (2,0) — start of \"bar\"", ln, col)
	}
}

func TestWordForwardOnceSkipsThroughAnAllBlankLine(t *testing.T) {
	buf := &Buffer{Lines: []string{"foo", "   ", "bar"}}
	ln, col := wordForwardOnce(buf, 0, 0)
	if ln != 2 || col != 0 {
		t.Fatalf("got (%d,%d), want (2,0) — the all-blank line is skipped through, not stopped on", ln, col)
	}
}

func TestWordForwardOnceClampsAtEOF(t *testing.T) {
	buf := &Buffer{Lines: []string{"foo"}}
	ln, col := wordForwardOnce(buf, 0, 0)
	if ln != 0 || col != 3 {
		t.Fatalf("got (%d,%d), want (0,3) — clamped just past the last word", ln, col)
	}
}

func TestWordBackwardOnceMirrorsForward(t *testing.T) {
	buf := &Buffer{Lines: []string{"foo bar"}}
	ln, col := wordBackwardOnce(buf, 0, 4)
	if ln != 0 || col != 0 {
		t.Fatalf("got (%d,%d), want (0,0) — start of \"foo\"", ln, col)
	}
}

func TestWordBackwardOncePunctuationIsItsOwnWord(t *testing.T) {
	buf := &Buffer{Lines: []string{"foo.bar"}}
	ln, col := wordBackwardOnce(buf, 0, 4)
	if ln != 0 || col != 3 {
		t.Fatalf("got (%d,%d), want (0,3) — the \".\"", ln, col)
	}
}

func TestWordBackwardOnceStopsOnAnEmptyLine(t *testing.T) {
	buf := &Buffer{Lines: []string{"foo", "", "bar"}}
	ln, col := wordBackwardOnce(buf, 2, 0)
	if ln != 1 || col != 0 {
		t.Fatalf("got (%d,%d), want (1,0) — the empty line is its own stop", ln, col)
	}
}

func TestWordBackwardOnceClampsAtBOF(t *testing.T) {
	buf := &Buffer{Lines: []string{"foo"}}
	ln, col := wordBackwardOnce(buf, 0, 0)
	if ln != 0 || col != 0 {
		t.Fatalf("got (%d,%d), want (0,0) — clamped at the start of the buffer", ln, col)
	}
}

func TestWordEndOnceAdvancesPastTheCurrentRunEnd(t *testing.T) {
	buf := &Buffer{Lines: []string{"foo bar"}}
	ln, col := wordEndOnce(buf, 0, 0)
	if ln != 0 || col != 2 {
		t.Fatalf("got (%d,%d), want (0,2) — end of \"foo\"", ln, col)
	}
	ln, col = wordEndOnce(buf, ln, col) // already on "foo"'s last rune
	if ln != 0 || col != 6 {
		t.Fatalf("got (%d,%d), want (0,6) — end of \"bar\", not staying put", ln, col)
	}
}

func TestWordEndOnceDoesNotStopOnABlankOrEmptyLine(t *testing.T) {
	buf := &Buffer{Lines: []string{"foo", "", "bar"}}
	ln, col := wordEndOnce(buf, 0, 2) // sitting on "foo"'s last rune
	if ln != 2 || col != 2 {
		t.Fatalf("got (%d,%d), want (2,2) — end of \"bar\", the empty line passed through", ln, col)
	}
}

func TestWordEndOnceClampsAtEOF(t *testing.T) {
	buf := &Buffer{Lines: []string{"foo"}}
	ln, col := wordEndOnce(buf, 0, 2)
	if ln != 0 || col != 2 {
		t.Fatalf("got (%d,%d), want (0,2) — clamped, nothing further to advance to", ln, col)
	}
}

func TestWordMotionsRespectTabExpandedCursorColumn(t *testing.T) {
	// A leading tab: raw column 1 is past the tab, but its EXPANDED column
	// depends on tabWidth — applyMovement must convert at that boundary
	// (see rawIndexForExpandedCol/expandedColForRawIndex) exactly like every
	// other buffer mutation does.
	v := NewView()
	v.tabs = []*tab{{buf: &Buffer{Lines: []string{"\tfoo bar"}}}}
	v.active = 0
	v.activeTab().cursorCol = 4 // expanded column onto "f" of "foo", after the tab

	v.HandleKey(layout.Key{Text: "w"})

	if got := v.activeTab().cursorCol; got != 8 {
		t.Fatalf("cursorCol = %d, want 8 (expanded column of \"bar\")", got)
	}
}

func TestBareWMovesToStartOfNextWord(t *testing.T) {
	v := NewView()
	v.tabs = []*tab{{buf: &Buffer{Lines: []string{"foo bar baz"}}}}
	v.active = 0

	v.HandleKey(layout.Key{Text: "w"})

	if got := v.activeTab().cursorCol; got != 4 {
		t.Fatalf("cursorCol = %d, want 4", got)
	}
}

func TestCountedWRepeatsTheMotion(t *testing.T) {
	v := NewView()
	v.tabs = []*tab{{buf: &Buffer{Lines: []string{"foo bar baz"}}}}
	v.active = 0

	pressKeys(v, "2w")

	if got := v.activeTab().cursorCol; got != 8 {
		t.Fatalf("cursorCol = %d, want 8 (start of \"baz\")", got)
	}
}

func TestBareEMovesToEndOfWord(t *testing.T) {
	v := NewView()
	v.tabs = []*tab{{buf: &Buffer{Lines: []string{"foo bar"}}}}
	v.active = 0

	v.HandleKey(layout.Key{Text: "e"})

	if got := v.activeTab().cursorCol; got != 2 {
		t.Fatalf("cursorCol = %d, want 2", got)
	}
}

func TestBareBMovesToStartOfPreviousWord(t *testing.T) {
	v := NewView()
	v.tabs = []*tab{{buf: &Buffer{Lines: []string{"foo bar"}}}}
	v.active = 0
	v.activeTab().cursorCol = 4

	v.HandleKey(layout.Key{Text: "b"})

	if got := v.activeTab().cursorCol; got != 0 {
		t.Fatalf("cursorCol = %d, want 0", got)
	}
}

func TestDWDeletesToStartOfNextWord(t *testing.T) {
	v := NewView()
	v.tabs = []*tab{{buf: &Buffer{Lines: []string{"foo bar"}}}}
	v.active = 0

	pressKeys(v, "dw")

	tb := v.activeTab()
	if got := tb.buf.Lines[0]; got != "bar" {
		t.Fatalf("Lines[0] = %q, want %q", got, "bar")
	}
	if tb.cursorCol != 0 {
		t.Fatalf("cursorCol = %d, want 0", tb.cursorCol)
	}
}

func TestCountedDWDeletesThatManyWords(t *testing.T) {
	v := NewView()
	v.tabs = []*tab{{buf: &Buffer{Lines: []string{"one two three four"}}}}
	v.active = 0

	pressKeys(v, "d2w")

	if got := v.activeTab().buf.Lines[0]; got != "three four" {
		t.Fatalf("Lines[0] = %q, want %q", got, "three four")
	}
}

func TestDDollarDeletesToEndOfLine(t *testing.T) {
	v := NewView()
	v.tabs = []*tab{{buf: &Buffer{Lines: []string{"hello world"}}}}
	v.active = 0
	v.activeTab().cursorCol = 6

	pressKeys(v, "d$")

	tb := v.activeTab()
	if got := tb.buf.Lines[0]; got != "hello " {
		t.Fatalf("Lines[0] = %q, want %q", got, "hello ")
	}
	// "hello world" becomes "hello " (6 runes: "hello" + a trailing space)
	// after "d$" deletes "world" — the cursor lands on that new last
	// character (the space, col 5), not past it, matching Normal mode's
	// last-character rule.
	if tb.cursorCol != 5 {
		t.Fatalf("cursorCol = %d, want 5", tb.cursorCol)
	}
}

func TestCWActsLikeCEAndDoesNotEatTrailingSpace(t *testing.T) {
	v := NewView()
	v.tabs = []*tab{{buf: &Buffer{Lines: []string{"foo bar"}}}}
	v.active = 0

	pressKeys(v, "cw")

	tb := v.activeTab()
	if got := tb.buf.Lines[0]; got != " bar" {
		t.Fatalf("Lines[0] = %q, want %q (trailing space preserved)", got, " bar")
	}
	if v.mode != modeInsert {
		t.Fatalf("mode = %v, want modeInsert", v.mode)
	}
	if tb.cursorCol != 0 {
		t.Fatalf("cursorCol = %d, want 0", tb.cursorCol)
	}
}

func TestDWAtLastWordOfLineDoesNotEatNextLinesIndent(t *testing.T) {
	v := NewView()
	v.tabs = []*tab{{buf: &Buffer{Lines: []string{"foo", "  bar"}}}}
	v.active = 0

	pressKeys(v, "dw")

	tb := v.activeTab()
	if got := strings.Join(tb.buf.Lines, "|"); got != "|  bar" {
		t.Fatalf("Lines = %q, want %q (next line's indent untouched)", got, "|  bar")
	}
}

func TestYWLeavesBufferAndCursorAlone(t *testing.T) {
	v := NewView()
	v.tabs = []*tab{{buf: &Buffer{Lines: []string{"foo bar"}}}}
	v.active = 0

	pressKeys(v, "yw")

	tb := v.activeTab()
	if got := tb.buf.Lines[0]; got != "foo bar" {
		t.Fatalf("yank changed the buffer: %q", got)
	}
	if tb.cursorCol != 0 {
		t.Fatalf("cursorCol = %d, want 0 (yank never repositions the cursor)", tb.cursorCol)
	}
	if got := strings.Join(v.register.Lines(), ""); got != "foo " {
		t.Fatalf("register = %q, want %q", got, "foo ")
	}
	if !v.register.Charwise() {
		t.Error("expected the register to be marked charwise")
	}
}

func TestDBDeletesThePreviousWord(t *testing.T) {
	v := NewView()
	v.tabs = []*tab{{buf: &Buffer{Lines: []string{"foo bar"}}}}
	v.active = 0
	v.activeTab().cursorCol = 4

	pressKeys(v, "db")

	tb := v.activeTab()
	if got := tb.buf.Lines[0]; got != "bar" {
		t.Fatalf("Lines[0] = %q, want %q", got, "bar")
	}
	if tb.cursorCol != 0 {
		t.Fatalf("cursorCol = %d, want 0", tb.cursorCol)
	}
}

func TestDWIsOneUndoEntry(t *testing.T) {
	v := NewView()
	v.tabs = []*tab{{buf: &Buffer{Lines: []string{"foo bar"}}}}
	v.active = 0

	pressKeys(v, "dw")
	v.undo(v.activeTab())

	if got := v.activeTab().buf.Lines[0]; got != "foo bar" {
		t.Fatalf("after undo, Lines[0] = %q, want the original restored", got)
	}
}

func TestWordObjectRangeOnIdentifier(t *testing.T) {
	start, end, ok := wordObjectRange("foo.bar", 1)
	if !ok || start != 0 || end != 3 {
		t.Fatalf("got (%d,%d,%v), want (0,3,true) — \"foo\"", start, end, ok)
	}
}

func TestWordObjectRangeOnPunctuationIsJustThatRune(t *testing.T) {
	start, end, ok := wordObjectRange("foo.bar", 3)
	if !ok || start != 3 || end != 4 {
		t.Fatalf("got (%d,%d,%v), want (3,4,true) — the \".\"", start, end, ok)
	}
}

func TestWordObjectRangeOnWhitespaceIsTheWhitespaceRun(t *testing.T) {
	start, end, ok := wordObjectRange("foo   bar", 4)
	if !ok || start != 3 || end != 6 {
		t.Fatalf("got (%d,%d,%v), want (3,6,true) — the whitespace run", start, end, ok)
	}
}

func TestAWordObjectRangePrefersTrailingWhitespace(t *testing.T) {
	start, end, ok := aWordObjectRange("foo bar", 0)
	if !ok || start != 0 || end != 4 {
		t.Fatalf("got (%d,%d,%v), want (0,4,true) — \"foo \"", start, end, ok)
	}
}

func TestAWordObjectRangeFallsBackToLeadingWhitespace(t *testing.T) {
	start, end, ok := aWordObjectRange("  foo", 2)
	if !ok || start != 0 || end != 5 {
		t.Fatalf("got (%d,%d,%v), want (0,5,true) — \"  foo\"", start, end, ok)
	}
}

func TestAWordObjectRangeOnWhitespaceIncludesTheFollowingWord(t *testing.T) {
	start, end, ok := aWordObjectRange("foo   bar", 4)
	if !ok || start != 3 || end != 9 {
		t.Fatalf("got (%d,%d,%v), want (3,9,true) — \"   bar\"", start, end, ok)
	}
}

func TestDIWDeletesTheInnerWordWithoutTrailingSpace(t *testing.T) {
	v := NewView()
	v.tabs = []*tab{{buf: &Buffer{Lines: []string{"foo bar"}}}}
	v.active = 0

	pressKeys(v, "diw")

	tb := v.activeTab()
	if got := tb.buf.Lines[0]; got != " bar" {
		t.Fatalf("Lines[0] = %q, want %q", got, " bar")
	}
	if tb.cursorCol != 0 {
		t.Fatalf("cursorCol = %d, want 0", tb.cursorCol)
	}
}

func TestDIWOnPunctuationDeletesJustThatRune(t *testing.T) {
	v := NewView()
	v.tabs = []*tab{{buf: &Buffer{Lines: []string{"foo.bar"}}}}
	v.active = 0
	v.activeTab().cursorCol = 3

	pressKeys(v, "diw")

	if got := v.activeTab().buf.Lines[0]; got != "foobar" {
		t.Fatalf("Lines[0] = %q, want %q", got, "foobar")
	}
}

func TestCIWEntersInsertModeAfterClearingTheWord(t *testing.T) {
	v := NewView()
	v.tabs = []*tab{{buf: &Buffer{Lines: []string{"foo bar"}}}}
	v.active = 0

	pressKeys(v, "ciw")

	tb := v.activeTab()
	if got := tb.buf.Lines[0]; got != " bar" {
		t.Fatalf("Lines[0] = %q, want %q", got, " bar")
	}
	if v.mode != modeInsert {
		t.Fatalf("mode = %v, want modeInsert", v.mode)
	}
}

func TestYIWFillsTheRegisterWithoutTouchingTheBuffer(t *testing.T) {
	v := NewView()
	v.tabs = []*tab{{buf: &Buffer{Lines: []string{"foo bar"}}}}
	v.active = 0

	pressKeys(v, "yiw")

	tb := v.activeTab()
	if got := tb.buf.Lines[0]; got != "foo bar" {
		t.Fatalf("yank changed the buffer: %q", got)
	}
	if got := strings.Join(v.register.Lines(), ""); got != "foo" {
		t.Fatalf("register = %q, want %q", got, "foo")
	}
}

func TestDAWDeletesTheWordAndItsTrailingSpace(t *testing.T) {
	v := NewView()
	v.tabs = []*tab{{buf: &Buffer{Lines: []string{"foo bar"}}}}
	v.active = 0

	pressKeys(v, "daw")

	if got := v.activeTab().buf.Lines[0]; got != "bar" {
		t.Fatalf("Lines[0] = %q, want %q", got, "bar")
	}
}

func TestDIUnboundObjectLetterAbortsWithoutChangingTheBuffer(t *testing.T) {
	v := NewView()
	v.tabs = []*tab{{buf: &Buffer{Lines: []string{"foo bar"}}}}
	v.active = 0

	pressKeys(v, "di")
	v.HandleKey(layout.Key{Text: "z"}) // not a recognized text-object letter

	tb := v.activeTab()
	if got := tb.buf.Lines[0]; got != "foo bar" {
		t.Fatalf("Lines[0] = %q, want the buffer untouched", got)
	}
	if v.pendingOp.action != "" {
		t.Fatalf("pendingOp.action = %q, want cleared by the unrecognized object letter", v.pendingOp.action)
	}
}

func TestTwoDThreeWDeletesSixWords(t *testing.T) {
	v := NewView()
	v.tabs = []*tab{{buf: &Buffer{Lines: []string{"one two three four five six seven"}}}}
	v.active = 0

	pressKeys(v, "2d3w") // vim's own count-multiplication rule: 2 * 3 = 6 words

	if got := v.activeTab().buf.Lines[0]; got != "seven" {
		t.Fatalf("Lines[0] = %q, want %q", got, "seven")
	}
}

func TestCWIsOneUndoEntryCoveringDeleteAndRetype(t *testing.T) {
	v := NewView()
	v.tabs = []*tab{{buf: &Buffer{Lines: []string{"foo bar"}}}}
	v.active = 0

	pressKeys(v, "cw")
	pressKeys(v, "baz")
	v.HandleKey(layout.Key{Named: layout.KeyEsc})

	if got := v.activeTab().buf.Lines[0]; got != "baz bar" {
		t.Fatalf("Lines[0] = %q, want %q", got, "baz bar")
	}
	v.undo(v.activeTab())
	if got := v.activeTab().buf.Lines[0]; got != "foo bar" {
		t.Fatalf("after undo, Lines[0] = %q, want the original restored in one step", got)
	}
}

func TestCIWIsOneUndoEntryCoveringDeleteAndRetype(t *testing.T) {
	v := NewView()
	v.tabs = []*tab{{buf: &Buffer{Lines: []string{"foo bar"}}}}
	v.active = 0

	pressKeys(v, "ciw")
	pressKeys(v, "baz")
	v.HandleKey(layout.Key{Named: layout.KeyEsc})

	if got := v.activeTab().buf.Lines[0]; got != "baz bar" {
		t.Fatalf("Lines[0] = %q, want %q", got, "baz bar")
	}
	v.undo(v.activeTab())
	if got := v.activeTab().buf.Lines[0]; got != "foo bar" {
		t.Fatalf("after undo, Lines[0] = %q, want the original restored in one step", got)
	}
}

func TestBareIStillEntersInsertModeWithNoOperatorArmed(t *testing.T) {
	v := NewView()
	v.tabs = []*tab{{buf: &Buffer{Lines: []string{"foo"}}}}
	v.active = 0

	v.HandleKey(layout.Key{Text: "i"})

	if v.mode != modeInsert {
		t.Fatalf("mode = %v, want modeInsert — bare 'i' with no operator armed", v.mode)
	}
}
