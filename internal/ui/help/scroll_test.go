package help

import (
	"testing"

	"github.com/bricejulia/nib/internal/layout"
)

func TestHelpScrollStateMirrorsTopAndLineCount(t *testing.T) {
	v := New("dev")
	v.lastRows = 20
	v.topLine = 15

	got := v.ScrollState()
	if got.Top != 15 || got.Viewport != 20 || got.Total != len(v.lines) {
		t.Errorf("got %+v", got)
	}
}

func TestHelpScrollToClampsToValidRange(t *testing.T) {
	v := New("dev")
	v.lastRows = 5 // real content is much longer than 5 rows

	v.ScrollTo(-5)
	if v.topLine != 0 {
		t.Errorf("negative top: topLine = %d, want 0", v.topLine)
	}

	maxTop := len(v.lines) - v.lastRows
	v.ScrollTo(10_000)
	if v.topLine != maxTop {
		t.Errorf("overshooting top: topLine = %d, want %d", v.topLine, maxTop)
	}
}

func TestHelpImplementsScrollInterfaces(t *testing.T) {
	var v layout.View = New("dev")
	if _, ok := v.(layout.Scrollable); !ok {
		t.Error("help.View should implement layout.Scrollable")
	}
	if _, ok := v.(layout.ScrollTarget); !ok {
		t.Error("help.View should implement layout.ScrollTarget")
	}
}
