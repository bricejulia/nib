package editor

import (
	"os"
	"path/filepath"
	"strings"
	"testing"
)

func fixturePath(t *testing.T, name string) string {
	t.Helper()
	abs, err := filepath.Abs(filepath.Join("../../../testdata", name))
	if err != nil {
		t.Fatal(err)
	}
	return abs
}

func TestLoadSplitsLinesWithoutTrailingNewline(t *testing.T) {
	buf, err := Load(fixturePath(t, "editor_sample.txt"))
	if err != nil {
		t.Fatalf("Load: %v", err)
	}
	if len(buf.Lines) != 4 {
		t.Fatalf("got %d lines, want 4: %+v", len(buf.Lines), buf.Lines)
	}
	if buf.Lines[0] != "line one" {
		t.Errorf("line 0 = %q", buf.Lines[0])
	}
	if buf.Lines[1] != "\ttabbed line" {
		t.Errorf("line 1 should retain its raw tab, got %q", buf.Lines[1])
	}
}

func TestLoadSourceMatchesLinesJoinedForTrailingNewlineFile(t *testing.T) {
	buf, err := Load(fixturePath(t, "editor_sample.txt")) // has a trailing \n on disk
	if err != nil {
		t.Fatalf("Load: %v", err)
	}
	want := strings.Join(buf.Lines, "\n")
	if string(buf.Source) != want {
		t.Errorf("Source = %q, want %q (Lines joined by \\n)", buf.Source, want)
	}
}

func TestLoadSourceMatchesLinesJoinedForNoTrailingNewlineFile(t *testing.T) {
	buf, err := Load(fixturePath(t, "no_trailing_newline.txt")) // no trailing \n on disk
	if err != nil {
		t.Fatalf("Load: %v", err)
	}
	if len(buf.Lines) != 1 || buf.Lines[0] != "no trailing newline" {
		t.Fatalf("got Lines=%+v", buf.Lines)
	}
	want := strings.Join(buf.Lines, "\n")
	if string(buf.Source) != want {
		t.Errorf("Source = %q, want %q", buf.Source, want)
	}
}

func TestLoadMissingFile(t *testing.T) {
	_, err := Load(fixturePath(t, "does-not-exist.txt"))
	if err == nil {
		t.Fatal("expected an error loading a missing file")
	}
}

func TestInsertTextInsertsAtRuneIndexAndMarksDirty(t *testing.T) {
	b := &Buffer{Lines: []string{"abc"}}
	newCol := b.InsertText(0, 1, "XY")
	if b.Lines[0] != "aXYbc" {
		t.Fatalf("Lines[0] = %q, want %q", b.Lines[0], "aXYbc")
	}
	if newCol != 3 {
		t.Fatalf("returned col = %d, want 3", newCol)
	}
	if !b.Dirty {
		t.Fatal("expected Dirty to be true after InsertText")
	}
	if string(b.Source) != "aXYbc" {
		t.Fatalf("Source = %q, want Lines resynced", b.Source)
	}
}

func TestSplitLineSplitsAtRuneIndex(t *testing.T) {
	b := &Buffer{Lines: []string{"one", "abcdef", "three"}}
	b.SplitLine(1, 3)
	want := []string{"one", "abc", "def", "three"}
	if len(b.Lines) != len(want) {
		t.Fatalf("Lines = %+v, want %+v", b.Lines, want)
	}
	for i := range want {
		if b.Lines[i] != want[i] {
			t.Errorf("Lines[%d] = %q, want %q", i, b.Lines[i], want[i])
		}
	}
	if !b.Dirty {
		t.Fatal("expected Dirty to be true after SplitLine")
	}
}

func TestDeleteBackwardMidLineDeletesPrecedingRune(t *testing.T) {
	b := &Buffer{Lines: []string{"abc"}}
	ln, col := b.DeleteBackward(0, 2)
	if ln != 0 || col != 1 {
		t.Fatalf("got (%d,%d), want (0,1)", ln, col)
	}
	if b.Lines[0] != "ac" {
		t.Fatalf("Lines[0] = %q, want %q", b.Lines[0], "ac")
	}
}

func TestDeleteBackwardAtLineStartJoinsWithPrevious(t *testing.T) {
	b := &Buffer{Lines: []string{"foo", "bar"}}
	ln, col := b.DeleteBackward(1, 0)
	if ln != 0 || col != 3 {
		t.Fatalf("got (%d,%d), want (0,3)", ln, col)
	}
	if len(b.Lines) != 1 || b.Lines[0] != "foobar" {
		t.Fatalf("Lines = %+v, want [\"foobar\"]", b.Lines)
	}
}

