package editor

import (
	"strings"
	"testing"

	"github.com/bricejulia/nib/internal/layout"
)

func TestWordUnderCursorFindsTheTouchedIdentifier(t *testing.T) {
	tb := &tab{buf: &Buffer{Lines: []string{"count := total"}}}
	cases := []struct {
		name string
		col  int
		want string
	}{
		{"middle of first word", 2, "count"},
		{"just past first word's end", 5, "count"},
		{"on the second word", 11, "total"},
		{"on punctuation/space", 6, ""},
	}
	for _, c := range cases {
		t.Run(c.name, func(t *testing.T) {
			tb.cursorCol = c.col
			if got := wordUnderCursor(tb, 4); got != c.want {
				t.Errorf("wordUnderCursor at col %d = %q, want %q", c.col, got, c.want)
			}
		})
	}
}

func TestWordUnderCursorEmptyLineIsEmpty(t *testing.T) {
	tb := &tab{buf: &Buffer{Lines: []string{""}}}
	if got := wordUnderCursor(tb, 4); got != "" {
		t.Errorf("got %q, want empty", got)
	}
}

func TestByteOffsetPositionRoundTrip(t *testing.T) {
	buf := &Buffer{Lines: []string{"package sample", "", "func greet() {}"}}

	cases := []struct {
		ln, col int
	}{
		{0, 0},
		{0, 7},
		{1, 0},
		{2, 0},
		{2, 5},
		{2, len("func greet() {}")}, // end of last line
	}
	for _, c := range cases {
		offset := byteOffsetForPosition(buf, c.ln, c.col)
		gotLn, gotCol := positionForByteOffset(buf, offset)
		if gotLn != c.ln || gotCol != c.col {
			t.Errorf("round-trip (%d,%d) -> offset %d -> (%d,%d)", c.ln, c.col, offset, gotLn, gotCol)
		}
	}
}

func TestByteOffsetForPositionMatchesJoinedSource(t *testing.T) {
	buf := &Buffer{Lines: []string{"abc", "de", "f"}}
	// "abc\nde\nf" -> offset of 'd' (line 1, col 0) should be 4.
	if got := byteOffsetForPosition(buf, 1, 0); got != 4 {
		t.Errorf("byteOffsetForPosition(1,0) = %d, want 4", got)
	}
	// offset of 'f' (line 2, col 0) should be 7.
	if got := byteOffsetForPosition(buf, 2, 0); got != 7 {
		t.Errorf("byteOffsetForPosition(2,0) = %d, want 7", got)
	}
}

func ctrlKey(text string) layout.Key {
	return layout.Key{Text: text, Mods: layout.ModCtrl}
}

func TestGoToParentMovesToAnAncestorAndPushesAJump(t *testing.T) {
	v := NewView()
	v.Open(fixturePath(t, "highlight_sample.go"))
	tb := v.activeTab()
	tb.cursorLn, tb.cursorCol = 3, 5 // inside "func greet(name string) {"
	startLn, startCol := tb.cursorLn, tb.cursorCol

	if !v.HandleKey(ctrlKey("g")) {
		t.Fatal("expected Ctrl+g to be consumed")
	}
	if tb.cursorLn == startLn && tb.cursorCol == startCol {
		t.Fatal("expected the cursor to move to a parent node")
	}
	if len(v.jumpStack) != 1 {
		t.Fatalf("expected one jump pushed, got %d", len(v.jumpStack))
	}
	if v.jumpStack[0].ln != startLn || v.jumpStack[0].col != startCol {
		t.Fatalf("pushed jump = %+v, want (%d,%d)", v.jumpStack[0], startLn, startCol)
	}
}

