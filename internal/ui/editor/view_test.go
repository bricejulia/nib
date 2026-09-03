package editor

import (
	"os"
	"path/filepath"
	"strings"
	"testing"
	"time"

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

// TestViewApplyLineStatusSkippedWhileExternallyModified is a regression
// test: gitstatus.FileHunks reads the file straight off disk, so a fresh
// result is indexed against whatever's on disk RIGHT NOW — but while
// ExternallyModified is true, the buffer hasn't caught up to that yet.
// Applying the new hunks anyway would draw gutter markers next to lines
// the editor isn't actually showing; the old (still-accurate, since the
// buffer hasn't changed) lineStatus must be left alone instead.
func TestViewApplyLineStatusSkippedWhileExternallyModified(t *testing.T) {
	v := NewView()
	path := fixturePath(t, "editor_sample.txt")
	v.Open(path)

	stale := map[int]gitstatus.LineStatus{0: gitstatus.LineAdded}
	v.ApplyLineStatus(path, stale)
	v.activeTab().buf.ExternallyModified = true

	v.ApplyLineStatus(path, map[int]gitstatus.LineStatus{1: gitstatus.LineModified})

	w := newFakeWindow(40, 10)
	v.Render(w)
	if got := w.segs[1][gitMarkerSeg]; got.Text != gitstyle.LineMarker(gitstatus.LineAdded) {
		t.Errorf("row 1 marker = %+v, want the stale (pre-conflict) marker to remain, %q", got, gitstyle.LineMarker(gitstatus.LineAdded))
	}
	if got := w.segs[2][gitMarkerSeg]; got.Text != " " {
		t.Errorf("row 2 marker = %+v, want no marker — the new hunks must not have been applied", got)
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

	// Line 2 is "\ttabbed line" (see testdata/editor_sample.txt): a single
	// Right from its start must cross the whole tab (tabWidth=4) in one
	// press, landing just past it — not stop one rendered column in.
	v.HandleKey(layout.Key{Named: layout.KeyDown})
	v.HandleKey(layout.Key{Named: layout.KeyRight})
	v.Render(w)
	if got := v.StatusText(); got != "Ln 2, Col 5" {
		t.Errorf("got %q, want %q", got, "Ln 2, Col 5")
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
	lineLen := len(currentLineRunes(tb, tb.cursorLn, tabWidthOf(tb)))
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

// Regression test for the crash maxRenderLineRunes exists to prevent: a
// single line far longer than anything a terminal could show (the
// motivating case was a PHP framework cache file with one
// 12.7-million-character line) must not make rendering pay a cost
// proportional to the line's true length — that would freeze the whole
// app, starting with the very first render right after Open, before any
// keystroke. Render runs on its own goroutine with a hard deadline so a
// regression here fails the test instead of hanging the suite.
func TestRenderBodyBoundsCostForPathologicallyLongLine(t *testing.T) {
	huge := strings.Repeat("x", 5_000_000)
	v := NewView()
	v.tabs = []*tab{{buf: &Buffer{Lines: []string{huge}}}}
	v.active = 0
	// Wide enough that the whole capped line (maxRenderLineRunes plus the
	// truncation marker) fits with no horizontal scrolling needed — the
	// marker sits right at the cap, well past any width a real terminal
	// would ever use.
	w := newFakeWindow(maxRenderLineRunes+100, 10)

	done := make(chan struct{})
	go func() {
		v.Render(w)
		close(done)
	}()
	select {
	case <-done:
	case <-time.After(3 * time.Second):
		t.Fatal("Render did not return in time — cost must be bounded by maxRenderLineRunes, not by the line's true length")
	}

	if !strings.Contains(w.lines[1], "…") {
		t.Errorf("expected row 1 (the buffer's only line) to show a truncation marker, got %q", w.lines[1])
	}
}

// Regression test for the follow-up bug found after maxRenderLineRunes
// shipped: opening a pathologically long line worked, but navigating it
// with arrow keys was slow, and holding one down made nib stop responding
// entirely. Root cause: applyMovement's move_left/move_right clamp and the
// shared clamp() helper both materialized the WHOLE line on every single
// keypress just to bound the cursor. A burst of key events (simulating
// "holding a key down" — internal/ui/app.go's event loop has no
// coalescing, so each repeat is a full HandleKey+clamp) must complete in
// bounded time, and — this doubles as the regression test for an
// off-by-one caught while fixing it — moving right and then all the way
// back left must actually return the cursor to column 0.
func TestArrowKeyNavigationStaysBoundedOnPathologicallyLongLine(t *testing.T) {
	huge := strings.Repeat("x", 5_000_000)
	v := NewView()
	v.tabs = []*tab{{buf: &Buffer{Lines: []string{huge}}}}
	v.active = 0
	w := newFakeWindow(40, 10)
	v.Render(w)

	// 50, not the 500 this started with: CI runs the full suite under
	// -race -coverprofile (see .github/workflows/ci.yml), and both add
	// real per-keypress overhead on top of the bounded (but not free)
	// clamp cost each press already pays — enough that 500+500 presses
	// measured over a second under that combination even on a quiet
	// machine, leaving a shared/loaded CI runner little margin before
	// tripping the deadline below. The bounded-cost property this
	// regresses on is caught just as reliably at this scale: reverting to
	// the O(line length) bug this guards against would make even a
	// handful of presses on a 5,000,000-rune line blow well past this
	// deadline, so the press count is about keeping CI stable, not about
	// how many are needed to detect the regression. Same reasoning as
	// TestArrowKeyNavigationStaysBoundedAfterCursorOvershootsTheRenderCap's
	// own workload reduction, below.
	const presses = 50
	done := make(chan struct{})
	go func() {
		for i := 0; i < presses; i++ {
			v.HandleKey(layout.Key{Named: layout.KeyRight})
		}
		for i := 0; i < presses; i++ {
			v.HandleKey(layout.Key{Named: layout.KeyLeft})
		}
		close(done)
	}()
	select {
	case <-done:
	case <-time.After(3 * time.Second):
		t.Fatal("a burst of arrow keys did not complete in time — cost must be bounded by cursor position, not by the line's true length")
	}

	if got := v.activeTab().cursorCol; got != 0 {
		t.Fatalf("cursorCol = %d, want 0 after moving right %d times then left %d times", got, presses, presses)
	}
}

// Vertical motion lands cursorCol-clamping (via clamp, see above) on
// whichever line the cursor now sits on — this covers Up/Down specifically,
// including a target line index the cursor starts with cursorCol=0 on
// (the cheapest possible case, so a regression here means even the fast
// path stopped being fast).
func TestVerticalMotionOntoPathologicallyLongLineStaysBounded(t *testing.T) {
	huge := strings.Repeat("x", 5_000_000)
	v := NewView()
	v.tabs = []*tab{{buf: &Buffer{Lines: []string{"short", huge}}}}
	v.active = 0
	w := newFakeWindow(40, 10)
	v.Render(w)

	done := make(chan struct{})
	go func() {
		v.HandleKey(layout.Key{Named: layout.KeyDown}) // lands the cursor on the huge line
		close(done)
	}()
	select {
	case <-done:
	case <-time.After(3 * time.Second):
		t.Fatal("moving onto a pathologically long line did not complete in time")
	}
	if got := v.activeTab().cursorLn; got != 1 {
		t.Fatalf("cursorLn = %d, want 1", got)
	}
}

// Regression test for the follow-up bug found after the render cap and the
// bounded-by-cursorCol clamp/applyMovement fixes had already shipped:
// pressing "$"/End on a pathologically long line used to compute the
// line's TRUE length (an unbounded full-line scan) before landing there —
// after which EVERY subsequent arrow press stayed slow, since "bounded by
// cursorCol" isn't small once cursorCol itself is huge. "$" must land at
// maxRenderLineRunes, not the line's true length — content past the
// render cap isn't reachable by scrolling anyway (see clipLineForRender).
func TestLineEndOnPathologicallyLongLineLandsAtTheRenderCapNotTheTrueLength(t *testing.T) {
	huge := strings.Repeat("x", 5_000_000)
	v := NewView()
	v.tabs = []*tab{{buf: &Buffer{Lines: []string{huge}}}}
	v.active = 0
	w := newFakeWindow(40, 10)
	v.Render(w)

	done := make(chan struct{})
	go func() {
		v.HandleKey(layout.Key{Named: layout.KeyEnd})
		close(done)
	}()
	select {
	case <-done:
	case <-time.After(3 * time.Second):
		t.Fatal("End did not complete in time — it must land at maxRenderLineRunes without computing the line's true length")
	}

	if got := v.activeTab().cursorCol; got != maxRenderLineRunes {
		t.Fatalf("cursorCol = %d, want exactly maxRenderLineRunes (%d), not the line's true length (%d)",
			got, maxRenderLineRunes, len(huge))
	}
}

// The real regression: a burst of arrow keys starting from a cursor
// already sitting past the render cap (exactly what "$"/End used to leave
// behind) must stay fast — not just a burst starting from column 0, which
// TestArrowKeyNavigationStaysBoundedOnPathologicallyLongLine already
// covers and which this fix doesn't change the cost of.
func TestArrowKeyNavigationStaysBoundedAfterCursorOvershootsTheRenderCap(t *testing.T) {
	huge := strings.Repeat("x", 5_000_000)
	v := NewView()
	v.tabs = []*tab{{buf: &Buffer{Lines: []string{huge}}}}
	v.active = 0
	w := newFakeWindow(40, 10)
	v.Render(w)

	tb := v.activeTab()
	tb.cursorCol = 3_000_000 // simulates a cursor left stranded past the cap by an old "$"/End, a search jump, etc.

	// Deliberately far fewer presses than this file's other bounded-cost
	// tests (e.g. TestArrowKeyNavigationStaysBoundedOnPathologicallyLongLine's
	// 500, starting at cursorCol 0): every press here is bounded by
	// ~maxRenderLineRunes (20,000) — clamp's ceiling pins cursorCol there
	// for the whole Right phase, then it descends from there for the Left
	// phase — vs. that sibling's cursorCol staying under ~500 throughout.
	// Same per-press cost model, ~80x more absolute work for the same
	// press count, which is what made this specific test (and not its
	// sibling) need a disproportionately larger CI timeout before finally
	// exceeding even 10s. Fewer presses brings the total workload back
	// in line with the sibling's already CI-stable scale, rather than
	// continuing to chase the timeout upward.
	const presses = 30
	done := make(chan struct{})
	go func() {
		for i := 0; i < presses; i++ {
			v.HandleKey(layout.Key{Named: layout.KeyRight})
		}
		for i := 0; i < presses; i++ {
			v.HandleKey(layout.Key{Named: layout.KeyLeft})
		}
		close(done)
	}()
	select {
	case <-done:
	case <-time.After(8 * time.Second):
		t.Fatal("a burst of arrow keys starting past the render cap did not complete in time")
	}

	// clamp's ceiling snaps an overshot cursorCol down to maxRenderLineRunes
	// on the very first call, so stepping left/right from there behaves
	// exactly like starting from maxRenderLineRunes, not from 3,000,000.
	if got := tb.cursorCol; got != maxRenderLineRunes-presses {
		t.Fatalf("cursorCol = %d, want %d (clamped to maxRenderLineRunes, then %d left, %d right)",
			got, maxRenderLineRunes-presses, presses, presses)
	}
}

func TestClampCapsAnOvershotCursorColAtTheRenderCap(t *testing.T) {
	huge := strings.Repeat("x", 5_000_000)
	v := NewView()
	v.tabs = []*tab{{buf: &Buffer{Lines: []string{huge}}}}
	v.active = 0
	tb := v.activeTab()
	tb.cursorCol = 3_000_000

	done := make(chan struct{})
	go func() {
		v.clamp(tb)
		close(done)
	}()
	select {
	case <-done:
	case <-time.After(3 * time.Second):
		t.Fatal("clamp did not return in time for a cursorCol far past maxRenderLineRunes")
	}

	if tb.cursorCol != maxRenderLineRunes {
		t.Fatalf("cursorCol = %d, want exactly maxRenderLineRunes (%d)", tb.cursorCol, maxRenderLineRunes)
	}
}

func TestClampLeavesAShortLineExactAtItsStaleCursorCol(t *testing.T) {
	// A line shorter than maxRenderLineRunes with a cursorCol that
	// overshoots even that cap (e.g. left over from before an edit
	// shrank the line) must fall through to the EXACT clamp, not the
	// maxRenderLineRunes ceiling — the ceiling only applies to lines
	// that actually exceed it.
	v := NewView()
	v.tabs = []*tab{{buf: &Buffer{Lines: []string{"short"}}}}
	v.active = 0
	tb := v.activeTab()
	tb.cursorCol = maxRenderLineRunes + 1000

	v.clamp(tb)

	if want := len([]rune("short")); tb.cursorCol != want {
		t.Fatalf("cursorCol = %d, want %d (the short line's own true length)", tb.cursorCol, want)
	}
}

// The cursor starts at column 0 on a freshly opened file — cursorDisplayColumn
// runs on every render (see renderBody), so even that trivial case must not
// touch the whole line to answer "column 0 is display column 0".
func TestCursorDisplayColumnBoundedAtColumnZeroOnPathologicallyLongLine(t *testing.T) {
	huge := strings.Repeat("x", 5_000_000)
	tb := &tab{buf: &Buffer{Lines: []string{huge}}}

	done := make(chan int)
	go func() { done <- cursorDisplayColumn(tb, 4) }()
	select {
	case got := <-done:
		if got != 0 {
			t.Errorf("cursorDisplayColumn at cursorCol=0 = %d, want 0", got)
		}
	case <-time.After(3 * time.Second):
		t.Fatal("cursorDisplayColumn did not return in time — cost must be bounded by cursorCol, not by the line's true length")
	}
}

func TestRunePrefixReturnsWholeStringWhenUnderBudget(t *testing.T) {
	prefix, count, truncated := runePrefix("héllo", 10)
	if prefix != "héllo" || count != 5 || truncated {
		t.Fatalf("got (%q, %d, %v), want (%q, 5, false)", prefix, count, truncated, "héllo")
	}
}

func TestRunePrefixTruncatesAtRuneBoundaryNotByteBoundary(t *testing.T) {
	// "héllo": h, é (2 bytes), l, l, o — cutting at 2 runes must land right
	// after "é", not mid-byte.
	prefix, count, truncated := runePrefix("héllo", 2)
	if prefix != "hé" || count != 2 || !truncated {
		t.Fatalf("got (%q, %d, %v), want (%q, 2, true)", prefix, count, truncated, "hé")
	}
}

func TestRunePrefixZeroBudget(t *testing.T) {
	prefix, count, truncated := runePrefix("abc", 0)
	if prefix != "" || count != 0 || !truncated {
		t.Fatalf("got (%q, %d, %v), want (\"\", 0, true)", prefix, count, truncated)
	}
}

func TestClipLineForRenderLeavesShortLinesUntouched(t *testing.T) {
	if clipped, truncated := clipLineForRender("short line"); clipped != "short line" || truncated {
		t.Fatalf("got (%q, %v), want (\"short line\", false)", clipped, truncated)
	}
}

func TestClipLineForRenderCapsPathologicallyLongLines(t *testing.T) {
	huge := strings.Repeat("x", maxRenderLineRunes+1000)
	clipped, truncated := clipLineForRender(huge)
	if !truncated {
		t.Fatal("expected truncated=true for a line over maxRenderLineRunes")
	}
	if got := len([]rune(clipped)); got != maxRenderLineRunes {
		t.Fatalf("clipped line has %d runes, want exactly %d", got, maxRenderLineRunes)
	}
}

func TestClipSegmentsForRenderLeavesSegmentsUnderBudgetUntouched(t *testing.T) {
	segs := []layout.Segment{{Text: "abc"}, {Text: "def"}}
	clipped, truncated := clipSegmentsForRender(segs)
	if truncated {
		t.Fatal("expected truncated=false: total text is far under maxRenderLineRunes")
	}
	if segText(clipped) != "abcdef" {
		t.Fatalf("got %q, want %q", segText(clipped), "abcdef")
	}
}

func TestClipSegmentsForRenderSplitsTheSegmentStraddlingTheBudget(t *testing.T) {
	segs := []layout.Segment{
		{Text: strings.Repeat("a", maxRenderLineRunes-1)},
		{Text: "bcdef"}, // straddles the boundary: 1 rune fits, 4 don't
		{Text: "ghij"},  // must never be reached
	}
	clipped, truncated := clipSegmentsForRender(segs)
	if !truncated {
		t.Fatal("expected truncated=true")
	}
	if got := segText(clipped); len([]rune(got)) != maxRenderLineRunes || !strings.HasSuffix(got, "b") {
		t.Fatalf("got %q (len %d), want exactly maxRenderLineRunes runes ending in \"b\"", got, len([]rune(got)))
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
	wantEnd := len(currentLineRunes(tb, 3, tabWidthOf(tb)))
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

func TestGoToLinePushesAJump(t *testing.T) {
	v := NewView()
	v.Open(fixturePath(t, "editor_sample.txt"))
	tb := v.activeTab()
	tb.cursorLn, tb.cursorCol = 0, 2
	startLn, startCol := tb.cursorLn, tb.cursorCol

	v.goToLine(3)

	if len(v.jumpStack) != 1 {
		t.Fatalf("expected one jump pushed, got %d", len(v.jumpStack))
	}
	if v.jumpStack[0].ln != startLn || v.jumpStack[0].col != startCol {
		t.Fatalf("pushed jump = %+v, want (%d,%d)", v.jumpStack[0], startLn, startCol)
	}

	v.jumpBack()
	if tb.cursorLn != startLn || tb.cursorCol != startCol {
		t.Fatalf("cursor after jumpBack = (%d,%d), want (%d,%d)", tb.cursorLn, tb.cursorCol, startLn, startCol)
	}
}

func TestOpenAtLinePushesAJumpOnTheSourcePane(t *testing.T) {
	v := NewView()
	pathA := fixturePath(t, "editor_sample.txt")
	v.Open(pathA)
	tb := v.activeTab()
	tb.cursorLn, tb.cursorCol = 1, 0
	startLn, startCol := tb.cursorLn, tb.cursorCol

	v.OpenAtLine("/some/other/file.txt", 10)

	if len(v.jumpStack) != 1 {
		t.Fatalf("expected one jump pushed, got %d", len(v.jumpStack))
	}
	if got := v.jumpStack[0]; got.path != pathA || got.ln != startLn || got.col != startCol {
		t.Fatalf("pushed jump = %+v, want path %q at (%d,%d)", got, pathA, startLn, startCol)
	}

	v.jumpBack()
	if v.activeTab().path != pathA {
		t.Fatalf("expected jumpBack to reopen %q, got %q", pathA, v.activeTab().path)
	}
	if v.activeTab().cursorLn != startLn || v.activeTab().cursorCol != startCol {
		t.Fatalf("cursor after jumpBack = (%d,%d), want (%d,%d)", v.activeTab().cursorLn, v.activeTab().cursorCol, startLn, startCol)
	}
}

func TestOpenAtLineWithNoLineStillPushesAJump(t *testing.T) {
	v := NewView()
	pathA := fixturePath(t, "editor_sample.txt")
	v.Open(pathA)

	v.OpenAtLine("/some/other/file.txt", 0)

	if len(v.jumpStack) != 1 {
		t.Fatalf("expected a jump pushed even for a plain file switch (line=0), got %d", len(v.jumpStack))
	}
	if v.jumpStack[0].path != pathA {
		t.Fatalf("pushed jump path = %q, want %q", v.jumpStack[0].path, pathA)
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

// CloseTabByPath must close whichever tab matches path even when it isn't
// the active one, and leave the active tab exactly where it was — by
// identity, not just by index, since removing an earlier tab shifts every
// later index down by one.
func TestCloseTabByPathClosesANonActiveTabAndPreservesActiveTab(t *testing.T) {
	v := NewView()
	v.tabs = []*tab{
		{path: "a.txt", buf: &Buffer{Lines: []string{"a"}}},
		{path: "b.txt", buf: &Buffer{Lines: []string{"b"}}},
		{path: "c.txt", buf: &Buffer{Lines: []string{"c"}}},
	}
	v.active = 2 // c.txt is active

	if !v.CloseTabByPath("a.txt") {
		t.Fatal("expected CloseTabByPath to report success")
	}
	if len(v.tabs) != 2 {
		t.Fatalf("got %d tabs, want 2", len(v.tabs))
	}
	if got := v.activeTab().path; got != "c.txt" {
		t.Fatalf("active tab = %q, want c.txt to still be active (its index should have shifted down)", got)
	}
	if v.active != 1 {
		t.Fatalf("active index = %d, want 1 (c.txt shifted down after a.txt was removed)", v.active)
	}
}

// Closing the active tab itself falls back to CloseTab's own rule.
func TestCloseTabByPathOnTheActiveTabActivatesItsNeighbor(t *testing.T) {
	v := NewView()
	v.tabs = []*tab{
		{path: "a.txt", buf: &Buffer{Lines: []string{"a"}}},
		{path: "b.txt", buf: &Buffer{Lines: []string{"b"}}},
	}
	v.active = 0

	if !v.CloseTabByPath("a.txt") {
		t.Fatal("expected CloseTabByPath to report success")
	}
	if got := v.activeTab().path; got != "b.txt" {
		t.Fatalf("active tab = %q, want b.txt", got)
	}
}

// A dirty tab must never be silently discarded, regardless of which tab
// asked to close it — the same rule closeActiveTab's ":q" already applies.
func TestCloseTabByPathRefusesADirtyTab(t *testing.T) {
	v := NewView()
	v.tabs = []*tab{{path: "a.txt", buf: &Buffer{Lines: []string{"a"}, Dirty: true}}}
	v.active = 0

	if v.CloseTabByPath("a.txt") {
		t.Fatal("expected CloseTabByPath to refuse a dirty tab")
	}
	if len(v.tabs) != 1 {
		t.Fatalf("got %d tabs, want the dirty one left open", len(v.tabs))
	}
}

func TestCloseTabByPathReturnsFalseForAPathNotOpen(t *testing.T) {
	v := NewView()
	v.tabs = []*tab{{path: "a.txt", buf: &Buffer{Lines: []string{"a"}}}}
	v.active = 0

	if v.CloseTabByPath("nope.txt") {
		t.Fatal("expected CloseTabByPath to report failure for a path that isn't open")
	}
	if len(v.tabs) != 1 {
		t.Fatalf("got %d tabs, want the only tab left untouched", len(v.tabs))
	}
}

func TestLargestBufferReturnsTheBiggestOpenTab(t *testing.T) {
	v := NewView()
	v.tabs = []*tab{
		{path: "small.txt", buf: &Buffer{Source: []byte("hi")}},
		{path: "big.txt", buf: &Buffer{Source: []byte(strings.Repeat("x", 1000))}},
		{path: "medium.txt", buf: &Buffer{Source: []byte(strings.Repeat("y", 100))}},
	}

	path, size := v.LargestBuffer()
	if path != "big.txt" || size != 1000 {
		t.Fatalf("LargestBuffer() = (%q, %d), want (big.txt, 1000)", path, size)
	}
}

func TestLargestBufferEmptyWhenNoTabsOpen(t *testing.T) {
	v := NewView()
	if path, size := v.LargestBuffer(); path != "" || size != 0 {
		t.Fatalf("LargestBuffer() = (%q, %d), want (\"\", 0)", path, size)
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

// StatusText/tabDisplayNames turn Buffer.HasLongLine into a notice, the
// same seams already used for tab.detached's "-- DELETED --"/"✗" — see
// TestViewCloseTabsUnderKeepsADirtyTabDetached in repath_test.go for that
// precedent.
func TestStatusTextAndTabBarShowLongLineIndicator(t *testing.T) {
	v := NewView()
	v.tabs = []*tab{{path: "huge.txt", buf: &Buffer{Lines: []string{"x"}, HasLongLine: true}}}
	v.active = 0

	if got := v.StatusText(); !strings.Contains(got, "-- LONG LINE --") {
		t.Errorf("StatusText() = %q, want a LONG LINE indicator", got)
	}
	if got := tabDisplayNames(v.tabs)[0]; !strings.Contains(got, "⚠") {
		t.Errorf("tab label = %q, want a long-line marker", got)
	}
}

func TestStatusTextAndTabBarOmitLongLineIndicatorForOrdinaryFiles(t *testing.T) {
	v := NewView()
	v.tabs = []*tab{{path: "normal.txt", buf: &Buffer{Lines: []string{"x"}}}}
	v.active = 0

	if got := v.StatusText(); strings.Contains(got, "LONG LINE") {
		t.Errorf("StatusText() = %q, did not expect a LONG LINE indicator", got)
	}
	if got := tabDisplayNames(v.tabs)[0]; strings.Contains(got, "⚠") {
		t.Errorf("tab label = %q, did not expect a long-line marker", got)
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

func TestSaveActiveDetectsConflictAndDoesNotWrite(t *testing.T) {
	dir := t.TempDir()
	path := filepath.Join(dir, "conflict.txt")
	if err := os.WriteFile(path, []byte("before"), 0o644); err != nil {
		t.Fatal(err)
	}

	v := NewView()
	v.Open(path)
	v.HandleKey(layout.Key{Text: "i"})
	v.activeTab().cursorCol = len(v.activeTab().buf.Lines[0])
	v.HandleKey(layout.Key{Text: "!"})
	v.HandleKey(layout.Key{Named: layout.KeyEsc})

	// Simulate an external editor rewriting the file after nib loaded it.
	if err := os.WriteFile(path, []byte("changed elsewhere"), 0o644); err != nil {
		t.Fatal(err)
	}
	future := time.Now().Add(time.Hour)
	if err := os.Chtimes(path, future, future); err != nil {
		t.Fatal(err)
	}

	var got SaveConflict
	calls := 0
	v.OnSaveConflict = func(c SaveConflict, onResolved func()) {
		calls++
		got = c
	}

	v.HandleKey(layout.Key{Text: "s", Mods: layout.ModCtrl})

	if calls != 1 {
		t.Fatalf("OnSaveConflict called %d times, want 1", calls)
	}
	if got.Path != path || got.Buf != v.activeTab().buf {
		t.Fatalf("SaveConflict = %+v, want Path=%q Buf=%p", got, path, v.activeTab().buf)
	}
	if !v.activeTab().buf.Dirty {
		t.Fatal("expected Dirty to remain true — the conflicting save must not have written")
	}

	onDisk, err := os.ReadFile(path)
	if err != nil {
		t.Fatal(err)
	}
	if string(onDisk) != "changed elsewhere" {
		t.Fatalf("file contents = %q, want the external write left untouched", onDisk)
	}
}

// TestColonWqRetriesCloseAfterConflictResolved is a regression test: a
// naive v.saveActive(); v.closeActiveTab(false) pair sees Dirty still
// true at the moment of the conflict (the real save hasn't happened yet
// — resolving it is asynchronous, driven by whatever OnSaveConflict does)
// and correctly refuses to close then, but a ":wq" that never retries the
// close once the conflict is actually resolved silently stops being a
// quit at all. saveActiveThen (via commitCommand's "wq"/"x") must retry.
func TestColonWqRetriesCloseAfterConflictResolved(t *testing.T) {
	dir := t.TempDir()
	path := filepath.Join(dir, "conflict.txt")
	if err := os.WriteFile(path, []byte("before"), 0o644); err != nil {
		t.Fatal(err)
	}

	v := NewView()
	v.Open(path)
	v.HandleKey(layout.Key{Text: "i"})
	v.activeTab().cursorCol = len(v.activeTab().buf.Lines[0])
	v.HandleKey(layout.Key{Text: "!"})
	v.HandleKey(layout.Key{Named: layout.KeyEsc})

	if err := os.WriteFile(path, []byte("changed elsewhere"), 0o644); err != nil {
		t.Fatal(err)
	}
	future := time.Now().Add(time.Hour)
	if err := os.Chtimes(path, future, future); err != nil {
		t.Fatal(err)
	}

	// Stand in for cmd/nib/main.go's reloadconfirm wiring: capture
	// onResolved instead of calling it immediately, to simulate the
	// prompt staying up until the user answers it.
	var onResolved func()
	v.OnSaveConflict = func(c SaveConflict, resolved func()) {
		if err := c.Buf.Save(); err != nil {
			t.Fatalf("Save: %v", err)
		}
		onResolved = resolved
	}

	v.HandleKey(layout.Key{Text: ":"})
	v.HandleKey(layout.Key{Text: "w"})
	v.HandleKey(layout.Key{Text: "q"})
	v.HandleKey(layout.Key{Named: layout.KeyEnter})

	if len(v.tabs) != 1 {
		t.Fatalf("expected the tab to stay open while the conflict is unresolved, got %d tabs", len(v.tabs))
	}
	if onResolved == nil {
		t.Fatal("expected OnSaveConflict to have been called")
	}

	// The user picks "Keep mine" in the (simulated) prompt.
	onResolved()

	if len(v.tabs) != 0 {
		t.Fatalf(":wq should have closed the tab once the conflict resolved, got %d remaining", len(v.tabs))
	}
}

// TestColonWqDoesNotCloseIfConflictCancelled complements the above: if
// the user cancels the prompt instead, onResolved is never called (see
// OnSaveConflict's contract), and the tab must stay open indefinitely,
// not close on its own.
func TestColonWqDoesNotCloseIfConflictCancelled(t *testing.T) {
	dir := t.TempDir()
	path := filepath.Join(dir, "conflict.txt")
	if err := os.WriteFile(path, []byte("before"), 0o644); err != nil {
		t.Fatal(err)
	}

	v := NewView()
	v.Open(path)
	v.HandleKey(layout.Key{Text: "i"})
	v.activeTab().cursorCol = len(v.activeTab().buf.Lines[0])
	v.HandleKey(layout.Key{Text: "!"})
	v.HandleKey(layout.Key{Named: layout.KeyEsc})

	if err := os.WriteFile(path, []byte("changed elsewhere"), 0o644); err != nil {
		t.Fatal(err)
	}
	future := time.Now().Add(time.Hour)
	if err := os.Chtimes(path, future, future); err != nil {
		t.Fatal(err)
	}

	v.OnSaveConflict = func(c SaveConflict, resolved func()) {
		// Cancel: never call resolved.
	}

	v.HandleKey(layout.Key{Text: ":"})
	v.HandleKey(layout.Key{Text: "w"})
	v.HandleKey(layout.Key{Text: "q"})
	v.HandleKey(layout.Key{Named: layout.KeyEnter})

	if len(v.tabs) != 1 {
		t.Fatalf("expected the tab to stay open, got %d tabs", len(v.tabs))
	}
	if !v.activeTab().buf.Dirty {
		t.Fatal("expected Dirty to remain true — nothing was ever saved")
	}
}

func TestSaveDirtyTabsCollectsConflictSeparatelyFromFailure(t *testing.T) {
	dir := t.TempDir()
	pathOK := filepath.Join(dir, "ok.txt")
	pathConflict := filepath.Join(dir, "conflict.txt")
	if err := os.WriteFile(pathOK, []byte("original ok"), 0o644); err != nil {
		t.Fatal(err)
	}
	if err := os.WriteFile(pathConflict, []byte("original conflict"), 0o644); err != nil {
		t.Fatal(err)
	}

	v := NewView()
	v.Open(pathOK)
	v.Open(pathConflict)
	v.tabs[0].buf.Lines[0] = "edited ok"
	v.tabs[0].buf.resync()
	v.tabs[1].buf.Lines[0] = "edited conflict"
	v.tabs[1].buf.resync()

	if err := os.WriteFile(pathConflict, []byte("changed elsewhere"), 0o644); err != nil {
		t.Fatal(err)
	}
	future := time.Now().Add(time.Hour)
	if err := os.Chtimes(pathConflict, future, future); err != nil {
		t.Fatal(err)
	}

	res := v.SaveDirtyTabs()
	if len(res.Failed) != 0 {
		t.Fatalf("Failed = %v, want none", res.Failed)
	}
	if len(res.Conflicts) != 1 || res.Conflicts[0].Path != pathConflict {
		t.Fatalf("Conflicts = %+v, want exactly one for %q", res.Conflicts, pathConflict)
	}
	if v.tabs[0].buf.Dirty {
		t.Fatal("expected the non-conflicting tab to have saved and cleared Dirty")
	}
	if !v.tabs[1].buf.Dirty {
		t.Fatal("expected the conflicting tab to remain Dirty — nothing was written for it")
	}
}

func TestRefreshBufferClampsCursorAfterShorterReload(t *testing.T) {
	dir := t.TempDir()
	path := filepath.Join(dir, "shrink.txt")
	if err := os.WriteFile(path, []byte("one\ntwo\nthree\nfour"), 0o644); err != nil {
		t.Fatal(err)
	}

	v := NewView()
	v.Open(path)
	v.activeTab().cursorLn = 3 // "four", the last line

	if err := os.WriteFile(path, []byte("one"), 0o644); err != nil {
		t.Fatal(err)
	}
	buf := v.activeTab().buf
	if err := buf.Reload(); err != nil {
		t.Fatalf("Reload: %v", err)
	}
	v.RefreshBuffer(buf)

	if v.activeTab().cursorLn >= len(buf.Lines) {
		t.Fatalf("cursorLn = %d, want clamped within %d line(s)", v.activeTab().cursorLn, len(buf.Lines))
	}
}

func TestExternallyModifiedMarkerAppearsInTabBar(t *testing.T) {
	v := NewView()
	v.tabs = []*tab{{path: "f.txt", buf: &Buffer{Lines: []string{"x"}, ExternallyModified: true}}}
	v.active = 0

	if got := tabDisplayNames(v.tabs)[0]; !strings.Contains(got, "⟳") {
		t.Errorf("tab label = %q, want an externally-modified marker", got)
	}
	if got := v.StatusText(); !strings.Contains(got, "-- MODIFIED ON DISK --") {
		t.Errorf("StatusText() = %q, want a MODIFIED ON DISK indicator", got)
	}
}

func TestExternallyModifiedMarkerOmittedWhenDetached(t *testing.T) {
	// A deleted file is reported as that, specifically — not also as
	// "merely modified" (see CloseTabsUnder, which never clears
	// ExternallyModified when it sets detached).
	v := NewView()
	v.tabs = []*tab{{path: "f.txt", detached: true, buf: &Buffer{Lines: []string{"x"}, ExternallyModified: true}}}
	v.active = 0

	if got := tabDisplayNames(v.tabs)[0]; strings.Contains(got, "⟳") {
		t.Errorf("tab label = %q, did not expect an externally-modified marker on a detached tab", got)
	}
}

func TestJustReloadedShowsStatusLineNoticeUntilNextKey(t *testing.T) {
	v := NewView()
	v.tabs = []*tab{{path: "f.txt", buf: &Buffer{Lines: []string{"x"}}, justReloaded: true}}
	v.active = 0

	if got := v.StatusText(); !strings.Contains(got, "-- RELOADED --") {
		t.Errorf("StatusText() = %q, want a RELOADED notice", got)
	}

	// A tooltip, not a mode: the very next key dismisses it (see HandleKey
	// clearing every tab's justReloaded alongside hoverText/gitPopup/etc).
	v.HandleKey(layout.Key{Named: layout.KeyRight})

	if got := v.StatusText(); strings.Contains(got, "RELOADED") {
		t.Errorf("StatusText() = %q, expected the notice to be dismissed by the next key", got)
	}
}

func TestMarkJustReloadedOnlyFlagsTabsShowingThatBuffer(t *testing.T) {
	v := NewView()
	buf := &Buffer{Lines: []string{"x"}}
	other := &Buffer{Lines: []string{"y"}}
	v.tabs = []*tab{
		{path: "a.txt", buf: buf},
		{path: "b.txt", buf: other},
	}
	v.active = 0

	v.MarkJustReloaded(buf)

	if !v.tabs[0].justReloaded {
		t.Error("expected the tab showing the reloaded buffer to be flagged")
	}
	if v.tabs[1].justReloaded {
		t.Error("did not expect an unrelated tab's buffer to be flagged")
	}
}

func TestBufferForPath(t *testing.T) {
	v := NewView()
	v.Open(fixturePath(t, "editor_sample.txt"))

	if got := v.BufferForPath(fixturePath(t, "editor_sample.txt")); got == nil || got != v.activeTab().buf {
		t.Errorf("BufferForPath = %p, want the open tab's buffer %p", got, v.activeTab().buf)
	}
	if got := v.BufferForPath("/no/such/file.txt"); got != nil {
		t.Errorf("BufferForPath for an unopened path = %v, want nil", got)
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

	// Squarely at the tab's own start (expanded col 0, i.e. the cursor
	// hasn't crossed any of the tab's rendered columns yet), the raw index
	// must be the tab itself (0), not snapped past it — this is what lets
	// deleteCharForward ('x') delete the tab rather than the rune after it.
	if got := rawIndexForExpandedCol(line, 0, tabWidth); got != 0 {
		t.Fatalf("column at the tab's own start should map to the tab's raw index, got %d, want 0", got)
	}
}

func TestRawIndexForExpandedColUsesRuneIndexNotByteOffsetBeforeATab(t *testing.T) {
	// "é" is a 2-byte UTF-8 rune (rune index 0, byte offset 0-1), so the
	// tab right after it sits at rune index 1 but byte offset 2. Landing
	// squarely at the tab's own expanded column must return its RUNE index
	// (1) — regression test for a bug where the "col == expanded" branch
	// returned the range loop's byte offset instead, overshooting by
	// however many extra bytes any earlier multi-byte rune added.
	const line = "é\tfoo"
	const tabWidth = 4

	if got := rawIndexForExpandedCol(line, 1, tabWidth); got != 1 {
		t.Fatalf("raw index at the tab's own start = %d, want 1 (its rune index, not a byte offset)", got)
	}
}

func TestArrowRightCrossesATabAfterANonASCIICharacter(t *testing.T) {
	// Regression test: a multi-byte rune ("é") before the tab used to make
	// rawIndexForExpandedCol return a byte offset instead of a rune index
	// once the cursor reached the tab's own column, overshooting into (or
	// past) the text after the tab on the very next press.
	v := NewView()
	v.tabs = []*tab{{buf: &Buffer{Lines: []string{"é\tabc"}, IndentWidth: 4}}}
	v.active = 0
	v.activeTab().cursorCol = 0 // sitting on 'é'

	v.HandleKey(layout.Key{Named: layout.KeyRight}) // cross 'é'
	if got := v.activeTab().cursorCol; got != 1 {
		t.Fatalf("cursorCol = %d, want 1 (right after 'é', squarely at the tab's own start)", got)
	}

	v.HandleKey(layout.Key{Named: layout.KeyRight}) // cross the whole tab
	if got := v.activeTab().cursorCol; got != 4 {
		t.Fatalf("cursorCol = %d, want 4 (past the whole 4-wide tab in one press, not overshot into 'abc')", got)
	}

	v.HandleKey(layout.Key{Named: layout.KeyLeft}) // back across the tab
	if got := v.activeTab().cursorCol; got != 1 {
		t.Fatalf("cursorCol = %d, want 1 (back at the tab's own start)", got)
	}
}

func TestArrowRightCrossesALeadingTabInOnePress(t *testing.T) {
	v := NewView()
	v.tabs = []*tab{{buf: &Buffer{Lines: []string{"\tabc"}, IndentWidth: 4}}}
	v.active = 0
	v.activeTab().cursorCol = 0 // sitting right at the start of the tab

	v.HandleKey(layout.Key{Named: layout.KeyRight})
	if got := v.activeTab().cursorCol; got != 4 {
		t.Fatalf("cursorCol = %d, want 4 (past the whole 4-wide tab in one press)", got)
	}

	v.HandleKey(layout.Key{Named: layout.KeyLeft})
	if got := v.activeTab().cursorCol; got != 0 {
		t.Fatalf("cursorCol = %d, want 0 (back across the whole tab in one press)", got)
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

	if !v.HandleKey(layout.Key{Text: "U"}) {
		t.Fatal("expected 'U' to be consumed")
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
	if !v.HandleKey(layout.Key{Text: "U"}) {
		t.Fatal("expected 'U' to still be consumed with an empty redo stack")
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

	v.HandleKey(layout.Key{Text: "U"}) // buffer: "original!" again
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

func TestXAtStartOfLeadingTabDeletesTheTabNotTheCharAfterIt(t *testing.T) {
	v := NewView()
	v.tabs = []*tab{{buf: &Buffer{Lines: []string{"\tabc"}, IndentWidth: 4}}}
	v.active = 0
	v.activeTab().cursorCol = 0 // sitting right at the start of the tab

	if !v.HandleKey(layout.Key{Text: "x"}) {
		t.Fatal("'x' should be consumed")
	}
	if got := v.activeTab().buf.Lines[0]; got != "abc" {
		t.Fatalf("Lines[0] = %q, want %q (the tab deleted, not 'a' after it)", got, "abc")
	}
	if v.activeTab().cursorCol != 0 {
		t.Fatalf("cursorCol = %d, want 0", v.activeTab().cursorCol)
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

func TestReplaceCharOverwritesRuneUnderCursorAndStaysInPlace(t *testing.T) {
	v := NewView()
	v.tabs = []*tab{{buf: &Buffer{Lines: []string{"abc"}}}}
	v.active = 0
	v.activeTab().cursorCol = 1 // sitting on 'b'

	if !v.HandleKey(layout.Key{Text: "r"}) {
		t.Fatal("'r' should be consumed")
	}
	if !v.awaitingReplaceChar {
		t.Fatal("expected 'r' to arm awaitingReplaceChar")
	}
	if !v.HandleKey(layout.Key{Text: "X"}) {
		t.Fatal("the replacement character should be consumed")
	}

	if got := v.activeTab().buf.Lines[0]; got != "aXc" {
		t.Fatalf("Lines[0] = %q, want %q", got, "aXc")
	}
	if v.activeTab().cursorCol != 1 {
		t.Fatalf("cursorCol = %d, want 1 (stays in place, unlike Insert mode)", v.activeTab().cursorCol)
	}
	if v.mode != modeNormal {
		t.Fatal("'r<char>' must not enter Insert mode")
	}
	if v.awaitingReplaceChar {
		t.Fatal("expected awaitingReplaceChar cleared after the replacement lands")
	}
}

func TestReplaceCharEscCancelsWithNoChange(t *testing.T) {
	v := NewView()
	v.tabs = []*tab{{buf: &Buffer{Lines: []string{"abc"}}}}
	v.active = 0
	v.activeTab().cursorCol = 1

	v.HandleKey(layout.Key{Text: "r"})
	v.HandleKey(layout.Key{Named: layout.KeyEsc})

	if got := v.activeTab().buf.Lines[0]; got != "abc" {
		t.Fatalf("Lines[0] = %q, want unchanged %q", got, "abc")
	}
	if v.awaitingReplaceChar {
		t.Fatal("expected awaitingReplaceChar cleared after Esc")
	}
}

func TestReplaceCharWithNamedKeyCancelsWithoutApplying(t *testing.T) {
	v := NewView()
	v.tabs = []*tab{{buf: &Buffer{Lines: []string{"abc"}}}}
	v.active = 0
	v.activeTab().cursorCol = 1

	v.HandleKey(layout.Key{Text: "r"})
	v.HandleKey(layout.Key{Named: layout.KeyDown})

	if got := v.activeTab().buf.Lines[0]; got != "abc" {
		t.Fatalf("Lines[0] = %q, want unchanged %q", got, "abc")
	}
	if v.activeTab().cursorCol != 1 {
		t.Fatalf("cursorCol = %d, want unchanged 1 (the arrow key should not move the cursor either)", v.activeTab().cursorCol)
	}
	if v.awaitingReplaceChar {
		t.Fatal("expected awaitingReplaceChar cleared, not left armed")
	}
}

func TestReplaceCharIsUndoable(t *testing.T) {
	v := NewView()
	v.tabs = []*tab{{buf: &Buffer{Lines: []string{"abc"}}}}
	v.active = 0
	v.activeTab().cursorCol = 1

	v.HandleKey(layout.Key{Text: "r"})
	v.HandleKey(layout.Key{Text: "X"})
	if got := v.activeTab().buf.Lines[0]; got != "aXc" {
		t.Fatalf("setup: Lines[0] = %q, want %q", got, "aXc")
	}

	v.HandleKey(layout.Key{Text: "u"})
	if got := v.activeTab().buf.Lines[0]; got != "abc" {
		t.Fatalf("Lines[0] after undo = %q, want %q", got, "abc")
	}
}

func TestReplaceCharAtEndOfLineIsNoop(t *testing.T) {
	v := NewView()
	v.tabs = []*tab{{buf: &Buffer{Lines: []string{""}}}}
	v.active = 0

	v.HandleKey(layout.Key{Text: "r"})
	v.HandleKey(layout.Key{Text: "X"})

	if got := v.activeTab().buf.Lines[0]; got != "" {
		t.Fatalf("Lines[0] = %q, want unchanged empty", got)
	}
	if len(v.activeTab().buf.undoStack) != 0 {
		t.Fatalf("expected no undo entry for a no-op 'r', got %d", len(v.activeTab().buf.undoStack))
	}
}

func TestExitEditingModesClearsAwaitingReplaceChar(t *testing.T) {
	v := NewView()
	v.tabs = []*tab{{buf: &Buffer{Lines: []string{"abc"}}}}
	v.active = 0
	v.activeTab().cursorCol = 1

	v.HandleKey(layout.Key{Text: "r"})
	if !v.awaitingReplaceChar {
		t.Fatal("setup: expected 'r' to arm awaitingReplaceChar")
	}

	v.ExitEditingModes()
	if v.awaitingReplaceChar {
		t.Fatal("expected ExitEditingModes to clear awaitingReplaceChar")
	}

	// The next keypress must behave as a fresh Normal-mode key, not as a
	// replacement character for the now-discarded "r".
	v.HandleKey(layout.Key{Text: "x"})
	if got := v.activeTab().buf.Lines[0]; got != "ac" {
		t.Fatalf("Lines[0] = %q, want %q ('x' should run its own action, not be swallowed as a replacement char)", got, "ac")
	}
}

func TestBareRDoesNotTriggerRedo(t *testing.T) {
	v := NewView()
	v.tabs = []*tab{{buf: &Buffer{Lines: []string{"abc"}}}}
	v.active = 0

	v.HandleKey(layout.Key{Text: "i"})
	v.HandleKey(layout.Key{Text: "d"})
	v.HandleKey(layout.Key{Named: layout.KeyEsc})
	v.HandleKey(layout.Key{Text: "u"}) // undo "d", populating the redo stack

	v.HandleKey(layout.Key{Text: "r"}) // arms replace_char; must NOT redo
	if got := v.activeTab().buf.Lines[0]; got != "abc" {
		t.Fatalf("Lines[0] = %q, want %q (bare 'r' alone must not redo)", got, "abc")
	}
	if !v.awaitingReplaceChar {
		t.Fatal("expected bare 'r' to arm awaitingReplaceChar")
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

	res := v.SaveDirtyTabs()
	if len(res.Failed) != 0 {
		t.Fatalf("SaveDirtyTabs() Failed = %v, want none", res.Failed)
	}
	if len(res.Conflicts) != 0 {
		t.Fatalf("SaveDirtyTabs() Conflicts = %v, want none", res.Conflicts)
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

	res := v.SaveDirtyTabs()
	if len(res.Failed) != 1 {
		t.Fatalf("SaveDirtyTabs() Failed = %v, want exactly one failure", res.Failed)
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
