package editor

import (
	"strings"
	"testing"

	"github.com/bricejulia/kiwi/internal/layout"
)

type fakeWindow struct {
	cols, rows int
	lines      []string
	segs       [][]layout.Segment
}

func newFakeWindow(cols, rows int) *fakeWindow {
	return &fakeWindow{cols: cols, rows: rows, lines: make([]string, rows), segs: make([][]layout.Segment, rows)}
}

func (w *fakeWindow) Size() (int, int) { return w.cols, w.rows }
func (w *fakeWindow) Println(row int, segs ...layout.Segment) {
	if row < 0 || row >= len(w.lines) {
		return
	}
	text := ""
	for _, s := range segs {
		text += s.Text
	}
	w.lines[row] = text
	w.segs[row] = segs
}
func (w *fakeWindow) Clear() {
	for i := range w.lines {
		w.lines[i] = ""
		w.segs[i] = nil
	}
}

func TestViewShowsPlaceholderWhenNoFileOpen(t *testing.T) {
	v := NewView()
	w := newFakeWindow(40, 10)
	v.Render(w)

	joined := strings.Join(w.lines, "\n")
	if !strings.Contains(joined, "No file open") {
		t.Errorf("expected a placeholder message when no tabs are open, got:\n%s", joined)
	}
}

func TestViewRenderShowsGutterAndContentBelowTabBar(t *testing.T) {
	v := NewView()
	v.Open(fixturePath(t, "editor_sample.txt"))

	w := newFakeWindow(40, 10)
	v.Render(w)

	if !strings.Contains(w.lines[0], "editor_sample.txt") {
		t.Errorf("row 0 should be the tab bar showing the open file, got %q", w.lines[0])
	}
	if !strings.Contains(w.lines[1], "line one") {
		t.Errorf("row 1 should show the first buffer line, got %q", w.lines[1])
	}
	if !strings.HasPrefix(strings.TrimSpace(w.lines[1]), "1") {
		t.Errorf("row 1 should have a gutter number, got %q", w.lines[1])
	}
}

func TestViewRenderExpandsTabsBeforeGutterContent(t *testing.T) {
	v := NewView()
	v.Open(fixturePath(t, "editor_sample.txt"))
	w := newFakeWindow(40, 10)
	v.Render(w)

	// Buffer line index 1 is "\ttabbed line", rendered on row 2 (row 0 is
	// the tab bar, row 1 is buffer line 0) - the raw tab must not reach
	// the fake window as a literal \t.
	if strings.Contains(w.lines[2], "\t") {
		t.Errorf("rendered line should have tabs expanded, got %q", w.lines[2])
	}
}

func TestViewOpenMissingFileShowsError(t *testing.T) {
	v := NewView()
	v.Open(fixturePath(t, "does-not-exist.txt"))

	w := newFakeWindow(40, 10)
	v.Render(w)
	if !strings.Contains(w.lines[1], "error") {
		t.Errorf("expected an error message rendered below the tab bar, got %q", w.lines[1])
	}
}

func TestViewCursorPositionTracksMovement(t *testing.T) {
	v := NewView()
	v.Open(fixturePath(t, "editor_sample.txt"))
	w := newFakeWindow(40, 10)
	v.Render(w)

	col, row, ok := v.CursorPosition()
	if !ok {
		t.Fatal("expected CursorPosition to report ok=true with a file open")
	}
	if row != 1 {
		t.Errorf("cursor starts on buffer line 0, which renders on row 1 (row 0 is the tab bar); got row=%d", row)
	}
	gutterCol := gutterWidthFor(v.activeTab())
	if col != gutterCol {
		t.Errorf("cursor starts at column 0 of the line, so screen col should equal the gutter width (%d), got %d", gutterCol, col)
	}

	v.HandleKey(layout.Key{Named: layout.KeyRight})
	v.Render(w)
	col2, _, _ := v.CursorPosition()
	if col2 != col+1 {
		t.Errorf("moving right once should advance the screen column by 1, got %d -> %d", col, col2)
	}

	v.HandleKey(layout.Key{Named: layout.KeyDown})
	v.Render(w)
	_, row2, _ := v.CursorPosition()
	if row2 != row+1 {
		t.Errorf("moving down once should advance the screen row by 1, got %d -> %d", row, row2)
	}
}

