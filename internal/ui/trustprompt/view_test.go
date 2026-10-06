package trustprompt

import (
	"strings"
	"testing"

	"github.com/bricejulia/nib/internal/layout"
)

func TestHandleKeyTrust(t *testing.T) {
	v := New()
	v.Show("/proj")

	var called bool
	v.OnTrust = func() { called = true }
	v.OnDecline = func() { t.Fatal("must not decline on \"y\"") }

	if !v.HandleKey(layout.Key{Text: "y"}) {
		t.Fatal("expected the key to be consumed")
	}
	if !called {
		t.Fatal("expected OnTrust to be called for \"y\"")
	}
}

func TestHandleKeyDecline(t *testing.T) {
	for name, k := range map[string]layout.Key{
		"n":   {Text: "n"},
		"Esc": {Named: layout.KeyEsc},
	} {
		t.Run(name, func(t *testing.T) {
			v := New()
			v.Show("/proj")

			var called bool
			v.OnDecline = func() { called = true }
			v.OnTrust = func() { t.Fatal("must not trust on " + name) }

			v.HandleKey(k)
			if !called {
				t.Fatalf("expected OnDecline to be called for %s", name)
			}
		})
	}
}

func TestHandleKeyIgnoresUnboundKeysButStillConsumesThem(t *testing.T) {
	v := New()
	v.Show("/proj")
	v.OnTrust = func() { t.Fatal("must not fire for an unrelated key") }
	v.OnDecline = func() { t.Fatal("must not fire for an unrelated key") }

	if !v.HandleKey(layout.Key{Text: "x"}) {
		t.Fatal("expected the key to be consumed even though it triggers nothing")
	}
}

func TestHandleKeyIgnoresRelease(t *testing.T) {
	v := New()
	v.OnTrust = func() { t.Fatal("must not fire on key release") }

	v.HandleKey(layout.Key{Text: "y", EventType: layout.EventRelease})
}

type fakeWindow struct{ lines []string }

func (w *fakeWindow) Size() (int, int) { return 60, len(w.lines) }
func (w *fakeWindow) Println(row int, segs ...layout.Segment) {
	if row < 0 || row >= len(w.lines) {
		return
	}
	text := ""
	for _, s := range segs {
		text += s.Text
	}
	w.lines[row] = text
}
func (w *fakeWindow) Clear() {
	for i := range w.lines {
		w.lines[i] = ""
	}
}

func TestRenderShowsFolderAndKeys(t *testing.T) {
	v := New()
	v.Show("/home/me/proj")
	w := &fakeWindow{lines: make([]string, 10)}
	v.Render(w)

	out := strings.Join(w.lines, "\n")
	for _, want := range []string{"/home/me/proj", ".nib", "[y] Trust", "[n] Don't trust"} {
		if !strings.Contains(out, want) {
			t.Errorf("expected %q in:\n%s", want, out)
		}
	}
}
