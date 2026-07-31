package editor

import (
	"errors"
	"os"
	"path/filepath"
	"reflect"
	"testing"
)

func TestFindOccurrencesIsCaseInsensitiveAndNonOverlapping(t *testing.T) {
	got := FindOccurrences("TODO fix TODO again todo", "todo")
	want := []int{0, 9, 20}
	if !reflect.DeepEqual(got, want) {
		t.Errorf("got %v, want %v", got, want)
	}
}

func TestFindOccurrencesNoMatch(t *testing.T) {
	if got := FindOccurrences("hello world", "xyz"); got != nil {
		t.Errorf("got %v, want nil", got)
	}
}

func TestFindOccurrencesEmptySearchReturnsNil(t *testing.T) {
	if got := FindOccurrences("hello", ""); got != nil {
		t.Errorf("got %v, want nil", got)
	}
}

func TestRewriteLineReplacesRequestedOrdinalsOnly(t *testing.T) {
	rewritten, applied, missed := rewriteLine("foo bar foo baz foo", "foo", "QUX", []int{0, 2})
	if rewritten != "QUX bar foo baz QUX" {
		t.Errorf("rewritten = %q", rewritten)
	}
	if !reflect.DeepEqual(applied, []int{0, 2}) {
		t.Errorf("applied = %v, want [0 2]", applied)
	}
	if len(missed) != 0 {
		t.Errorf("missed = %v, want none", missed)
	}
}

func TestRewriteLinePreservesReplacementCasingRegardlessOfMatchCasing(t *testing.T) {
	rewritten, _, _ := rewriteLine("TODO and todo", "todo", "done", []int{0, 1})
	if rewritten != "done and done" {
		t.Errorf("rewritten = %q, want the replacement's exact typed casing both times", rewritten)
	}
}

func TestRewriteLineOrdinalPastEndOfLineIsMissedNotMisapplied(t *testing.T) {
	rewritten, applied, missed := rewriteLine("only one foo here", "foo", "bar", []int{0, 5})
	if rewritten != "only one bar here" {
		t.Errorf("rewritten = %q", rewritten)
	}
	if !reflect.DeepEqual(applied, []int{0}) {
		t.Errorf("applied = %v, want [0]", applied)
	}
	if !reflect.DeepEqual(missed, []int{5}) {
		t.Errorf("missed = %v, want [5]", missed)
	}
}

func TestRewriteLineNoOrdinalsRequestedLeavesLineUnchanged(t *testing.T) {
	rewritten, applied, missed := rewriteLine("foo foo foo", "foo", "bar", nil)
	if rewritten != "foo foo foo" {
		t.Errorf("rewritten = %q, want unchanged", rewritten)
	}
	if len(applied) != 0 || len(missed) != 0 {
		t.Errorf("applied=%v missed=%v, want both empty", applied, missed)
	}
}

func TestRewriteLineNegativeOrdinalIsMissed(t *testing.T) {
	_, applied, missed := rewriteLine("foo", "foo", "bar", []int{-1})
	if len(applied) != 0 {
		t.Errorf("applied = %v, want none", applied)
	}
	if !reflect.DeepEqual(missed, []int{-1}) {
		t.Errorf("missed = %v, want [-1]", missed)
	}
}

// TestReplaceLinesIsOneUndoEntryRegardlessOfLineCount guards the central
// undo guarantee: replacing occurrences across many lines in one call must
// be reverted by exactly one Ctrl+Z, not one per line.
func TestReplaceLinesIsOneUndoEntryRegardlessOfLineCount(t *testing.T) {
	v := NewView()
	buf := &Buffer{Path: "/x/f.go", Lines: []string{"foo one", "foo two", "foo three"}}
	tb := &tab{path: "/x/f.go", buf: buf}
	v.tabs = []*tab{tb}
	v.active = 0

	ok, replaced, skipped := v.ReplaceLines("/x/f.go", "foo", "bar", map[int][]int{
		0: {0},
		1: {0},
		2: {0},
	})
	if !ok {
		t.Fatal("expected ReplaceLines to report ok for an open path")
	}
	if replaced != 3 {
		t.Errorf("replaced = %d, want 3", replaced)
	}
	if len(skipped) != 0 {
		t.Errorf("skipped = %+v, want none", skipped)
	}
	want := []string{"bar one", "bar two", "bar three"}
	if !reflect.DeepEqual(buf.Lines, want) {
		t.Fatalf("Lines = %v, want %v", buf.Lines, want)
	}
	if len(buf.undoStack) != 1 {
		t.Fatalf("undoStack has %d entries, want exactly 1", len(buf.undoStack))
	}

	v.undo(tb)
	original := []string{"foo one", "foo two", "foo three"}
	if !reflect.DeepEqual(buf.Lines, original) {
		t.Errorf("after undo, Lines = %v, want the pre-replace content %v", buf.Lines, original)
	}
}