func TestViewCursorPositionFalseWithNoFileOpen(t *testing.T) {
	v := NewView()
	if _, _, ok := v.CursorPosition(); ok {
		t.Fatal("expected CursorPosition ok=false with no tabs open")
	}
}

func TestViewStatusTextReflectsCursor(t *testing.T) {
	v := NewView()
	v.Open(fixturePath(t, "editor_sample.txt"))
	w := newFakeWindow(40, 10)
	v.Render(w)

	if got := v.StatusText(); got != "Ln 1, Col 1" {
		t.Errorf("got %q, want %q", got, "Ln 1, Col 1")
	}

	v.HandleKey(layout.Key{Named: layout.KeyDown})
	v.HandleKey(layout.Key{Named: layout.KeyRight})
	v.Render(w)
	if got := v.StatusText(); got != "Ln 2, Col 2" {
		t.Errorf("got %q, want %q", got, "Ln 2, Col 2")
	}
}

func TestViewStatusTextEmptyWithNoFileOpen(t *testing.T) {
	v := NewView()
	if got := v.StatusText(); got != "" {
		t.Errorf("got %q, want empty string", got)
	}
}

func TestViewCursorDownClampsAtLastLine(t *testing.T) {
	v := NewView()
	v.Open(fixturePath(t, "editor_sample.txt")) // 4 lines
	w := newFakeWindow(40, 10)
	v.Render(w)

	for i := 0; i < 20; i++ {
		v.HandleKey(layout.Key{Named: layout.KeyDown})
	}
	if v.activeTab().cursorLn != 3 {
		t.Fatalf("cursorLn = %d, want 3 (clamped to last line)", v.activeTab().cursorLn)
	}
}

func TestViewCursorColClampsAtLineLength(t *testing.T) {
	v := NewView()
	v.tabs = []*tab{{buf: &Buffer{Lines: []string{"abc"}}}}
	v.active = 0
	w := newFakeWindow(40, 10)
	v.Render(w)

	for i := 0; i < 20; i++ {
		v.HandleKey(layout.Key{Named: layout.KeyRight})
	}
	if v.activeTab().cursorCol != 3 {
		t.Fatalf("cursorCol = %d, want 3 (clamped to line length, cursor may sit one-past-the-end)", v.activeTab().cursorCol)
	}

	for i := 0; i < 20; i++ {
		v.HandleKey(layout.Key{Named: layout.KeyLeft})
	}
	if v.activeTab().cursorCol != 0 {
		t.Fatalf("cursorCol = %d, want 0", v.activeTab().cursorCol)
	}
}

func TestViewCursorColClampsWhenMovingToShorterLine(t *testing.T) {
	v := NewView()
	v.tabs = []*tab{{buf: &Buffer{Lines: []string{"a long first line", "x"}}}}
	v.active = 0
	w := newFakeWindow(40, 10)
	v.Render(w)

	v.activeTab().cursorCol = 10
	v.HandleKey(layout.Key{Named: layout.KeyDown})
	if v.activeTab().cursorCol != 1 {
		t.Fatalf("moving to a 1-char line should clamp cursorCol to 1, got %d", v.activeTab().cursorCol)
	}
}

