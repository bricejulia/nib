package editor

import (
	"fmt"
	"testing"

	"github.com/bricejulia/nib/internal/layout"
	"github.com/bricejulia/nib/internal/vcs/gitstatus"
)

// manyLines returns n placeholder lines, for tests that need a buffer
// longer than one screen — the fixture files are too short for that.
func manyLines(n int) []string {
	lines := make([]string, n)
	for i := range lines {
		lines[i] = fmt.Sprintf("line %d", i)
	}
	return lines
}

func TestScrollStateNoTabsOpenReportsZero(t *testing.T) {
	v := NewView()
	if got := v.ScrollState(); got != (layout.ScrollState{}) {
		t.Errorf("got %+v, want the zero value with no tabs open", got)
	}
}

func TestScrollStateMirrorsTopLineAndLineCount(t *testing.T) {
	v := NewView()
	v.tabs = []*tab{{buf: &Buffer{Lines: manyLines(200)}, topLine: 30}}
	v.active = 0
	v.lastHeight = 21 // 1 tab-bar row + 20 body rows

	got := v.ScrollState()
	want := layout.ScrollState{Top: 30, Viewport: 20, Total: 200, RowOffset: 1}
	if got != want {
		t.Errorf("got %+v, want %+v", got, want)
	}
}

func TestScrollToMovesTopLine(t *testing.T) {
	v := NewView()
	v.tabs = []*tab{{buf: &Buffer{Lines: manyLines(200)}, cursorLn: 30}}
	v.active = 0
	v.lastHeight = 21

	v.ScrollTo(50)
	if v.tabs[0].topLine != 50 {
		t.Errorf("topLine = %d, want 50", v.tabs[0].topLine)
	}
}

func TestScrollToClampsToValidRange(t *testing.T) {
	v := NewView()
	v.tabs = []*tab{{buf: &Buffer{Lines: manyLines(200)}}}
	v.active = 0
	v.lastHeight = 21 // viewport 20, so max top is 200-20=180

	v.ScrollTo(-10)
	if v.tabs[0].topLine != 0 {
		t.Errorf("negative top: topLine = %d, want clamped to 0", v.tabs[0].topLine)
	}

	v.ScrollTo(10_000)
	if v.tabs[0].topLine != 180 {
		t.Errorf("overshooting top: topLine = %d, want clamped to 180", v.tabs[0].topLine)
	}
}

// TestScrollToPullsTheCursorIntoTheNewViewport guards the one real subtlety
// in editor.View.ScrollTo: renderBody re-derives topLine from cursorLn on
// every frame, so a scroll that leaves the cursor outside the new viewport
// would otherwise be silently undone on the very next Render.
func TestScrollToPullsTheCursorIntoTheNewViewport(t *testing.T) {
	v := NewView()
	v.tabs = []*tab{{buf: &Buffer{Lines: manyLines(200)}, cursorLn: 5}}
	v.active = 0
	v.lastHeight = 21 // viewport 20

	v.ScrollTo(100) // cursor (5) is now far above the new viewport [100,120)

	tb := v.tabs[0]
	if tb.cursorLn < tb.topLine || tb.cursorLn >= tb.topLine+20 {
		t.Fatalf("cursorLn=%d is outside the new viewport starting at topLine=%d", tb.cursorLn, tb.topLine)
	}

	w := newFakeWindow(40, 21)
	v.Render(w)
	if tb.topLine != 100 {
		t.Errorf("topLine drifted to %d after Render — the scroll did not stick", tb.topLine)
	}
}

func TestScrollToLeavesTheCursorAloneWhenAlreadyVisible(t *testing.T) {
	v := NewView()
	v.tabs = []*tab{{buf: &Buffer{Lines: manyLines(200)}, cursorLn: 55, cursorCol: 3}}
	v.active = 0
	v.lastHeight = 21 // viewport 20

	v.ScrollTo(50) // cursor (55) is still inside [50,70)

	tb := v.tabs[0]
	if tb.cursorLn != 55 || tb.cursorCol != 3 {
		t.Errorf("cursor moved to (%d,%d), want it left at (55,3)", tb.cursorLn, tb.cursorCol)
	}
}

func TestScrollToClearsSelectionOnlyWhenItForcesTheCursor(t *testing.T) {
	v := NewView()
	tb := &tab{buf: &Buffer{Lines: manyLines(200)}, cursorLn: 55, selAnchor: position{ln: 50}, hasSel: true}
	v.tabs = []*tab{tb}
	v.active = 0
	v.lastHeight = 21

	v.ScrollTo(50) // cursor stays visible: selection must survive
	if !tb.hasSel {
		t.Error("expected the selection to survive a scroll that doesn't move the cursor")
	}

	v.ScrollTo(100) // cursor is now forced to move: selection should clear
	if tb.hasSel {
		t.Error("expected the selection to clear once the scroll forces the cursor to move")
	}
}

func TestScrollToNoTabsIsANoOp(t *testing.T) {
	v := NewView()
	v.ScrollTo(10) // must not panic with no active tab
}

func TestScrollMarksMapsLineStatusToMarks(t *testing.T) {
	v := NewView()
	v.tabs = []*tab{{
		buf: &Buffer{Lines: manyLines(10)},
		lineStatus: map[int]gitstatus.LineStatus{
			2: gitstatus.LineAdded,
			5: gitstatus.LineDeletedBefore,
		},
	}}
	v.active = 0

	marks := v.ScrollMarks()
	if len(marks) != 2 {
		t.Fatalf("got %d marks, want 2", len(marks))
	}

	byLine := map[int]layout.ScrollMark{}
	for _, m := range marks {
		byLine[m.Line] = m
	}
	if byLine[5].Priority <= byLine[2].Priority {
		t.Errorf("expected a deletion (line 5, priority %d) to outrank an addition (line 2, priority %d)",
			byLine[5].Priority, byLine[2].Priority)
	}
}

func TestScrollMarksEmptyWhenNoLineStatus(t *testing.T) {
	v := NewView()
	v.tabs = []*tab{{buf: &Buffer{Lines: manyLines(10)}}}
	v.active = 0

	if got := v.ScrollMarks(); got != nil {
		t.Errorf("expected no marks for a clean tab, got %+v", got)
	}
}

func TestEditorImplementsScrollInterfaces(t *testing.T) {
	var v layout.View = NewView()
	if _, ok := v.(layout.Scrollable); !ok {
		t.Error("editor.View should implement layout.Scrollable")
	}
	if _, ok := v.(layout.ScrollTarget); !ok {
		t.Error("editor.View should implement layout.ScrollTarget")
	}
	if _, ok := v.(layout.ScrollMarker); !ok {
		t.Error("editor.View should implement layout.ScrollMarker")
	}
}
