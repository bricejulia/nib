package quitconfirm

import (
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

// fakeWindow is a minimal layout.Window that discards everything, enough
// to exercise Render without needing a real terminal.
type fakeWindow struct{}

func (fakeWindow) Size() (int, int)                        { return 80, 24 }
func (fakeWindow) Println(row int, segs ...layout.Segment) {}
func (fakeWindow) Clear()                                  {}