func TestReplaceLinesLeavesTheBufferDirtyWithoutSaving(t *testing.T) {
	dir := t.TempDir()
	path := filepath.Join(dir, "f.txt")
	if err := os.WriteFile(path, []byte("foo\n"), 0o644); err != nil {
		t.Fatal(err)
	}

	v := NewView()
	buf, err := Load(path)
	if err != nil {
		t.Fatalf("Load: %v", err)
	}
	tb := &tab{path: path, buf: buf}
	v.tabs = []*tab{tb}
	v.active = 0

	v.ReplaceLines(path, "foo", "bar", map[int][]int{0: {0}})
	if !buf.Dirty {
		t.Error("expected the buffer to be left Dirty")
	}

	onDisk, err := os.ReadFile(path)
	if err != nil {
		t.Fatal(err)
	}
	if string(onDisk) != "foo\n" {
		t.Errorf("on-disk content changed to %q, want it untouched until an explicit save", onDisk)
	}
}

func TestReplaceLinesNoOpWhenPathNotOpenInThisPane(t *testing.T) {
	v := NewView()
	v.tabs = []*tab{{path: "/other.go", buf: &Buffer{Lines: []string{"foo"}}}}
	v.active = 0

	ok, replaced, skipped := v.ReplaceLines("/x/f.go", "foo", "bar", map[int][]int{0: {0}})
	if ok {
		t.Error("expected ok=false for a path not open in this pane")
	}
	if replaced != 0 || skipped != nil {
		t.Errorf("replaced=%d skipped=%v, want zero values", replaced, skipped)
	}
}

func TestReplaceLinesOutOfRangeLineIsSkipped(t *testing.T) {
	v := NewView()
	buf := &Buffer{Path: "/x/f.go", Lines: []string{"foo"}}
	tb := &tab{path: "/x/f.go", buf: buf}
	v.tabs = []*tab{tb}
	v.active = 0

	ok, replaced, skipped := v.ReplaceLines("/x/f.go", "foo", "bar", map[int][]int{99: {0}})
	if !ok {
		t.Fatal("expected ok=true — the path IS open, even though the requested line isn't valid")
	}
	if replaced != 0 {
		t.Errorf("replaced = %d, want 0", replaced)
	}
	if len(skipped) != 1 || skipped[0].Line != 100 {
		t.Errorf("skipped = %+v, want one entry for line 100 (99+1)", skipped)
	}
}

func TestRewriteFileReplacesAndSavesWithPreservedMode(t *testing.T) {
	dir := t.TempDir()
	path := filepath.Join(dir, "f.txt")
	if err := os.WriteFile(path, []byte("foo one\nfoo two\n"), 0o600); err != nil {
		t.Fatal(err)
	}

	replaced, skipped, err := RewriteFile(path, "foo", "bar", map[int][]int{0: {0}, 1: {0}})
	if err != nil {
		t.Fatalf("RewriteFile: %v", err)
	}
	if replaced != 2 {
		t.Errorf("replaced = %d, want 2", replaced)
	}
	if len(skipped) != 0 {
		t.Errorf("skipped = %+v, want none", skipped)
	}

	got, err := os.ReadFile(path)
	if err != nil {
		t.Fatal(err)
	}
	if string(got) != "bar one\nbar two" {
		t.Errorf("on-disk content = %q, want %q", got, "bar one\nbar two")
	}

	info, err := os.Stat(path)
	if err != nil {
		t.Fatal(err)
	}
	if info.Mode().Perm() != 0o600 {
		t.Errorf("mode = %v, want the original 0600 preserved", info.Mode().Perm())
	}
}

