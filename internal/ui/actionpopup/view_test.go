package actionpopup

import (
	"strings"
	"testing"

	"github.com/bricejulia/nib/internal/layout"
)

// fakeWindow is an in-memory layout.Window double so View.Render is
// testable without a live terminal — copied from help's own test double
// (internal/ui/help/view_test.go), since both packages need the same
// minimal Println-into-a-slice fake.
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

func typeText(v *View, s string) {
	for _, r := range s {
		v.HandleKey(layout.Key{Text: string(r)})
	}
}

func TestTitleIsActions(t *testing.T) {
	if got := New().Title(); got != "Actions" {
		t.Errorf("got %q, want %q", got, "Actions")
	}
}

func TestRenderShowsEveryEntryWithNoQuery(t *testing.T) {
	v := New()
	v.Open(nil, nil)
	w := newFakeWindow(80, len(entries)+1) // tall enough for every entry to render unscrolled
	v.Render(w)

	joined := strings.Join(w.lines, "\n")
	for _, want := range []string{"Go to definition", "Save the active tab", "Open file finder"} {
		if !strings.Contains(joined, want) {
			t.Errorf("expected action list to contain %q, got:\n%s", want, joined)
		}
	}
}

func TestTypingFiltersToMatchingEntries(t *testing.T) {
	v := New()
	v.Open(nil, nil)
	typeText(v, "go to def")

	w := newFakeWindow(80, len(entries)+1)
	v.Render(w)
	joined := strings.Join(w.lines, "\n")

	if !strings.Contains(joined, "Go to definition") {
		t.Errorf("expected filtered list to contain %q, got:\n%s", "Go to definition", joined)
	}
	if strings.Contains(joined, "Open file finder") {
		t.Errorf("expected filtered list to no longer contain %q, got:\n%s", "Open file finder", joined)
	}
}

func TestNoMatchesShowsPlaceholder(t *testing.T) {
	v := New()
	v.Open(nil, nil)
	typeText(v, "xyzzy no such command")

	w := newFakeWindow(80, 5)
	v.Render(w)
	if !strings.Contains(strings.Join(w.lines, "\n"), "no matching commands") {
		t.Errorf("expected the no-match placeholder, got:\n%s", strings.Join(w.lines, "\n"))
	}
}

func TestMoveDownAndUpClampAtEnds(t *testing.T) {
	v := New()
	v.Open(nil, nil)

	for i := 0; i < len(entries)+5; i++ {
		v.HandleKey(layout.Key{Named: layout.KeyDown})
	}
	if v.cursor != len(v.filtered)-1 {
		t.Errorf("cursor = %d after moving past the end, want %d", v.cursor, len(v.filtered)-1)
	}

	for i := 0; i < len(entries)+5; i++ {
		v.HandleKey(layout.Key{Named: layout.KeyUp})
	}
	if v.cursor != 0 {
		t.Errorf("cursor = %d after moving past the start, want 0", v.cursor)
	}
}

func TestEnterExecutesSelectedEntry(t *testing.T) {
	v := New()
	v.Open(nil, nil)
	typeText(v, "go to def") // narrows to exactly one entry: "go_to_definition"

	var got string
	v.OnExecute = func(id string) { got = id }

	if !v.HandleKey(layout.Key{Named: layout.KeyEnter}) {
		t.Fatal("expected Enter to be reported as consumed")
	}
	if got != "go_to_definition" {
		t.Errorf("OnExecute called with %q, want %q", got, "go_to_definition")
	}
}

func TestEnterOnEmptyListDoesNothing(t *testing.T) {
	v := New()
	v.Open(nil, nil)
	typeText(v, "xyzzy no such command")

	called := false
	v.OnExecute = func(id string) { called = true }

	v.HandleKey(layout.Key{Named: layout.KeyEnter})
	if called {
		t.Error("expected OnExecute not to be called with no matching entries")
	}
}

func TestEscClosesOverlay(t *testing.T) {
	v := New()
	v.Open(nil, nil)
	closed := false
	v.OnClose = func() { closed = true }

	if !v.HandleKey(layout.Key{Named: layout.KeyEsc}) {
		t.Fatal("expected Esc to be reported as consumed")
	}
	if !closed {
		t.Error("expected OnClose to be called on Esc")
	}
}

func TestOpenShowsResolvedKeybindHint(t *testing.T) {
	v := New()
	v.Open(map[string]string{"Ctrl+p": "open_finder"}, map[string]string{"Ctrl+]": "go_to_definition"})
	typeText(v, "go to def")

	w := newFakeWindow(80, 5)
	v.Render(w)
	if !strings.Contains(strings.Join(w.lines, "\n"), "Ctrl+]") {
		t.Errorf("expected the resolved keybind hint in the row, got:\n%s", strings.Join(w.lines, "\n"))
	}
}

func TestOpenResetsQueryAndSelection(t *testing.T) {
	v := New()
	v.Open(nil, nil)
	typeText(v, "definition")
	v.HandleKey(layout.Key{Named: layout.KeyDown})

	v.Open(nil, nil)
	if v.query.String() != "" {
		t.Errorf("query = %q after Open, want empty", v.query.String())
	}
	if v.cursor != 0 {
		t.Errorf("cursor = %d after Open, want 0", v.cursor)
	}
	if len(v.filtered) != len(entries) {
		t.Errorf("filtered has %d entries after Open, want all %d", len(v.filtered), len(entries))
	}
}
