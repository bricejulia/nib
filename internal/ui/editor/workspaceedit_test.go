package editor

import (
	"errors"
	"os"
	"path/filepath"
	"testing"

	"github.com/bricejulia/nib/internal/lsp"
)

func TestApplyWorkspaceEditRoutesToOpenTabAndOpensBackgroundFile(t *testing.T) {
	dir := t.TempDir()
	closedPath := filepath.Join(dir, "closed.txt")
	if err := os.WriteFile(closedPath, []byte("foo"), 0o644); err != nil {
		t.Fatal(err)
	}

	v := NewView()
	openBuf := &Buffer{Path: "/open/f.go", Lines: []string{"foo"}}
	openTab := &tab{path: "/open/f.go", buf: openBuf}
	v.tabs = []*tab{openTab}
	v.active = 0

	findPane := func(absPath string) (*View, bool) {
		if absPath == "/open/f.go" {
			return v, true
		}
		return nil, false
	}

	edit := lsp.WorkspaceEdit{Changes: map[string][]lsp.TextEdit{
		pathURIForTest("/open/f.go"): {{
			Range:   lsp.Range{Start: lsp.Position{Character: 0}, End: lsp.Position{Character: 3}},
			NewText: "bar",
		}},
		pathURIForTest(closedPath): {{
			Range:   lsp.Range{Start: lsp.Position{Character: 0}, End: lsp.Position{Character: 3}},
			NewText: "bar",
		}},
	}}

	res := ApplyWorkspaceEdit(edit, findPane, v)

	if res.FilesChanged != 2 {
		t.Errorf("FilesChanged = %d, want 2", res.FilesChanged)
	}
	if len(res.Failed) != 0 {
		t.Errorf("Failed = %+v, want none", res.Failed)
	}
	if openBuf.Lines[0] != "bar" {
		t.Errorf("open buffer's line = %q, want %q", openBuf.Lines[0], "bar")
	}
	if !openBuf.Dirty {
		t.Error("the already-open file should be left dirty, same as any other edit")
	}

	// The file that wasn't open anywhere should now be a background tab in
	// v (the trigger view), with focus left untouched — but, like the
	// already-open file, left dirty rather than auto-saved: nothing about
	// applying a WorkspaceEdit writes to disk on its own.
	if v.active != 0 {
		t.Errorf("active tab = %d, want 0 (opening the background file must not steal focus)", v.active)
	}
	closedTab := v.tabForPath(closedPath)
	if closedTab == nil {
		t.Fatal("expected the previously-closed file to be opened as a tab")
	}
	if closedTab.buf.Lines[0] != "bar" {
		t.Errorf("background tab's line = %q, want %q", closedTab.buf.Lines[0], "bar")
	}
	if !closedTab.buf.Dirty {
		t.Error("the background file should be left dirty too, not auto-saved")
	}
	onDisk, _ := os.ReadFile(closedPath)
	if string(onDisk) != "foo" {
		t.Errorf("closed file's on-disk content = %q, want unchanged %q until it's manually saved", onDisk, "foo")
	}
}

// TestApplyWorkspaceEditOneFileFailureDoesNotAbortTheRest guards the same
// "continue, collect, report" convention replace.go's Apply uses.
func TestApplyWorkspaceEditOneFileFailureDoesNotAbortTheRest(t *testing.T) {
	dir := t.TempDir()
	goodPath := filepath.Join(dir, "good.txt")
	if err := os.WriteFile(goodPath, []byte("foo"), 0o644); err != nil {
		t.Fatal(err)
	}
	badPath := filepath.Join(dir, "does-not-exist.txt")

	v := NewView()
	findPane := func(string) (*View, bool) { return nil, false }
	edit := lsp.WorkspaceEdit{Changes: map[string][]lsp.TextEdit{
		pathURIForTest(goodPath): {{
			Range:   lsp.Range{Start: lsp.Position{Character: 0}, End: lsp.Position{Character: 3}},
			NewText: "bar",
		}},
		pathURIForTest(badPath): {{
			Range:   lsp.Range{Start: lsp.Position{Character: 0}, End: lsp.Position{Character: 3}},
			NewText: "bar",
		}},
	}}

	res := ApplyWorkspaceEdit(edit, findPane, v)

	if res.FilesChanged != 1 {
		t.Errorf("FilesChanged = %d, want 1 (the good file)", res.FilesChanged)
	}
	if _, ok := res.Failed[badPath]; !ok {
		t.Errorf("expected Failed to record an error for %q, got %+v", badPath, res.Failed)
	}
	if !errors.Is(res.Failed[badPath], os.ErrNotExist) {
		t.Errorf("expected an ErrNotExist-flavored error, got %v", res.Failed[badPath])
	}

	goodTab := v.tabForPath(goodPath)
	if goodTab == nil {
		t.Fatal("expected the good file to be opened as a tab despite the other one failing")
	}
	if goodTab.buf.Lines[0] != "bar" {
		t.Errorf("good file's content = %q, want %q — one failure must not block the rest", goodTab.buf.Lines[0], "bar")
	}
}

