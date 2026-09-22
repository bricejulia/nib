package refview

import (
	"os"
	"path/filepath"
	"strings"
	"testing"

	"github.com/bricejulia/nib/internal/layout"
	"github.com/bricejulia/nib/internal/lsp"
)

// fakeWindow is an in-memory layout.Window double — copied from
// actionpopup's own test double (internal/ui/actionpopup/view_test.go),
// since every small overlay package needs the same minimal
// Println-into-a-slice fake.
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

func TestTitleIsReferences(t *testing.T) {
	if got := New("/root").Title(); got != "References" {
		t.Errorf("got %q, want %q", got, "References")
	}
}

func TestRenderShowsPathLineAndText(t *testing.T) {
	dir := t.TempDir()
	path := filepath.Join(dir, "a.go")
	if err := os.WriteFile(path, []byte("package main\n\nfunc greet() {}\n"), 0o644); err != nil {
		t.Fatal(err)
	}

	v := New(dir)
	v.Open([]lsp.Location{{URI: "file://" + path, Range: lsp.Range{Start: lsp.Position{Line: 2}}}})

	w := newFakeWindow(80, 3)
	v.Render(w)
	joined := strings.Join(w.lines, "\n")

	if !strings.Contains(joined, "1 reference") {
		t.Errorf("expected the header row to count one reference, got:\n%s", joined)
	}
	if !strings.Contains(joined, "a.go:3") || !strings.Contains(joined, "func greet() {}") {
		t.Errorf("expected the row to show the relative path, 1-based line, and line text, got:\n%s", joined)
	}
}

func TestRenderWithMissingFileFallsBackToPathAndLine(t *testing.T) {
	v := New("/root")
	v.Open([]lsp.Location{{URI: "file:///root/gone.go", Range: lsp.Range{Start: lsp.Position{Line: 4}}}})

	w := newFakeWindow(80, 3)
	v.Render(w)
	joined := strings.Join(w.lines, "\n")

	if !strings.Contains(joined, "gone.go:5") {
		t.Errorf("expected a path:line fallback for an unreadable file, got:\n%s", joined)
	}
}

func TestMoveDownAndUpClampAtEnds(t *testing.T) {
	v := New("/root")
	refs := []lsp.Location{
		{URI: "file:///root/a.go"}, {URI: "file:///root/b.go"}, {URI: "file:///root/c.go"},
	}
	v.Open(refs)

	for range 10 {
		v.HandleKey(layout.Key{Named: layout.KeyDown})
	}
	if v.cursor != len(refs)-1 {
		t.Errorf("cursor = %d after moving past the end, want %d", v.cursor, len(refs)-1)
	}

	for range 10 {
		v.HandleKey(layout.Key{Named: layout.KeyUp})
	}
	if v.cursor != 0 {
		t.Errorf("cursor = %d after moving past the start, want 0", v.cursor)
	}
}

func TestEnterSelectsCurrentLocation(t *testing.T) {
	v := New("/root")
	want := lsp.Location{URI: "file:///root/b.go", Range: lsp.Range{Start: lsp.Position{Line: 9}}}
	v.Open([]lsp.Location{{URI: "file:///root/a.go"}, want})
	v.HandleKey(layout.Key{Named: layout.KeyDown})

	var got lsp.Location
	selected := false
	v.OnSelect = func(loc lsp.Location) { got, selected = loc, true }

	if !v.HandleKey(layout.Key{Named: layout.KeyEnter}) {
		t.Fatal("expected Enter to be reported as consumed")
	}
	if !selected || got.URI != want.URI {
		t.Errorf("OnSelect got selected=%v loc=%+v, want %+v", selected, got, want)
	}
}

func TestEnterOnEmptyListDoesNothing(t *testing.T) {
	v := New("/root")
	v.Open(nil)

	called := false
	v.OnSelect = func(lsp.Location) { called = true }

	v.HandleKey(layout.Key{Named: layout.KeyEnter})
	if called {
		t.Error("expected OnSelect not to be called with no references")
	}
}

func TestEscClosesOverlay(t *testing.T) {
	v := New("/root")
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
	v := New("/root")
	v.Open([]lsp.Location{{URI: "file:///root/a.go"}, {URI: "file:///root/b.go"}})
	v.HandleKey(layout.Key{Named: layout.KeyDown})

	v.Open([]lsp.Location{{URI: "file:///root/c.go"}})
	if v.cursor != 0 {
		t.Errorf("cursor = %d after Open, want 0", v.cursor)
	}
	if len(v.refs) != 1 {
		t.Errorf("refs has %d entries after Open, want 1", len(v.refs))
	}
}