func TestViewScrollsVerticallyWhenCursorPassesWindowBottom(t *testing.T) {
	// A regression test for the viewport-follow math specifically: with a
	// buffer taller than the window, moving the cursor past the bottom
	// row must advance topLine, not just clamp cursorLn.
	v := NewView()
	v.tabs = []*tab{{buf: &Buffer{Lines: linesN(100)}}}
	v.active = 0

	w := newFakeWindow(40, 10) // 1 tab-bar row + 9 body rows
	v.Render(w)
	if v.activeTab().topLine != 0 {
		t.Fatalf("topLine should start at 0, got %d", v.activeTab().topLine)
	}

	for i := 0; i < 15; i++ {
		v.HandleKey(layout.Key{Named: layout.KeyDown})
		v.Render(w)
	}

	tb := v.activeTab()
	if tb.cursorLn != 15 {
		t.Fatalf("cursorLn = %d, want 15", tb.cursorLn)
	}
	if tb.topLine == 0 {
		t.Fatalf("topLine should have advanced past 0 once the cursor left the first screen, got 0")
	}
	// The cursor's line must actually be visible: topLine <= cursorLn <
	// topLine + bodyRows.
	if tb.cursorLn < tb.topLine || tb.cursorLn >= tb.topLine+9 {
		t.Fatalf("cursor line %d not within visible range [%d, %d)", tb.cursorLn, tb.topLine, tb.topLine+9)
	}
}

func linesN(n int) []string {
	lines := make([]string, n)
	for i := range lines {
		lines[i] = "line content"
	}
	return lines
}

func TestViewGAndCapitalGJumpToTopAndBottom(t *testing.T) {
	v := NewView()
	v.Open(fixturePath(t, "editor_sample.txt"))
	w := newFakeWindow(40, 10)
	v.Render(w)

	v.HandleKey(layout.Key{Text: "G"})
	if v.activeTab().cursorLn != 3 {
		t.Fatalf("'G' should jump to last line, got cursorLn=%d", v.activeTab().cursorLn)
	}
	v.HandleKey(layout.Key{Text: "g"})
	if v.activeTab().cursorLn != 0 {
		t.Fatalf("'g' should jump to first line, got cursorLn=%d", v.activeTab().cursorLn)
	}
}

func TestViewHorizontalScrollDoesNotMangleWideRunes(t *testing.T) {
	v := NewView()
	v.Open(fixturePath(t, "editor_sample.txt"))
	w := newFakeWindow(12, 10) // narrow window forces horizontal scrolling relevance
	v.Render(w)

	tb := v.activeTab()
	tb.cursorLn = 2 // the CJK line
	// Step the cursor across the whole line one rune at a time, forcing
	// leftCol to follow all the way through the CJK cluster and back out.
	lineLen := len(currentLineRunes(tb, tb.cursorLn, v.tabWidth))
	for i := 0; i < lineLen; i++ {
		v.HandleKey(layout.Key{Named: layout.KeyRight})
		v.Render(w)
		for _, r := range w.lines[3] { // CJK line renders on row 3 (tab bar + 2 lines before it)
			if r == '�' {
				t.Fatalf("rendered line contains the replacement character (mangled rune) at cursorCol=%d: %q", tb.cursorCol, w.lines[3])
			}
		}
	}
}

func TestViewHorizontalScrollIsBoundedByLineLength(t *testing.T) {
	// "Limit the horizontal scroll": since leftCol is derived from the
	// cursor's position and the cursor itself cannot move past the end of
	// the line, leftCol can never scroll past what's needed to show the
	// end of the longest line — there is no free-scrolling into empty
	// space.
	v := NewView()
	v.tabs = []*tab{{buf: &Buffer{Lines: []string{"short"}}}}
	v.active = 0
	w := newFakeWindow(10, 5)
	v.Render(w)

	for i := 0; i < 50; i++ {
		v.HandleKey(layout.Key{Named: layout.KeyRight})
		v.Render(w)
	}

	tb := v.activeTab()
	if tb.cursorCol != len([]rune("short")) {
		t.Fatalf("cursorCol should clamp to the line length (%d), got %d", len([]rune("short")), tb.cursorCol)
	}
	if tb.leftCol > tb.cursorCol {
		t.Fatalf("leftCol (%d) should never exceed the cursor's own display column (%d)", tb.leftCol, tb.cursorCol)
	}
}

