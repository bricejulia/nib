package diffview

import (
	"testing"

	"github.com/bricejulia/nib/internal/layout"
)

func TestDiffviewScrollStateMirrorsTopAndLineCount(t *testing.T) {
	v := New()
	lines := make([]string, 100)
	v.Show("f.go", lines)
	v.lastRows = 20
	v.top = 30

	got := v.ScrollState()
	want := layout.ScrollState{Top: 30, Viewport: 20, Total: 100}
	if got != want {
		t.Errorf("got %+v, want %+v", got, want)
	}
}

func TestDiffviewScrollToClampsToValidRange(t *testing.T) {
	v := New()
	v.Show("f.go", make([]string, 100))
	v.lastRows = 20 // max top is 100-20=80

	v.ScrollTo(-5)
	if v.top != 0 {
		t.Errorf("negative top: top = %d, want 0", v.top)
	}
	v.ScrollTo(10_000)
	if v.top != 80 {
		t.Errorf("overshooting top: top = %d, want 80", v.top)
	}
	v.ScrollTo(40)
	if v.top != 40 {
		t.Errorf("top = %d, want 40", v.top)
	}
}

func TestDiffviewImplementsScrollInterfaces(t *testing.T) {
	var v layout.View = New()
	if _, ok := v.(layout.Scrollable); !ok {
		t.Error("diffview.View should implement layout.Scrollable")
	}
	if _, ok := v.(layout.ScrollTarget); !ok {
		t.Error("diffview.View should implement layout.ScrollTarget")
	}
}
