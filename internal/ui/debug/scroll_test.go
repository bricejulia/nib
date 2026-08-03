package debug

import (
	"testing"

	"github.com/bricejulia/nib/internal/debuglog"
	"github.com/bricejulia/nib/internal/layout"
)

func fakeEntries(n int) []debuglog.Entry {
	entries := make([]debuglog.Entry, n)
	for i := range entries {
		entries[i] = debuglog.Entry{Text: "entry"}
	}
	return entries
}

func newScrollTestView(n int) *View {
	v := New()
	v.EntriesFunc = func() []debuglog.Entry { return fakeEntries(n) }
	return v
}

// TestDebugScrollStatePinnedToNewestReportsFullyScrolledDown guards the
// inverted representation this pane uses: offsetFromBottom=0 (pinned to
// the newest entry, the default) must convert to the LARGEST possible Top,
// not 0.
func TestDebugScrollStatePinnedToNewestReportsFullyScrolledDown(t *testing.T) {
	v := newScrollTestView(100)
	v.lastRows = 20

	got := v.ScrollState()
	want := layout.ScrollState{Top: 80, Viewport: 20, Total: 100} // 100-20-0
	if got != want {
		t.Errorf("got %+v, want %+v", got, want)
	}
}

func TestDebugScrollStateScrolledToOldestReportsTopZero(t *testing.T) {
	v := newScrollTestView(100)
	v.lastRows = 20
	v.offsetFromBottom = 80 // fully scrolled up (see oldest handler: len(visibleEntries()))

	if got := v.ScrollState().Top; got != 0 {
		t.Errorf("Top = %d, want 0 when scrolled all the way to the oldest entry", got)
	}
}

func TestDebugScrollToConvertsTopBackToOffsetFromBottom(t *testing.T) {
	v := newScrollTestView(100)
	v.lastRows = 20

	v.ScrollTo(0) // scroll to the very oldest entry
	if v.offsetFromBottom != 80 {
		t.Errorf("offsetFromBottom = %d, want 80 (fully scrolled up)", v.offsetFromBottom)
	}

	v.ScrollTo(80) // scroll back to pinned-to-newest
	if v.offsetFromBottom != 0 {
		t.Errorf("offsetFromBottom = %d, want 0 (pinned to newest)", v.offsetFromBottom)
	}
}

func TestDebugScrollToClampsOutOfRangeTop(t *testing.T) {
	v := newScrollTestView(100)
	v.lastRows = 20

	v.ScrollTo(-10)
	if v.offsetFromBottom != 80 {
		t.Errorf("negative top: offsetFromBottom = %d, want 80 (clamped to fully scrolled up)", v.offsetFromBottom)
	}
	v.ScrollTo(10_000)
	if v.offsetFromBottom != 0 {
		t.Errorf("overshooting top: offsetFromBottom = %d, want 0 (clamped to pinned-to-newest)", v.offsetFromBottom)
	}
}

func TestDebugScrollStateAndScrollToRoundTrip(t *testing.T) {
	v := newScrollTestView(100)
	v.lastRows = 20

	for _, top := range []int{0, 25, 50, 80} {
		v.ScrollTo(top)
		if got := v.ScrollState().Top; got != top {
			t.Errorf("ScrollTo(%d) then ScrollState().Top = %d", top, got)
		}
	}
}

func TestDebugImplementsScrollInterfaces(t *testing.T) {
	var v layout.View = New()
	if _, ok := v.(layout.Scrollable); !ok {
		t.Error("debug.View should implement layout.Scrollable")
	}
	if _, ok := v.(layout.ScrollTarget); !ok {
		t.Error("debug.View should implement layout.ScrollTarget")
	}
}
