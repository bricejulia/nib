package editor

import (
	"strings"
	"testing"

	"github.com/bricejulia/kiwi/internal/layout"
)

// selectionView builds a pane holding lines, sized so Render's own
// topLine/leftCol derivation has room to be a no-op — the mouse coordinate
// math is measured against a viewport, so the tests need one.
func selectionView(lines ...string) (*View, *tab) {
	v := NewView()
	t := &tab{buf: &Buffer{Lines: lines}}
	v.tabs = []*tab{t}
	v.active = 0
	// What Render would set; positionAt reads lastHeight to decide whether a
	// row is outside the pane.
	v.lastWidth, v.lastHeight = 40, 10
	return v, t
}

// press/motion/release build the three mouse events a drag is made of, in
// pane-relative coordinates (row 0 is the tab bar).
func press(col, row, clicks int) layout.Mouse {
	return layout.Mouse{Col: col, Row: row, Button: layout.MouseLeft, EventType: layout.EventPress, Clicks: clicks}
}

func motion(col, row int) layout.Mouse {
	return layout.Mouse{Col: col, Row: row, Button: layout.MouseLeft, EventType: layout.EventMotion}
}

func release(col, row int) layout.Mouse {
	return layout.Mouse{Col: col, Row: row, Button: layout.MouseLeft, EventType: layout.EventRelease}
}

// gutterFor is the first column holding text, for a buffer of n lines.
func gutterFor(t *tab) int { return gutterWidthFor(t) }

func TestClickPlacesCursorWithoutSelecting(t *testing.T) {
	v, tb := selectionView("hello world")
	g := gutterFor(tb)

	if !v.HandleMouse(press(g+6, 1, 1)) {
		t.Fatal("expected the press to be consumed")
	}
	if tb.cursorLn != 0 || tb.cursorCol != 6 {
		t.Errorf("cursor = (%d,%d), want (0,6)", tb.cursorLn, tb.cursorCol)
	}
	if tb.hasSel {
		t.Error("a single click should place the cursor, not select anything")
	}
}

func TestClickOnSecondRowSelectsSecondLine(t *testing.T) {
	// Row 0 is the tab bar, so row 2 is the buffer's second line.
	v, tb := selectionView("one", "two", "three")
	g := gutterFor(tb)

	v.HandleMouse(press(g+1, 2, 1))
	if tb.cursorLn != 1 || tb.cursorCol != 1 {
		t.Errorf("cursor = (%d,%d), want (1,1)", tb.cursorLn, tb.cursorCol)
	}
}

func TestClickOnTabBarIsNotConsumed(t *testing.T) {
	// Row 0 is the tab bar. Left unclaimed so click-to-switch-tabs can be
	// added later without having to undo anything.
	v, _ := selectionView("hello")
	if v.HandleMouse(press(5, 0, 1)) {
		t.Error("a click on the tab bar should not be consumed by the text area")
	}
}

func TestClickInGutterSelectsFromLineStart(t *testing.T) {
	v, tb := selectionView("hello world")

	v.HandleMouse(press(0, 1, 1))
	if tb.cursorCol != 0 {
		t.Errorf("cursorCol = %d, want 0 (a gutter click reads as the line start)", tb.cursorCol)
	}
}

func TestWheelIsNotConsumedSoAppStillScrolls(t *testing.T) {
	// Regression guard: App turns a wheel tick into Up/Down key presses, and
	// that only happens for an event the View declines.
	v, _ := selectionView("a", "b", "c")
	for _, b := range []layout.MouseButton{layout.MouseWheelUp, layout.MouseWheelDown} {
		m := layout.Mouse{Col: 5, Row: 2, Button: b, EventType: layout.EventPress, Clicks: 1}
		if v.HandleMouse(m) {
			t.Errorf("button %v: the editor should leave the wheel to App", b)
		}
	}
}

func TestBareHoverDoesNotSelect(t *testing.T) {
	// All-motion tracking reports the pointer crossing the screen with
	// nothing held down; that must not start or extend a selection.
	v, tb := selectionView("hello world")
	g := gutterFor(tb)

	hover := layout.Mouse{Col: g + 3, Row: 1, Button: layout.MouseNone, EventType: layout.EventMotion}
	if v.HandleMouse(hover) {
		t.Error("bare hover should not be consumed")
	}
	if tb.hasSel {
		t.Error("bare hover must not create a selection")
	}
}

