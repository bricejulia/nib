// This file deliberately lives in the external test package so it can import
// the editor alongside the file tree: it stands in for cmd/nib's own wiring
// (which has no test harness) and checks the two panes agree end to end when
// a file is renamed or deleted from the tree.
package filetree_test

import (
	"os"
	"path/filepath"
	"strings"
	"testing"

	"github.com/bricejulia/nib/internal/layout"
	"github.com/bricejulia/nib/internal/ui/editor"
	"github.com/bricejulia/nib/internal/ui/filetree"
)

// nullWindow is just enough layout.Window to let Render populate the tree's
// row cache; nothing here asserts on what's drawn.
type nullWindow struct{ cols, rows int }

func (w nullWindow) Size() (int, int)             { return w.cols, w.rows }
func (nullWindow) Println(int, ...layout.Segment) {}
func (nullWindow) Clear()                         {}

// wire mirrors cmd/nib/main.go: the tree's mutation callbacks fanned out
// over the editor panes sharing one buffer store.
func wire(tree *filetree.View, panes ...*editor.View) {
	tree.OnPathMoved = func(oldPath, newPath string) {
		for _, p := range panes {
			p.Repath(oldPath, newPath)
		}
	}
	tree.OnPathDeleted = func(path string) {
		for _, p := range panes {
			p.CloseTabsUnder(path)
		}
	}
}

func key(text string) layout.Key { return layout.Key{Text: text} }

func typeText(v *filetree.View, s string) {
	for _, r := range s {
		v.HandleKey(key(string(r)))
	}
}

// selectRowByName presses Down until the tree's rendered selection is name,
// using only the exported API (this is an external test package).
func selectRowByName(t *testing.T, v *filetree.View, w layout.Window, name string) {
	t.Helper()
	for range 50 {
		if opened := probeSelection(v, w); opened == name {
			return
		}
		v.HandleKey(layout.Key{Named: layout.KeyDown})
	}
	t.Fatalf("never landed on a row named %q", name)
}

// probeSelection reports the base name of whatever row the cursor is on, by
// asking the tree to "open" it and catching the path — the only way to
// observe the selection from outside the package.
func probeSelection(v *filetree.View, w layout.Window) string {
	var got string
	prev := v.OnOpen
	v.OnOpen = func(path string) { got = filepath.Base(path) }
	v.Render(w)
	v.HandleKey(layout.Key{Named: layout.KeyEnter})
	v.OnOpen = prev
	return got
}

// The headline bug, across the real package boundary: rename a file that's
// open in an editor pane, then save, and the bytes must land at the new path
// with nothing recreated at the old one.
func TestRenameFromTheTreeRedirectsTheEditorsSave(t *testing.T) {
	root := t.TempDir()
	oldPath := filepath.Join(root, "a.txt")
	if err := os.WriteFile(oldPath, []byte("one\n"), 0o644); err != nil {
		t.Fatal(err)
	}

	tree := filetree.New(root)
	pane := editor.NewView()
	pane.SetBufferStore(editor.NewBufferStore())
	wire(tree, pane)

	w := nullWindow{cols: 40, rows: 10}
	tree.Render(w)
	selectRowByName(t, tree, w, "a.txt")
	pane.Open(oldPath)

	tree.HandleKey(key("r"))
	for range "a.txt" {
		tree.HandleKey(layout.Key{Named: layout.KeyBackspace})
	}
	typeText(tree, "renamed.txt")
	tree.HandleKey(layout.Key{Named: layout.KeyEnter})

	newPath := filepath.Join(root, "renamed.txt")
	if got := pane.OpenPaths(); len(got) != 1 || got[0] != newPath {
		t.Fatalf("editor open paths = %v, want [%s]", got, newPath)
	}

	// Edit and save through the editor's own keys (Insert mode, then Ctrl+s).
	pane.HandleKey(key("i"))
	pane.HandleKey(key("x"))
	pane.HandleKey(layout.Key{Named: layout.KeyEsc})
	pane.HandleKey(layout.Key{Text: "s", Mods: layout.ModCtrl})

	got, err := os.ReadFile(newPath)
	if err != nil {
		t.Fatalf("read renamed file: %v", err)
	}
	if !strings.HasPrefix(string(got), "xone") {
		t.Errorf("renamed file = %q, want the edit saved into it", got)
	}
	if _, err := os.Stat(oldPath); !os.IsNotExist(err) {
		t.Error("saving must not recreate the file at its old path")
	}
}

