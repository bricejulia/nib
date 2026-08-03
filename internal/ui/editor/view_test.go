package editor

import (
	"os"
	"path/filepath"
	"strings"
	"testing"

	"github.com/bricejulia/nib/internal/layout"
	"github.com/bricejulia/nib/internal/ui/gitstyle"
	"github.com/bricejulia/nib/internal/vcs/gitstatus"
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

func TestViewOpenPathsReturnsEveryOpenTab(t *testing.T) {
	v := NewView()
	if paths := v.OpenPaths(); len(paths) != 0 {
		t.Fatalf("expected no open paths, got %v", paths)
	}

	path := fixturePath(t, "editor_sample.txt")
	v.Open(path)
	paths := v.OpenPaths()
	if len(paths) != 1 || paths[0] != path {
		t.Fatalf("got %v, want [%q]", paths, path)
	}
}

func TestViewApplyLineStatusIgnoresUnknownPath(t *testing.T) {
	v := NewView()
	v.Open(fixturePath(t, "editor_sample.txt"))
	// Applying status for a path that isn't open must not panic and must
	// not affect the tab that is open.
	v.ApplyLineStatus("/no/such/file.txt", map[int]gitstatus.LineStatus{0: gitstatus.LineAdded})

	w := newFakeWindow(40, 10)
	v.Render(w)
	if strings.Contains(w.segs[1][gitMarkerSeg].Text, "+") {
		t.Errorf("row 1's gutter marker should be untouched, got %q", w.segs[1][gitMarkerSeg].Text)
	}
}

func TestViewRenderShowsLineStatusMarkerInGutter(t *testing.T) {
	v := NewView()
	path := fixturePath(t, "editor_sample.txt")
	v.Open(path)
	v.ApplyLineStatus(path, map[int]gitstatus.LineStatus{
		0: gitstatus.LineAdded,
		1: gitstatus.LineModified,
	})

	w := newFakeWindow(40, 10)
	v.Render(w)

	// Row 0 is the tab bar; buffer line 0 renders on row 1, line 1 on row
	// 2. The git marker is the gutter's SECOND segment — the diagnostic
	// marker (see ApplyDiagnostics) precedes it (see renderBody).
	if got := w.segs[1][gitMarkerSeg]; got.Text != gitstyle.LineMarker(gitstatus.LineAdded) || got.Style != gitstyle.LineStyle(gitstatus.LineAdded) {
		t.Errorf("row 1 marker = %+v, want text %q style %+v", got, gitstyle.LineMarker(gitstatus.LineAdded), gitstyle.LineStyle(gitstatus.LineAdded))
	}
	if got := w.segs[2][gitMarkerSeg]; got.Text != gitstyle.LineMarker(gitstatus.LineModified) || got.Style != gitstyle.LineStyle(gitstatus.LineModified) {
		t.Errorf("row 2 marker = %+v, want text %q style %+v", got, gitstyle.LineMarker(gitstatus.LineModified), gitstyle.LineStyle(gitstatus.LineModified))
	}
	// Line 2 has no entry in the map: unchanged, so a blank marker.
	if got := w.segs[3][gitMarkerSeg]; got.Text != " " {
		t.Errorf("row 3 marker = %+v, want a blank (unchanged) marker", got)
	}
}

// Gutter segment indices within a rendered body row, in renderBody's order:
// diagnostic marker, git-diff marker, then the line number.
const (
	diagMarkerSeg = 0
	gitMarkerSeg  = 1
)

func TestActivePathReflectsOpenTab(t *testing.T) {
	v := NewView()
	if got := v.ActivePath(); got != "" {
		t.Errorf("expected empty ActivePath with no tabs open, got %q", got)
	}

	path := fixturePath(t, "editor_sample.txt")
	v.Open(path)
	if got := v.ActivePath(); got != path {
		t.Errorf("got %q, want %q", got, path)
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

func TestSetKeymapOverridesATrigger(t *testing.T) {
	v := NewView()
	v.OpenAtLine(fixturePath(t, "editor_sample.txt"), 3) // not line 1: move_up must have room to move
	v.SetKeymap(map[string]string{"j": "move_up"})       // reverse j's default action

	before := v.activeTab().cursorLn
	if !v.HandleKey(layout.Key{Text: "j"}) {
		t.Fatal("expected the overridden trigger to still be consumed")
	}
	if v.activeTab().cursorLn != before-1 {
		t.Fatalf("cursorLn = %d, want %d (j remapped to move_up)", v.activeTab().cursorLn, before-1)
	}
}

func TestSetKeymapLeavesUnrelatedDefaultsIntact(t *testing.T) {
	v := NewView()
	v.OpenAtLine(fixturePath(t, "editor_sample.txt"), 1)
	v.SetKeymap(map[string]string{"j": "move_up"})

	before := v.activeTab().cursorLn
	if !v.HandleKey(layout.Key{Named: layout.KeyDown}) {
		t.Fatal("expected Down to still be consumed")
	}
	if v.activeTab().cursorLn != before+1 {
		t.Fatalf("cursorLn = %d, want %d (Down's default action must survive overriding j)", v.activeTab().cursorLn, before+1)
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

	var sawReverseActive, sawPlainInactive bool
	for _, s := range w.segs[0] {
		if strings.Contains(s.Text, "file.txt") && !strings.Contains(s.Text, "editor_sample") {
			if s.Style.Attr&layout.AttrReverse != 0 {
				sawReverseActive = true
			}
		}
		if strings.Contains(s.Text, "editor_sample.txt") && s.Style.Attr&layout.AttrReverse == 0 {
			sawPlainInactive = true
		}
	}
	if !sawReverseActive {
		t.Errorf("expected the active tab's segment to be styled AttrReverse, got %+v", w.segs[0])
	}
	if !sawPlainInactive {
		t.Errorf("expected the inactive tab's segment to be unstyled, got %+v", w.segs[0])
	}
}

func TestTabBarPrefixesParentFolderWhenNamesClash(t *testing.T) {
	v := NewView()
	v.Open("/repo/internal/editor/view.go")
	v.Open("/repo/internal/finder/view.go")

	w := newFakeWindow(80, 10)
	v.Render(w)

	if !strings.Contains(w.lines[0], "editor/view.go") {
		t.Errorf("expected the first clashing tab prefixed with its parent folder, got %q", w.lines[0])
	}
	if !strings.Contains(w.lines[0], "finder/view.go") {
		t.Errorf("expected the second clashing tab prefixed with its parent folder, got %q", w.lines[0])
	}
	if strings.Contains(w.lines[0], "internal/editor/view.go") {
		t.Errorf("expected only one parent segment, not the full path, got %q", w.lines[0])
	}
}

func TestTabBarLeavesUniqueNamesAlone(t *testing.T) {
	v := NewView()
	v.Open(fixturePath(t, "editor_sample.txt"))
	v.Open("/some/other/file.txt")

	w := newFakeWindow(80, 10)
	v.Render(w)

	// Neither name clashes with the other, so no parent-folder prefix
	// should appear for either.
	if strings.Contains(w.lines[0], "/editor_sample.txt") {
		t.Errorf("did not expect a parent-folder prefix on a unique name, got %q", w.lines[0])
	}
	if strings.Contains(w.lines[0], "other/file.txt") {
		t.Errorf("did not expect a parent-folder prefix on a unique name, got %q", w.lines[0])
	}
}

func TestTabBarAddsMoreParentsWhenOneLevelStillClashes(t *testing.T) {
	v := NewView()
	v.Open("/repo/a/x/foo.go")
	v.Open("/repo/b/x/foo.go")

	w := newFakeWindow(80, 10)
	v.Render(w)

	if !strings.Contains(w.lines[0], "a/x/foo.go") {
		t.Errorf("expected enough parents to disambiguate a/x/foo.go, got %q", w.lines[0])
	}
	if !strings.Contains(w.lines[0], "b/x/foo.go") {
		t.Errorf("expected enough parents to disambiguate b/x/foo.go, got %q", w.lines[0])
	}
}

func TestPlaceholderMessageIsDimmed(t *testing.T) {
	v := NewView()
	w := newFakeWindow(60, 10)
	v.Render(w)

	for _, segs := range w.segs {
		for _, s := range segs {
			if strings.Contains(s.Text, "No file open") {
				if s.Style.Attr&layout.AttrDim == 0 {
					t.Errorf("expected the \"No file open\" placeholder to be dim-styled, got %+v", s)
				}
				return
			}
		}
	}
	t.Errorf("\"No file open\" placeholder not found in any rendered row: %+v", w.segs)
}

func TestBracketKeysSwitchTabs(t *testing.T) {
	v := NewView()
	v.Open(fixturePath(t, "editor_sample.txt"))
	v.Open("/some/other/file.txt")

	if !v.HandleKey(layout.Key{Text: "["}) {
		t.Fatal("'[' should be consumed to switch tabs")
	}
	if v.active != 0 {
		t.Fatalf("'[' should have switched to tab 0, got %d", v.active)
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

// TestOpenRealGoFileRendersRealTreeSitterHighlighting is the end-to-end
// substitute for eyeballing this in a real terminal (not possible in this
// sandbox — see the plan's verification notes): opens a real multi-line
// .go fixture through the actual Open -> highlightBuffer -> Render
// pipeline and asserts specific tokens land on the expected rows with
// real tree-sitter-derived styles, including a raw string literal that
// spans two lines (proving splitHighlightsByLine's line-splitting works
// end-to-end, not just in its own unit tests).
func TestOpenRealGoFileRendersRealTreeSitterHighlighting(t *testing.T) {
	v := NewView()
	v.Open(fixturePath(t, "highlight_sample.go"))

	w := newFakeWindow(60, 15)
	v.Render(w)

	// Row 0 is the tab bar; buffer line i renders on row 1+i. Fixture:
	//   0 package sample
	//   1 (blank)
	//   2 // greet prints a friendly message.
	//   3 func greet(name string) {
	//   4     count := 3
	//   5     message := `line one
	//   6 line two`
	//   7     println("hello", name, count, message)
	//   8 }
	if !rowHasStyledText(w, 1, "package", layout.ColorYellow) {
		t.Errorf("expected \"package\" styled as a keyword on row 1, got %q %+v", w.lines[1], w.segs[1])
	}
	if !rowHasStyle(w, 2, func(s layout.Style) bool { return s.Attr&layout.AttrDim != 0 }) {
		t.Errorf("expected a dim comment on row 2, got %q %+v", w.lines[2], w.segs[2])
	}
	if !rowHasStyledText(w, 5, "3", layout.ColorMagenta) {
		t.Errorf("expected \"3\" styled as a number on row 5, got %q %+v", w.lines[5], w.segs[5])
	}
	// The backtick raw string spans buffer lines 5-6 (rows 6-7): both
	// halves must render as a string-styled segment, proving a
	// multi-line highlight range was correctly split across the two rows
	// rather than merged, truncated, or losing its style partway through.
	if !rowHasStyle(w, 6, func(s layout.Style) bool { return s.Foreground == layout.ColorGreen }) {
		t.Errorf("expected the first half of the multi-line string styled green on row 6, got %q %+v", w.lines[6], w.segs[6])
	}
	if !rowHasStyle(w, 7, func(s layout.Style) bool { return s.Foreground == layout.ColorGreen }) {
		t.Errorf("expected the second half of the multi-line string styled green on row 7, got %q %+v", w.lines[7], w.segs[7])
	}
	if !strings.Contains(w.lines[6], "line one") {
		t.Errorf("expected row 6 text to contain \"line one\", got %q", w.lines[6])
	}
	if !strings.Contains(w.lines[7], "line two") {
		t.Errorf("expected row 7 text to contain \"line two\", got %q", w.lines[7])
	}
}

func rowHasStyledText(w *fakeWindow, row int, text string, color layout.Color) bool {
	if row < 0 || row >= len(w.segs) {
		return false
	}
	for _, s := range w.segs[row] {
		if strings.Contains(s.Text, text) && s.Style.Foreground == color {
			return true
		}
	}
	return false
}

func rowHasStyle(w *fakeWindow, row int, match func(layout.Style) bool) bool {
	if row < 0 || row >= len(w.segs) {
		return false
	}
	for _, s := range w.segs[row] {
		if match(s.Style) {
			return true
		}
	}
	return false
}

func TestIAndEscToggleInsertAndNormalMode(t *testing.T) {
	v := NewView()
	v.tabs = []*tab{{buf: &Buffer{Lines: []string{"abc"}}}}
	v.active = 0

	if v.mode != modeNormal {
		t.Fatalf("expected to start in Normal mode")
	}
	if !v.HandleKey(layout.Key{Text: "i"}) {
		t.Fatal("'i' should be consumed to enter Insert mode")
	}
	if v.mode != modeInsert {
		t.Fatal("expected Insert mode after 'i'")
	}
	if !v.HandleKey(layout.Key{Named: layout.KeyEsc}) {
		t.Fatal("Esc should be consumed to return to Normal mode")
	}
	if v.mode != modeNormal {
		t.Fatal("expected Normal mode after Esc")
	}
}

func TestTypingInInsertModeInsertsText(t *testing.T) {
	v := NewView()
	v.tabs = []*tab{{buf: &Buffer{Lines: []string{"ac"}}}}
	v.active = 0
	v.HandleKey(layout.Key{Text: "i"})
	v.activeTab().cursorCol = 1

	v.HandleKey(layout.Key{Text: "b"})

	if got := v.activeTab().buf.Lines[0]; got != "abc" {
		t.Fatalf("Lines[0] = %q, want %q", got, "abc")
	}
	if v.activeTab().cursorCol != 2 {
		t.Fatalf("cursorCol = %d, want 2 (advanced past the inserted rune)", v.activeTab().cursorCol)
	}
	if !v.activeTab().buf.Dirty {
		t.Fatal("expected the buffer to be marked Dirty after typing")
	}
}

func TestNormalModeLettersMoveInsteadOfInsertingText(t *testing.T) {
	v := NewView()
	v.tabs = []*tab{{buf: &Buffer{Lines: []string{"line one", "line two"}}}}
	v.active = 0

	v.HandleKey(layout.Key{Text: "j"}) // move_down, not a literal "j"

	if v.activeTab().cursorLn != 1 {
		t.Fatalf("expected 'j' to move the cursor down in Normal mode, cursorLn=%d", v.activeTab().cursorLn)
	}
	if v.activeTab().buf.Lines[0] != "line one" {
		t.Fatalf("Normal mode must not insert text: Lines[0] = %q", v.activeTab().buf.Lines[0])
	}
}

func TestLettersBoundToActionsAreLiteralWhileInserting(t *testing.T) {
	v := NewView()
	v.tabs = []*tab{{buf: &Buffer{Lines: []string{""}}}}
	v.active = 0
	v.HandleKey(layout.Key{Text: "i"})

	v.HandleKey(layout.Key{Text: "j"}) // "j" is move_down in Normal mode

	if v.activeTab().cursorLn != 0 {
		t.Fatalf("typing 'j' while inserting must not move the cursor, cursorLn=%d", v.activeTab().cursorLn)
	}
	if got := v.activeTab().buf.Lines[0]; got != "j" {
		t.Fatalf("Lines[0] = %q, want %q (the letter typed literally)", got, "j")
	}
}

func TestEnterInInsertModeSplitsLine(t *testing.T) {
	v := NewView()
	v.tabs = []*tab{{buf: &Buffer{Lines: []string{"abcdef"}}}}
	v.active = 0
	v.HandleKey(layout.Key{Text: "i"})
	v.activeTab().cursorCol = 3

	v.HandleKey(layout.Key{Named: layout.KeyEnter})

	tb := v.activeTab()
	if len(tb.buf.Lines) != 2 || tb.buf.Lines[0] != "abc" || tb.buf.Lines[1] != "def" {
		t.Fatalf("Lines = %+v, want [\"abc\" \"def\"]", tb.buf.Lines)
	}
	if tb.cursorLn != 1 || tb.cursorCol != 0 {
		t.Fatalf("cursor = (%d,%d), want (1,0)", tb.cursorLn, tb.cursorCol)
	}
}

func TestBackspaceInInsertModeDeletesAndJoinsLines(t *testing.T) {
	v := NewView()
	v.tabs = []*tab{{buf: &Buffer{Lines: []string{"foo", "bar"}}}}
	v.active = 0
	v.HandleKey(layout.Key{Text: "i"})
	v.activeTab().cursorLn = 1
	v.activeTab().cursorCol = 0

	v.HandleKey(layout.Key{Named: layout.KeyBackspace})

	tb := v.activeTab()
	if len(tb.buf.Lines) != 1 || tb.buf.Lines[0] != "foobar" {
		t.Fatalf("Lines = %+v, want [\"foobar\"]", tb.buf.Lines)
	}
	if tb.cursorLn != 0 || tb.cursorCol != 3 {
		t.Fatalf("cursor = (%d,%d), want (0,3)", tb.cursorLn, tb.cursorCol)
	}
}

func TestDirtyMarkerAppearsInTabBarAfterEdit(t *testing.T) {
	v := NewView()
	v.Open(fixturePath(t, "editor_sample.txt"))
	w := newFakeWindow(60, 10)
	v.Render(w)
	if strings.Contains(w.lines[0], "*") {
		t.Fatalf("did not expect a dirty marker before any edit, got %q", w.lines[0])
	}

	v.HandleKey(layout.Key{Text: "i"})
	v.HandleKey(layout.Key{Text: "x"})
	v.Render(w)

	if !strings.Contains(w.lines[0], "*") {
		t.Errorf("expected a dirty marker in the tab bar after an edit, got %q", w.lines[0])
	}
}

func TestStatusTextShowsInsertModeIndicator(t *testing.T) {
	v := NewView()
	v.Open(fixturePath(t, "editor_sample.txt"))
	w := newFakeWindow(40, 10)
	v.Render(w)

	if got := v.StatusText(); strings.Contains(got, "INSERT") {
		t.Fatalf("did not expect an INSERT indicator in Normal mode, got %q", got)
	}

	v.HandleKey(layout.Key{Text: "i"})
	if got := v.StatusText(); !strings.Contains(got, "INSERT") {
		t.Errorf("expected an INSERT indicator in Insert mode, got %q", got)
	}
}

func TestCtrlSSavesActiveTabToDisk(t *testing.T) {
	dir := t.TempDir()
	path := filepath.Join(dir, "save_me.txt")
	if err := os.WriteFile(path, []byte("before"), 0o644); err != nil {
		t.Fatal(err)
	}

	v := NewView()
	v.Open(path)
	v.HandleKey(layout.Key{Text: "i"})
	v.activeTab().cursorCol = len(v.activeTab().buf.Lines[0])
	v.HandleKey(layout.Key{Text: "!"})
	v.HandleKey(layout.Key{Named: layout.KeyEsc})

	if !v.HandleKey(layout.Key{Text: "s", Mods: layout.ModCtrl}) {
		t.Fatal("expected Ctrl+s to be consumed")
	}

	if v.activeTab().buf.Dirty {
		t.Fatal("expected Dirty to clear after saving")
	}
	got, err := os.ReadFile(path)
	if err != nil {
		t.Fatal(err)
	}
	if string(got) != "before!" {
		t.Fatalf("file contents = %q, want %q", got, "before!")
	}
}

func TestSaveWorksWhileStillInInsertMode(t *testing.T) {
	dir := t.TempDir()
	path := filepath.Join(dir, "save_me.txt")
	if err := os.WriteFile(path, []byte("x"), 0o644); err != nil {
		t.Fatal(err)
	}

	v := NewView()
	v.Open(path)
	v.HandleKey(layout.Key{Text: "i"})

	if !v.HandleKey(layout.Key{Text: "s", Mods: layout.ModCtrl}) {
		t.Fatal("expected Ctrl+s to be consumed while inserting")
	}
	if v.mode != modeInsert {
		t.Fatal("saving should not exit Insert mode")
	}
}

func TestSetKeymapOverridesInsertModeTrigger(t *testing.T) {
	v := NewView()
	v.tabs = []*tab{{buf: &Buffer{Lines: []string{"abc"}}}}
	v.active = 0
	v.SetKeymap(map[string]string{"Ctrl+g": "insert_mode"})

	if !v.HandleKey(layout.Key{Text: "g", Mods: layout.ModCtrl}) {
		t.Fatal("expected the overridden trigger to be consumed")
	}
	if v.mode != modeInsert {
		t.Fatal("expected Ctrl+g remapped to insert_mode to enter Insert mode")
	}
}

func TestRawExpandedColumnRoundTripThroughTab(t *testing.T) {
	// "\ttabbed line" from testdata/editor_sample.txt: a tab (expands to
	// tabWidth runes at tabWidth=4) followed by plain text.
	const line = "\ttabbed line"
	const tabWidth = 4

	rawEnd := len([]rune(line))
	expandedEnd := expandedColForRawIndex(line, rawEnd, tabWidth)
	if got := rawIndexForExpandedCol(line, expandedEnd, tabWidth); got != rawEnd {
		t.Fatalf("round-trip at end of line: got raw index %d, want %d", got, rawEnd)
	}

	// A column requested squarely inside the tab's expansion must snap to
	// just past the tab (raw index 1), not split it.
	if got := rawIndexForExpandedCol(line, 1, tabWidth); got != 1 {
		t.Fatalf("mid-tab column should snap past the tab, got raw index %d, want 1", got)
	}

	// Past the tab, columns should map 1:1 back onto raw indices (no more
	// tabs on this line).
	for rawIdx := 1; rawIdx <= rawEnd; rawIdx++ {
		expanded := expandedColForRawIndex(line, rawIdx, tabWidth)
		if got := rawIndexForExpandedCol(line, expanded, tabWidth); got != rawIdx {
			t.Errorf("round-trip at raw index %d: expanded=%d, got back raw index %d", rawIdx, expanded, got)
		}
	}
}

func TestArrowKeysMoveCursorWhileInserting(t *testing.T) {
	v := NewView()
	v.tabs = []*tab{{buf: &Buffer{Lines: []string{"one", "two"}}}}
	v.active = 0
	v.HandleKey(layout.Key{Text: "i"})

	if !v.HandleKey(layout.Key{Named: layout.KeyDown}) {
		t.Fatal("expected Down to be consumed while inserting")
	}
	if v.activeTab().cursorLn != 1 {
		t.Fatalf("cursorLn = %d, want 1 (arrow keys should move even in Insert mode)", v.activeTab().cursorLn)
	}
	if v.mode != modeInsert {
		t.Fatal("moving with an arrow key must not leave Insert mode")
	}
	if v.activeTab().buf.Lines[1] != "two" {
		t.Fatalf("Lines[1] = %q, want unchanged %q (arrow keys must not insert text)", v.activeTab().buf.Lines[1], "two")
	}
}

func TestHjklStillInsertLiterallyWhileArrowsMove(t *testing.T) {
	v := NewView()
	v.tabs = []*tab{{buf: &Buffer{Lines: []string{"", "second"}}}}
	v.active = 0
	v.HandleKey(layout.Key{Text: "i"})

	v.HandleKey(layout.Key{Text: "j"}) // must insert "j", not move down

	if v.activeTab().cursorLn != 0 {
		t.Fatalf("cursorLn = %d, want 0 (letters must not move the cursor while inserting)", v.activeTab().cursorLn)
	}
	if got := v.activeTab().buf.Lines[0]; got != "j" {
		t.Fatalf("Lines[0] = %q, want %q", got, "j")
	}
}

func TestAAppendsAfterCursor(t *testing.T) {
	v := NewView()
	v.tabs = []*tab{{buf: &Buffer{Lines: []string{"abc"}}}}
	v.active = 0
	v.activeTab().cursorCol = 1 // sitting on 'b'

	if !v.HandleKey(layout.Key{Text: "a"}) {
		t.Fatal("'a' should be consumed to enter Insert mode")
	}
	if v.mode != modeInsert {
		t.Fatal("'a' should enter Insert mode")
	}
	if v.activeTab().cursorCol != 2 {
		t.Fatalf("cursorCol = %d, want 2 ('a' inserts after the cursor, not before)", v.activeTab().cursorCol)
	}

	v.HandleKey(layout.Key{Text: "X"})
	if got := v.activeTab().buf.Lines[0]; got != "abXc" {
		t.Fatalf("Lines[0] = %q, want %q", got, "abXc")
	}
}

func TestAAtEndOfLineAppendsPastTheEnd(t *testing.T) {
	v := NewView()
	v.tabs = []*tab{{buf: &Buffer{Lines: []string{"abc"}}}}
	v.active = 0
	v.activeTab().cursorCol = 3 // already one-past-the-end

	v.HandleKey(layout.Key{Text: "a"})
	v.HandleKey(layout.Key{Text: "!"})

	if got := v.activeTab().buf.Lines[0]; got != "abc!" {
		t.Fatalf("Lines[0] = %q, want %q", got, "abc!")
	}
}

func TestUndoRevertsLastInsertSession(t *testing.T) {
	v := NewView()
	v.tabs = []*tab{{buf: &Buffer{Lines: []string{"abc"}}}}
	v.active = 0
	v.activeTab().cursorCol = 3

	v.HandleKey(layout.Key{Text: "i"})
	v.HandleKey(layout.Key{Text: "d"})
	v.HandleKey(layout.Key{Text: "e"})
	v.HandleKey(layout.Key{Text: "f"})
	v.HandleKey(layout.Key{Named: layout.KeyEsc})

	if got := v.activeTab().buf.Lines[0]; got != "abcdef" {
		t.Fatalf("Lines[0] = %q, want %q", got, "abcdef")
	}

	if !v.HandleKey(layout.Key{Text: "u"}) {
		t.Fatal("expected 'u' to be consumed")
	}
	tb := v.activeTab()
	if tb.buf.Lines[0] != "abc" {
		t.Fatalf("Lines[0] = %q, want %q after undo", tb.buf.Lines[0], "abc")
	}
	if tb.cursorCol != 3 {
		t.Fatalf("cursorCol = %d, want 3 (restored to its pre-Insert-session position)", tb.cursorCol)
	}
}

func TestRedoReappliesUndoneInsertSession(t *testing.T) {
	v := NewView()
	v.tabs = []*tab{{buf: &Buffer{Lines: []string{"abc"}}}}
	v.active = 0
	v.activeTab().cursorCol = 3

	v.HandleKey(layout.Key{Text: "i"})
	v.HandleKey(layout.Key{Text: "d"})
	v.HandleKey(layout.Key{Named: layout.KeyEsc})
	v.HandleKey(layout.Key{Text: "u"})
	if v.activeTab().buf.Lines[0] != "abc" {
		t.Fatalf("expected undo to revert to %q, got %q", "abc", v.activeTab().buf.Lines[0])
	}

	if !v.HandleKey(layout.Key{Text: "r"}) {
		t.Fatal("expected 'r' to be consumed")
	}
	if got := v.activeTab().buf.Lines[0]; got != "abcd" {
		t.Fatalf("Lines[0] = %q, want %q after redo", got, "abcd")
	}
}

func TestNoOpInsertSessionLeavesNoUndoEntry(t *testing.T) {
	v := NewView()
	v.tabs = []*tab{{buf: &Buffer{Lines: []string{"abc"}}}}
	v.active = 0

	v.HandleKey(layout.Key{Text: "i"})
	v.HandleKey(layout.Key{Named: layout.KeyEsc}) // typed nothing

	if len(v.activeTab().buf.undoStack) != 0 {
		t.Fatalf("expected no undo entry for a no-op Insert session, got %d", len(v.activeTab().buf.undoStack))
	}
}

func TestNewEditClearsRedoStack(t *testing.T) {
	v := NewView()
	v.tabs = []*tab{{buf: &Buffer{Lines: []string{"abc"}}}}
	v.active = 0
	v.activeTab().cursorCol = 3

	v.HandleKey(layout.Key{Text: "i"})
	v.HandleKey(layout.Key{Text: "d"})
	v.HandleKey(layout.Key{Named: layout.KeyEsc})
	v.HandleKey(layout.Key{Text: "u"}) // undo "d", populating the redo stack

	v.HandleKey(layout.Key{Text: "i"})
	v.HandleKey(layout.Key{Text: "z"})
	v.HandleKey(layout.Key{Named: layout.KeyEsc}) // a fresh edit

	if len(v.activeTab().buf.redoStack) != 0 {
		t.Fatalf("expected a new edit to clear the redo stack, got %d entries", len(v.activeTab().buf.redoStack))
	}
}

func TestUndoAndRedoOnEmptyStacksAreNoops(t *testing.T) {
	v := NewView()
	v.tabs = []*tab{{buf: &Buffer{Lines: []string{"abc"}}}}
	v.active = 0

	if !v.HandleKey(layout.Key{Text: "u"}) {
		t.Fatal("expected 'u' to still be consumed with an empty undo stack")
	}
	if !v.HandleKey(layout.Key{Text: "r"}) {
		t.Fatal("expected 'r' to still be consumed with an empty redo stack")
	}
	if got := v.activeTab().buf.Lines[0]; got != "abc" {
		t.Fatalf("Lines[0] = %q, want unchanged %q", got, "abc")
	}
}

func TestColonEntersCommandModeAndJumpsToLine(t *testing.T) {
	v := NewView()
	v.Open(fixturePath(t, "editor_sample.txt")) // 4 lines

	if !v.HandleKey(layout.Key{Text: ":"}) {
		t.Fatal("':' should be consumed to enter Command mode")
	}
	if v.mode != modeCommand {
		t.Fatal("expected Command mode after ':'")
	}
	if got := v.StatusText(); got != ":" {
		t.Fatalf("StatusText = %q, want %q", got, ":")
	}

	v.HandleKey(layout.Key{Text: "3"})
	if got := v.StatusText(); got != ":3" {
		t.Fatalf("StatusText = %q, want %q", got, ":3")
	}
	v.HandleKey(layout.Key{Named: layout.KeyEnter})

	if v.mode != modeNormal {
		t.Fatal("expected Enter to return to Normal mode")
	}
	if v.activeTab().cursorLn != 2 {
		t.Fatalf("cursorLn = %d, want 2 (line 3, 0-indexed)", v.activeTab().cursorLn)
	}
}

func TestColonJumpClampsBeyondEndOfFile(t *testing.T) {
	v := NewView()
	v.Open(fixturePath(t, "editor_sample.txt")) // 4 lines

	v.HandleKey(layout.Key{Text: ":"})
	for _, r := range "999" {
		v.HandleKey(layout.Key{Text: string(r)})
	}
	v.HandleKey(layout.Key{Named: layout.KeyEnter})

	if v.activeTab().cursorLn != 3 {
		t.Fatalf("cursorLn = %d, want 3 (clamped to last line)", v.activeTab().cursorLn)
	}
}

func TestColonEscCancelsWithoutMovingCursor(t *testing.T) {
	v := NewView()
	v.Open(fixturePath(t, "editor_sample.txt"))
	before := v.activeTab().cursorLn

	v.HandleKey(layout.Key{Text: ":"})
	v.HandleKey(layout.Key{Text: "3"})
	v.HandleKey(layout.Key{Named: layout.KeyEsc})

	if v.mode != modeNormal {
		t.Fatal("expected Esc to cancel back to Normal mode")
	}
	if v.activeTab().cursorLn != before {
		t.Fatalf("cursorLn = %d, want unchanged %d", v.activeTab().cursorLn, before)
	}
}

func TestColonBackspaceEditsTypedNumber(t *testing.T) {
	v := NewView()
	v.Open(fixturePath(t, "editor_sample.txt"))

	v.HandleKey(layout.Key{Text: ":"})
	v.HandleKey(layout.Key{Text: "4"})
	v.HandleKey(layout.Key{Text: "2"})
	v.HandleKey(layout.Key{Named: layout.KeyBackspace})

	if got := v.StatusText(); got != ":4" {
		t.Fatalf("StatusText = %q, want %q", got, ":4")
	}
}

// TestUndoSaveRedoShowsDirtyWhenBufferDivergesFromDisk is a regression
// test for a reported bug: edit, exit Insert, save, undo, save again,
// redo — the file appeared "not dirty" after that final redo even though
// the buffer no longer matched what was actually on disk. See
// buffer_test.go's TestRestoreDirtyReflectsSaveThatHappenedAfterTheSnapshot
// for the same scenario at the Buffer level; this drives it through the
// real key sequence a user would type.
func TestUndoSaveRedoShowsDirtyWhenBufferDivergesFromDisk(t *testing.T) {
	dir := t.TempDir()
	path := filepath.Join(dir, "f.txt")
	if err := os.WriteFile(path, []byte("original"), 0o644); err != nil {
		t.Fatal(err)
	}

	v := NewView()
	v.Open(path)
	v.activeTab().cursorCol = len(v.activeTab().buf.Lines[0])

	v.HandleKey(layout.Key{Text: "i"})
	v.HandleKey(layout.Key{Text: "!"})
	v.HandleKey(layout.Key{Named: layout.KeyEsc})
	v.HandleKey(layout.Key{Text: "s", Mods: layout.ModCtrl}) // disk: "original!"

	v.HandleKey(layout.Key{Text: "u"}) // buffer reverts to "original"
	if !v.activeTab().buf.Dirty {
		t.Fatal("expected Dirty after undo: buffer no longer matches the just-saved disk content")
	}
	v.HandleKey(layout.Key{Text: "s", Mods: layout.ModCtrl}) // disk: "original"

	v.HandleKey(layout.Key{Text: "r"}) // buffer: "original!" again
	if got := v.activeTab().buf.Lines[0]; got != "original!" {
		t.Fatalf("Lines[0] = %q, want %q after redo", got, "original!")
	}
	if !v.activeTab().buf.Dirty {
		t.Fatal("bug: expected Dirty after redo, since the buffer now diverges from disk (\"original\") again")
	}

	onDisk, err := os.ReadFile(path)
	if err != nil {
		t.Fatal(err)
	}
	if string(onDisk) == v.activeTab().buf.Lines[0] {
		t.Fatalf("test setup invalid: disk (%q) should NOT match the buffer (%q) at this point", onDisk, v.activeTab().buf.Lines[0])
	}
}

func TestXDeletesCharUnderCursorAndIsUndoable(t *testing.T) {
	v := NewView()
	v.tabs = []*tab{{buf: &Buffer{Lines: []string{"abc"}}}}
	v.active = 0
	v.activeTab().cursorCol = 1 // sitting on 'b'

	if !v.HandleKey(layout.Key{Text: "x"}) {
		t.Fatal("'x' should be consumed")
	}
	if got := v.activeTab().buf.Lines[0]; got != "ac" {
		t.Fatalf("Lines[0] = %q, want %q", got, "ac")
	}
	if v.activeTab().cursorCol != 1 {
		t.Fatalf("cursorCol = %d, want 1 (stays put; 'c' slid into the deleted position)", v.activeTab().cursorCol)
	}
	if v.mode != modeNormal {
		t.Fatal("'x' must not enter Insert mode")
	}

	v.HandleKey(layout.Key{Text: "u"})
	if got := v.activeTab().buf.Lines[0]; got != "abc" {
		t.Fatalf("Lines[0] after undo = %q, want %q", got, "abc")
	}
}

func TestXAtEndOfLineIsNoopAndPushesNoUndoEntry(t *testing.T) {
	v := NewView()
	v.tabs = []*tab{{buf: &Buffer{Lines: []string{"abc"}}}}
	v.active = 0
	v.activeTab().cursorCol = 3 // one-past-the-end: nothing under the cursor

	v.HandleKey(layout.Key{Text: "x"})

	if got := v.activeTab().buf.Lines[0]; got != "abc" {
		t.Fatalf("Lines[0] = %q, want unchanged %q", got, "abc")
	}
	if len(v.activeTab().buf.undoStack) != 0 {
		t.Fatalf("expected no undo entry for a no-op 'x', got %d", len(v.activeTab().buf.undoStack))
	}
}

func TestXOnEmptyLineIsNoop(t *testing.T) {
	v := NewView()
	v.tabs = []*tab{{buf: &Buffer{Lines: []string{""}}}}
	v.active = 0

	if !v.HandleKey(layout.Key{Text: "x"}) {
		t.Fatal("'x' should still be consumed on an empty line")
	}
	if v.activeTab().buf.Lines[0] != "" {
		t.Fatalf("Lines[0] = %q, want unchanged empty", v.activeTab().buf.Lines[0])
	}
}

func TestCapitalXDeletesCharBeforeCursor(t *testing.T) {
	v := NewView()
	v.tabs = []*tab{{buf: &Buffer{Lines: []string{"abc"}}}}
	v.active = 0
	v.activeTab().cursorCol = 2 // sitting on 'c'

	if !v.HandleKey(layout.Key{Text: "X"}) {
		t.Fatal("'X' should be consumed")
	}
	if got := v.activeTab().buf.Lines[0]; got != "ac" {
		t.Fatalf("Lines[0] = %q, want %q", got, "ac")
	}
	if v.activeTab().cursorCol != 1 {
		t.Fatalf("cursorCol = %d, want 1", v.activeTab().cursorCol)
	}
	if v.mode != modeNormal {
		t.Fatal("'X' must not enter Insert mode")
	}

	v.HandleKey(layout.Key{Text: "u"})
	if got := v.activeTab().buf.Lines[0]; got != "abc" {
		t.Fatalf("Lines[0] after undo = %q, want %q", got, "abc")
	}
}

func TestOOpensBlankLineBelowAndEntersInsertMode(t *testing.T) {
	v := NewView()
	v.tabs = []*tab{{buf: &Buffer{Lines: []string{"first", "second"}}}}
	v.active = 0

	if !v.HandleKey(layout.Key{Text: "o"}) {
		t.Fatal("'o' should be consumed")
	}
	if v.mode != modeInsert {
		t.Fatal("'o' should enter Insert mode")
	}
	tb := v.activeTab()
	if len(tb.buf.Lines) != 3 || tb.buf.Lines[0] != "first" || tb.buf.Lines[1] != "" || tb.buf.Lines[2] != "second" {
		t.Fatalf("Lines = %+v, want [\"first\" \"\" \"second\"]", tb.buf.Lines)
	}
	if tb.cursorLn != 1 || tb.cursorCol != 0 {
		t.Fatalf("cursor = (%d,%d), want (1,0)", tb.cursorLn, tb.cursorCol)
	}

	v.HandleKey(layout.Key{Text: "x"})
	v.HandleKey(layout.Key{Text: "y"})
	v.HandleKey(layout.Key{Named: layout.KeyEsc})
	if got := v.activeTab().buf.Lines[1]; got != "xy" {
		t.Fatalf("Lines[1] = %q, want %q", got, "xy")
	}

	// The opened line plus the typed text undo as ONE unit, back to the
	// original two-line buffer.
	v.HandleKey(layout.Key{Text: "u"})
	tb = v.activeTab()
	if len(tb.buf.Lines) != 2 || tb.buf.Lines[0] != "first" || tb.buf.Lines[1] != "second" {
		t.Fatalf("Lines after undo = %+v, want [\"first\" \"second\"]", tb.buf.Lines)
	}
}

func TestColonQClosesCleanTab(t *testing.T) {
	v := NewView()
	v.Open(fixturePath(t, "editor_sample.txt"))
	v.Open("/some/other/file.txt")

	v.HandleKey(layout.Key{Text: ":"})
	v.HandleKey(layout.Key{Text: "q"})
	v.HandleKey(layout.Key{Named: layout.KeyEnter})

	if len(v.tabs) != 1 {
		t.Fatalf("expected 1 tab remaining after :q, got %d", len(v.tabs))
	}
	if v.mode != modeNormal {
		t.Fatal("expected :q to return to Normal mode")
	}
}

func TestColonQRefusesToCloseDirtyTab(t *testing.T) {
	v := NewView()
	v.tabs = []*tab{{path: "a.txt", buf: &Buffer{Lines: []string{"abc"}, Dirty: true}}}
	v.active = 0

	v.HandleKey(layout.Key{Text: ":"})
	v.HandleKey(layout.Key{Text: "q"})
	v.HandleKey(layout.Key{Named: layout.KeyEnter})

	if len(v.tabs) != 1 {
		t.Fatal(":q should have refused to close a dirty tab")
	}
}

func TestColonQForceClosesDirtyTab(t *testing.T) {
	v := NewView()
	v.tabs = []*tab{{path: "a.txt", buf: &Buffer{Lines: []string{"abc"}, Dirty: true}}}
	v.active = 0

	v.HandleKey(layout.Key{Text: ":"})
	v.HandleKey(layout.Key{Text: "q"})
	v.HandleKey(layout.Key{Text: "!"})
	v.HandleKey(layout.Key{Named: layout.KeyEnter})

	if len(v.tabs) != 0 {
		t.Fatalf("':q!' should have force-closed the dirty tab, got %d tabs remaining", len(v.tabs))
	}
}

func TestColonQaRefusesWhenAnyTabDirty(t *testing.T) {
	v := NewView()
	v.tabs = []*tab{
		{path: "a.txt", buf: &Buffer{Lines: []string{"a"}, Dirty: false}},
		{path: "b.txt", buf: &Buffer{Lines: []string{"b"}, Dirty: true}},
	}
	v.active = 0

	v.HandleKey(layout.Key{Text: ":"})
	v.HandleKey(layout.Key{Text: "q"})
	v.HandleKey(layout.Key{Text: "a"})
	v.HandleKey(layout.Key{Named: layout.KeyEnter})

	if len(v.tabs) != 2 {
		t.Fatal(":qa should have refused to close while any tab is dirty")
	}
}

func TestColonQaForceClosesAllTabs(t *testing.T) {
	v := NewView()
	v.tabs = []*tab{
		{path: "a.txt", buf: &Buffer{Lines: []string{"a"}, Dirty: true}},
		{path: "b.txt", buf: &Buffer{Lines: []string{"b"}, Dirty: true}},
	}
	v.active = 0

	v.HandleKey(layout.Key{Text: ":"})
	v.HandleKey(layout.Key{Text: "q"})
	v.HandleKey(layout.Key{Text: "a"})
	v.HandleKey(layout.Key{Text: "!"})
	v.HandleKey(layout.Key{Named: layout.KeyEnter})

	if len(v.tabs) != 0 {
		t.Fatalf("':qa!' should have force-closed every tab, got %d remaining", len(v.tabs))
	}
}

func TestDirtyPathsListsOnlyUnsavedTabs(t *testing.T) {
	v := NewView()
	v.tabs = []*tab{
		{path: "a.txt", buf: &Buffer{Lines: []string{"a"}, Dirty: false}},
		{path: "b.txt", buf: &Buffer{Lines: []string{"b"}, Dirty: true}},
		{path: "c.txt", buf: &Buffer{Lines: []string{"c"}, Dirty: true}},
	}
	v.active = 0

	got := v.DirtyPaths()
	want := []string{"b.txt", "c.txt"}
	if len(got) != len(want) || got[0] != want[0] || got[1] != want[1] {
		t.Fatalf("DirtyPaths() = %v, want %v", got, want)
	}
}

func TestSaveDirtyTabsSavesEveryDirtyTabAndClearsDirty(t *testing.T) {
	dir := t.TempDir()
	pathA := filepath.Join(dir, "a.txt")
	pathB := filepath.Join(dir, "b.txt")
	if err := os.WriteFile(pathA, []byte("original a"), 0o644); err != nil {
		t.Fatal(err)
	}
	if err := os.WriteFile(pathB, []byte("original b"), 0o644); err != nil {
		t.Fatal(err)
	}

	v := NewView()
	v.Open(pathA)
	v.Open(pathB)
	// One tab edited (dirty), one left untouched (clean) — SaveDirtyTabs
	// must only write the dirty one.
	v.tabs[0].buf.Lines[0] = "edited a"
	v.tabs[0].buf.resync()

	failed := v.SaveDirtyTabs()
	if len(failed) != 0 {
		t.Fatalf("SaveDirtyTabs() failed = %v, want none", failed)
	}
	if v.tabs[0].buf.Dirty {
		t.Fatal("expected the edited tab's Dirty to be cleared after save")
	}

	got, err := os.ReadFile(pathA)
	if err != nil {
		t.Fatal(err)
	}
	if string(got) != "edited a" {
		t.Fatalf("a.txt contents = %q, want %q", got, "edited a")
	}
	gotB, err := os.ReadFile(pathB)
	if err != nil {
		t.Fatal(err)
	}
	if string(gotB) != "original b" {
		t.Fatalf("b.txt should be untouched, got %q", gotB)
	}
}

func TestSaveDirtyTabsReportsFailure(t *testing.T) {
	v := NewView()
	// A path inside a nonexistent directory: Save fails with ENOENT, and
	// Dirty must stay true so the caller doesn't think it succeeded.
	v.tabs = []*tab{{path: "missing.txt", buf: &Buffer{Lines: []string{"x"}, Dirty: true, Path: filepath.Join(t.TempDir(), "no-such-dir", "f.txt")}}}
	v.active = 0

	failed := v.SaveDirtyTabs()
	if len(failed) != 1 {
		t.Fatalf("SaveDirtyTabs() failed = %v, want exactly one failure", failed)
	}
	if !v.tabs[0].buf.Dirty {
		t.Fatal("expected Dirty to remain true after a failed save")
	}
}

func TestColonWSavesWithoutClosing(t *testing.T) {
	dir := t.TempDir()
	path := filepath.Join(dir, "f.txt")
	if err := os.WriteFile(path, []byte("x"), 0o644); err != nil {
		t.Fatal(err)
	}

	v := NewView()
	v.Open(path)
	v.activeTab().cursorCol = len(v.activeTab().buf.Lines[0])
	v.HandleKey(layout.Key{Text: "i"})
	v.HandleKey(layout.Key{Text: "!"})
	v.HandleKey(layout.Key{Named: layout.KeyEsc})

	v.HandleKey(layout.Key{Text: ":"})
	v.HandleKey(layout.Key{Text: "w"})
	v.HandleKey(layout.Key{Named: layout.KeyEnter})

	if len(v.tabs) != 1 {
		t.Fatal(":w must not close the tab")
	}
	if v.activeTab().buf.Dirty {
		t.Fatal("expected :w to save (clearing Dirty)")
	}
	got, err := os.ReadFile(path)
	if err != nil {
		t.Fatal(err)
	}
	if string(got) != "x!" {
		t.Fatalf("file contents = %q, want %q", got, "x!")
	}
}

func TestColonWqSavesThenCloses(t *testing.T) {
	dir := t.TempDir()
	path := filepath.Join(dir, "f.txt")
	if err := os.WriteFile(path, []byte("x"), 0o644); err != nil {
		t.Fatal(err)
	}

	v := NewView()
	v.Open(path)
	v.activeTab().cursorCol = len(v.activeTab().buf.Lines[0])
	v.HandleKey(layout.Key{Text: "i"})
	v.HandleKey(layout.Key{Text: "!"})
	v.HandleKey(layout.Key{Named: layout.KeyEsc})

	v.HandleKey(layout.Key{Text: ":"})
	v.HandleKey(layout.Key{Text: "w"})
	v.HandleKey(layout.Key{Text: "q"})
	v.HandleKey(layout.Key{Named: layout.KeyEnter})

	if len(v.tabs) != 0 {
		t.Fatalf(":wq should have closed the tab, got %d remaining", len(v.tabs))
	}
	got, err := os.ReadFile(path)
	if err != nil {
		t.Fatal(err)
	}
	if string(got) != "x!" {
		t.Fatalf("file contents = %q, want %q", got, "x!")
	}
}

func TestUnknownCommandIsIgnored(t *testing.T) {
	v := NewView()
	v.Open(fixturePath(t, "editor_sample.txt"))

	v.HandleKey(layout.Key{Text: ":"})
	v.HandleKey(layout.Key{Text: "z"})
	v.HandleKey(layout.Key{Named: layout.KeyEnter})

	if v.mode != modeNormal {
		t.Fatal("an unrecognized command should still close the prompt")
	}
	if len(v.tabs) != 1 {
		t.Fatal("an unrecognized command must not close any tab")
	}
}

func TestOnAllTabsClosedFiresWhenLastTabCloses(t *testing.T) {
	v := NewView()
	v.Open(fixturePath(t, "editor_sample.txt"))
	calls := 0
	v.OnAllTabsClosed = func() { calls++ }

	v.CloseTab()

	if calls != 1 {
		t.Fatalf("expected OnAllTabsClosed to fire exactly once, got %d", calls)
	}
}

func TestOnAllTabsClosedFiresForCloseAllTabs(t *testing.T) {
	v := NewView()
	v.Open(fixturePath(t, "editor_sample.txt"))
	v.Open("/some/other/file.txt")
	calls := 0
	v.OnAllTabsClosed = func() { calls++ }

	v.CloseAllTabs()

	if calls != 1 {
		t.Fatalf("expected OnAllTabsClosed to fire exactly once, got %d", calls)
	}
}

func TestOnAllTabsClosedDoesNotFireWhileTabsRemain(t *testing.T) {
	v := NewView()
	v.Open(fixturePath(t, "editor_sample.txt"))
	v.Open("/some/other/file.txt")
	calls := 0
	v.OnAllTabsClosed = func() { calls++ }

	v.CloseTab() // one tab remains

	if calls != 0 {
		t.Fatalf("expected OnAllTabsClosed NOT to fire while a tab remains, got %d calls", calls)
	}
}

func TestOnAllTabsClosedFiresViaColonQ(t *testing.T) {
	v := NewView()
	v.Open(fixturePath(t, "editor_sample.txt"))
	calls := 0
	v.OnAllTabsClosed = func() { calls++ }

	v.HandleKey(layout.Key{Text: ":"})
	v.HandleKey(layout.Key{Text: "q"})
	v.HandleKey(layout.Key{Named: layout.KeyEnter})

	if calls != 1 {
		t.Fatalf("expected OnAllTabsClosed to fire once via :q, got %d", calls)
	}
}

// sharedPanes opens path in two Views that share one BufferStore — the
// split-pane-on-the-same-file scenario cmd/nib/main.go sets up.
func sharedPanes(t *testing.T, path string) (v1, v2 *View) {
	t.Helper()
	store := NewBufferStore()
	v1, v2 = NewView(), NewView()
	v1.SetBufferStore(store)
	v2.SetBufferStore(store)
	v1.Open(path)
	v2.Open(path)
	return v1, v2
}

func TestTwoPanesSharingAStoreShareTheSameBuffer(t *testing.T) {
	v1, v2 := sharedPanes(t, fixturePath(t, "editor_sample.txt"))
	if v1.activeTab().buf != v2.activeTab().buf {
		t.Fatal("expected both panes' tabs to point at the same *Buffer")
	}
}

func TestEditInOnePaneIsVisibleInAnotherSharingTheBuffer(t *testing.T) {
	v1, v2 := sharedPanes(t, fixturePath(t, "editor_sample.txt"))

	v1.HandleKey(layout.Key{Text: "A"}) // not bound; harmless
	v1.activeTab().cursorCol = len(v1.activeTab().buf.Lines[0])
	v1.HandleKey(layout.Key{Text: "i"})
	v1.HandleKey(layout.Key{Text: "!"})
	v1.HandleKey(layout.Key{Named: layout.KeyEsc})

	if got := v2.activeTab().buf.Lines[0]; got != "line one!" {
		t.Fatalf("pane 2's Lines[0] = %q, want %q (edit made in pane 1)", got, "line one!")
	}
	if !v2.activeTab().buf.Dirty {
		t.Fatal("expected pane 2's tab bar to also see the buffer as dirty")
	}
}

func TestSavingFromOnePaneIsReflectedInTheOther(t *testing.T) {
	dir := t.TempDir()
	path := filepath.Join(dir, "f.txt")
	if err := os.WriteFile(path, []byte("x"), 0o644); err != nil {
		t.Fatal(err)
	}

	v1, v2 := sharedPanes(t, path)
	v1.activeTab().cursorCol = 1
	v1.HandleKey(layout.Key{Text: "i"})
	v1.HandleKey(layout.Key{Text: "!"})
	v1.HandleKey(layout.Key{Named: layout.KeyEsc})
	v1.HandleKey(layout.Key{Text: "s", Mods: layout.ModCtrl})

	if v2.activeTab().buf.Dirty {
		t.Fatal("expected pane 2 to see the buffer as saved (not dirty) after pane 1's Ctrl+s")
	}
	got, err := os.ReadFile(path)
	if err != nil {
		t.Fatal(err)
	}
	if string(got) != "x!" {
		t.Fatalf("file contents = %q, want %q", got, "x!")
	}
}

func TestUndoInOnePaneUndoesEditMadeInAnotherPane(t *testing.T) {
	v1, v2 := sharedPanes(t, fixturePath(t, "editor_sample.txt"))

	v1.activeTab().cursorCol = len(v1.activeTab().buf.Lines[0])
	v1.HandleKey(layout.Key{Text: "i"})
	v1.HandleKey(layout.Key{Text: "!"})
	v1.HandleKey(layout.Key{Named: layout.KeyEsc})
	if got := v2.activeTab().buf.Lines[0]; got != "line one!" {
		t.Fatalf("setup: pane 2 Lines[0] = %q, want %q", got, "line one!")
	}

	if !v2.HandleKey(layout.Key{Text: "u"}) {
		t.Fatal("expected 'u' in pane 2 to be consumed")
	}
	if got := v2.activeTab().buf.Lines[0]; got != "line one" {
		t.Fatalf("pane 2's Lines[0] after undo = %q, want %q (reverts an edit made in pane 1)", got, "line one")
	}
	if v2.activeTab().cursorCol != len("line one") {
		t.Fatalf("expected pane 2's OWN cursor to move to the undone edit's position, got %d", v2.activeTab().cursorCol)
	}
}

func TestClosingOneTabDoesNotDisturbSiblingPaneStillShowingTheBuffer(t *testing.T) {
	v1, v2 := sharedPanes(t, fixturePath(t, "editor_sample.txt"))

	v1.CloseTab()

	if v2.activeTab().buf == nil || v2.activeTab().buf.Lines[0] != "line one" {
		t.Fatal("expected pane 2's buffer to remain valid and unaffected by pane 1 closing its own tab")
	}
}

func TestClosingEveryReferencingTabEvictsFromStore(t *testing.T) {
	store := NewBufferStore()
	v1, v2 := NewView(), NewView()
	v1.SetBufferStore(store)
	v2.SetBufferStore(store)
	path := fixturePath(t, "editor_sample.txt")
	v1.Open(path)
	v2.Open(path)

	v1.CloseTab()
	if store.Len() != 1 {
		t.Fatalf("Len() = %d, want 1 (pane 2 still references it)", store.Len())
	}
	v2.CloseTab()
	if store.Len() != 0 {
		t.Fatalf("Len() = %d, want 0 (no pane references it anymore)", store.Len())
	}
}

func TestExitEditingModesCommitsInsertSessionAndClearsCommandPrompt(t *testing.T) {
	v := NewView()
	v.tabs = []*tab{{buf: &Buffer{Lines: []string{"abc"}}}}
	v.active = 0
	v.HandleKey(layout.Key{Text: "i"})
	v.HandleKey(layout.Key{Text: "X"})

	v.ExitEditingModes()

	if v.mode != modeNormal {
		t.Fatal("expected Normal mode after ExitEditingModes")
	}
	if got := v.activeTab().buf.Lines[0]; got != "Xabc" {
		t.Fatalf("Lines[0] = %q, want %q (typed text committed)", got, "Xabc")
	}
	if !v.HandleKey(layout.Key{Text: "u"}) {
		t.Fatal("expected the committed session to be undoable")
	}
	if got := v.activeTab().buf.Lines[0]; got != "abc" {
		t.Fatalf("Lines[0] after undo = %q, want %q", got, "abc")
	}

	v.HandleKey(layout.Key{Text: ":"})
	v.HandleKey(layout.Key{Text: "4"})
	v.ExitEditingModes()
	if v.mode != modeNormal || v.commandBuf != "" {
		t.Fatalf("expected ExitEditingModes to clear the Command prompt, got mode=%v commandBuf=%q", v.mode, v.commandBuf)
	}
}

func TestExitEditingModesCancelsSearchPrompt(t *testing.T) {
	v := NewView()
	v.tabs = []*tab{{buf: &Buffer{Lines: []string{"abc", "xyz"}}}}
	v.active = 0
	v.activeTab().cursorLn, v.activeTab().cursorCol = 0, 1

	v.HandleKey(layout.Key{Text: "/"})
	v.HandleKey(layout.Key{Text: "x"})
	if v.mode != modeSearch || len(v.searchMatches) == 0 {
		t.Fatal("expected Search mode with a live match before ExitEditingModes")
	}

	v.ExitEditingModes()

	if v.mode != modeNormal {
		t.Fatalf("expected Normal mode after ExitEditingModes, got %v", v.mode)
	}
	if v.searchBuf != "" {
		t.Fatalf("expected the Search prompt to be cleared, got searchBuf=%q", v.searchBuf)
	}
	if v.searchMatches != nil {
		t.Fatalf("expected the in-progress match highlights to be cleared, got %+v", v.searchMatches)
	}
	if ln, col := v.activeTab().cursorLn, v.activeTab().cursorCol; ln != 0 || col != 1 {
		t.Fatalf("expected the cursor restored to (0, 1), got (%d, %d)", ln, col)
	}
}

func TestRenderClampsCursorAfterSiblingPaneShrinksSharedBuffer(t *testing.T) {
	store := NewBufferStore()
	v1, v2 := NewView(), NewView()
	v1.SetBufferStore(store)
	v2.SetBufferStore(store)
	shared := &Buffer{Lines: []string{"one", "two", "three", "four", "five"}}
	store.bufs["shared"] = &storedBuffer{buf: shared, count: 2}
	v1.tabs = []*tab{{path: "shared", buf: shared}}
	v2.tabs = []*tab{{path: "shared", buf: shared}}
	v1.active, v2.active = 0, 0
	v2.activeTab().cursorLn = 4 // last line

	shared.Restore([]string{"only line"}) // simulates pane 1 shrinking the buffer

	w := newFakeWindow(40, 10)
	v2.Render(w) // must not panic, and must clamp instead of rendering an empty body

	if v2.activeTab().cursorLn != 0 {
		t.Fatalf("cursorLn = %d, want 0 (clamped into the shrunk buffer)", v2.activeTab().cursorLn)
	}
	if !strings.Contains(w.lines[1], "only line") {
		t.Fatalf("expected the shrunk content to actually render, got %q", w.lines[1])
	}
}

func TestTabInsertsTabCharacterInInsertMode(t *testing.T) {
	v := NewView()
	v.tabs = []*tab{{buf: &Buffer{Lines: []string{"ab"}}}}
	v.active = 0
	v.activeTab().cursorCol = 1
	v.HandleKey(layout.Key{Text: "i"})

	if !v.HandleKey(layout.Key{Named: layout.KeyTab}) {
		t.Fatal("expected Tab to be consumed while inserting")
	}
	if got := v.activeTab().buf.Lines[0]; got != "a\tb" {
		t.Fatalf("Lines[0] = %q, want %q", got, "a\tb")
	}
}

func TestTabInNormalModeIsNotConsumedByEditor(t *testing.T) {
	v := NewView()
	v.tabs = []*tab{{buf: &Buffer{Lines: []string{"ab"}}}}
	v.active = 0

	if v.HandleKey(layout.Key{Named: layout.KeyTab}) {
		t.Fatal("expected Tab in Normal mode to fall through unconsumed, so the global focus-cycle keybind still fires")
	}
}

// TestHandlePasteInInsertModeSplitsIntoMultipleLines is a regression test
// for the reported bug: a multi-line paste used to glue every line onto the
// cursor's line instead of actually creating new lines.
func TestHandlePasteInInsertModeSplitsIntoMultipleLines(t *testing.T) {
	v := NewView()
	v.tabs = []*tab{{buf: &Buffer{Lines: []string{"ac"}}}}
	v.active = 0
	v.HandleKey(layout.Key{Text: "i"})
	v.activeTab().cursorCol = 1

	if !v.HandlePaste("1\n2\n3") {
		t.Fatal("expected HandlePaste to report the paste as handled")
	}

	want := []string{"a1", "2", "3c"}
	if got := v.activeTab().buf.Lines; !linesEqual(got, want) {
		t.Fatalf("Lines = %q, want %q", got, want)
	}
	if v.activeTab().cursorLn != 2 || v.activeTab().cursorCol != 1 {
		t.Fatalf("cursor = (%d,%d), want (2,1) — just after the pasted \"3\"", v.activeTab().cursorLn, v.activeTab().cursorCol)
	}
}

// TestHandlePasteInNormalModeInsertsAsOneUndoEntry guards that a paste while
// in Normal mode (not already in the middle of typing) still inserts text —
// bracketed paste means "this is literal content", not "replay these as
// Normal-mode commands" — and that the whole paste undoes in a single step.
func TestHandlePasteInNormalModeInsertsAsOneUndoEntry(t *testing.T) {
	v := NewView()
	v.tabs = []*tab{{buf: &Buffer{Lines: []string{""}}}}
	v.active = 0

	v.HandlePaste("one\ntwo")

	if v.mode != modeNormal {
		t.Fatalf("expected Normal mode to be restored after the paste, got mode=%v", v.mode)
	}
	want := []string{"one", "two"}
	if got := v.activeTab().buf.Lines; !linesEqual(got, want) {
		t.Fatalf("Lines = %q, want %q", got, want)
	}

	v.undo(v.activeTab())
	if got := v.activeTab().buf.Lines; !linesEqual(got, []string{""}) {
		t.Fatalf("expected a single undo to revert the whole paste, got Lines = %q", got)
	}
}

// TestHandlePasteInCommandModeStripsNewlines guards that pasting into the
// ":" prompt (a single-line field) appends the text instead of committing
// partway through the way a literal Enter keystroke would.
func TestHandlePasteInCommandModeStripsNewlines(t *testing.T) {
	v := NewView()
	v.tabs = []*tab{{buf: &Buffer{Lines: []string{"one", "two", "three"}}}}
	v.active = 0
	v.mode = modeCommand

	v.HandlePaste("1\n0")

	if v.mode != modeCommand {
		t.Fatalf("expected to stay in Command mode, got mode=%v", v.mode)
	}
	if v.commandBuf != "10" {
		t.Fatalf("commandBuf = %q, want %q (newlines stripped, not committed)", v.commandBuf, "10")
	}
}

// TestHandlePasteInSearchModeStripsNewlines is the Search-mode counterpart
// of TestHandlePasteInCommandModeStripsNewlines.
func TestHandlePasteInSearchModeStripsNewlines(t *testing.T) {
	v := NewView()
	v.tabs = []*tab{{buf: &Buffer{Lines: []string{"needle", "other"}}}}
	v.active = 0
	v.enterSearchMode()

	v.HandlePaste("nee\ndle")

	if v.mode != modeSearch {
		t.Fatalf("expected to stay in Search mode, got mode=%v", v.mode)
	}
	if v.searchBuf != "needle" {
		t.Fatalf("searchBuf = %q, want %q (newlines stripped)", v.searchBuf, "needle")
	}
}