func TestDragSelectsWithinOneLine(t *testing.T) {
	v, tb := selectionView("hello world")
	g := gutterFor(tb)

	v.HandleMouse(press(g+0, 1, 1))
	v.HandleMouse(motion(g+5, 1))
	v.HandleMouse(release(g+5, 1))

	if !tb.hasSel {
		t.Fatal("expected a selection after a drag")
	}
	if got := strings.Join(v.selectionText(tb), "\n"); got != "hello" {
		t.Errorf("selected %q, want %q", got, "hello")
	}
}

func TestSelectionSurvivesTheRelease(t *testing.T) {
	// The whole point: the selection has to still be there afterwards for
	// "y" to copy it.
	v, tb := selectionView("hello world")
	g := gutterFor(tb)

	v.HandleMouse(press(g, 1, 1))
	v.HandleMouse(motion(g+5, 1))
	v.HandleMouse(release(g+5, 1))

	if !tb.hasSel {
		t.Error("the selection should outlive the button release")
	}
	if v.dragging {
		t.Error("release should end the drag")
	}
}

func TestMotionWithoutAPressDoesNotSelect(t *testing.T) {
	v, tb := selectionView("hello world")
	g := gutterFor(tb)

	if v.HandleMouse(motion(g+5, 1)) {
		t.Error("motion outside a drag should not be consumed")
	}
	if tb.hasSel {
		t.Error("motion outside a drag must not select")
	}
}

func TestDragBackwardsSelectsTheSameRange(t *testing.T) {
	// A drag can run right-to-left; selectionSpan normalises, so the text is
	// the same either way.
	v, tb := selectionView("hello world")
	g := gutterFor(tb)

	v.HandleMouse(press(g+5, 1, 1))
	v.HandleMouse(motion(g+0, 1))

	if got := strings.Join(v.selectionText(tb), "\n"); got != "hello" {
		t.Errorf("selected %q, want %q", got, "hello")
	}
}

func TestDragAcrossLinesSelectsTheLineBreak(t *testing.T) {
	v, tb := selectionView("one", "two", "three")
	g := gutterFor(tb)

	v.HandleMouse(press(g+1, 1, 1)) // after "o" of "one"
	v.HandleMouse(motion(g+2, 3))   // into "three"

	got := v.selectionText(tb)
	want := []string{"ne", "two", "th"}
	if len(got) != len(want) {
		t.Fatalf("selected %q, want %q", got, want)
	}
	for i := range want {
		if got[i] != want[i] {
			t.Fatalf("selected %q, want %q", got, want)
		}
	}
}

func TestDragUpwardsSelectsTheSameRangeAsDownwards(t *testing.T) {
	v, tb := selectionView("one", "two", "three")
	g := gutterFor(tb)

	v.HandleMouse(press(g+2, 3, 1)) // start in "three"
	v.HandleMouse(motion(g+1, 1))   // drag up into "one"

	if got := strings.Join(v.selectionText(tb), "\n"); got != "ne\ntwo\nth" {
		t.Errorf("selected %q, want %q", got, "ne\ntwo\nth")
	}
}

func TestDoubleClickSelectsTheWord(t *testing.T) {
	v, tb := selectionView("alpha beta gamma")
	g := gutterFor(tb)

	v.HandleMouse(press(g+7, 1, 2)) // inside "beta"

	if got := strings.Join(v.selectionText(tb), "\n"); got != "beta" {
		t.Errorf("selected %q, want %q", got, "beta")
	}
}

func TestDoubleClickOffAWordSelectsNothing(t *testing.T) {
	// Rather than guessing at a neighbouring word.
	v, tb := selectionView("a    b")
	g := gutterFor(tb)

	v.HandleMouse(press(g+3, 1, 2)) // in the run of spaces
	if tb.hasSel {
		t.Errorf("expected no selection, got %q", v.selectionText(tb))
	}
}

func TestTripleClickSelectsTheWholeLineIncludingItsBreak(t *testing.T) {
	// Ending at the start of the next line is what makes a triple-clicked
	// copy paste back as a whole line.
	v, tb := selectionView("one", "two", "three")
	g := gutterFor(tb)

	v.HandleMouse(press(g+1, 2, 3)) // on "two"

	got := v.selectionText(tb)
	if len(got) != 2 || got[0] != "two" || got[1] != "" {
		t.Errorf("selected %q, want [\"two\" \"\"]", got)
	}
}