func TestViewHomeAndEndMoveCursorColToLineBoundaries(t *testing.T) {
	v := NewView()
	v.Open(fixturePath(t, "editor_sample.txt"))
	w := newFakeWindow(20, 10)
	v.Render(w)

	tb := v.activeTab()
	tb.cursorLn = 3 // the 200-char long line
	v.HandleKey(layout.Key{Named: layout.KeyEnd})
	v.Render(w)
	wantEnd := len(currentLineRunes(tb, 3, v.tabWidth))
	if tb.cursorCol != wantEnd {
		t.Fatalf("End should move cursorCol to the line length (%d), got %d", wantEnd, tb.cursorCol)
	}
	if tb.leftCol == 0 {
		t.Fatalf("scrolling to the end of a long line should have advanced leftCol, got 0")
	}

	v.HandleKey(layout.Key{Named: layout.KeyHome})
	v.Render(w)
	if tb.cursorCol != 0 {
		t.Fatalf("Home should reset cursorCol to 0, got %d", tb.cursorCol)
	}
	if tb.leftCol != 0 {
		t.Fatalf("scrolling back to cursorCol=0 should reset leftCol to 0, got %d", tb.leftCol)
	}
}

func TestViewHandleKeyOnEmptyViewIsNoop(t *testing.T) {
	v := NewView() // no Open called
	if v.HandleKey(layout.Key{Named: layout.KeyDown}) {
		t.Fatal("an empty view (no buffer loaded) should not consume keys")
	}
}

func TestOpenAtLineMovesCursorToRequestedLine(t *testing.T) {
	v := NewView()
	v.OpenAtLine(fixturePath(t, "editor_sample.txt"), 3) // 1-based

	tb := v.activeTab()
	if tb.cursorLn != 2 {
		t.Fatalf("cursorLn = %d, want 2 (line 3, 0-indexed)", tb.cursorLn)
	}
	if tb.cursorCol != 0 {
		t.Fatalf("cursorCol = %d, want 0", tb.cursorCol)
	}
}

func TestOpenAtLineClampsBeyondEndOfFile(t *testing.T) {
	v := NewView()
	v.OpenAtLine(fixturePath(t, "editor_sample.txt"), 999) // file has 4 lines
	if tb := v.activeTab(); tb.cursorLn != 3 {
		t.Fatalf("cursorLn = %d, want 3 (clamped to last line)", tb.cursorLn)
	}
}

func TestOpenAtLineZeroDoesNotMoveCursor(t *testing.T) {
	v := NewView()
	v.OpenAtLine(fixturePath(t, "editor_sample.txt"), 0)
	if tb := v.activeTab(); tb.cursorLn != 0 {
		t.Fatalf("cursorLn = %d, want 0 (line=0 means \"no specific line\")", tb.cursorLn)
	}
}

func TestOpenAtLineJumpsEvenWhenFileAlreadyOpenInAnotherTab(t *testing.T) {
	v := NewView()
	path := fixturePath(t, "editor_sample.txt")
	v.Open(path)
	v.activeTab().cursorLn = 1 // simulate having scrolled somewhere else
	v.Open("/some/other/file.txt")

	v.OpenAtLine(path, 4)

	if v.active != 0 {
		t.Fatalf("expected the existing tab for %q to be reactivated, active=%d", path, v.active)
	}
	if tb := v.activeTab(); tb.cursorLn != 3 {
		t.Fatalf("cursorLn = %d, want 3 (OpenAtLine should jump even on an already-open tab)", tb.cursorLn)
	}
}

func TestOpenSamePathTwiceActivatesExistingTabInsteadOfDuplicating(t *testing.T) {
	v := NewView()
	path := fixturePath(t, "editor_sample.txt")
	v.Open(path)
	v.Open("/some/other/file.txt")
	v.Open(path)

	if len(v.tabs) != 2 {
		t.Fatalf("re-opening an already-open path should not create a duplicate tab, got %d tabs", len(v.tabs))
	}
	if v.active != 0 {
		t.Fatalf("re-opening an already-open path should activate its existing tab, got active=%d", v.active)
	}
}

