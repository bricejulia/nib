package finder

import (
	"fmt"
	"testing"

	"github.com/bricejulia/kiwi/internal/layout"
)

func manyItems(n int) []string {
	items := make([]string, n)
	for i := range items {
		items[i] = fmt.Sprintf("file%d.go", i)
	}
	return items
}

func TestFinderScrollStateMirrorsScrollTopAndResultCount(t *testing.T) {
	v := newTestView(manyItems(200)...)
	v.scrollTop = 30
	v.lastListRows = 20

	got := v.ScrollState()
	want := layout.ScrollState{Top: 30, Viewport: 20, Total: 200}
	if got != want {
		t.Errorf("got %+v, want %+v", got, want)
	}
}

func TestFinderScrollToClampsToValidRange(t *testing.T) {
	v := newTestView(manyItems(200)...)
	v.lastListRows = 20 // max top is 200-20=180

	v.ScrollTo(-5)
	if v.scrollTop != 0 {
		t.Errorf("negative top: scrollTop = %d, want 0", v.scrollTop)
	}
	v.ScrollTo(10_000)
	if v.scrollTop != 180 {
		t.Errorf("overshooting top: scrollTop = %d, want 180", v.scrollTop)
	}
}

// TestFinderScrollToPullsTheCursorIntoTheNewViewport guards the same
// subtlety the editor and file tree's ScrollTo have to handle: Render
// re-derives scrollTop from the cursor row on every frame, so a scroll
// leaving the cursor outside the new viewport would otherwise be undone on
// the very next Render.
func TestFinderScrollToPullsTheCursorIntoTheNewViewport(t *testing.T) {
	v := newTestView(manyItems(200)...)
	v.lastListRows = 20
	v.cursor = 5

	v.ScrollTo(100)
	if v.cursor < v.scrollTop || v.cursor >= v.scrollTop+20 {
		t.Fatalf("cursor=%d is outside the new viewport starting at scrollTop=%d", v.cursor, v.scrollTop)
	}

	w := newFakeWindow(40, 21)
	v.Render(w)
	if v.scrollTop != 100 {
		t.Errorf("scrollTop drifted to %d after Render — the scroll did not stick", v.scrollTop)
	}
}

func TestFinderScrollToLeavesCursorAloneWhenAlreadyVisible(t *testing.T) {
	v := newTestView(manyItems(200)...)
	v.lastListRows = 20
	v.cursor = 55

	v.ScrollTo(50) // cursor stays inside [50,70)
	if v.cursor != 55 {
		t.Errorf("cursor = %d, want left at 55", v.cursor)
	}
}

func TestFinderImplementsScrollInterfaces(t *testing.T) {
	var v layout.View = New("/project")
	if _, ok := v.(layout.Scrollable); !ok {
		t.Error("finder.View should implement layout.Scrollable")
	}
	if _, ok := v.(layout.ScrollTarget); !ok {
		t.Error("finder.View should implement layout.ScrollTarget")
	}
}