func TestTripleClickOnTheLastLineSelectsToItsEnd(t *testing.T) {
	// There is no following line to end at, so it stops at end-of-line.
	v, tb := selectionView("one", "last")
	g := gutterFor(tb)

	v.HandleMouse(press(g+1, 2, 3))

	if got := strings.Join(v.selectionText(tb), "\n"); got != "last" {
		t.Errorf("selected %q, want %q", got, "last")
	}
}

func TestShiftClickExtendsAnExistingSelection(t *testing.T) {
	v, tb := selectionView("hello world")
	g := gutterFor(tb)

	v.HandleMouse(press(g, 1, 1))
	v.HandleMouse(motion(g+5, 1))
	v.HandleMouse(release(g+5, 1))

	ext := press(g+11, 1, 1)
	ext.Mods = layout.ModShift
	v.HandleMouse(ext)

	if got := strings.Join(v.selectionText(tb), "\n"); got != "hello world" {
		t.Errorf("selected %q, want %q", got, "hello world")
	}
}

func TestShiftClickWithNoSelectionJustPlacesTheCursor(t *testing.T) {
	v, tb := selectionView("hello world")
	g := gutterFor(tb)

	m := press(g+6, 1, 1)
	m.Mods = layout.ModShift
	v.HandleMouse(m)

	if tb.hasSel {
		t.Error("Shift+click with nothing selected should not invent a selection")
	}
	if tb.cursorCol != 6 {
		t.Errorf("cursorCol = %d, want 6", tb.cursorCol)
	}
}

func TestDragBelowThePaneScrollsDown(t *testing.T) {
	lines := make([]string, 50)
	for i := range lines {
		lines[i] = "line"
	}
	v, tb := selectionView(lines...)
	g := gutterFor(tb)

	v.HandleMouse(press(g, 1, 1))
	before := tb.cursorLn
	// bodyRows is lastHeight-1 == 9, so row 10 is past the bottom edge.
	v.HandleMouse(motion(g, 10))

	if tb.cursorLn <= before {
		t.Errorf("cursorLn = %d, want past %d — dragging below the pane should scroll", tb.cursorLn, before)
	}
	if !tb.hasSel {
		t.Error("expected the drag to still be selecting")
	}
}

func TestDragAbovePaneScrollsUp(t *testing.T) {
	lines := make([]string, 50)
	for i := range lines {
		lines[i] = "line"
	}
	v, tb := selectionView(lines...)
	tb.topLine = 20
	tb.cursorLn = 25
	g := gutterFor(tb)

	v.HandleMouse(press(g, 3, 1))
	// Row 0 is the tab bar, so any row below 1 is past the top edge.
	v.HandleMouse(motion(g, 0))

	if tb.cursorLn != 19 {
		t.Errorf("cursorLn = %d, want 19 (one line above topLine 20)", tb.cursorLn)
	}
}

func TestDragPastTheTopOfTheBufferClampsToZero(t *testing.T) {
	v, tb := selectionView("one", "two")
	g := gutterFor(tb)

	v.HandleMouse(press(g, 2, 1))
	v.HandleMouse(motion(g, 0)) // topLine is 0, so this asks for line -1

	if tb.cursorLn != 0 {
		t.Errorf("cursorLn = %d, want 0", tb.cursorLn)
	}
}

func TestDragHonoursHorizontalScroll(t *testing.T) {
	// leftCol shifts which display column a given screen cell names; getting
	// this wrong silently selects the wrong text on any long line.
	v, tb := selectionView("abcdefghijklmnopqrstuvwxyz")
	tb.leftCol = 10
	g := gutterFor(tb)

	v.HandleMouse(press(g, 1, 1))
	if tb.cursorCol != 10 {
		t.Errorf("cursorCol = %d, want 10 (column 0 of the pane at leftCol 10)", tb.cursorCol)
	}
}

func TestClickPastEndOfLineClampsToItsEnd(t *testing.T) {
	v, tb := selectionView("hi")
	g := gutterFor(tb)

	v.HandleMouse(press(g+30, 1, 1))
	if tb.cursorCol != 2 {
		t.Errorf("cursorCol = %d, want 2 (one past the last rune)", tb.cursorCol)
	}
}

