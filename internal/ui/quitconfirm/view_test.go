package quitconfirm

import (
	"strings"
	"testing"

	"github.com/bricejulia/nib/internal/layout"
)

func TestHandleKeySaveAndQuit(t *testing.T) {
	v := New()
	v.Show([]string{"a.go", "b.go"})

	var called bool
	v.OnSaveAndQuit = func() { called = true }

	if !v.HandleKey(layout.Key{Text: "s"}) {
		t.Fatal("expected the key to be consumed")
	}
	if !called {
		t.Fatal("expected OnSaveAndQuit to be called for \"s\"")
	}
}

func TestHandleKeyDiscardAndQuit(t *testing.T) {
	v := New()
	v.Show([]string{"a.go"})

	var called bool
	v.OnDiscardAndQuit = func() { called = true }

	v.HandleKey(layout.Key{Text: "q"})
	if !called {
		t.Fatal("expected OnDiscardAndQuit to be called for \"q\"")
	}
}

func TestHandleKeyCancel(t *testing.T) {
	v := New()
	v.Show([]string{"a.go"})

	var called bool
	v.OnCancel = func() { called = true }

	v.HandleKey(layout.Key{Named: layout.KeyEsc})
	if !called {
		t.Fatal("expected OnCancel to be called for Esc")
	}
}

func TestHandleKeyIgnoresUnboundKeysButStillConsumesThem(t *testing.T) {
	v := New()
	v.Show([]string{"a.go"})
	v.OnSaveAndQuit = func() { t.Fatal("must not fire for an unrelated key") }
	v.OnDiscardAndQuit = func() { t.Fatal("must not fire for an unrelated key") }
	v.OnCancel = func() { t.Fatal("must not fire for an unrelated key") }

	if !v.HandleKey(layout.Key{Text: "x"}) {
		t.Fatal("expected the key to be consumed even though it triggers nothing")
	}
}

func TestHandleKeyIgnoresReleaseEvents(t *testing.T) {
	v := New()
	v.Show([]string{"a.go"})
	v.OnSaveAndQuit = func() { t.Fatal("must not fire on a release event") }

	if !v.HandleKey(layout.Key{Text: "s", EventType: layout.EventRelease}) {
		t.Fatal("expected a release event to be reported as consumed")
	}
}

func TestRenderDoesNotPanic(t *testing.T) {
	v := New()
	v.Show([]string{"a.go", "b.go"})
	v.Render(fakeWindow{})
}

func TestRenderWithNoUnsavedFilesDoesNotPanic(t *testing.T) {
	v := New()
	v.Show(nil)
	v.Render(fakeWindow{})
}

// TestRenderWithNoUnsavedFilesIsAPlainQuitPrompt checks that with nothing
// unsaved, Render falls back to a minimal "quit or cancel" prompt rather
// than the "N unsaved files" listing and its now-meaningless "save all"
// option — see View.Show and confirmQuit in cmd/nib/main.go, which shows
// this dialog on every quit attempt, not just when something's at stake.
func TestRenderWithNoUnsavedFilesIsAPlainQuitPrompt(t *testing.T) {
	v := New()
	v.Show(nil)

	w := &recordingWindow{}
	v.Render(w)

	if !w.contains("Quit nib?") {
		t.Errorf("expected a plain quit prompt, got %v", w.lines)
	}
	if w.contains("Save all and quit") {
		t.Errorf("expected no save option with nothing unsaved, got %v", w.lines)
	}
	if w.contains("unsaved") {
		t.Errorf("expected no unsaved-file listing, got %v", w.lines)
	}
	if v.Title() != "Quit nib?" {
		t.Errorf("Title() = %q, want %q", v.Title(), "Quit nib?")
	}
}

func TestRenderWithUnsavedFilesOffersToSave(t *testing.T) {
	v := New()
	v.Show([]string{"a.go"})

	w := &recordingWindow{}
	v.Render(w)

	if !w.contains("Save all and quit") {
		t.Errorf("expected a save option with unsaved files, got %v", w.lines)
	}
	if v.Title() != "Unsaved changes" {
		t.Errorf("Title() = %q, want %q", v.Title(), "Unsaved changes")
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
