package editor

import (
	"strings"
	"testing"

	"github.com/bricejulia/nib/internal/layout"
)

// pressKeys sends each rune of s as its own Normal-mode keypress, so a
// two-key gesture like "dd" reads the way it's typed.
func pressKeys(v *View, s string) {
	for _, r := range s {
		v.HandleKey(layout.Key{Text: string(r)})
	}
}

func TestDeleteLineDoubledDeletesTheCursorLine(t *testing.T) {
	v := NewView()
	v.tabs = []*tab{{buf: &Buffer{Lines: []string{"one", "two", "three"}}}}
	v.active = 0
	v.activeTab().cursorLn = 1

	pressKeys(v, "dd")

	if got := strings.Join(v.activeTab().buf.Lines, "|"); got != "one|three" {
		t.Fatalf("Lines = %q, want %q", got, "one|three")
	}
	if ln := v.activeTab().cursorLn; ln != 1 {
		t.Fatalf("cursorLn = %d, want 1 (the line that moved up)", ln)
	}
}

func TestCountedDDDeletesThatManyLinesFromTheCursor(t *testing.T) {
	v := NewView()
	v.tabs = []*tab{{buf: &Buffer{Lines: []string{"one", "two", "three", "four", "five"}}}}
	v.active = 0
	v.activeTab().cursorLn = 1

	pressKeys(v, "3dd")

	if got := strings.Join(v.activeTab().buf.Lines, "|"); got != "one|five" {
		t.Fatalf("Lines = %q, want %q", got, "one|five")
	}
}

func TestDCountDDeletesThatManyLinesFromTheCursor(t *testing.T) {
	v := NewView()
	v.tabs = []*tab{{buf: &Buffer{Lines: []string{"one", "two", "three", "four", "five"}}}}
	v.active = 0
	v.activeTab().cursorLn = 1

	pressKeys(v, "d3d")

	if got := strings.Join(v.activeTab().buf.Lines, "|"); got != "one|five" {
		t.Fatalf("Lines = %q, want %q", got, "one|five")
	}
}

func TestCountsBeforeAndAfterTheOperatorMultiply(t *testing.T) {
	v := NewView()
	v.tabs = []*tab{{buf: &Buffer{Lines: []string{"1", "2", "3", "4", "5", "6", "7"}}}}
	v.active = 0

	pressKeys(v, "2d3d") // 2 * 3 = 6 lines, vim's own count-multiplication rule

	if got := strings.Join(v.activeTab().buf.Lines, "|"); got != "7" {
		t.Fatalf("Lines = %q, want %q", got, "7")
	}
}

func TestCountedYYYanksThatManyLinesWithoutDeleting(t *testing.T) {
	v := NewView()
	v.tabs = []*tab{{buf: &Buffer{Lines: []string{"one", "two", "three"}}}}
	v.active = 0

	pressKeys(v, "2yy")

	if got := strings.Join(v.activeTab().buf.Lines, "|"); got != "one|two|three" {
		t.Fatalf("yank changed the buffer: %q", got)
	}
	if got := strings.Join(v.register.Lines(), "|"); got != "one|two" {
		t.Fatalf("register = %q, want %q", got, "one|two")
	}
}

func TestCCClearsTheLineAndEntersInsertMode(t *testing.T) {
	v := NewView()
	v.tabs = []*tab{{buf: &Buffer{Lines: []string{"one", "two", "three"}}}}
	v.active = 0
	v.activeTab().cursorLn = 1

	pressKeys(v, "cc")

	if got := strings.Join(v.activeTab().buf.Lines, "|"); got != "one||three" {
		t.Fatalf("Lines = %q, want %q (the middle line cleared, not removed)", got, "one||three")
	}
	if v.mode != modeInsert {
		t.Fatalf("mode = %v, want modeInsert", v.mode)
	}
	if got := strings.Join(v.register.Lines(), "|"); got != "two" {
		t.Fatalf("register = %q, want the cleared text %q", got, "two")
	}
}