func TestSelectionAccountsForTabExpansion(t *testing.T) {
	// The line is stored with a tab but rendered expanded, so a click lands
	// on a display column that has no matching raw rune index.
	v, tb := selectionView("\tindented")
	g := gutterFor(tb)

	// tabWidth is 4, so "indented" starts at display column 4.
	v.HandleMouse(press(g+4, 1, 1))
	v.HandleMouse(motion(g+12, 1))

	if got := strings.Join(v.selectionText(tb), "\n"); got != "indented" {
		t.Errorf("selected %q, want %q", got, "indented")
	}
}

func TestSelectionAccountsForWideRunes(t *testing.T) {
	// A CJK glyph is two cells wide, so display columns and rune indices
	// diverge — clicking the second cell of a glyph must not overshoot it.
	v, tb := selectionView("世界ok")
	g := gutterFor(tb)

	v.HandleMouse(press(g, 1, 1))
	v.HandleMouse(motion(g+4, 1)) // past both glyphs

	if got := strings.Join(v.selectionText(tb), "\n"); got != "世界" {
		t.Errorf("selected %q, want %q", got, "世界")
	}
}

func TestClearSelectionCollapsesIt(t *testing.T) {
	v, tb := selectionView("hello")
	g := gutterFor(tb)

	v.HandleMouse(press(g, 1, 1))
	v.HandleMouse(motion(g+5, 1))
	tb.clearSelection()

	if tb.hasSel {
		t.Error("hasSel should be false after clearSelection")
	}
	if v.selectionText(tb) != nil {
		t.Errorf("selectionText = %q, want nil", v.selectionText(tb))
	}
}

func TestMovementKeyClearsTheSelection(t *testing.T) {
	v, tb := selectionView("hello world")
	g := gutterFor(tb)

	v.HandleMouse(press(g, 1, 1))
	v.HandleMouse(motion(g+5, 1))
	v.HandleMouse(release(g+5, 1))

	v.HandleKey(layout.Key{Named: layout.KeyRight})
	if tb.hasSel {
		t.Error("moving the cursor should collapse the selection")
	}
}

func TestEscClearsTheSelection(t *testing.T) {
	v, tb := selectionView("hello world")
	g := gutterFor(tb)

	v.HandleMouse(press(g, 1, 1))
	v.HandleMouse(motion(g+5, 1))
	v.HandleMouse(release(g+5, 1))

	if !v.HandleKey(layout.Key{Named: layout.KeyEsc}) {
		t.Error("Esc should be consumed when it has a selection to dismiss")
	}
	if tb.hasSel {
		t.Error("Esc should clear the selection")
	}
}

func TestEscWithNoSelectionStillBubblesToTheGlobalKeymap(t *testing.T) {
	// Regression guard: consuming a bare Esc here would quietly break
	// whatever the global keymap binds it to.
	v, _ := selectionView("hello")
	if v.HandleKey(layout.Key{Named: layout.KeyEsc}) {
		t.Error("Esc with nothing selected should not be consumed")
	}
}

func TestYankCopiesTheSelectionOnTheFirstPress(t *testing.T) {
	// Unlike "yy", which needs two: the range is already chosen.
	v, tb := selectionView("hello world")
	g := gutterFor(tb)

	var copied string
	v.CopyFunc = func(s string) { copied = s }

	v.HandleMouse(press(g, 1, 1))
	v.HandleMouse(motion(g+5, 1))
	v.HandleMouse(release(g+5, 1))

	v.HandleKey(layout.Key{Text: "y"})

	if copied != "hello" {
		t.Errorf("clipboard got %q, want %q", copied, "hello")
	}
	if got := strings.Join(v.register.Lines(), "\n"); got != "hello" {
		t.Errorf("register got %q, want %q", got, "hello")
	}
	if !v.register.Charwise() {
		t.Error("a selection copy must be marked charwise, not linewise")
	}
	if tb.hasSel {
		t.Error("the selection should be consumed by the copy")
	}
	if v.pendingAction != "" {
		t.Errorf("pendingAction = %q, want empty — a selection yank must not arm a doubled operator", v.pendingAction)
	}
}

