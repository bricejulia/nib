package editor

import (
	"os"
	"path/filepath"
	"strings"
	"testing"

	"github.com/bricejulia/nib/internal/vcs/gitstatus"
)

// writeTemp creates dir/name (creating intermediate directories) with body,
// and returns its absolute path.
func writeTemp(t *testing.T, dir, name, body string) string {
	t.Helper()
	p := filepath.Join(dir, filepath.FromSlash(name))
	if err := os.MkdirAll(filepath.Dir(p), 0o755); err != nil {
		t.Fatal(err)
	}
	if err := os.WriteFile(p, []byte(body), 0o644); err != nil {
		t.Fatal(err)
	}
	return p
}

// The bug this whole feature exists to prevent: after a rename, a tab that
// still carries the old path makes ":w" recreate the file where it used to
// be.
func TestViewRepathRedirectsSave(t *testing.T) {
	dir := t.TempDir()
	oldPath := writeTemp(t, dir, "a.txt", "one\n")
	newPath := filepath.Join(dir, "b.txt")

	v := NewView()
	v.Open(oldPath)
	if err := os.Rename(oldPath, newPath); err != nil {
		t.Fatal(err)
	}
	if got := v.Repath(oldPath, newPath); got != 1 {
		t.Fatalf("Repath updated %d tabs, want 1", got)
	}

	tb := v.activeTab()
	if tb.path != newPath {
		t.Errorf("tab.path = %q, want %q", tb.path, newPath)
	}
	tb.buf.InsertText(0, 0, "x")
	v.saveActive()

	got, err := os.ReadFile(newPath)
	if err != nil {
		t.Fatalf("read new path: %v", err)
	}
	if !strings.HasPrefix(string(got), "xone") {
		t.Errorf("new file content = %q, want the edit", got)
	}
	if _, err := os.Stat(oldPath); !os.IsNotExist(err) {
		t.Error("saving must not recreate the file at its old path")
	}
}

func TestViewRepathOfADirectoryUpdatesEveryTabUnderneath(t *testing.T) {
	dir := t.TempDir()
	a := writeTemp(t, dir, "d/a.txt", "a")
	b := writeTemp(t, dir, "d/sub/b.txt", "b")
	// A sibling directory whose name merely starts with the moved one's —
	// the case a bare prefix check would wrongly rewrite.
	c := writeTemp(t, dir, "dx/c.txt", "c")

	v := NewView()
	v.Open(a)
	v.Open(b)
	v.Open(c)

	oldDir := filepath.Join(dir, "d")
	newDir := filepath.Join(dir, "d2")
	if err := os.Rename(oldDir, newDir); err != nil {
		t.Fatal(err)
	}
	if got := v.Repath(oldDir, newDir); got != 2 {
		t.Fatalf("Repath updated %d tabs, want 2", got)
	}

	want := []string{
		filepath.Join(newDir, "a.txt"),
		filepath.Join(newDir, "sub", "b.txt"),
		c,
	}
	for i, tb := range v.tabs {
		if tb.path != want[i] {
			t.Errorf("tab %d path = %q, want %q", i, tb.path, want[i])
		}
		if tb.buf.Path != want[i] {
			t.Errorf("tab %d buffer path = %q, want %q", i, tb.buf.Path, want[i])
		}
	}
}

// Two panes sharing one store: the entry must move exactly once and keep its
// reference count, so closing both panes still drains the store.
func TestViewRepathAcrossTwoPanesRekeysTheStoreOnce(t *testing.T) {
	dir := t.TempDir()
	oldPath := writeTemp(t, dir, "a.txt", "hi")
	newPath := filepath.Join(dir, "b.txt")

	store := NewBufferStore()
	v1, v2 := NewView(), NewView()
	v1.SetBufferStore(store)
	v2.SetBufferStore(store)
	v1.Open(oldPath)
	v2.Open(oldPath)
	if store.Len() != 1 {
		t.Fatalf("store.Len() = %d, want 1", store.Len())
	}

	if err := os.Rename(oldPath, newPath); err != nil {
		t.Fatal(err)
	}
	v1.Repath(oldPath, newPath)
	v2.Repath(oldPath, newPath)

	if store.Len() != 1 {
		t.Fatalf("store.Len() = %d, want 1", store.Len())
	}
	if v1.tabs[0].path != newPath || v2.tabs[0].path != newPath {
		t.Errorf("tab paths = %q / %q, want both %q", v1.tabs[0].path, v2.tabs[0].path, newPath)
	}
	if v1.tabs[0].buf != v2.tabs[0].buf {
		t.Error("both panes should still share the same *Buffer")
	}

	v1.CloseAllTabs()
	v2.CloseAllTabs()
	if store.Len() != 0 {
		t.Errorf("store.Len() = %d after closing both panes, want 0 — the refcount was lost", store.Len())
	}
}