func TestGoToParentRepeatedPressesClimbFurther(t *testing.T) {
	v := NewView()
	v.Open(fixturePath(t, "highlight_sample.go"))
	tb := v.activeTab()
	tb.cursorLn, tb.cursorCol = 3, 5

	v.HandleKey(ctrlKey("g"))
	afterOne := tb.cursorLn
	oneCol := tb.cursorCol
	v.HandleKey(ctrlKey("g"))
	if tb.cursorLn == afterOne && tb.cursorCol == oneCol {
		t.Fatal("expected a second Ctrl+g to climb further, cursor did not move")
	}
	if len(v.jumpStack) != 2 {
		t.Fatalf("expected two jumps pushed, got %d", len(v.jumpStack))
	}
}

// TestGoToParentDoesNotGetStuckOnSameStartByteAncestors is a regression
// test: a bare call with no receiver ("helper()") has its identifier,
// call_expression, and expression_statement nodes all share the exact
// same start byte, which used to make repeated Ctrl+g presses hit a fixed
// point (re-querying "the node at the cursor" after moving to a parent's
// start just re-found the same innermost node, forever) instead of
// climbing further each time.
func TestGoToParentDoesNotGetStuckOnSameStartByteAncestors(t *testing.T) {
	lines := []string{
		"package main",
		"",
		"func helper() {}",
		"",
		"func main() {",
		"\thelper()",
		"}",
	}
	v := NewView()
	v.tabs = []*tab{{
		path: "test.go",
		buf:  &Buffer{Path: "test.go", Lines: lines, Source: []byte(strings.Join(lines, "\n"))},
	}}
	v.active = 0
	tb := v.activeTab()
	line := lines[5] // "\thelper()"
	rawIdx := strings.Index(line, "helper") + 1
	tb.cursorLn = 5
	tb.cursorCol = expandedColForRawIndex(line, rawIdx, tabWidthOf(tb))

	seen := map[[2]int]bool{{tb.cursorLn, tb.cursorCol}: true}
	moved := 0
	for i := 0; i < 6; i++ {
		before := [2]int{tb.cursorLn, tb.cursorCol}
		v.HandleKey(ctrlKey("g"))
		after := [2]int{tb.cursorLn, tb.cursorCol}
		if after == before {
			break // reached the root (or an outermost same-start node): expected to stop eventually
		}
		if seen[after] {
			t.Fatalf("press %d returned to an already-visited position %v — stuck in a cycle", i+1, after)
		}
		seen[after] = true
		moved++
	}
	if moved < 2 {
		t.Fatalf("expected at least 2 presses to make distinct progress, got %d", moved)
	}
}

func TestJumpBackReturnsToPriorPosition(t *testing.T) {
	v := NewView()
	v.Open(fixturePath(t, "highlight_sample.go"))
	tb := v.activeTab()
	tb.cursorLn, tb.cursorCol = 3, 5
	startLn, startCol := tb.cursorLn, tb.cursorCol

	v.HandleKey(ctrlKey("g"))
	if !v.HandleKey(ctrlKey("b")) {
		t.Fatal("expected Ctrl+b to be consumed")
	}
	if tb.cursorLn != startLn || tb.cursorCol != startCol {
		t.Fatalf("cursor = (%d,%d), want (%d,%d)", tb.cursorLn, tb.cursorCol, startLn, startCol)
	}
	if len(v.jumpStack) != 0 {
		t.Fatalf("expected the jump stack to be drained, got %d entries", len(v.jumpStack))
	}
}

func TestJumpBackOnEmptyStackIsNoop(t *testing.T) {
	v := NewView()
	v.tabs = []*tab{{buf: &Buffer{Lines: []string{"x"}}}}
	v.active = 0

	if !v.HandleKey(ctrlKey("b")) {
		t.Fatal("expected Ctrl+b to still be consumed with an empty jump stack")
	}
}

