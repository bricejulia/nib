package reloadconfirm

import (
	"strings"
	"testing"

	"github.com/bricejulia/nib/internal/layout"
)

func TestHandleKeyKeepMine(t *testing.T) {
	v := New()
	v.Show("f.txt", 0)

	var called bool
	v.OnKeepMine = func() { called = true }

	if !v.HandleKey(layout.Key{Text: "k"}) {
		t.Fatal("expected the key to be consumed")
	}
	if !called {
		t.Fatal("expected OnKeepMine to be called for \"k\"")
	}
}

func TestHandleKeyReloadFromDisk(t *testing.T) {
	v := New()
	v.Show("f.txt", 0)

	var called bool
	v.OnReloadFromDisk = func() { called = true }

	if !v.HandleKey(layout.Key{Text: "r"}) {
		t.Fatal("expected the key to be consumed")
	}
	if !called {
		t.Fatal("expected OnReloadFromDisk to be called for \"r\"")
	}
}

func TestHandleKeyCancel(t *testing.T) {
	v := New()
	v.Show("f.txt", 0)

	var called bool
	v.OnCancel = func() { called = true }

	v.HandleKey(layout.Key{Named: layout.KeyEsc})
	if !called {
		t.Fatal("expected OnCancel to be called for Esc")
	}
}

func TestHandleKeyIsCaseInsensitive(t *testing.T) {
	v := New()
	v.Show("f.txt", 0)

	var keep, reload bool
	v.OnKeepMine = func() { keep = true }
	v.OnReloadFromDisk = func() { reload = true }

	v.HandleKey(layout.Key{Text: "K"})
	v.HandleKey(layout.Key{Text: "R"})

	if !keep || !reload {
		t.Fatalf("expected both uppercase K and R to trigger their actions, got keep=%v reload=%v", keep, reload)
	}
}

func TestHandleKeyIgnoresUnboundKeysButStillConsumesThem(t *testing.T) {
	v := New()
	v.Show("f.txt", 0)
	v.OnKeepMine = func() { t.Fatal("must not fire for an unrelated key") }
	v.OnReloadFromDisk = func() { t.Fatal("must not fire for an unrelated key") }
	v.OnCancel = func() { t.Fatal("must not fire for an unrelated key") }

	if !v.HandleKey(layout.Key{Text: "x"}) {
		t.Fatal("expected the key to be consumed even though it triggers nothing")
	}
}

func TestHandleKeyIgnoresReleaseEvents(t *testing.T) {
	v := New()
	v.Show("f.txt", 0)
	v.OnKeepMine = func() { t.Fatal("must not fire on a release event") }

	if !v.HandleKey(layout.Key{Text: "k", EventType: layout.EventRelease}) {
		t.Fatal("expected a release event to be reported as consumed")
	}
}

func TestRenderDoesNotPanic(t *testing.T) {
	v := New()
	v.Show("f.txt", 0)
	v.Render(fakeWindow{})
}

func TestRenderNamesTheFile(t *testing.T) {
	v := New()
	v.Show("path/to/f.txt", 0)

	w := &recordingWindow{}
	v.Render(w)

	if !w.contains("path/to/f.txt") {
		t.Errorf("expected the conflicting path in the render, got %v", w.lines)
	}
}

func TestRenderShowsRemainingCountWhenQueued(t *testing.T) {
	v := New()
	v.Show("f.txt", 2)

	w := &recordingWindow{}
	v.Render(w)

	if !w.contains("2") {
		t.Errorf("expected the queued count in the render, got %v", w.lines)
	}
}

func TestRenderOmitsRemainingCountWhenLastOne(t *testing.T) {
	v := New()
	v.Show("f.txt", 0)

	w := &recordingWindow{}
	v.Render(w)

	if w.contains("more") {
		t.Errorf("did not expect a queued-count line for the last conflict, got %v", w.lines)
	}
}

func TestTitle(t *testing.T) {
	v := New()
	if got := v.Title(); got != "File changed on disk" {
		t.Errorf("Title() = %q, want %q", got, "File changed on disk")
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