func TestCountedCCClearsThatManyLines(t *testing.T) {
	v := NewView()
	v.tabs = []*tab{{buf: &Buffer{Lines: []string{"one", "two", "three", "four"}}}}
	v.active = 0
	v.activeTab().cursorLn = 1

	pressKeys(v, "2cc")

	if got := strings.Join(v.activeTab().buf.Lines, "|"); got != "one||four" {
		t.Fatalf("Lines = %q, want %q", got, "one||four")
	}
}

func TestCCOnEntireBufferLeavesExactlyOneBlankLine(t *testing.T) {
	v := NewView()
	v.tabs = []*tab{{buf: &Buffer{Lines: []string{"one", "two"}}}}
	v.active = 0

	pressKeys(v, "2cc")

	if got := strings.Join(v.activeTab().buf.Lines, "|"); got != "" {
		t.Fatalf("Lines = %q, want a single empty line", got)
	}
	if len(v.activeTab().buf.Lines) != 1 {
		t.Fatalf("Lines = %+v, want exactly one empty line, not two", v.activeTab().buf.Lines)
	}
}

func TestCCIsOneUndoEntryCoveringClearAndRetype(t *testing.T) {
	v := NewView()
	v.tabs = []*tab{{buf: &Buffer{Lines: []string{"one", "two", "three"}}}}
	v.active = 0
	v.activeTab().cursorLn = 1

	pressKeys(v, "cc")
	pressKeys(v, "REPLACED")
	v.HandleKey(layout.Key{Named: layout.KeyEsc})

	if got := strings.Join(v.activeTab().buf.Lines, "|"); got != "one|REPLACED|three" {
		t.Fatalf("Lines = %q, want %q", got, "one|REPLACED|three")
	}
	v.undo(v.activeTab())
	if got := strings.Join(v.activeTab().buf.Lines, "|"); got != "one|two|three" {
		t.Fatalf("after undo, Lines = %q, want the original restored in one step", got)
	}
}

func TestCountedBareMotionRepeatsTheMovement(t *testing.T) {
	v := NewView()
	v.tabs = []*tab{{buf: &Buffer{Lines: []string{"one", "two", "three", "four", "five"}}}}
	v.active = 0

	pressKeys(v, "3j")

	if ln := v.activeTab().cursorLn; ln != 3 {
		t.Fatalf("cursorLn = %d, want 3", ln)
	}
}

func TestSingleDDoesNotDeleteAnything(t *testing.T) {
	v := NewView()
	v.tabs = []*tab{{buf: &Buffer{Lines: []string{"one", "two"}}}}
	v.active = 0

	if !v.HandleKey(layout.Key{Text: "d"}) {
		t.Fatal("expected the first 'd' to be consumed")
	}
	if got := strings.Join(v.activeTab().buf.Lines, "|"); got != "one|two" {
		t.Fatalf("a lone 'd' changed the buffer: %q", got)
	}
	if v.pendingOp.action != "delete_line" {
		t.Fatalf("pendingOp.action = %q, want %q", v.pendingOp.action, "delete_line")
	}
}

func TestKeyBetweenTheTwoDsAbortsTheDelete(t *testing.T) {
	v := NewView()
	v.tabs = []*tab{{buf: &Buffer{Lines: []string{"one", "two"}}}}
	v.active = 0

	pressKeys(v, "djd") // "d", cursor down, "d" — arms twice, never doubles

	if got := strings.Join(v.activeTab().buf.Lines, "|"); got != "one|two" {
		t.Fatalf("Lines = %q, want the buffer untouched", got)
	}
	if v.pendingOp.action != "delete_line" {
		t.Fatalf("pendingOp.action = %q, want the trailing 'd' to have re-armed", v.pendingOp.action)
	}
}

func TestUnboundKeyClearsAPendingOperator(t *testing.T) {
	v := NewView()
	v.tabs = []*tab{{buf: &Buffer{Lines: []string{"one", "two"}}}}
	v.active = 0

	v.HandleKey(layout.Key{Text: "d"})
	v.HandleKey(layout.Key{Text: "é"}) // bound to nothing in Normal mode
	if v.pendingOp.action != "" {
		t.Fatalf("pendingOp.action = %q, want cleared by an unbound key", v.pendingOp.action)
	}

	v.HandleKey(layout.Key{Text: "d"}) // this is a FIRST 'd' again, not a second
	if got := strings.Join(v.activeTab().buf.Lines, "|"); got != "one|two" {
		t.Fatalf("Lines = %q, want the buffer untouched", got)
	}
}

