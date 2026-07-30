package diffview

import (
	"fmt"
	"strings"
	"testing"

	"github.com/bricejulia/kiwi/internal/layout"
)

type fakeWindow struct {
	cols, rows int
	lines      []string
	styles     []layout.Style // the first segment's style per row
}

func newFakeWindow(cols, rows int) *fakeWindow {
	return &fakeWindow{cols: cols, rows: rows, lines: make([]string, rows), styles: make([]layout.Style, rows)}
}

func (w *fakeWindow) Size() (int, int) { return w.cols, w.rows }
func (w *fakeWindow) Println(row int, segs ...layout.Segment) {
	if row < 0 || row >= len(w.lines) {
		return
	}
	var b strings.Builder
	for _, s := range segs {
		b.WriteString(s.Text)
	}
	w.lines[row] = b.String()
	if len(segs) > 0 {
		w.styles[row] = segs[0].Style
	}
}
func (w *fakeWindow) Clear() {
	for i := range w.lines {
		w.lines[i] = ""
		w.styles[i] = layout.Style{}
	}
}

func key(named string) layout.Key { return layout.Key{Named: named} }

func TestEmptyDiffSaysSo(t *testing.T) {
	v := New()
	v.Show("f.go", nil)

	w := newFakeWindow(40, 5)
	v.Render(w)

	if !strings.Contains(w.lines[0], "no changes") {
		t.Errorf("expected a no-changes message, got %q", w.lines[0])
	}
}

func TestTitleNamesTheFile(t *testing.T) {
	v := New()
	if got := v.Title(); got != "Diff" {
		t.Errorf("empty title = %q, want %q", got, "Diff")
	}
	v.Show("internal/ui/editor/view.go", []string{"@@ -1 +1 @@"})
	if got, want := v.Title(), "Diff: internal/ui/editor/view.go"; got != want {
		t.Errorf("title = %q, want %q", got, want)
	}
}

func TestRendersFromTheTop(t *testing.T) {
	v := New()
	v.Show("f.go", []string{"line0", "line1", "line2"})

	w := newFakeWindow(40, 3)
	v.Render(w)

	for i, want := range []string{"line0", "line1", "line2"} {
		if w.lines[i] != want {
			t.Errorf("row %d = %q, want %q", i, w.lines[i], want)
		}
	}
}

func TestLineStyles(t *testing.T) {
	cases := []struct {
		line string
		want layout.Style
	}{
		{"+added", layout.Style{Foreground: layout.ColorGreen}},
		{"-removed", layout.Style{Foreground: layout.ColorRed}},
		{"@@ -1,2 +1,3 @@", layout.Style{Foreground: layout.ColorCyan}},
		{" context", layout.Style{}},
		// File-header lines start with the same characters as content lines
		// but are metadata, so they must not read as a huge add/remove.
		{"+++ b/f.go", layout.Style{Attr: layout.AttrDim}},
		{"--- a/f.go", layout.Style{Attr: layout.AttrDim}},
		{"diff --git a/f.go b/f.go", layout.Style{Attr: layout.AttrDim}},
		{"index 123..456 100644", layout.Style{Attr: layout.AttrDim}},
		{"new file mode 100644", layout.Style{Attr: layout.AttrDim}},
		{`\ No newline at end of file`, layout.Style{Attr: layout.AttrDim}},
	}
	for _, c := range cases {
		if got := lineStyle(c.line); got != c.want {
			t.Errorf("lineStyle(%q) = %+v, want %+v", c.line, got, c.want)
		}
	}
}