func TestDeleteBackwardAtBufferStartIsNoop(t *testing.T) {
	b := &Buffer{Lines: []string{"foo"}}
	ln, col := b.DeleteBackward(0, 0)
	if ln != 0 || col != 0 {
		t.Fatalf("got (%d,%d), want (0,0)", ln, col)
	}
	if b.Lines[0] != "foo" {
		t.Fatalf("Lines[0] = %q, want unchanged %q", b.Lines[0], "foo")
	}
	if b.Dirty {
		t.Fatal("expected Dirty to stay false for a no-op delete")
	}
}

func TestSaveWritesSourceAndClearsDirty(t *testing.T) {
	dir := t.TempDir()
	path := filepath.Join(dir, "out.txt")
	if err := os.WriteFile(path, []byte("original"), 0o644); err != nil {
		t.Fatal(err)
	}

	buf, err := Load(path)
	if err != nil {
		t.Fatalf("Load: %v", err)
	}
	buf.InsertText(0, len(buf.Lines[0]), "!")
	if !buf.Dirty {
		t.Fatal("expected Dirty after InsertText")
	}

	if err := buf.Save(); err != nil {
		t.Fatalf("Save: %v", err)
	}
	if buf.Dirty {
		t.Fatal("expected Dirty to clear after Save")
	}

	got, err := os.ReadFile(path)
	if err != nil {
		t.Fatal(err)
	}
	if string(got) != "original!" {
		t.Fatalf("file contents = %q, want %q", got, "original!")
	}
}

func TestSavePreservesFileMode(t *testing.T) {
	dir := t.TempDir()
	path := filepath.Join(dir, "script.sh")
	if err := os.WriteFile(path, []byte("#!/bin/sh"), 0o755); err != nil {
		t.Fatal(err)
	}

	buf, err := Load(path)
	if err != nil {
		t.Fatalf("Load: %v", err)
	}
	buf.InsertText(0, 0, "x")
	if err := buf.Save(); err != nil {
		t.Fatalf("Save: %v", err)
	}

	info, err := os.Stat(path)
	if err != nil {
		t.Fatal(err)
	}
	if info.Mode().Perm() != 0o755 {
		t.Fatalf("file mode = %v, want 0755 preserved", info.Mode().Perm())
	}
}

func TestRestoreReplacesLinesAndResyncsSource(t *testing.T) {
	b := &Buffer{Lines: []string{"old"}, saved: []string{"old"}}
	b.Restore([]string{"new", "lines"})

	if len(b.Lines) != 2 || b.Lines[0] != "new" || b.Lines[1] != "lines" {
		t.Fatalf("Lines = %+v, want [\"new\" \"lines\"]", b.Lines)
	}
	if string(b.Source) != "new\nlines" {
		t.Fatalf("Source = %q, want %q", b.Source, "new\nlines")
	}
}

func TestRestoreRecomputesDirtyAgainstSavedBaseline(t *testing.T) {
	b := &Buffer{Lines: []string{"a"}, saved: []string{"a"}}

	b.Restore([]string{"b"}) // differs from the saved baseline
	if !b.Dirty {
		t.Fatal("expected Restore to set Dirty when the restored content differs from saved")
	}

	b.Restore([]string{"a"}) // matches the saved baseline again
	if b.Dirty {
		t.Fatal("expected Restore to clear Dirty when the restored content matches saved")
	}
}

// TestRestoreDirtyReflectsSaveThatHappenedAfterTheSnapshot is a regression
// test for a real reported bug: edit, exit Insert, Save, undo, Save again,
// redo — the redone content ends up not matching what's now on disk, but
// with a naive "restore the Dirty flag captured at snapshot time" the tab
// incorrectly showed no unsaved changes. Dirty must always be computed
// against the CURRENT saved baseline, not one carried along in the
// snapshot from before the intervening Saves.
func TestRestoreDirtyReflectsSaveThatHappenedAfterTheSnapshot(t *testing.T) {
	dir := t.TempDir()
	path := filepath.Join(dir, "f.txt")
	if err := os.WriteFile(path, []byte("original"), 0o644); err != nil {
		t.Fatal(err)
	}

	buf, err := Load(path)
	if err != nil {
		t.Fatalf("Load: %v", err)
	}
	original := append([]string(nil), buf.Lines...)

	buf.InsertText(0, len(buf.Lines[0]), "!") // "original!"
	edited := append([]string(nil), buf.Lines...)
	if err := buf.Save(); err != nil { // disk now "original!"
		t.Fatalf("Save: %v", err)
	}

	buf.Restore(original) // undo: buffer reverts, disk still has "original!"
	if !buf.Dirty {
		t.Fatal("expected Dirty after undo: buffer no longer matches what was just saved to disk")
	}
	if err := buf.Save(); err != nil { // disk now back to "original"
		t.Fatalf("Save: %v", err)
	}

	buf.Restore(edited) // redo: buffer has "original!" again, disk has "original"
	if !buf.Dirty {
		t.Fatal("expected Dirty after redo: buffer diverges from disk again, must not read as saved")
	}
}