func TestViewRepathReopensWithTheLanguageServer(t *testing.T) {
	dir := t.TempDir()
	oldPath := writeTemp(t, dir, "a.go", "package main\n")
	newPath := filepath.Join(dir, "b.go")

	fake := &fakeLSP{ready: true}
	v := NewView()
	v.lsp = fake
	v.Open(oldPath)
	fake.opened, fake.openedLangs, fake.closed = nil, nil, nil

	v.Repath(oldPath, newPath)

	if len(fake.closed) != 1 || fake.closed[0] != oldPath {
		t.Errorf("closed = %v, want just the old path", fake.closed)
	}
	if len(fake.opened) != 1 || fake.opened[0] != newPath {
		t.Errorf("opened = %v, want just the new path", fake.opened)
	}
	if len(fake.openedLangs) != 1 || fake.openedLangs[0] != "go" {
		t.Errorf("opened languages = %v, want [go]", fake.openedLangs)
	}
}

func TestViewRepathToAnUnservedLanguageOnlyCloses(t *testing.T) {
	dir := t.TempDir()
	oldPath := writeTemp(t, dir, "a.go", "package main\n")
	newPath := filepath.Join(dir, "a.xyz") // an extension no grammar claims

	fake := &fakeLSP{ready: true}
	v := NewView()
	v.lsp = fake
	v.Open(oldPath)
	fake.opened, fake.closed = nil, nil

	v.Repath(oldPath, newPath)

	if len(fake.closed) != 1 || fake.closed[0] != oldPath {
		t.Errorf("closed = %v, want the old path", fake.closed)
	}
	if len(fake.opened) != 0 {
		t.Errorf("opened = %v, want nothing (no language, so no server)", fake.opened)
	}
}

func TestViewRepathClearsDiagnosticsWhenTheLanguageChanges(t *testing.T) {
	dir := t.TempDir()
	oldPath := writeTemp(t, dir, "a.go", "package main\nfunc(\n")
	newPath := filepath.Join(dir, "a.txt")

	v := NewView()
	v.Open(oldPath)
	if len(v.activeTab().diagnostics) == 0 {
		t.Fatal("expected tree-sitter parse-error diagnostics on a broken Go file")
	}

	v.Repath(oldPath, newPath)

	if got := len(v.activeTab().diagnostics); got != 0 {
		t.Errorf("%d diagnostics survived a language change, want 0", got)
	}
}

func TestViewRepathRewritesTheJumpStack(t *testing.T) {
	dir := t.TempDir()
	oldPath := writeTemp(t, dir, "d/a.txt", "a")
	other := writeTemp(t, dir, "other.txt", "o")

	v := NewView()
	v.Open(oldPath)
	v.jumpStack = []jumpLocation{{path: oldPath, ln: 3}, {path: other, ln: 1}}

	oldDir := filepath.Join(dir, "d")
	newDir := filepath.Join(dir, "d2")
	v.Repath(oldDir, newDir)

	if want := filepath.Join(newDir, "a.txt"); v.jumpStack[0].path != want {
		t.Errorf("jumpStack[0].path = %q, want %q", v.jumpStack[0].path, want)
	}
	if v.jumpStack[0].ln != 3 {
		t.Errorf("jumpStack[0].ln = %d, want it preserved at 3", v.jumpStack[0].ln)
	}
	if v.jumpStack[1].path != other {
		t.Errorf("jumpStack[1].path = %q, want it untouched", v.jumpStack[1].path)
	}
}

// Defensive: the file tree refuses a move onto an existing path, but if one
// ever got through, two tabs must not end up on the same path.
func TestViewRepathReplacesACleanTabAlreadyOnTheDestination(t *testing.T) {
	dir := t.TempDir()
	src := writeTemp(t, dir, "a.txt", "a")
	dst := writeTemp(t, dir, "b.txt", "b")

	v := NewView()
	v.Open(src)
	v.Open(dst)
	if err := os.Rename(src, dst); err != nil {
		t.Fatal(err)
	}

	v.Repath(src, dst)

	if len(v.tabs) != 1 {
		t.Fatalf("%d tabs, want 1", len(v.tabs))
	}
	if v.tabs[0].path != dst {
		t.Errorf("tab path = %q, want %q", v.tabs[0].path, dst)
	}
	if v.active != 0 {
		t.Errorf("active = %d, want 0", v.active)
	}
	if v.store.Len() != 1 {
		t.Errorf("store.Len() = %d, want 1", v.store.Len())
	}
}

