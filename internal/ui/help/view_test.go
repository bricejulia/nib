package help

import (
	"strings"
	"testing"

	"github.com/bricejulia/nib/internal/layout"
)

// fakeWindow is an in-memory layout.Window double so View.Render is
// testable without a live terminal.
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

func TestRenderShowsVersionAndBindings(t *testing.T) {
	v := New("1.2.3")
	w := newFakeWindow(60, 100) // tall enough for every section's bindings to render unscrolled
	v.Render(w)

	joined := strings.Join(w.lines, "\n")
	for _, want := range []string{"nib 1.2.3", "Global", "Ctrl+c", "Editor", "Finder"} {
		if !strings.Contains(joined, want) {
			t.Errorf("expected help text to contain %q, got:\n%s", want, joined)
		}
	}
}

func TestTitleIsHelp(t *testing.T) {
	if got := New("dev").Title(); got != "Help" {
		t.Errorf("got %q, want \"Help\"", got)
	}
}

func TestEscClosesOverlay(t *testing.T) {
	v := New("dev")
	closed := false
	v.OnClose = func() { closed = true }

	if !v.HandleKey(layout.Key{Named: layout.KeyEsc}) {
		t.Fatal("expected Esc to be reported as consumed")
	}
	if !closed {
		t.Error("expected OnClose to be called on Esc")
	}
}

func typeText(v *View, s string) {
	for _, r := range s {
		v.HandleKey(layout.Key{Text: string(r)})
	}
}

func TestTypingFiltersToMatchingBindings(t *testing.T) {
	v := New("dev")
	typeText(v, "tree")

	w := newFakeWindow(60, 100) // tall enough to render everything unscrolled
	v.Render(w)
	joined := strings.Join(w.lines, "\n")

	if !strings.Contains(joined, "Ctrl+t") {
		t.Errorf("expected the filtered list to still contain the matching \"Ctrl+t\" row, got:\n%s", joined)
	}
	if strings.Contains(joined, "Ctrl+c") {
		t.Errorf("expected a non-matching row (\"Ctrl+c\") to be filtered out, got:\n%s", joined)
	}
	if strings.Contains(joined, "Finder") {
		t.Errorf("expected a section with no matches (\"Finder\") to be dropped entirely, got:\n%s", joined)
	}
}

func TestClearingQueryRestoresFullList(t *testing.T) {
	v := New("dev")
	typeText(v, "tree")
	for i := 0; i < 4; i++ {
		v.HandleKey(layout.Key{Named: layout.KeyBackspace})
	}

	w := newFakeWindow(60, 100)
	v.Render(w)
	joined := strings.Join(w.lines, "\n")
	for _, want := range []string{"nib dev", "Global", "Editor", "Finder"} {
		if !strings.Contains(joined, want) {
			t.Errorf("expected the unfiltered list to contain %q, got:\n%s", want, joined)
		}
	}
}

func TestSearchWithNoMatchesShowsAHint(t *testing.T) {
	v := New("dev")
	typeText(v, "zzzznomatch")

	w := newFakeWindow(60, 20)
	v.Render(w)
	joined := strings.Join(w.lines, "\n")
	if !strings.Contains(joined, "no matching keybindings") {
		t.Errorf("expected a no-matches hint, got:\n%s", joined)
	}
}

func TestCursorPositionTracksTheSearchCaret(t *testing.T) {
	v := New("dev")
	typeText(v, "ab")

	col, row, ok := v.CursorPosition()
	if !ok {
		t.Fatal("expected the cursor to always be visible in the search box")
	}
	if row != 0 {
		t.Errorf("row = %d, want 0 (the search bar)", row)
	}
	wantCol := len(searchLabel) + 2
	if col != wantCol {
		t.Errorf("col = %d, want %d", col, wantCol)
	}
}

func TestEscClearsTheSearchQueryToo(t *testing.T) {
	v := New("dev")
	typeText(v, "tree")
	if v.query.String() != "tree" {
		t.Fatalf("query = %q, want \"tree\"", v.query.String())
	}

	v.HandleKey(layout.Key{Named: layout.KeyEsc})
	if v.query.String() != "" {
		t.Errorf("query = %q, want empty after Esc", v.query.String())
	}
}

func TestScrollDownThenHomeReturnsToTop(t *testing.T) {
	v := New("dev")
	w := newFakeWindow(60, 3) // row 0 is the search bar; content starts at row 1
	v.Render(w)

	for i := 0; i < 10; i++ {
		v.HandleKey(layout.Key{Named: layout.KeyDown})
	}
	v.Render(w)
	if strings.Contains(w.lines[1], "nib dev") {
		t.Errorf("expected the header to have scrolled out of view, got %q", w.lines[1])
	}

	v.HandleKey(layout.Key{Named: layout.KeyHome})
	v.Render(w)
	if !strings.Contains(w.lines[1], "nib dev") {
		t.Errorf("expected Home to scroll back to the top, got %q", w.lines[1])
	}
}

func TestSetKeymapOverridesATrigger(t *testing.T) {
	v := New("dev")
	w := newFakeWindow(60, 3)
	v.Render(w)
	v.SetKeymap(map[string]string{"Down": "close"}) // reverse Down's default action

	closed := false
	v.OnClose = func() { closed = true }
	v.HandleKey(layout.Key{Named: layout.KeyDown})
	if !closed {
		t.Fatal("expected Down (remapped to close) to invoke OnClose")
	}
	if v.topLine != 0 {
		t.Fatalf("topLine = %d, want 0 (Down's default scroll must not also fire)", v.topLine)
	}
}

func TestScrollClampsAtBottom(t *testing.T) {
	v := New("dev")
	w := newFakeWindow(60, 5) // row 0 is the search bar, so 4 content rows

	for i := 0; i < 1000; i++ {
		v.HandleKey(layout.Key{Named: layout.KeyDown})
	}
	v.Render(w) // must not panic, and must clamp rather than scroll past the content

	if v.topLine > len(v.lines)-4 {
		t.Errorf("expected topLine to clamp to the last full page, got %d (len=%d)", v.topLine, len(v.lines))
	}
}

func TestReleaseEventsAreIgnored(t *testing.T) {
	v := New("dev")
	before := v.topLine
	v.HandleKey(layout.Key{Named: layout.KeyDown, EventType: layout.EventRelease})
	if v.topLine != before {
		t.Errorf("expected a release event to be a no-op, topLine changed from %d to %d", before, v.topLine)
	}
}