func TestDeleteFromTheTreeClosesTheEditorTab(t *testing.T) {
	root := t.TempDir()
	path := filepath.Join(root, "a.txt")
	if err := os.WriteFile(path, []byte("one\n"), 0o644); err != nil {
		t.Fatal(err)
	}

	tree := filetree.New(root)
	pane := editor.NewView()
	pane.SetBufferStore(editor.NewBufferStore())
	wire(tree, pane)

	w := nullWindow{cols: 40, rows: 10}
	tree.Render(w)
	selectRowByName(t, tree, w, "a.txt")
	pane.Open(path)

	tree.HandleKey(key("d"))
	tree.HandleKey(key("y"))

	if _, err := os.Stat(path); !os.IsNotExist(err) {
		t.Fatal("the file should be gone")
	}
	if got := pane.OpenPaths(); len(got) != 0 {
		t.Errorf("editor still has %v open, want the tab closed", got)
	}
}

// An unsaved buffer is never silently discarded: the tab stays, marked, and
// a save writes the file back.
func TestDeleteFromTheTreeKeepsAnUnsavedBufferOpen(t *testing.T) {
	root := t.TempDir()
	path := filepath.Join(root, "a.txt")
	if err := os.WriteFile(path, []byte("one\n"), 0o644); err != nil {
		t.Fatal(err)
	}

	tree := filetree.New(root)
	pane := editor.NewView()
	pane.SetBufferStore(editor.NewBufferStore())
	wire(tree, pane)

	w := nullWindow{cols: 40, rows: 10}
	tree.Render(w)
	selectRowByName(t, tree, w, "a.txt")
	pane.Open(path)
	pane.HandleKey(key("i"))
	pane.HandleKey(key("x"))
	pane.HandleKey(layout.Key{Named: layout.KeyEsc})

	tree.HandleKey(key("d"))
	tree.HandleKey(key("y"))

	if got := pane.OpenPaths(); len(got) != 1 || got[0] != path {
		t.Fatalf("editor open paths = %v, want the unsaved buffer kept", got)
	}
	if got := pane.StatusText(); !strings.Contains(got, "-- DELETED --") {
		t.Errorf("StatusText() = %q, want it to report the file is gone", got)
	}

	pane.HandleKey(layout.Key{Text: "s", Mods: layout.ModCtrl})
	got, err := os.ReadFile(path)
	if err != nil {
		t.Fatalf("save should have recreated the file: %v", err)
	}
	if !strings.HasPrefix(string(got), "xone") {
		t.Errorf("restored file = %q, want the unsaved edit", got)
	}
}

// Creating from the tree puts the file on disk and selects it, so Enter
// opens the thing that was just made.
func TestCreateFromTheTreeSelectsTheNewFile(t *testing.T) {
	root := t.TempDir()
	if err := os.WriteFile(filepath.Join(root, "a.txt"), nil, 0o644); err != nil {
		t.Fatal(err)
	}

	tree := filetree.New(root)
	w := nullWindow{cols: 40, rows: 10}
	tree.Render(w)

	tree.HandleKey(key("a"))
	typeText(tree, "pkg/new.go")
	tree.HandleKey(layout.Key{Named: layout.KeyEnter})

	if _, err := os.Stat(filepath.Join(root, "pkg", "new.go")); err != nil {
		t.Fatalf("expected pkg/new.go: %v", err)
	}
	if got := probeSelection(tree, w); got != "new.go" {
		t.Errorf("selection = %q, want new.go", got)
	}
}