func TestApplyWorkspaceEditAppliesMultipleEditsInReverseOrderWithoutCorruption(t *testing.T) {
	// Same reverse-order requirement applyTextEdits guards, exercised
	// through the background-open path.
	dir := t.TempDir()
	path := filepath.Join(dir, "f.txt")
	if err := os.WriteFile(path, []byte("aaaa\nbbbb\ncccc\n"), 0o644); err != nil {
		t.Fatal(err)
	}

	edit := lsp.WorkspaceEdit{Changes: map[string][]lsp.TextEdit{
		pathURIForTest(path): {
			{Range: lsp.Range{Start: lsp.Position{Line: 0, Character: 0}, End: lsp.Position{Line: 0, Character: 4}}, NewText: "AAAA\nEXTRA"},
			{Range: lsp.Range{Start: lsp.Position{Line: 2, Character: 0}, End: lsp.Position{Line: 2, Character: 4}}, NewText: "CCCC"},
		},
	}}

	v := NewView()
	res := ApplyWorkspaceEdit(edit, func(string) (*View, bool) { return nil, false }, v)
	if res.FilesChanged != 1 || len(res.Failed) != 0 {
		t.Fatalf("res = %+v", res)
	}

	tb := v.tabForPath(path)
	if tb == nil {
		t.Fatal("expected the file to be opened as a tab")
	}
	want := []string{"AAAA", "EXTRA", "bbbb", "CCCC"}
	if len(tb.buf.Lines) != len(want) {
		t.Fatalf("lines = %q, want %q", tb.buf.Lines, want)
	}
	for i, line := range want {
		if tb.buf.Lines[i] != line {
			t.Errorf("line %d = %q, want %q", i, tb.buf.Lines[i], line)
		}
	}
}

func newGroupedEditFixture(t *testing.T) (v *View, openTab *tab, closedPath string) {
	t.Helper()
	dir := t.TempDir()
	closedPath = filepath.Join(dir, "closed.txt")
	if err := os.WriteFile(closedPath, []byte("foo"), 0o644); err != nil {
		t.Fatal(err)
	}

	v = NewView()
	openBuf := &Buffer{Path: "/open/f.go", Lines: []string{"foo"}}
	openTab = &tab{path: "/open/f.go", buf: openBuf}
	v.tabs = []*tab{openTab}
	v.active = 0

	findPane := func(absPath string) (*View, bool) {
		if absPath == "/open/f.go" {
			return v, true
		}
		return nil, false
	}
	edit := lsp.WorkspaceEdit{Changes: map[string][]lsp.TextEdit{
		pathURIForTest("/open/f.go"): {{Range: lsp.Range{Start: lsp.Position{Character: 0}, End: lsp.Position{Character: 3}}, NewText: "bar"}},
		pathURIForTest(closedPath):   {{Range: lsp.Range{Start: lsp.Position{Character: 0}, End: lsp.Position{Character: 3}}, NewText: "bar"}},
	}}
	if res := ApplyWorkspaceEdit(edit, findPane, v); len(res.Failed) != 0 {
		t.Fatalf("apply failed: %+v", res.Failed)
	}
	return v, openTab, closedPath
}

func TestApplyWorkspaceEditUndoRevertsEveryFileAtOnce(t *testing.T) {
	v, openTab, closedPath := newGroupedEditFixture(t)
	closedTab := v.tabForPath(closedPath)
	if closedTab == nil {
		t.Fatal("expected the closed file to be opened as a tab")
	}

	// Undo from the file the rename was actually done in — not the
	// background file — matching how the user actually triggers it.
	v.undo(openTab)

	if openTab.buf.Lines[0] != "foo" {
		t.Errorf("open file's line = %q, want reverted to %q", openTab.buf.Lines[0], "foo")
	}
	if closedTab.buf.Lines[0] != "foo" {
		t.Errorf("background file's line = %q, want reverted to %q", closedTab.buf.Lines[0], "foo")
	}
	if v.active != 0 {
		t.Errorf("active tab = %d, want 0 (undo must not change focus)", v.active)
	}
}

func TestApplyWorkspaceEditUndoFromTheBackgroundFileAlsoRevertsEverything(t *testing.T) {
	v, openTab, closedPath := newGroupedEditFixture(t)
	closedTab := v.tabForPath(closedPath)
	if closedTab == nil {
		t.Fatal("expected the closed file to be opened as a tab")
	}

	// This time undo from the OTHER file the rename touched — the one that
	// only just got opened — not the one the user actually triggered it in.
	v.undo(closedTab)

	if openTab.buf.Lines[0] != "foo" {
		t.Errorf("open file's line = %q, want reverted to %q", openTab.buf.Lines[0], "foo")
	}
	if closedTab.buf.Lines[0] != "foo" {
		t.Errorf("background file's line = %q, want reverted to %q", closedTab.buf.Lines[0], "foo")
	}
}

func TestApplyWorkspaceEditUndoLeavesASiblingWithNewerEditsAlone(t *testing.T) {
	v, openTab, closedPath := newGroupedEditFixture(t)
	closedTab := v.tabForPath(closedPath)
	if closedTab == nil {
		t.Fatal("expected the closed file to be opened as a tab")
	}

	// The background file gets an unrelated edit after the rename.
	before := snapshotTab(closedTab)
	closedTab.buf.Restore([]string{"unrelated"})
	v.pushUndoIfChanged(closedTab, before)

	v.undo(openTab)

	if openTab.buf.Lines[0] != "foo" {
		t.Errorf("open file's line = %q, want reverted to %q", openTab.buf.Lines[0], "foo")
	}
	if closedTab.buf.Lines[0] != "unrelated" {
		t.Errorf("background file's later edit was clobbered: got %q, want %q", closedTab.buf.Lines[0], "unrelated")
	}
}

func TestApplyWorkspaceEditRedoReappliesEveryFileAtOnce(t *testing.T) {
	v, openTab, closedPath := newGroupedEditFixture(t)
	closedTab := v.tabForPath(closedPath)

	v.undo(openTab)
	v.redo(openTab)

	if openTab.buf.Lines[0] != "bar" {
		t.Errorf("open file's line = %q, want reapplied to %q", openTab.buf.Lines[0], "bar")
	}
	if closedTab.buf.Lines[0] != "bar" {
		t.Errorf("background file's line = %q, want reapplied to %q", closedTab.buf.Lines[0], "bar")
	}
}
