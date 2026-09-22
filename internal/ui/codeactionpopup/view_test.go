package codeactionpopup

import (
	"strings"
	"testing"

	"github.com/bricejulia/nib/internal/layout"
	"github.com/bricejulia/nib/internal/lsp"
)

// fakeWindow is an in-memory layout.Window double — copied from
// actionpopup's own test double (internal/ui/actionpopup/view_test.go).
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

func TestTitleIsCodeActions(t *testing.T) {
	if got := New().Title(); got != "Code Actions" {
		t.Errorf("got %q, want %q", got, "Code Actions")
	}
}

func TestRenderShowsEveryActionTitle(t *testing.T) {
	v := New()
	v.Open([]lsp.CodeAction{{Title: "Remove unused import"}, {Title: "Organize imports"}})

	w := newFakeWindow(80, 5)
	v.Render(w)
	joined := strings.Join(w.lines, "\n")

	for _, want := range []string{"Remove unused import", "Organize imports"} {
		if !strings.Contains(joined, want) {
			t.Errorf("expected the action list to contain %q, got:\n%s", want, joined)
		}
	}
}

func TestMoveDownAndUpClampAtEnds(t *testing.T) {
	v := New()
	actions := []lsp.CodeAction{{Title: "a"}, {Title: "b"}, {Title: "c"}}
	v.Open(actions)

	for range 10 {
		v.HandleKey(layout.Key{Named: layout.KeyDown})
	}
	if v.cursor != len(actions)-1 {
		t.Errorf("cursor = %d after moving past the end, want %d", v.cursor, len(actions)-1)
	}

	for range 10 {
		v.HandleKey(layout.Key{Named: layout.KeyUp})
	}
	if v.cursor != 0 {
		t.Errorf("cursor = %d after moving past the start, want 0", v.cursor)
	}
}

func TestEnterExecutesSelectedAction(t *testing.T) {
	v := New()
	want := lsp.CodeAction{Title: "Organize imports"}
	v.Open([]lsp.CodeAction{{Title: "Remove unused import"}, want})
	v.HandleKey(layout.Key{Named: layout.KeyDown})

	var got lsp.CodeAction
	executed := false
	v.OnExecute = func(a lsp.CodeAction) { got, executed = a, true }

	if !v.HandleKey(layout.Key{Named: layout.KeyEnter}) {
		t.Fatal("expected Enter to be reported as consumed")
	}
	if !executed || got.Title != want.Title {
		t.Errorf("OnExecute got executed=%v action=%+v, want %+v", executed, got, want)
	}
}

func TestEnterOnEmptyListDoesNothing(t *testing.T) {
	v := New()
	v.Open(nil)

	called := false
	v.OnExecute = func(lsp.CodeAction) { called = true }

	v.HandleKey(layout.Key{Named: layout.KeyEnter})
	if called {
		t.Error("expected OnExecute not to be called with no actions")
	}
}

func TestEscClosesOverlay(t *testing.T) {
	v := New()
	v.Open(nil)
	closed := false
	v.OnClose = func() { closed = true }

	if !v.HandleKey(layout.Key{Named: layout.KeyEsc}) {
		t.Fatal("expected Esc to be reported as consumed")
	}
	if !closed {
		t.Error("expected OnClose to be called on Esc")
	}
}

func TestOpenResetsSelection(t *testing.T) {
	v := New()
	v.Open([]lsp.CodeAction{{Title: "a"}, {Title: "b"}})
	v.HandleKey(layout.Key{Named: layout.KeyDown})

	v.Open([]lsp.CodeAction{{Title: "c"}})
	if v.cursor != 0 {
		t.Errorf("cursor = %d after Open, want 0", v.cursor)
	}
	if len(v.actions) != 1 {
		t.Errorf("actions has %d entries after Open, want 1", len(v.actions))
	}
}
