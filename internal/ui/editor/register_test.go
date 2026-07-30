package editor

import (
	"strings"
	"testing"

	"github.com/bricejulia/kiwi/internal/layout"
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
	if v.pendingAction != "delete_line" {
		t.Fatalf("pendingAction = %q, want %q", v.pendingAction, "delete_line")
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
	if v.pendingAction != "delete_line" {
		t.Fatalf("pendingAction = %q, want the trailing 'd' to have re-armed", v.pendingAction)
	}
}

func TestUnboundKeyClearsAPendingOperator(t *testing.T) {
	v := NewView()
	v.tabs = []*tab{{buf: &Buffer{Lines: []string{"one", "two"}}}}
	v.active = 0

	v.HandleKey(layout.Key{Text: "d"})
	v.HandleKey(layout.Key{Text: "é"}) // bound to nothing in Normal mode
	if v.pendingAction != "" {
		t.Fatalf("pendingAction = %q, want cleared by an unbound key", v.pendingAction)
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