func TestExitEditingModesDiscardsAPendingOperator(t *testing.T) {
	v := NewView()
	v.tabs = []*tab{{buf: &Buffer{Lines: []string{"one", "two"}}}}
	v.active = 0

	v.HandleKey(layout.Key{Text: "d"})
	v.ExitEditingModes() // focus moves away mid-gesture
	v.HandleKey(layout.Key{Text: "d"})

	if got := strings.Join(v.activeTab().buf.Lines, "|"); got != "one|two" {
		t.Fatalf("Lines = %q, want the buffer untouched", got)
	}
}

func TestDeleteLineIsOneUndoEntryAndRestoresTheLine(t *testing.T) {
	v := NewView()
	v.tabs = []*tab{{buf: &Buffer{Lines: []string{"one", "two", "three"}}}}
	v.active = 0
	v.activeTab().cursorLn = 1

	pressKeys(v, "dd")
	if n := len(v.activeTab().buf.undoStack); n != 1 {
		t.Fatalf("undoStack has %d entries, want 1 for a single 'dd'", n)
	}

	v.HandleKey(layout.Key{Text: "u"})
	if got := strings.Join(v.activeTab().buf.Lines, "|"); got != "one|two|three" {
		t.Fatalf("after undo Lines = %q, want the deleted line back", got)
	}
}

func TestDeleteOnlyLineLeavesOneEmptyLine(t *testing.T) {
	v := NewView()
	v.tabs = []*tab{{buf: &Buffer{Lines: []string{"only"}}}}
	v.active = 0

	pressKeys(v, "dd")

	lines := v.activeTab().buf.Lines
	if len(lines) != 1 || lines[0] != "" {
		t.Fatalf("Lines = %+v, want exactly one empty line", lines)
	}
	if ln := v.activeTab().cursorLn; ln != 0 {
		t.Fatalf("cursorLn = %d, want 0", ln)
	}
}

func TestDeleteLastLineClampsCursorOntoTheNewLast(t *testing.T) {
	v := NewView()
	v.tabs = []*tab{{buf: &Buffer{Lines: []string{"one", "two"}}}}
	v.active = 0
	v.activeTab().cursorLn = 1

	pressKeys(v, "dd")

	if ln := v.activeTab().cursorLn; ln != 0 {
		t.Fatalf("cursorLn = %d, want 0 (clamped onto the new last line)", ln)
	}
}

func TestYankAndPutDuplicatesALineBelowTheCursor(t *testing.T) {
	v := NewView()
	v.tabs = []*tab{{buf: &Buffer{Lines: []string{"one", "two", "three"}}}}
	v.active = 0
	v.activeTab().cursorLn = 0

	pressKeys(v, "yyp")

	if got := strings.Join(v.activeTab().buf.Lines, "|"); got != "one|one|two|three" {
		t.Fatalf("Lines = %q, want %q", got, "one|one|two|three")
	}
	if ln := v.activeTab().cursorLn; ln != 1 {
		t.Fatalf("cursorLn = %d, want 1 (the put line)", ln)
	}
}

func TestYankLeavesTheBufferAndUndoStackAlone(t *testing.T) {
	v := NewView()
	v.tabs = []*tab{{buf: &Buffer{Lines: []string{"one", "two"}}}}
	v.active = 0

	pressKeys(v, "yy")

	if got := strings.Join(v.activeTab().buf.Lines, "|"); got != "one|two" {
		t.Fatalf("Lines = %q, want unchanged", got)
	}
	if n := len(v.activeTab().buf.undoStack); n != 0 {
		t.Fatalf("undoStack has %d entries, want 0 — a yank changes nothing", n)
	}
}

func TestDeleteThenPutMovesALine(t *testing.T) {
	v := NewView()
	v.tabs = []*tab{{buf: &Buffer{Lines: []string{"one", "two", "three"}}}}
	v.active = 0

	pressKeys(v, "dd") // cut "one"; "two" is now line 0
	pressKeys(v, "jp") // put it after "three"

	if got := strings.Join(v.activeTab().buf.Lines, "|"); got != "two|three|one" {
		t.Fatalf("Lines = %q, want %q", got, "two|three|one")
	}
}

