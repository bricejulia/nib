package editor

import (
	"errors"
	"os"
	"path/filepath"
	"testing"

	"github.com/bricejulia/nib/internal/lsp"
)

func TestApplyWorkspaceEditRoutesToOpenBufferAndDisk(t *testing.T) {
	dir := t.TempDir()
	closedPath := filepath.Join(dir, "closed.txt")
	if err := os.WriteFile(closedPath, []byte("foo\n"), 0o644); err != nil {
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

	res := ApplyWorkspaceEdit(edit, findPane)

	if res.FilesChanged != 2 {
		t.Errorf("FilesChanged = %d, want 2", res.FilesChanged)
	}
	if len(res.Failed) != 0 {
		t.Errorf("Failed = %+v, want none", res.Failed)
	}
	if openBuf.Lines[0] != "bar" {
		t.Errorf("open buffer's line = %q, want %q", openBuf.Lines[0], "bar")
	}
	onDisk, _ := os.ReadFile(closedPath)
	if string(onDisk) != "bar" {
		t.Errorf("closed file's content = %q, want %q", onDisk, "bar")
	}
}

// TestApplyWorkspaceEditOneFileFailureDoesNotAbortTheRest guards the same
// "continue, collect, report" convention replace.go's Apply uses.
func TestApplyWorkspaceEditOneFileFailureDoesNotAbortTheRest(t *testing.T) {
	dir := t.TempDir()
	goodPath := filepath.Join(dir, "good.txt")
	if err := os.WriteFile(goodPath, []byte("foo\n"), 0o644); err != nil {
		t.Fatal(err)
	}
	badPath := filepath.Join(dir, "does-not-exist.txt")

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

	res := ApplyWorkspaceEdit(edit, findPane)

	if res.FilesChanged != 1 {
		t.Errorf("FilesChanged = %d, want 1 (the good file)", res.FilesChanged)
	}
	if _, ok := res.Failed[badPath]; !ok {
		t.Errorf("expected Failed to record an error for %q, got %+v", badPath, res.Failed)
	}
	if !errors.Is(res.Failed[badPath], os.ErrNotExist) {
		t.Errorf("expected an ErrNotExist-flavored error, got %v", res.Failed[badPath])
	}

	onDisk, _ := os.ReadFile(goodPath)
	if string(onDisk) != "bar" {
		t.Errorf("good file's content = %q, want %q — one failure must not block the rest", onDisk, "bar")
	}
}

func TestApplyWorkspaceEditAppliesMultipleEditsInReverseOrderWithoutCorruption(t *testing.T) {
	// Same reverse-order requirement applyTextEdits guards, exercised
	// through the disk-write path (rewriteFileWithTextEdits).
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

	res := ApplyWorkspaceEdit(edit, func(string) (*View, bool) { return nil, false })
	if res.FilesChanged != 1 || len(res.Failed) != 0 {
		t.Fatalf("res = %+v", res)
	}

	got, err := os.ReadFile(path)
	if err != nil {
		t.Fatal(err)
	}
	want := "AAAA\nEXTRA\nbbbb\nCCCC"
	if string(got) != want {
		t.Errorf("on-disk content = %q, want %q", got, want)
	}
}