func TestGoToDefinitionJumpsToDeclaration(t *testing.T) {
	// gotreesitter's ExtractDefinitionSpans only captures top-level Go
	// declarations (functions/types/consts/vars), not local ":=" variables
	// inside a function body — confirmed by inspecting its output against
	// testdata/highlight_sample.go. So this exercises the scenario the
	// extractor actually supports: a call site jumping to its function's
	// top-level declaration, using an in-memory fixture rather than a file
	// on disk (parseTree only needs Buffer.Path's extension to detect Go).
	lines := []string{
		"package main",
		"",
		"func helper() {",
		"}",
		"",
		"func main() {",
		"\thelper()",
		"}",
	}
	v := NewView()
	v.tabs = []*tab{{
		path: "test.go",
		// parseTree parses Source, not Lines — a hand-built Buffer (unlike
		// one from Load) has to set both itself, kept in sync.
		buf: &Buffer{Path: "test.go", Lines: lines, Source: []byte(strings.Join(lines, "\n"))},
	}}
	v.active = 0
	tb := v.activeTab()
	line := tb.buf.Lines[6] // "\thelper()"
	rawIdx := strings.Index(line, "helper") + 1
	tb.cursorLn = 6
	tb.cursorCol = expandedColForRawIndex(line, rawIdx, tabWidthOf(tb))

	if !v.HandleKey(ctrlKey("]")) {
		t.Fatal("expected Ctrl+] to be consumed")
	}
	if tb.cursorLn != 2 { // "func helper() {" is line 3 (0-indexed 2)
		t.Fatalf("cursorLn = %d, want 2 (the \"func helper()\" declaration)", tb.cursorLn)
	}
	if len(v.jumpStack) != 1 {
		t.Fatalf("expected one jump pushed, got %d", len(v.jumpStack))
	}
}

func TestGoToDefinitionNoMatchIsNoop(t *testing.T) {
	v := NewView()
	v.Open(fixturePath(t, "highlight_sample.go"))
	tb := v.activeTab()
	tb.cursorLn, tb.cursorCol = 0, 8 // "package sample" -> on "sample", no declaration named that

	startLn, startCol := tb.cursorLn, tb.cursorCol
	v.HandleKey(ctrlKey("]"))
	if tb.cursorLn != startLn || tb.cursorCol != startCol {
		t.Fatalf("expected no movement, got (%d,%d)", tb.cursorLn, tb.cursorCol)
	}
	if len(v.jumpStack) != 0 {
		t.Fatal("expected no jump pushed when nothing matched")
	}
}

// TestViewWordUnderCursorDelegatesToTheActiveTab guards the View-level
// wrapper cmd/nib/main.go's global "find references" (Ctrl+F) handler
// actually calls — the word-touching logic itself is covered by
// TestWordUnderCursorFindsTheTouchedIdentifier above; this only needs to
// confirm the wrapper resolves the active tab and tabWidth correctly.
func TestViewWordUnderCursorDelegatesToTheActiveTab(t *testing.T) {
	v := NewView()
	v.tabs = []*tab{{buf: &Buffer{Lines: []string{"count := total"}}}}
	v.active = 0
	v.activeTab().cursorCol = 2

	if got := v.WordUnderCursor(); got != "count" {
		t.Fatalf("WordUnderCursor() = %q, want %q", got, "count")
	}
}

func TestViewWordUnderCursorEmptyWhenNotTouchingAWord(t *testing.T) {
	v := NewView()
	v.tabs = []*tab{{buf: &Buffer{Lines: []string{"a  b"}}}}
	v.active = 0
	v.activeTab().cursorCol = 2 // the middle of the two-space gap between "a" and "b" — not touching either

	if got := v.WordUnderCursor(); got != "" {
		t.Fatalf("WordUnderCursor() = %q, want empty", got)
	}
}

func TestViewWordUnderCursorNoActiveTabReturnsEmpty(t *testing.T) {
	v := NewView() // no tabs open at all — the "No file open" placeholder state
	if got := v.WordUnderCursor(); got != "" {
		t.Fatalf("WordUnderCursor() = %q, want empty with no active tab", got)
	}
}