func TestViewRepathLeavesAFailedLoadTabConsistent(t *testing.T) {
	dir := t.TempDir()
	missing := filepath.Join(dir, "gone.txt")
	moved := filepath.Join(dir, "moved.txt")

	v := NewView()
	v.Open(missing) // a tab with err set and no buffer
	if v.activeTab().buf != nil {
		t.Fatal("expected a nil buffer for a missing file")
	}

	if got := v.Repath(missing, moved); got != 1 {
		t.Fatalf("Repath updated %d tabs, want 1", got)
	}
	if v.activeTab().path != moved {
		t.Errorf("tab.path = %q, want %q", v.activeTab().path, moved)
	}
	if v.store.Len() != 0 {
		t.Errorf("store.Len() = %d, want 0 (a failed load never registered)", v.store.Len())
	}
}

func TestViewCloseTabsUnderClosesCleanTabsAndReleasesThem(t *testing.T) {
	dir := t.TempDir()
	a := writeTemp(t, dir, "d/a.txt", "a")
	b := writeTemp(t, dir, "d/sub/b.txt", "b")
	outside := writeTemp(t, dir, "keep.txt", "k")

	fake := &fakeLSP{ready: true}
	v := NewView()
	v.lsp = fake
	v.Open(a)
	v.Open(b)
	v.Open(outside)
	fake.closed = nil

	deleted := filepath.Join(dir, "d")
	if err := os.RemoveAll(deleted); err != nil {
		t.Fatal(err)
	}
	if detached := v.CloseTabsUnder(deleted); len(detached) != 0 {
		t.Errorf("detached = %v, want none (no unsaved changes)", detached)
	}

	if len(v.tabs) != 1 || v.tabs[0].path != outside {
		t.Fatalf("tabs = %v, want only the file outside the deleted directory", v.OpenPaths())
	}
	if v.store.Len() != 1 {
		t.Errorf("store.Len() = %d, want 1", v.store.Len())
	}
	if len(fake.closed) != 2 {
		t.Errorf("closed %d documents with the language server, want 2: %v", len(fake.closed), fake.closed)
	}
}

// Deleting a file must never be a way to silently discard unsaved edits.
func TestViewCloseTabsUnderKeepsADirtyTabDetached(t *testing.T) {
	dir := t.TempDir()
	path := writeTemp(t, dir, "a.txt", "one\n")

	fake := &fakeLSP{ready: true}
	v := NewView()
	v.lsp = fake
	v.Open(path)
	tb := v.activeTab()
	tb.buf.InsertText(0, 0, "x")
	tb.lineStatus = map[int]gitstatus.LineStatus{0: gitstatus.LineModified}
	fake.closed = nil

	if err := os.Remove(path); err != nil {
		t.Fatal(err)
	}
	detached := v.CloseTabsUnder(path)

	if len(detached) != 1 || detached[0] != path {
		t.Fatalf("detached = %v, want [%s]", detached, path)
	}
	if len(v.tabs) != 1 {
		t.Fatalf("%d tabs, want the dirty one kept", len(v.tabs))
	}
	if !tb.detached {
		t.Error("the surviving tab should be flagged detached")
	}
	if tb.lineStatus != nil {
		t.Error("the git gutter should be cleared: there's no file left to diff against")
	}
	if v.store.Len() != 1 {
		t.Errorf("store.Len() = %d, want 1 (the buffer is still open)", v.store.Len())
	}
	if len(fake.closed) != 0 {
		t.Errorf("closed = %v, want none: an open document with unsaved content stays open to the server", fake.closed)
	}

	// The status bar and the tab bar are the only places the user can find
	// this out without opening the debug log.
	if got := v.StatusText(); !strings.Contains(got, "-- DELETED --") {
		t.Errorf("StatusText() = %q, want a DELETED indicator", got)
	}
	if got := tabDisplayNames(v.tabs)[0]; !strings.Contains(got, "✗") {
		t.Errorf("tab label = %q, want a detached marker", got)
	}

	// Saving recreates the file, so the tab is no longer detached.
	v.saveActive()
	if _, err := os.Stat(path); err != nil {
		t.Fatalf("save should have recreated the file: %v", err)
	}
	if tb.detached {
		t.Error("a successful save should clear the detached flag")
	}
}

