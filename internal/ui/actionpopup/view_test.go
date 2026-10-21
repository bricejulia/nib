package actionpopup

import (
	"strings"
	"testing"

	"github.com/bricejulia/nib/internal/config"
	"github.com/bricejulia/nib/internal/layout"
)

var testScopes = []config.Scope{
	{Name: "global", Actions: config.Actions{
		{ID: "open_finder", Keys: []string{"Ctrl+p"}, Desc: "Open file finder", InPopup: true},
		{ID: "focus_next", Keys: []string{"Tab"}, Desc: "Focus next pane"}, // not offered
	}},
	{Name: "editor", Actions: config.Actions{
		{ID: "save", Keys: []string{"Ctrl+s"}, Desc: "Save the active tab", InPopup: true},
		{ID: "go_to_definition", Keys: []string{"Ctrl+]", "gd"}, Desc: "Go to definition", InPopup: true},
		{ID: "move_down", Keys: []string{"j"}, Desc: "Move down"}, // not offered
	}},
}

var entries = popupEntries(testScopes)

func noOverrides(string) map[string]string { return nil }

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
	if got := New(testScopes).Title(); got != "Actions" {
		t.Errorf("got %q, want %q", got, "Actions")
	}
}

func TestRenderShowsEveryEntryWithNoQuery(t *testing.T) {
	v := New(testScopes)
	v.Open(noOverrides)
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
	v := New(testScopes)
	v.Open(noOverrides)
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
	v := New(testScopes)
	v.Open(noOverrides)
	typeText(v, "xyzzy no such command")

	w := newFakeWindow(80, 5)
	v.Render(w)
	if !strings.Contains(strings.Join(w.lines, "\n"), "no matching commands") {
		t.Errorf("expected the no-match placeholder, got:\n%s", strings.Join(w.lines, "\n"))
	}
}

func TestMoveDownAndUpClampAtEnds(t *testing.T) {
	v := New(testScopes)
	v.Open(noOverrides)

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
	v := New(testScopes)
	v.Open(noOverrides)
	typeText(v, "go to def") // narrows to exactly one entry: "go_to_definition"

	var gotScope, got string
	v.OnExecute = func(scope, id string) { gotScope, got = scope, id }

	if !v.HandleKey(layout.Key{Named: layout.KeyEnter}) {
		t.Fatal("expected Enter to be reported as consumed")
	}
	if gotScope != "editor" || got != "go_to_definition" {
		t.Errorf("OnExecute called with %q, %q, want editor, go_to_definition", gotScope, got)
	}
}

func TestEnterOnEmptyListDoesNothing(t *testing.T) {
	v := New(testScopes)
	v.Open(noOverrides)
	typeText(v, "xyzzy no such command")

	called := false
	v.OnExecute = func(string, string) { called = true }

	v.HandleKey(layout.Key{Named: layout.KeyEnter})
	if called {
		t.Error("expected OnExecute not to be called with no matching entries")
	}
}

func TestEscClosesOverlay(t *testing.T) {
	v := New(testScopes)
	v.Open(noOverrides)
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
	v := New(testScopes)
	v.Open(noOverrides)
	typeText(v, "go to def")

	w := newFakeWindow(80, 5)
	v.Render(w)
	if !strings.Contains(strings.Join(w.lines, "\n"), "Ctrl+]") {
		t.Errorf("expected the resolved keybind hint in the row, got:\n%s", strings.Join(w.lines, "\n"))
	}
}

func TestOpenResetsQueryAndSelection(t *testing.T) {
	v := New(testScopes)
	v.Open(noOverrides)
	typeText(v, "definition")
	v.HandleKey(layout.Key{Named: layout.KeyDown})

	v.Open(noOverrides)
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

func TestOnlyInPopupActionsAreListed(t *testing.T) {
	var ids []string
	for _, e := range entries {
		ids = append(ids, e.ID)
	}
	if got, want := strings.Join(ids, " "), "open_finder save go_to_definition"; got != want {
		t.Errorf("entries = %s, want %s", got, want)
	}
}

func TestHintIsTheFirstKeyAndFollowsOverrides(t *testing.T) {
	v := New(testScopes)
	v.Open(func(scope string) map[string]string {
		if scope == "editor" {
			return map[string]string{"Ctrl+]": "save"} // takes Ctrl+] away
		}
		return nil
	})

	if got := v.keybind["editor:go_to_definition"]; got != "gd" {
		t.Errorf("go_to_definition hint = %q, want its remaining key %q", got, "gd")
	}
	if got := v.keybind["editor:save"]; got != "Ctrl+s" {
		t.Errorf("save hint = %q, want its default key first", got)
	}
}

func TestHiddenCommandsAreLeftOutOnEveryOpen(t *testing.T) {
	v := New(testScopes)
	hide := true
	v.Hidden = func(scope, id string) bool { return hide && scope == "editor" && id == "save" }
	v.Open(noOverrides)

	w := newFakeWindow(80, len(entries)+1)
	v.Render(w)
	if joined := strings.Join(w.lines, "\n"); strings.Contains(joined, "Save the active tab") {
		t.Fatalf("hidden command still listed:\n%s", joined)
	}

	hide = false
	v.Open(noOverrides)
	v.Render(w)
	if joined := strings.Join(w.lines, "\n"); !strings.Contains(joined, "Save the active tab") {
		t.Fatalf("command should be back once no longer hidden:\n%s", joined)
	}
}
