package filetree

import (
	"fmt"
	"testing"

	"github.com/bricejulia/kiwi/internal/layout"
)

// manyRows returns n placeholder rows, for tests that need a tree taller
// than one screen — the on-disk fixture is too small for that.
func manyRows(n int) []Row {
	rows := make([]Row, n)
	for i := range rows {
		rows[i] = Row{Node: &Node{Name: fmt.Sprintf("file%d", i)}}
	}
	return rows
}

func newScrollTestView(n int) *View {
	v := &View{root: &Node{}, rows: manyRows(n)}
	return v
}

func TestFiletreeScrollStateMirrorsScrollTopAndRowCount(t *testing.T) {
	v := newScrollTestView(200)
	v.scrollTop = 30
	v.lastHeight = 20

	got := v.ScrollState()
	want := layout.ScrollState{Top: 30, Viewport: 20, Total: 200}
	if got != want {
		t.Errorf("got %+v, want %+v", got, want)
	}
}

func TestFiletreeScrollStateShrinksForAnOpenPrompt(t *testing.T) {
	v := newScrollTestView(200)
	v.lastHeight = 20
	v.prompt = promptRename

	if got := v.ScrollState().Viewport; got != 19 {
		t.Errorf("Viewport = %d, want 19 (one row reserved for the prompt)", got)
	}
}

func TestFiletreeScrollToMovesScrollTop(t *testing.T) {
	v := newScrollTestView(200)
	v.lastHeight = 20

	v.ScrollTo(50)
	if v.scrollTop != 50 {
		t.Errorf("scrollTop = %d, want 50", v.scrollTop)
	}
}

func TestFiletreeScrollToClampsToValidRange(t *testing.T) {
	v := newScrollTestView(200)
	v.lastHeight = 20 // max top is 200-20=180

	v.ScrollTo(-5)
	if v.scrollTop != 0 {
		t.Errorf("negative top: scrollTop = %d, want 0", v.scrollTop)
	}
	v.ScrollTo(10_000)
	if v.scrollTop != 180 {
		t.Errorf("overshooting top: scrollTop = %d, want 180", v.scrollTop)
	}
}

// TestFiletreeScrollToPullsTheCursorIntoTheNewViewport guards the same
// subtlety the editor's ScrollTo has to handle: Render re-derives scrollTop
// from the cursor row on every frame, so a scroll leaving the cursor
// outside the new viewport would otherwise be undone on the next Render.
func TestFiletreeScrollToPullsTheCursorIntoTheNewViewport(t *testing.T) {
	v := newScrollTestView(200)
	v.lastHeight = 20
	v.cursor = 5

	v.ScrollTo(100)
	if v.cursor < v.scrollTop || v.cursor >= v.scrollTop+20 {
		t.Fatalf("cursor=%d is outside the new viewport starting at scrollTop=%d", v.cursor, v.scrollTop)
	}

	w := newFakeWindow(40, 20)
	v.Render(w)
	if v.scrollTop != 100 {
		t.Errorf("scrollTop drifted to %d after Render — the scroll did not stick", v.scrollTop)
	}
}

func TestFiletreeScrollToLeavesCursorAloneWhenAlreadyVisible(t *testing.T) {
	v := newScrollTestView(200)
	v.lastHeight = 20
	v.cursor = 55

	v.ScrollTo(50) // cursor stays inside [50,70)
	if v.cursor != 55 {
		t.Errorf("cursor = %d, want left at 55", v.cursor)
	}
}

func TestFiletreeImplementsScrollInterfaces(t *testing.T) {
	var v layout.View = New(t.TempDir())
	if _, ok := v.(layout.Scrollable); !ok {
		t.Error("filetree.View should implement layout.Scrollable")
	}
	if _, ok := v.(layout.ScrollTarget); !ok {
		t.Error("filetree.View should implement layout.ScrollTarget")
	}
}