func TestYankWithNoSelectionStillNeedsDoubling(t *testing.T) {
	// "yy" must keep working exactly as it did.
	v, tb := selectionView("hello", "world")

	v.HandleKey(layout.Key{Text: "y"})
	if v.pendingAction != "yank_line" {
		t.Fatalf("pendingAction = %q, want yank_line after one press", v.pendingAction)
	}
	v.HandleKey(layout.Key{Text: "y"})

	if got := strings.Join(v.register.Lines(), "\n"); got != "hello" {
		t.Errorf("register got %q, want %q", got, "hello")
	}
	if v.register.Charwise() {
		t.Error("\"yy\" must stay linewise")
	}
	_ = tb
}

func TestCopySelectionWorksWithoutACopyFunc(t *testing.T) {
	// CopyFunc is optional — the register half must still work, so "p"
	// inside kiwi is unaffected by a terminal that can't take OSC 52.
	v, tb := selectionView("hello world")
	g := gutterFor(tb)

	v.HandleMouse(press(g, 1, 1))
	v.HandleMouse(motion(g+5, 1))

	if !v.copySelection(tb) {
		t.Fatal("expected the copy to report success")
	}
	if got := strings.Join(v.register.Lines(), "\n"); got != "hello" {
		t.Errorf("register got %q, want %q", got, "hello")
	}
}

func TestCopySelectionWithNothingSelectedReportsFalse(t *testing.T) {
	v, tb := selectionView("hello")
	if v.copySelection(tb) {
		t.Error("expected false with no selection")
	}
}

func TestSelectionRendersWithABackgroundOverSyntaxColours(t *testing.T) {
	// The layering contract: selection adds a background, and the syntax
	// foreground underneath survives.
	v, tb := selectionView("abc")
	tb.buf.highlighted = [][]layout.Segment{
		{{Text: "abc", Style: layout.Style{Foreground: layout.ColorGreen}}},
	}
	g := gutterFor(tb)

	v.HandleMouse(press(g, 1, 1))
	v.HandleMouse(motion(g+2, 1))

	w := newFakeWindow(40, 10)
	v.Render(w)

	found := false
	for _, s := range w.segs[1] {
		if s.Style.Background == selectionStyle.Background && s.Text != "" && strings.TrimSpace(s.Text) != "" {
			found = true
			if s.Style.Foreground != layout.ColorGreen {
				t.Errorf("segment %q lost its syntax colour: %v", s.Text, s.Style.Foreground)
			}
		}
	}
	if !found {
		t.Errorf("no selection-styled segment on the rendered row: %+v", w.segs[1])
	}
}

func TestUnselectedTextRendersWithNoBackground(t *testing.T) {
	v, tb := selectionView("abcdef")
	g := gutterFor(tb)

	v.HandleMouse(press(g, 1, 1))
	v.HandleMouse(motion(g+3, 1)) // select "abc" only

	w := newFakeWindow(40, 10)
	v.Render(w)

	for _, s := range w.segs[1] {
		if strings.Contains(s.Text, "def") && s.Style.Background != layout.ColorDefault {
			t.Errorf("unselected %q should have no background, got %v", s.Text, s.Style.Background)
		}
	}
}

func TestSelectedBlankLineIsStillVisiblyHighlighted(t *testing.T) {
	// A blank line produces no segments at all, so without the end-of-line
	// pad a selection spanning it would appear to skip it.
	v, tb := selectionView("one", "", "three")
	g := gutterFor(tb)

	v.HandleMouse(press(g, 1, 1)) // start of "one"
	v.HandleMouse(motion(g+2, 3)) // into "three"

	w := newFakeWindow(40, 10)
	v.Render(w)

	// Row 2 renders the blank line.
	if !rowHasStyle(w, 2, func(s layout.Style) bool { return s.Background == selectionStyle.Background }) {
		t.Errorf("the selected blank line has no highlight: %+v", w.segs[2])
	}
}

func TestSelectedLineBreakPadsToTheRightEdge(t *testing.T) {
	// An intermediate line of a multi-line selection has its line break
	// selected too, so the highlight runs to the edge rather than stopping
	// ragged at the end of the text.
	v, tb := selectionView("ab", "cd", "ef")
	g := gutterFor(tb)

	v.HandleMouse(press(g, 1, 1))
	v.HandleMouse(motion(g+1, 3))

	w := newFakeWindow(40, 10)
	v.Render(w)

	width := 0
	for _, s := range w.segs[1] {
		if s.Style.Background == selectionStyle.Background {
			width += len(s.Text)
		}
	}
	// The pane is 40 wide; text starts after the gutter.
	if want := 40 - g; width != want {
		t.Errorf("selected width on row 1 = %d, want %d (padded to the right edge)", width, want)
	}
}