func TestRewriteFileNothingToReplaceDoesNotTouchDisk(t *testing.T) {
	dir := t.TempDir()
	path := filepath.Join(dir, "f.txt")
	if err := os.WriteFile(path, []byte("only one foo\n"), 0o644); err != nil {
		t.Fatal(err)
	}
	before, err := os.Stat(path)
	if err != nil {
		t.Fatal(err)
	}

	// Ordinal 5 doesn't exist on this line — every requested edit is stale.
	replaced, skipped, err := RewriteFile(path, "foo", "bar", map[int][]int{0: {5}})
	if err != nil {
		t.Fatalf("RewriteFile: %v", err)
	}
	if replaced != 0 {
		t.Errorf("replaced = %d, want 0", replaced)
	}
	if len(skipped) != 1 {
		t.Errorf("skipped = %+v, want one stale ordinal reported", skipped)
	}

	after, err := os.Stat(path)
	if err != nil {
		t.Fatal(err)
	}
	if before.ModTime() != after.ModTime() {
		t.Error("expected the file to not be rewritten when nothing actually changed")
	}
}

func TestRewriteFileMissingFileReturnsError(t *testing.T) {
	_, _, err := RewriteFile("/does/not/exist.txt", "foo", "bar", map[int][]int{0: {0}})
	if err == nil {
		t.Error("expected an error for a nonexistent file")
	}
}

func TestApplyRoutesToOpenBufferAndDisk(t *testing.T) {
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

	res := Apply("foo", "bar", []Occurrence{
		{AbsPath: "/open/f.go", Line: 1, Ordinal: 0},
		{AbsPath: closedPath, Line: 1, Ordinal: 0},
	}, findPane)

	if res.Replaced != 2 {
		t.Errorf("Replaced = %d, want 2", res.Replaced)
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

// TestApplyOneFileFailureDoesNotAbortTheRest guards the "continue, collect,
// report" failure convention: a bad path among many must not discard the
// other successful replacements.
func TestApplyOneFileFailureDoesNotAbortTheRest(t *testing.T) {
	dir := t.TempDir()
	goodPath := filepath.Join(dir, "good.txt")
	if err := os.WriteFile(goodPath, []byte("foo\n"), 0o644); err != nil {
		t.Fatal(err)
	}
	badPath := filepath.Join(dir, "does-not-exist.txt")

	findPane := func(string) (*View, bool) { return nil, false }
	res := Apply("foo", "bar", []Occurrence{
		{AbsPath: goodPath, Line: 1, Ordinal: 0},
		{AbsPath: badPath, Line: 1, Ordinal: 0},
	}, findPane)

	if res.Replaced != 1 {
		t.Errorf("Replaced = %d, want 1 (the good file)", res.Replaced)
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

func TestApplyGroupsMultipleOccurrencesOnOneLineIntoOneRewrite(t *testing.T) {
	v := NewView()
	buf := &Buffer{Path: "/x/f.go", Lines: []string{"foo foo foo"}}
	tb := &tab{path: "/x/f.go", buf: buf}
	v.tabs = []*tab{tb}
	v.active = 0
	findPane := func(string) (*View, bool) { return v, true }

	res := Apply("foo", "bar", []Occurrence{
		{AbsPath: "/x/f.go", Line: 1, Ordinal: 0},
		{AbsPath: "/x/f.go", Line: 1, Ordinal: 2},
	}, findPane)

	if res.Replaced != 2 {
		t.Errorf("Replaced = %d, want 2", res.Replaced)
	}
	if buf.Lines[0] != "bar foo bar" {
		t.Errorf("Lines[0] = %q, want %q (middle occurrence left untouched)", buf.Lines[0], "bar foo bar")
	}
	if len(buf.undoStack) != 1 {
		t.Errorf("undoStack has %d entries, want 1", len(buf.undoStack))
	}
}
