package statusbar

import (
	"strings"
	"testing"

	"github.com/bricejulia/kiwi/internal/layout"
)

type fakeWindow struct {
	cols, rows int
	lines      []string
}

func newFakeWindow(cols, rows int) *fakeWindow {
	return &fakeWindow{cols: cols, rows: rows, lines: make([]string, rows)}
}

func (w *fakeWindow) Size() (int, int) { return w.cols, w.rows }
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

func TestViewRendersTextRightAligned(t *testing.T) {
	v := New()
	v.TextFunc = func() string { return "Ln 1, Col 1" }

	w := newFakeWindow(20, 1)
	v.Render(w)

	if !strings.HasSuffix(w.lines[0], "Ln 1, Col 1") {
		t.Fatalf("expected text right-aligned at the end of the line, got %q", w.lines[0])
	}
	if len(w.lines[0]) != 20 {
		t.Fatalf("expected the line padded to the full width (20), got len=%d: %q", len(w.lines[0]), w.lines[0])
	}
}

func TestViewNilTextFuncRendersEmpty(t *testing.T) {
	v := New()
	w := newFakeWindow(20, 1)
	v.Render(w)
	if w.lines[0] != "" {
		t.Fatalf("expected empty output with no TextFunc, got %q", w.lines[0])
	}
}

func TestViewTruncatesTextWiderThanWindow(t *testing.T) {
	v := New()
	v.TextFunc = func() string { return "this text is way too long to fit" }
	w := newFakeWindow(10, 1)
	v.Render(w) // must not panic
	if len(w.lines[0]) > 10 {
		t.Fatalf("expected truncation to window width, got len=%d", len(w.lines[0]))
	}
}

func TestViewIsUnfocusableAndConsumesNoKeys(t *testing.T) {
	v := New()
	if !v.Unfocusable() {
		t.Fatal("statusbar.View must report Unfocusable() == true")
	}
	if v.HandleKey(layout.Key{Text: "j"}) {
		t.Fatal("statusbar.View must never consume a key")
	}
}