func TestLastLineOfASelectionStopsAtTheSelectionEnd(t *testing.T) {
	// The final line ends mid-text, so it must NOT be padded — that's what
	// distinguishes "the break is selected" from "the selection ends here".
	v, tb := selectionView("ab", "cdef")
	g := gutterFor(tb)

	v.HandleMouse(press(g, 1, 1))
	v.HandleMouse(motion(g+2, 2)) // stop inside "cdef"

	w := newFakeWindow(40, 10)
	v.Render(w)

	width := 0
	for _, s := range w.segs[2] {
		if s.Style.Background == selectionStyle.Background {
			width += len(s.Text)
		}
	}
	if width != 2 {
		t.Errorf("selected width on the last row = %d, want 2 (\"cd\", unpadded)", width)
	}
}

func TestNoSelectionRendersNoBackgroundAnywhere(t *testing.T) {
	v, _ := selectionView("alpha", "beta")

	w := newFakeWindow(40, 10)
	v.Render(w)

	for row := range w.segs {
		for _, s := range w.segs[row] {
			if s.Style.Background != layout.ColorDefault {
				t.Errorf("row %d segment %q has a background with nothing selected", row, s.Text)
			}
		}
	}
}

func TestSelectionAndSearchHighlightCompose(t *testing.T) {
	// The reason selection uses a background rather than reverse video: a
	// search match inside a selection has to stay distinguishable.
	v, tb := selectionView("foo bar foo")
	v.searchPattern = "bar"
	v.searchMatches = findMatches(tb.buf, "bar")
	g := gutterFor(tb)

	v.HandleMouse(press(g, 1, 1))
	v.HandleMouse(motion(g+11, 1))

	w := newFakeWindow(40, 10)
	v.Render(w)

	both := false
	for _, s := range w.segs[1] {
		if strings.Contains(s.Text, "bar") &&
			s.Style.Background == selectionStyle.Background &&
			s.Style.Attr&layout.AttrReverse != 0 {
			both = true
		}
	}
	if !both {
		t.Errorf("the match inside the selection should carry both styles: %+v", w.segs[1])
	}
}

func TestSelectionIsPerTab(t *testing.T) {
	// Selection is view state on the tab, like the cursor — switching tabs
	// must not carry it across.
	v, first := selectionView("hello")
	second := &tab{buf: &Buffer{Lines: []string{"world"}}}
	v.tabs = append(v.tabs, second)
	g := gutterFor(first)

	v.HandleMouse(press(g, 1, 1))
	v.HandleMouse(motion(g+5, 1))
	if !first.hasSel {
		t.Fatal("expected a selection on the first tab")
	}

	v.NextTab()
	if second.hasSel {
		t.Error("the second tab should have its own (empty) selection")
	}
	if !first.hasSel {
		t.Error("switching away should not discard the first tab's selection")
	}
}

func TestExitEditingModesEndsADragButKeepsTheSelection(t *testing.T) {
	// Focus moving away takes the release with it, so the drag has to be
	// ended here — but the selection survives, like the cursor position.
	v, tb := selectionView("hello world")
	g := gutterFor(tb)

	v.HandleMouse(press(g, 1, 1))
	v.HandleMouse(motion(g+5, 1))

	v.ExitEditingModes()

	if v.dragging {
		t.Error("expected the drag to be ended")
	}
	if !tb.hasSel {
		t.Error("expected the selection to survive losing focus")
	}
}

func TestHandleMouseWithNoFileOpenIsNotConsumed(t *testing.T) {
	v := NewView()
	if v.HandleMouse(press(5, 1, 1)) {
		t.Error("a click with no file open should not be consumed")
	}
}

func TestRightAndMiddleButtonsAreLeftUnclaimed(t *testing.T) {
	v, _ := selectionView("hello")
	for _, b := range []layout.MouseButton{layout.MouseRight, layout.MouseMiddle} {
		m := layout.Mouse{Col: 5, Row: 1, Button: b, EventType: layout.EventPress, Clicks: 1}
		if v.HandleMouse(m) {
			t.Errorf("button %v should be left unclaimed for future use", b)
		}
	}
}