func TestDetachedPathsReturnsOnlyDetachedTabs(t *testing.T) {
	dir := t.TempDir()
	kept := writeTemp(t, dir, "kept.txt", "one\n")
	deleted := writeTemp(t, dir, "deleted.txt", "two\n")

	v := NewView()
	v.Open(kept)
	v.Open(deleted)
	v.activeTab().buf.InsertText(0, 0, "x")

	if err := os.Remove(deleted); err != nil {
		t.Fatal(err)
	}
	v.CloseTabsUnder(deleted)

	got := v.DetachedPaths()
	if len(got) != 1 || got[0] != deleted {
		t.Fatalf("DetachedPaths() = %v, want [%s]", got, deleted)
	}
}

func TestViewCloseTabsUnderMatchesOnlyRealChildren(t *testing.T) {
	dir := t.TempDir()
	keep := writeTemp(t, dir, "foobar/x.txt", "x")
	gone := writeTemp(t, dir, "foo/y.txt", "y")

	v := NewView()
	v.Open(keep)
	v.Open(gone)

	v.CloseTabsUnder(filepath.Join(dir, "foo"))

	if len(v.tabs) != 1 || v.tabs[0].path != keep {
		t.Errorf("open paths = %v, want only %q — /foobar is not under /foo", v.OpenPaths(), keep)
	}
}

func TestViewCloseTabsUnderKeepsTheActiveTabStable(t *testing.T) {
	dir := t.TempDir()
	first := writeTemp(t, dir, "d/a.txt", "a")
	second := writeTemp(t, dir, "b.txt", "b")
	third := writeTemp(t, dir, "c.txt", "c")

	v := NewView()
	v.Open(first)
	v.Open(second)
	v.Open(third)
	wasActive := v.activeTab()

	v.CloseTabsUnder(filepath.Join(dir, "d"))

	if v.activeTab() != wasActive {
		t.Errorf("active tab changed to %q, want %q", v.activeTab().path, third)
	}
}

func TestViewCloseTabsUnderFiresOnAllTabsClosed(t *testing.T) {
	dir := t.TempDir()
	path := writeTemp(t, dir, "a.txt", "a")

	v := NewView()
	fired := 0
	v.OnAllTabsClosed = func() { fired++ }
	v.Open(path)

	v.CloseTabsUnder(path)

	if len(v.tabs) != 0 {
		t.Fatalf("%d tabs, want 0", len(v.tabs))
	}
	if fired != 1 {
		t.Errorf("OnAllTabsClosed fired %d times, want 1", fired)
	}
}

func TestViewCloseTabsUnderDropsJumpStackEntriesIntoTheDeletedTree(t *testing.T) {
	dir := t.TempDir()
	gone := writeTemp(t, dir, "d/a.txt", "a")
	keep := writeTemp(t, dir, "b.txt", "b")

	v := NewView()
	v.Open(keep)
	v.jumpStack = []jumpLocation{{path: gone, ln: 1}, {path: keep, ln: 2}}

	v.CloseTabsUnder(filepath.Join(dir, "d"))

	if len(v.jumpStack) != 1 || v.jumpStack[0].path != keep {
		t.Errorf("jumpStack = %v, want only the surviving path", v.jumpStack)
	}
}

// The rebuild half of the rename contract: the store drops highlighting
// keyed on the old extension (see TestBufferStoreRekeyDropsHighlightingWhenTheLanguageChanges),
// and the View is what puts the new language's colors back.
func TestViewRepathRehighlightsWhenTheLanguageChanges(t *testing.T) {
	dir := t.TempDir()
	oldPath := writeTemp(t, dir, "x.txt", "package main\n\nfunc main() {}\n")
	newPath := filepath.Join(dir, "x.go")

	v := NewView()
	v.Open(oldPath)
	if v.activeTab().buf.highlighted != nil {
		t.Fatal("a .txt file should have no tree-sitter highlighting to start with")
	}
	if err := os.Rename(oldPath, newPath); err != nil {
		t.Fatal(err)
	}

	if got := v.Repath(oldPath, newPath); got != 1 {
		t.Fatalf("Repath updated %d tabs, want 1", got)
	}
	if v.activeTab().buf.highlighted == nil {
		t.Error("renaming .txt -> .go should have produced Go highlighting")
	}
}
