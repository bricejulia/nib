package scratchview

import (
	"strings"
	"testing"
	"time"

	"github.com/bricejulia/nib/internal/layout"
	"github.com/bricejulia/nib/internal/scratch"
)

// fakeWindow is the same minimal Println-into-a-slice double actionpopup's
// tests use.
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

var (
	enter = layout.Key{Named: layout.KeyEnter}
	esc   = layout.Key{Named: layout.KeyEsc}
	down  = layout.Key{Named: layout.KeyDown}
	ctrlN = layout.Key{Text: "n", Mods: layout.ModCtrl}
	ctrlD = layout.Key{Text: "d", Mods: layout.ModCtrl}
)

func typeText(v *View, s string) {
	for _, r := range s {
		v.HandleKey(layout.Key{Text: string(r)})
	}
}

func listView() *View {
	now := time.Now()
	v := New()
	v.Reset()
	v.ShowList([]scratch.Entry{
		{Path: "/p/notes.md", Name: "notes.md", ModTime: now},
		{Path: "/p/query.sql", Name: "query.sql", ModTime: now.Add(-2 * time.Hour)},
	})
	return v
}

func TestFilterAndOpen(t *testing.T) {
	v := listView()
	var opened string
	v.OnOpen = func(p string) { opened = p }
	typeText(v, "SQL")
	v.HandleKey(enter)
	if opened != "/p/query.sql" {
		t.Fatalf("opened = %q, want /p/query.sql", opened)
	}
}

func TestDownThenOpen(t *testing.T) {
	v := listView()
	var opened string
	v.OnOpen = func(p string) { opened = p }
	v.HandleKey(down)
	v.HandleKey(enter)
	if opened != "/p/query.sql" {
		t.Fatalf("opened = %q", opened)
	}
}

func TestEnterWithNoMatchCreates(t *testing.T) {
	v := listView()
	var created string
	v.OnCreate = func(n string) { created = n }
	typeText(v, "todo.md")
	v.HandleKey(enter)
	if created != "todo.md" {
		t.Fatalf("created = %q", created)
	}
}

func TestDeleteNeedsConfirmation(t *testing.T) {
	v := listView()
	var deleted []string
	v.OnDelete = func(p string) { deleted = append(deleted, p) }

	v.HandleKey(ctrlD)
	v.HandleKey(layout.Key{Text: "n"})
	if len(deleted) != 0 {
		t.Fatalf("deleted without a yes: %v", deleted)
	}
	v.HandleKey(ctrlD)
	w := newFakeWindow(80, 10)
	v.Render(w)
	if !strings.Contains(strings.Join(w.lines, "\n"), "Delete notes.md?") {
		t.Fatalf("confirm prompt not rendered:\n%s", strings.Join(w.lines, "\n"))
	}
	v.HandleKey(layout.Key{Text: "y"})
	if len(deleted) != 1 || deleted[0] != "/p/notes.md" {
		t.Fatalf("deleted = %v", deleted)
	}
}

func TestCtrlNSwitchesToNamePromptWithQuery(t *testing.T) {
	v := listView()
	var created string
	v.OnCreate = func(n string) { created = n }
	typeText(v, "draft")
	v.HandleKey(ctrlN)
	typeText(v, ".go")
	v.HandleKey(enter)
	if created != "draft.go" {
		t.Fatalf("created = %q", created)
	}
}

func TestCreateModeEscCloses(t *testing.T) {
	v := New()
	v.ShowCreate("")
	closed := false
	v.OnClose = func() { closed = true }
	v.OnCreate = func(string) { t.Fatal("Esc must not create") }
	v.HandleKey(esc)
	if !closed {
		t.Fatal("Esc did not close")
	}
}

func TestEmptyListShowsHint(t *testing.T) {
	v := New()
	v.Reset()
	v.ShowList(nil)
	w := newFakeWindow(80, 5)
	v.Render(w)
	if !strings.Contains(w.lines[1], "No scratch files") {
		t.Fatalf("row 1 = %q", w.lines[1])
	}
}

func TestRenderShowsAge(t *testing.T) {
	v := listView()
	w := newFakeWindow(80, 5)
	v.Render(w)
	if !strings.Contains(w.lines[1], "notes.md") || !strings.Contains(w.lines[1], "just now") {
		t.Fatalf("row 1 = %q", w.lines[1])
	}
	if !strings.Contains(w.lines[2], "2h ago") {
		t.Fatalf("row 2 = %q", w.lines[2])
	}
}
