package memprompt

import (
	"strings"
	"testing"

	"github.com/bricejulia/nib/internal/layout"
)

func TestHandleKeyConfirmClose(t *testing.T) {
	v := New()
	v.Show("big.txt", 500<<20, 600<<20)

	var called bool
	v.OnConfirmClose = func() { called = true }

	if !v.HandleKey(layout.Key{Text: "c"}) {
		t.Fatal("expected the key to be consumed")
	}
	if !called {
		t.Fatal("expected OnConfirmClose to be called for \"c\"")
	}
}

func TestHandleKeyCancel(t *testing.T) {
	v := New()
	v.Show("big.txt", 500<<20, 600<<20)

	var called bool
	v.OnCancel = func() { called = true }

	v.HandleKey(layout.Key{Named: layout.KeyEsc})
	if !called {
		t.Fatal("expected OnCancel to be called for Esc")
	}
}

func TestHandleKeyIgnoresUnboundKeysButStillConsumesThem(t *testing.T) {
	v := New()
	v.Show("big.txt", 500<<20, 600<<20)
	v.OnConfirmClose = func() { t.Fatal("must not fire for an unrelated key") }
	v.OnCancel = func() { t.Fatal("must not fire for an unrelated key") }

	if !v.HandleKey(layout.Key{Text: "x"}) {
		t.Fatal("expected the key to be consumed even though it triggers nothing")
	}
}

func TestHandleKeyIgnoresReleaseEvents(t *testing.T) {
	v := New()
	v.Show("big.txt", 500<<20, 600<<20)
	v.OnConfirmClose = func() { t.Fatal("must not fire on a release event") }

	if !v.HandleKey(layout.Key{Text: "c", EventType: layout.EventRelease}) {
		t.Fatal("expected a release event to be reported as consumed")
	}
}

func TestRenderDoesNotPanic(t *testing.T) {
	v := New()
	v.Show("big.txt", 500<<20, 600<<20)
	v.Render(fakeWindow{})
}

func TestRenderNamesTheFileAndBothSizes(t *testing.T) {
	v := New()
	v.Show("path/to/big.txt", 500<<20, 600<<20)

	w := &recordingWindow{}
	v.Render(w)

	if !w.contains("path/to/big.txt") {
		t.Errorf("expected the target path in the render, got %v", w.lines)
	}
	if !w.contains("600") {
		t.Errorf("expected the heap size (MiB) in the render, got %v", w.lines)
	}
	if !w.contains("500") {
		t.Errorf("expected the target file's own size (MiB) in the render, got %v", w.lines)
	}
}

func TestTitle(t *testing.T) {
	v := New()
	if got := v.Title(); got != "High memory usage" {
		t.Errorf("Title() = %q, want %q", got, "High memory usage")
	}
}

// fakeWindow is a minimal layout.Window that discards everything, enough
// to exercise Render without needing a real terminal.
type fakeWindow struct{}

func (fakeWindow) Size() (int, int)                        { return 80, 24 }
func (fakeWindow) Println(row int, segs ...layout.Segment) {}
func (fakeWindow) Clear()                                  {}

// recordingWindow captures each Println call's text (segments joined, no
// styling) by row, so a test can assert on what Render actually wrote.
type recordingWindow struct {
	lines []string
}

func (w *recordingWindow) Size() (int, int) { return 80, 24 }

func (w *recordingWindow) Println(row int, segs ...layout.Segment) {
	for len(w.lines) <= row {
		w.lines = append(w.lines, "")
	}
	text := ""
	for _, s := range segs {
		text += s.Text
	}
	w.lines[row] = text
}

func (w *recordingWindow) Clear() { w.lines = nil }

func (w *recordingWindow) contains(substr string) bool {
	for _, l := range w.lines {
		if strings.Contains(l, substr) {
			return true
		}
	}
	return false
}
