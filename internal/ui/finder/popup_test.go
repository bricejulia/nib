package finder

import (
	"testing"

	"github.com/bricejulia/nib/internal/layout"
)

// newPopupTestView is the panel's finder shown as the Ctrl+P popup:
// undocked, with somewhere to dock to.
func newPopupTestView(items ...string) (v *View, docked *bool) {
	v = newTestView(items...)
	d := false
	v.OnDock = func() { d = true }
	return v, &d
}

func TestPopupEnterSelectsAndCloses(t *testing.T) {
	v, _ := newPopupTestView("a.go", "b.go")
	var selected string
	closed := false
	v.OnSelect = func(p string, _ int) { selected = p }
	v.OnClose = func() { closed = true }
	v.HandleKey(layout.Key{Text: "b"})
	v.HandleKey(keyEnter)
	if selected != "/project/b.go" || !closed {
		t.Fatalf("selected=%q closed=%v", selected, closed)
	}
	if v.query.String() != "b" {
		t.Errorf("query not kept after closing: %q", v.query.String())
	}
}

func TestPopupClosesOnFirstEsc(t *testing.T) {
	v, _ := newPopupTestView("a.go")
	closed := false
	v.OnClose = func() { closed = true }
	v.HandleKey(keyEsc)
	if !closed {
		t.Error("a popup with a panel behind it should close on the first Esc")
	}
}

func TestPopupTabDocks(t *testing.T) {
	v, docked := newPopupTestView("a.go")
	if !v.HandleKey(layout.Key{Named: layout.KeyTab}) || !*docked {
		t.Fatal("Tab should call OnDock in the popup")
	}
}

func TestDockedTabBubbles(t *testing.T) {
	v, docked := newPopupTestView("a.go")
	v.SetDocked(true)
	if v.HandleKey(layout.Key{Named: layout.KeyTab}) {
		t.Error("docked, Tab should reach the global keymap (switch pane)")
	}
	if *docked {
		t.Error("docked, Tab should not dock again")
	}
}

func TestSelectedQueryIsReplacedByTypingAndKeptByArrows(t *testing.T) {
	v := newTestView("alpha.go", "beta.go")
	v.query.InsertText("alp")
	v.querySelected = true
	v.HandleKey(layout.Key{Text: "b"})
	if v.query.String() != "b" {
		t.Errorf("typing over a selected query: %q, want %q", v.query.String(), "b")
	}

	v.query.InsertText("eta")
	v.querySelected = true
	v.HandleKey(layout.Key{Named: layout.KeyDown})
	if v.query.String() != "beta" || v.querySelected {
		t.Errorf("an arrow should keep the query and drop the selection: %q selected=%v", v.query.String(), v.querySelected)
	}
}

func TestReopenKeepsQuerySelected(t *testing.T) {
	dir := newContentSearchRepo(t)
	v := New(dir)
	v.Open()
	for _, r := range "hay" {
		v.HandleKey(layout.Key{Text: string(r)})
	}
	v.HandleKey(keyEsc) // browse (no OnDock: the two-step Esc)
	v.Reopen()
	if v.query.String() != "hay" || !v.querySelected || !v.Typing() {
		t.Fatalf("Reopen: query=%q selected=%v typing=%v", v.query.String(), v.querySelected, v.Typing())
	}
	if v.resultCount() == 0 {
		t.Error("Reopen should re-run the kept query")
	}
}