func TestNextPrevTabWrapAround(t *testing.T) {
	v := NewView()
	v.Open(fixturePath(t, "editor_sample.txt"))
	v.Open("/some/other/file.txt")

	if v.active != 1 {
		t.Fatalf("expected the second Open to activate tab 1, got %d", v.active)
	}
	v.NextTab()
	if v.active != 0 {
		t.Fatalf("NextTab should wrap around to 0, got %d", v.active)
	}
	v.PrevTab()
	if v.active != 1 {
		t.Fatalf("PrevTab should wrap back to 1, got %d", v.active)
	}
}

func TestCloseTabRemovesActiveTab(t *testing.T) {
	v := NewView()
	v.Open(fixturePath(t, "editor_sample.txt"))
	v.Open("/some/other/file.txt")

	v.CloseTab()
	if len(v.tabs) != 1 {
		t.Fatalf("expected 1 tab remaining, got %d", len(v.tabs))
	}
	if v.tabs[0].buf == nil || v.tabs[0].buf.Path != fixturePath(t, "editor_sample.txt") {
		t.Fatalf("expected the first tab to remain open")
	}
}

func TestCloseLastTabLeavesViewEmpty(t *testing.T) {
	v := NewView()
	v.Open(fixturePath(t, "editor_sample.txt"))
	v.CloseTab()

	if len(v.tabs) != 0 {
		t.Fatalf("expected no tabs remaining, got %d", len(v.tabs))
	}
	w := newFakeWindow(40, 10)
	v.Render(w) // must not panic
	joined := strings.Join(w.lines, "\n")
	if !strings.Contains(joined, "No file open") {
		t.Errorf("expected placeholder after closing the last tab, got:\n%s", joined)
	}
}

func TestTabBarHighlightsActiveTab(t *testing.T) {
	v := NewView()
	v.Open(fixturePath(t, "editor_sample.txt"))
	v.Open("/some/other/file.txt")

	w := newFakeWindow(60, 10)
	v.Render(w)

	if !strings.Contains(w.lines[0], "[file.txt]") {
		t.Errorf("expected the active tab to be bracket-highlighted in the tab bar, got %q", w.lines[0])
	}
	if !strings.Contains(w.lines[0], "editor_sample.txt") {
		t.Errorf("expected the inactive tab to still be listed in the tab bar, got %q", w.lines[0])
	}
}

func TestBracketKeysSwitchTabsAndXClosesActiveTab(t *testing.T) {
	v := NewView()
	v.Open(fixturePath(t, "editor_sample.txt"))
	v.Open("/some/other/file.txt")

	if !v.HandleKey(layout.Key{Text: "["}) {
		t.Fatal("'[' should be consumed to switch tabs")
	}
	if v.active != 0 {
		t.Fatalf("'[' should have switched to tab 0, got %d", v.active)
	}
	if !v.HandleKey(layout.Key{Text: "x"}) {
		t.Fatal("'x' should be consumed to close the active tab")
	}
	if len(v.tabs) != 1 {
		t.Fatalf("expected 1 tab remaining after close, got %d", len(v.tabs))
	}
}

func TestViewAppliesSyntaxHighlightSegments(t *testing.T) {
	v := NewView()
	v.tabs = []*tab{{buf: &Buffer{Lines: []string{`x = "hello" // a comment`}}}}
	v.active = 0

	w := newFakeWindow(60, 10)
	v.Render(w)

	segs := w.segs[1] // buffer line 0 renders on row 1
	var sawString, sawComment bool
	for _, s := range segs {
		if strings.Contains(s.Text, "hello") && s.Style.Foreground == layout.ColorGreen {
			sawString = true
		}
		if strings.Contains(s.Text, "a comment") && s.Style.Attr&layout.AttrDim != 0 {
			sawComment = true
		}
	}
	if !sawString {
		t.Errorf("expected a green-styled segment containing the string literal, got %+v", segs)
	}
	if !sawComment {
		t.Errorf("expected a dim-styled segment containing the comment, got %+v", segs)
	}
}