func TestViewImplementsMouseHandler(t *testing.T) {
	// The optional interface App type-asserts on; a signature drift here
	// would silently disable the mouse rather than fail to compile.
	var _ layout.MouseHandler = NewView()
}

// TestSelectRealFileEndToEnd is the automated stand-in for eyeballing a drag
// in a real terminal: it opens a real .go fixture through the actual Open ->
// highlightBuffer -> Render pipeline, drags a selection across the blank line
// AND across the two halves of the multi-line raw string, and asserts that
// real tree-sitter colours survive underneath the selection, that the copied
// text matches the file byte for byte, and that "y" reaches both the
// clipboard and the register.
func TestSelectRealFileEndToEnd(t *testing.T) {
	v := NewView()
	v.Open(fixturePath(t, "highlight_sample.go"))

	w := newFakeWindow(60, 15)
	v.Render(w) // establishes lastHeight and the tree-sitter highlight cache

	tb := v.activeTab()
	g := gutterWidthFor(tb)

	var copied string
	v.CopyFunc = func(s string) { copied = s }

	// Fixture line 0 is "package sample", line 1 is blank, line 2 a comment.
	// Drag from the start of line 0 to column 2 of line 2, crossing the blank.
	v.HandleMouse(press(g, 1, 1))
	v.HandleMouse(motion(g+2, 3))
	v.HandleMouse(release(g+2, 3))

	got := v.selectionText(tb)
	want := []string{tb.buf.Lines[0], tb.buf.Lines[1], string([]rune(tb.buf.Lines[2])[:2])}
	if len(got) != len(want) {
		t.Fatalf("selected %q, want %q", got, want)
	}
	for i := range want {
		if got[i] != want[i] {
			t.Fatalf("selected %q, want %q", got, want)
		}
	}

	v.Render(w)
	// "package" is a keyword (yellow) AND inside the selection, so it must
	// carry both — the layering contract, against real highlighter output.
	found := false
	for _, s := range w.segs[1] {
		if strings.Contains(s.Text, "package") {
			found = true
			if s.Style.Foreground != layout.ColorYellow {
				t.Errorf("%q lost its keyword colour under the selection: %v", s.Text, s.Style.Foreground)
			}
			if s.Style.Background != selectionStyle.Background {
				t.Errorf("%q is inside the selection but has no selection background", s.Text)
			}
		}
	}
	if !found {
		t.Fatalf("no \"package\" segment on the rendered row: %+v", w.segs[1])
	}
	// Row 2 is the blank line, which produces no segments of its own.
	if !rowHasStyle(w, 2, func(s layout.Style) bool { return s.Background == selectionStyle.Background }) {
		t.Errorf("the selected blank line is invisible: %+v", w.segs[2])
	}

	v.HandleKey(layout.Key{Text: "y"})
	if copied != strings.Join(want, "\n") {
		t.Errorf("clipboard got %q, want %q", copied, strings.Join(want, "\n"))
	}
	if !v.register.Charwise() {
		t.Error("expected the register to be marked charwise")
	}
}

// TestSelectMultiLineStringLiteralAcrossRows drags across the two halves of
// the fixture's backtick raw string, the one place a single highlight range
// spans a line break — so the selection overlay and the highlight splitter
// have to agree about where each row's runes start.
func TestSelectMultiLineStringLiteralAcrossRows(t *testing.T) {
	v := NewView()
	v.Open(fixturePath(t, "highlight_sample.go"))
	w := newFakeWindow(60, 15)
	v.Render(w)

	tb := v.activeTab()
	g := gutterWidthFor(tb)

	// Buffer lines 5-6 hold the raw string; they render on rows 6-7.
	v.HandleMouse(press(g, 6, 1))
	v.HandleMouse(motion(g+9, 7))
	v.Render(w)

	for _, row := range []int{6, 7} {
		if !rowHasStyle(w, row, func(s layout.Style) bool {
			return s.Foreground == layout.ColorGreen && s.Background == selectionStyle.Background
		}) {
			t.Errorf("row %d: expected a green string segment carrying the selection background, got %+v", row, w.segs[row])
		}
	}
}
