package filetree

import (
	"os"
	"path/filepath"
	"testing"

	"github.com/bricejulia/kiwi/internal/vcs/gitstatus"
)

func fixtureRoot(t *testing.T) string {
	t.Helper()
	abs, err := filepath.Abs("../../../testdata/filetree_fixture")
	if err != nil {
		t.Fatal(err)
	}
	return abs
}

func TestEnsureLoadedPopulatesImmediateChildrenOnly(t *testing.T) {
	root := NewRoot(fixtureRoot(t))
	if err := root.EnsureLoaded(); err != nil {
		t.Fatalf("EnsureLoaded: %v", err)
	}
	if !root.Loaded {
		t.Fatal("root should be marked Loaded")
	}

	names := map[string]*Node{}
	for _, c := range root.Children {
		names[c.Name] = c
	}
	for _, want := range []string{"a.txt", "b.txt", "sub", "vendor"} {
		if _, ok := names[want]; !ok {
			t.Errorf("missing expected child %q, got %v", want, names)
		}
	}

	sub, ok := names["sub"]
	if !ok || !sub.IsDir {
		t.Fatalf("expected sub to be a loaded-eligible directory node")
	}
	if sub.Loaded {
		t.Errorf("sub should NOT be loaded until it is expanded — lazy-load contract violated")
	}
	vendor, ok := names["vendor"]
	if !ok || vendor.Loaded {
		t.Errorf("vendor should not be eagerly walked at root load time")
	}
}

func TestEnsureLoadedIsIdempotent(t *testing.T) {
	root := NewRoot(fixtureRoot(t))
	if err := root.EnsureLoaded(); err != nil {
		t.Fatal(err)
	}
	first := root.Children
	if err := root.EnsureLoaded(); err != nil {
		t.Fatal(err)
	}
	if len(root.Children) != len(first) {
		t.Fatalf("second EnsureLoaded call should be a no-op, got different child count")
	}
}

func TestEnsureLoadedSortsDirsFirstThenAlpha(t *testing.T) {
	root := NewRoot(fixtureRoot(t))
	if err := root.EnsureLoaded(); err != nil {
		t.Fatal(err)
	}
	var order []string
	for _, c := range root.Children {
		order = append(order, c.Name)
	}
	// Dirs (sub, vendor) sorted alpha, then files (a.txt, b.txt) sorted alpha.
	want := []string{"sub", "vendor", "a.txt", "b.txt"}
	if len(order) != len(want) {
		t.Fatalf("got order %v, want %v", order, want)
	}
	for i := range want {
		if order[i] != want[i] {
			t.Errorf("got order %v, want %v", order, want)
			break
		}
	}
}

func TestEnsureLoadedPreservesExpandedStateAcrossReload(t *testing.T) {
	// Regression test: reloading a directory (e.g. after an fsnotify
	// refresh invalidates it) must not silently collapse subdirectories
	// that were expanded before the reload.
	root := NewRoot(fixtureRoot(t))
	if err := root.EnsureLoaded(); err != nil {
		t.Fatal(err)
	}

	var sub *Node
	for _, c := range root.Children {
		if c.Name == "sub" {
			sub = c
		}
	}
	if sub == nil {
		t.Fatal(`expected a "sub" child`)
	}
	sub.Expanded = true
	if err := sub.EnsureLoaded(); err != nil {
		t.Fatal(err)
	}
	if len(sub.Children) == 0 {
		t.Fatal("expected sub to have loaded children (c.txt)")
	}

	// Simulate what invalidateExpanded does before a reload.
	root.Loaded = false
	if err := root.EnsureLoaded(); err != nil {
		t.Fatal(err)
	}

	var reloadedSub *Node
	for _, c := range root.Children {
		if c.Name == "sub" {
			reloadedSub = c
		}
	}
	if reloadedSub == nil {
		t.Fatal(`expected "sub" to still exist after reload`)
	}
	if !reloadedSub.Expanded {
		t.Error("sub's Expanded state was lost across a reload — this collapses folders on every fsnotify refresh")
	}
	if !reloadedSub.Loaded {
		t.Error("expected sub's Loaded state to be preserved across a reload")
	}
	if len(reloadedSub.Children) == 0 {
		t.Error("expected sub's previously-loaded children to be preserved across a reload")
	}
}

func TestEnsureLoadedPreservesStatusAcrossReload(t *testing.T) {
	root := NewRoot(fixtureRoot(t))
	if err := root.EnsureLoaded(); err != nil {
		t.Fatal(err)
	}
	for _, c := range root.Children {
		if c.Name == "a.txt" {
			c.Status = gitstatus.Modified
		}
	}

	root.Loaded = false
	if err := root.EnsureLoaded(); err != nil {
		t.Fatal(err)
	}
	for _, c := range root.Children {
		if c.Name == "a.txt" && c.Status != gitstatus.Modified {
			t.Errorf("expected a.txt's Status to survive the reload, got %v", c.Status)
		}
	}
}

func TestEnsureLoadedPicksUpNewAndRemovedFilesOnReload(t *testing.T) {
	dir := t.TempDir()
	mustWrite := func(name string) {
		if err := os.WriteFile(filepath.Join(dir, name), []byte("x"), 0o644); err != nil {
			t.Fatal(err)
		}
	}
	mustWrite("keep.txt")
	mustWrite("remove.txt")

	root := NewRoot(dir)
	if err := root.EnsureLoaded(); err != nil {
		t.Fatal(err)
	}
	if len(root.Children) != 2 {
		t.Fatalf("got %d children, want 2", len(root.Children))
	}

	if err := os.Remove(filepath.Join(dir, "remove.txt")); err != nil {
		t.Fatal(err)
	}
	mustWrite("new.txt")

	root.Loaded = false
	if err := root.EnsureLoaded(); err != nil {
		t.Fatal(err)
	}

	names := map[string]bool{}
	for _, c := range root.Children {
		names[c.Name] = true
	}
	if !names["keep.txt"] {
		t.Error("keep.txt should still be present after reload")
	}
	if names["remove.txt"] {
		t.Error("remove.txt should be gone after reload")
	}
	if !names["new.txt"] {
		t.Error("new.txt should appear after reload")
	}
}

func TestEnsureLoadedOnFileIsNoop(t *testing.T) {
	n := &Node{Path: filepath.Join(fixtureRoot(t), "a.txt"), IsDir: false}
	if err := n.EnsureLoaded(); err != nil {
		t.Fatalf("EnsureLoaded on a file should be a no-op, got error: %v", err)
	}
	if n.Children != nil {
		t.Errorf("a file node should never have children")
	}
}