func TestPutWithAnEmptyRegisterIsANoop(t *testing.T) {
	v := NewView()
	v.tabs = []*tab{{buf: &Buffer{Lines: []string{"one"}}}}
	v.active = 0

	if !v.HandleKey(layout.Key{Text: "p"}) {
		t.Fatal("expected 'p' to be consumed even with nothing yanked")
	}
	if got := strings.Join(v.activeTab().buf.Lines, "|"); got != "one" {
		t.Fatalf("Lines = %q, want unchanged", got)
	}
	if n := len(v.activeTab().buf.undoStack); n != 0 {
		t.Fatalf("undoStack has %d entries, want 0 for a no-op put", n)
	}
}

func TestPutIsOneUndoEntry(t *testing.T) {
	v := NewView()
	v.tabs = []*tab{{buf: &Buffer{Lines: []string{"one", "two"}}}}
	v.active = 0

	pressKeys(v, "yyp")
	v.HandleKey(layout.Key{Text: "u"})

	if got := strings.Join(v.activeTab().buf.Lines, "|"); got != "one|two" {
		t.Fatalf("after undo Lines = %q, want the put reverted in one step", got)
	}
}

func TestSharedRegisterCarriesAYankBetweenPanes(t *testing.T) {
	reg := NewRegister()
	src, dst := NewView(), NewView()
	src.SetRegister(reg)
	dst.SetRegister(reg)
	src.tabs = []*tab{{buf: &Buffer{Lines: []string{"copy me"}}}}
	src.active = 0
	dst.tabs = []*tab{{buf: &Buffer{Lines: []string{"target"}}}}
	dst.active = 0

	pressKeys(src, "yy")
	pressKeys(dst, "p")

	if got := strings.Join(dst.activeTab().buf.Lines, "|"); got != "target|copy me" {
		t.Fatalf("Lines = %q, want the yank to have crossed panes", got)
	}
}

func TestPrivateRegistersDoNotLeakBetweenPanes(t *testing.T) {
	a, b := NewView(), NewView() // never given a shared register
	a.tabs = []*tab{{buf: &Buffer{Lines: []string{"copy me"}}}}
	a.active = 0
	b.tabs = []*tab{{buf: &Buffer{Lines: []string{"target"}}}}
	b.active = 0

	pressKeys(a, "yy")
	pressKeys(b, "p")

	if got := strings.Join(b.activeTab().buf.Lines, "|"); got != "target" {
		t.Fatalf("Lines = %q, want unchanged — the panes share no register", got)
	}
}

func TestRegisterCopiesOnSetAndGet(t *testing.T) {
	r := NewRegister()
	src := []string{"held"}
	r.Set(src)
	src[0] = "mutated after Set"
	if got := r.Lines(); got[0] != "held" {
		t.Fatalf("Lines()[0] = %q, want %q — Set must copy", got[0], "held")
	}

	out := r.Lines()
	out[0] = "mutated after Lines"
	if got := r.Lines(); got[0] != "held" {
		t.Fatalf("Lines()[0] = %q, want %q — Lines must copy", got[0], "held")
	}
}

func TestEmptyRegisterHasNoLines(t *testing.T) {
	if got := NewRegister().Lines(); got != nil {
		t.Fatalf("Lines() = %+v, want nil for an empty register", got)
	}
}

func TestPuttingALineDeletedFromAnEditedBufferKeepsItsText(t *testing.T) {
	v := NewView()
	v.tabs = []*tab{{buf: &Buffer{Lines: []string{"cut me", "other"}}}}
	v.active = 0

	pressKeys(v, "dd")
	// Edit the line the buffer reused, then put: the register must still
	// hold "cut me", not whatever the buffer's slice says now.
	v.HandleKey(layout.Key{Text: "i"})
	v.HandleKey(layout.Key{Text: "Z"})
	v.HandleKey(layout.Key{Named: layout.KeyEsc})
	pressKeys(v, "p")

	if got := strings.Join(v.activeTab().buf.Lines, "|"); got != "Zother|cut me" {
		t.Fatalf("Lines = %q, want %q", got, "Zother|cut me")
	}
}