func TestScrollingClampsToTheContent(t *testing.T) {
	v := New()
	lines := make([]string, 10)
	for i := range lines {
		lines[i] = fmt.Sprintf("line%d", i)
	}
	v.Show("f.go", lines)
	w := newFakeWindow(40, 4)
	v.Render(w)

	// Scrolling up at the top stays at the top.
	v.HandleKey(key(layout.KeyUp))
	v.Render(w)
	if w.lines[0] != "line0" {
		t.Errorf("after Up at the top, first row = %q, want line0", w.lines[0])
	}

	v.HandleKey(key(layout.KeyDown))
	v.HandleKey(key(layout.KeyDown))
	v.Render(w)
	if w.lines[0] != "line2" {
		t.Errorf("after two Downs, first row = %q, want line2", w.lines[0])
	}

	// End, then far past it: the last screenful stays filled rather than
	// scrolling into blank rows.
	v.HandleKey(key(layout.KeyEnd))
	v.Render(w)
	if w.lines[0] != "line6" || w.lines[3] != "line9" {
		t.Errorf("after End, rows = %q, want line6..line9", w.lines)
	}
	for range 5 {
		v.HandleKey(key(layout.KeyPageDown))
	}
	v.Render(w)
	if w.lines[0] != "line6" {
		t.Errorf("after paging past the end, first row = %q, want line6", w.lines[0])
	}

	v.HandleKey(key(layout.KeyHome))
	v.Render(w)
	if w.lines[0] != "line0" {
		t.Errorf("after Home, first row = %q, want line0", w.lines[0])
	}
}

func TestPeekingRightClampsToTheWidestVisibleLine(t *testing.T) {
	v := New()
	v.Show("f.go", []string{"+0123456789abcde"}) // 16 columns

	w := newFakeWindow(10, 1)
	v.Render(w)
	if w.lines[0] != "+012345678" {
		t.Fatalf("unpeeked row = %q", w.lines[0])
	}

	v.HandleKey(key(layout.KeyRight))
	v.Render(w)
	if w.lines[0] != "56789abcde" {
		t.Errorf("after one peek right, row = %q, want the tail", w.lines[0])
	}

	// Further peeking can't run off the end of the content.
	for range 5 {
		v.HandleKey(key(layout.KeyRight))
	}
	v.Render(w)
	if w.lines[0] != "56789abcde" {
		t.Errorf("peeking past the end changed the row to %q", w.lines[0])
	}

	v.HandleKey(key(layout.KeyLeft))
	v.Render(w)
	if w.lines[0] != "+012345678" {
		t.Errorf("after peeking back left, row = %q", w.lines[0])
	}
}

func TestShowResetsScrollPosition(t *testing.T) {
	v := New()
	v.Show("a.go", []string{"a0", "a1", "a2", "a3"})
	w := newFakeWindow(40, 2)
	v.Render(w)
	v.HandleKey(key(layout.KeyEnd))
	v.HandleKey(key(layout.KeyRight))
	v.Render(w)

	v.Show("b.go", []string{"b0", "b1", "b2", "b3"})
	v.Render(w)
	if w.lines[0] != "b0" {
		t.Errorf("expected a new diff to start at the top, got %q", w.lines[0])
	}
}

func TestEscCloses(t *testing.T) {
	v := New()
	closed := false
	v.OnClose = func() { closed = true }
	v.HandleKey(key(layout.KeyEsc))
	if !closed {
		t.Error("expected Esc to call OnClose")
	}
}

// A modal must never leak input to whatever is behind it.
func TestHandleKeyAlwaysConsumes(t *testing.T) {
	v := New()
	v.Show("f.go", []string{"x"})
	for _, k := range []layout.Key{
		key(layout.KeyEsc), key(layout.KeyDown), {Text: "z"}, {Named: layout.KeyEnter},
	} {
		if !v.HandleKey(k) {
			t.Errorf("key %+v was not consumed", k)
		}
	}
}

func TestKeymapOverridesApply(t *testing.T) {
	v := New()
	v.SetKeymap(map[string]string{"q": "close"})
	closed := false
	v.OnClose = func() { closed = true }

	v.HandleKey(layout.Key{Text: "q"})
	if !closed {
		t.Error("expected the overridden \"q\" binding to close the pane")
	}
}