func TestDollarAndZeroJumpToLineEnds(t *testing.T) {
	v := NewView()
	v.tabs = []*tab{{buf: &Buffer{Lines: []string{"hello"}}}}
	v.active = 0

	if !v.HandleKey(layout.Key{Text: "$"}) {
		t.Fatal("expected '$' to be consumed")
	}
	if col := v.activeTab().cursorCol; col != 5 {
		t.Fatalf("cursorCol = %d, want 5 (one past the last rune, like End)", col)
	}

	if !v.HandleKey(layout.Key{Text: "0"}) {
		t.Fatal("expected '0' to be consumed")
	}
	if col := v.activeTab().cursorCol; col != 0 {
		t.Fatalf("cursorCol = %d, want 0", col)
	}
}

func TestDollarOnATabbedLineUsesExpandedWidth(t *testing.T) {
	v := NewView()
	v.tabs = []*tab{{buf: &Buffer{Lines: []string{"\tab"}}}}
	v.active = 0

	v.HandleKey(layout.Key{Text: "$"})

	// tabWidth 4: the tab expands to 4 runes, plus "ab" = 6.
	if col := v.activeTab().cursorCol; col != 6 {
		t.Fatalf("cursorCol = %d, want 6", col)
	}
}

func TestZeroAndDollarInsertLiterallyWhileInserting(t *testing.T) {
	v := NewView()
	v.tabs = []*tab{{buf: &Buffer{Lines: []string{""}}}}
	v.active = 0

	v.HandleKey(layout.Key{Text: "i"})
	pressKeys(v, "0$dyp")

	if got := v.activeTab().buf.Lines[0]; got != "0$dyp" {
		t.Fatalf("Lines[0] = %q, want the keys typed literally", got)
	}
}

func TestCharwisePutSplicesIntoTheCurrentLine(t *testing.T) {
	v, tb := selectionView("abcd")
	v.register.SetCharwise([]string{"XY"})
	tb.cursorCol = 1 // on "b"

	v.putAfter(tb)

	if got := tb.buf.Lines[0]; got != "abXYcd" {
		t.Errorf("line = %q, want %q (spliced after the cursor, like vim's p)", got, "abXYcd")
	}
}

func TestCharwisePutAtEndOfLineAppends(t *testing.T) {
	// There is no character to go "after", so the cursor position is already
	// the insertion point.
	v, tb := selectionView("ab")
	v.register.SetCharwise([]string{"XY"})
	tb.cursorCol = 2

	v.putAfter(tb)

	if got := tb.buf.Lines[0]; got != "abXY" {
		t.Errorf("line = %q, want %q", got, "abXY")
	}
}

func TestCharwisePutOnAnEmptyLineInsertsAtColumnZero(t *testing.T) {
	v, tb := selectionView("")
	v.register.SetCharwise([]string{"hi"})

	v.putAfter(tb)

	if got := tb.buf.Lines[0]; got != "hi" {
		t.Errorf("line = %q, want %q", got, "hi")
	}
}

func TestCharwisePutLeavesCursorOnTheLastCharacterPut(t *testing.T) {
	// vim's behaviour, and what makes a repeated "p" append rather than
	// build up in reverse.
	v, tb := selectionView("ab")
	v.register.SetCharwise([]string{"XYZ"})
	tb.cursorCol = 0

	v.putAfter(tb)

	// "aXYZb": the last character put is the "Z" at index 3.
	if tb.cursorCol != 3 {
		t.Errorf("cursorCol = %d, want 3 (on the %q)", tb.cursorCol, "Z")
	}
}

func TestRepeatedCharwisePutAppendsRatherThanReverses(t *testing.T) {
	v, tb := selectionView("")
	v.register.SetCharwise([]string{"ab"})

	v.putAfter(tb)
	v.putAfter(tb)

	if got := tb.buf.Lines[0]; got != "abab" {
		t.Errorf("line = %q, want %q", got, "abab")
	}
}

func TestCharwisePutOfAMultiLineFragmentSplitsTheLine(t *testing.T) {
	v, tb := selectionView("[]")
	v.register.SetCharwise([]string{"one", "two", "three"})
	tb.cursorCol = 0 // on "["

	v.putAfter(tb)

	want := []string{"[one", "two", "three]"}
	if len(tb.buf.Lines) != len(want) {
		t.Fatalf("lines = %q, want %q", tb.buf.Lines, want)
	}
	for i := range want {
		if tb.buf.Lines[i] != want[i] {
			t.Fatalf("lines = %q, want %q", tb.buf.Lines, want)
		}
	}
}

func TestCharwisePutOfATwoLineFragmentHasNoMiddleLines(t *testing.T) {
	v, tb := selectionView("[]")
	v.register.SetCharwise([]string{"one", "two"})
	tb.cursorCol = 0

	v.putAfter(tb)

	want := []string{"[one", "two]"}
	if len(tb.buf.Lines) != len(want) {
		t.Fatalf("lines = %q, want %q", tb.buf.Lines, want)
	}
	for i := range want {
		if tb.buf.Lines[i] != want[i] {
			t.Fatalf("lines = %q, want %q", tb.buf.Lines, want)
		}
	}
}

func TestCopyThenPutRoundTripsTheSelectedText(t *testing.T) {
	// The property that matters: TextBetween takes text apart and putCharwise
	// puts it back together, so a copy followed by a put reproduces it.
	v, tb := selectionView("one", "two", "three")
	g := gutterFor(tb)

	v.HandleMouse(press(g+1, 1, 1)) // after "o" of "one"
	v.HandleMouse(motion(g+2, 3))   // into "three"
	v.copySelection(tb)

	// Put it at the very end of the buffer.
	tb.clearSelection()
	tb.cursorLn = 2
	tb.cursorCol = len([]rune("three"))
	v.putAfter(tb)

	joined := strings.Join(tb.buf.Lines, "\n")
	if want := "one\ntwo\nthreene\ntwo\nth"; joined != want {
		t.Errorf("buffer =\n%q\nwant\n%q", joined, want)
	}
}

func TestCharwisePutIsOneUndoEntry(t *testing.T) {
	v, tb := selectionView("[]")
	v.register.SetCharwise([]string{"one", "two", "three"})
	tb.cursorCol = 0

	v.putAfter(tb)
	if n := len(tb.buf.undoStack); n != 1 {
		t.Fatalf("undoStack has %d entries, want 1 — a put is one change", n)
	}

	v.undo(tb)
	if len(tb.buf.Lines) != 1 || tb.buf.Lines[0] != "[]" {
		t.Errorf("after undo, lines = %q, want [\"[]\"]", tb.buf.Lines)
	}
}

func TestLinewisePutIsUnaffectedByTheCharwiseBranch(t *testing.T) {
	// Regression guard on "dd"/"yy"/"p", which must keep inserting whole
	// lines below the cursor.
	v, tb := selectionView("one", "two")
	v.register.Set([]string{"inserted"})
	tb.cursorLn = 0

	v.putAfter(tb)

	want := []string{"one", "inserted", "two"}
	if len(tb.buf.Lines) != len(want) {
		t.Fatalf("lines = %q, want %q", tb.buf.Lines, want)
	}
	for i := range want {
		if tb.buf.Lines[i] != want[i] {
			t.Fatalf("lines = %q, want %q", tb.buf.Lines, want)
		}
	}
	if tb.cursorLn != 1 || tb.cursorCol != 0 {
		t.Errorf("cursor = (%d,%d), want (1,0) — start of the put line", tb.cursorLn, tb.cursorCol)
	}
}

func TestSetMarksLinewiseAndSetCharwiseMarksCharwise(t *testing.T) {
	// The flag has to be stored, not inferred: []string{"foo"} is a valid
	// value for both.
	r := NewRegister()
	r.SetCharwise([]string{"foo"})
	if !r.Charwise() {
		t.Error("SetCharwise should mark the register charwise")
	}
	r.Set([]string{"foo"})
	if r.Charwise() {
		t.Error("Set should mark the register linewise")
	}
}
