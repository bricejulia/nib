package finder

import (
	"os"
	"os/exec"
	"path/filepath"
	"strings"
	"testing"

	"github.com/bricejulia/nib/internal/layout"
	"github.com/bricejulia/nib/internal/ui/editor"
)

// newReplaceTestRepo builds a small git repo with the query "todo"
// appearing twice on one line of one file and once in another file, for
// occurrence-expansion and multi-file tests.
func newReplaceTestRepo(t *testing.T) string {
	t.Helper()
	dir := t.TempDir()
	run := func(args ...string) {
		t.Helper()
		cmd := exec.Command("git", args...)
		cmd.Dir = dir
		if out, err := cmd.CombinedOutput(); err != nil {
			t.Fatalf("git %v: %v\n%s", args, err, out)
		}
	}
	run("init", "-q")
	run("config", "user.email", "test@example.com")
	run("config", "user.name", "test")

	if err := os.WriteFile(filepath.Join(dir, "a.go"), []byte("// todo: fix todo\npackage a\n"), 0o644); err != nil {
		t.Fatal(err)
	}
	if err := os.WriteFile(filepath.Join(dir, "b.go"), []byte("// todo: cleanup\n"), 0o644); err != nil {
		t.Fatal(err)
	}
	run("add", "a.go", "b.go")
	run("commit", "-q", "-m", "init")
	return dir
}

func typeText(v *ReplaceView, s string) {
	for _, r := range s {
		v.HandleKey(layout.Key{Text: string(r)})
	}
}

func TestReplaceViewExpandsMultipleOccurrencesOnOneLineIntoSeparateRows(t *testing.T) {
	dir := newReplaceTestRepo(t)
	v := NewReplaceView(dir)
	v.Open()
	typeText(v, "todo")

	files, occs := v.counts()
	if files != 2 {
		t.Errorf("files = %d, want 2", files)
	}
	if occs != 3 {
		t.Errorf("occurrences = %d, want 3 (two on a.go's line, one on b.go's)", occs)
	}
}

func TestReplaceViewBuildsFileHeaderPerFile(t *testing.T) {
	dir := newReplaceTestRepo(t)
	v := NewReplaceView(dir)
	v.Open()
	typeText(v, "todo")

	var paths []string
	for _, r := range v.rows {
		if r.isFile {
			paths = append(paths, r.path)
		}
	}
	if len(paths) != 2 {
		t.Fatalf("got %d file headers, want 2: %+v", len(paths), paths)
	}
}

func TestReplaceViewTooShortQueryShowsNoRows(t *testing.T) {
	dir := newReplaceTestRepo(t)
	v := NewReplaceView(dir)
	v.Open()
	typeText(v, "t") // below minContentQueryLen

	if len(v.rows) != 0 {
		t.Errorf("got %d rows, want 0 for a too-short query", len(v.rows))
	}
	w := newFakeWindow(80, 20)
	v.Render(w)
	if !strings.Contains(w.lines[2], "at least") {
		t.Errorf("expected the status line to prompt for more characters, got %q", w.lines[2])
	}
}

func TestReplaceViewTabCyclesFocus(t *testing.T) {
	v := NewReplaceView(t.TempDir())
	v.Open()
	if v.focus != focusFind {
		t.Fatalf("expected to start on focusFind, got %v", v.focus)
	}
	v.HandleKey(layout.Key{Named: layout.KeyTab})
	if v.focus != focusReplace {
		t.Errorf("after one Tab, focus = %v, want focusReplace", v.focus)
	}
	v.HandleKey(layout.Key{Named: layout.KeyTab})
	if v.focus != focusResults {
		t.Errorf("after two Tabs, focus = %v, want focusResults", v.focus)
	}
	v.HandleKey(layout.Key{Named: layout.KeyTab})
	if v.focus != focusFind {
		t.Errorf("after three Tabs, focus = %v, want back to focusFind", v.focus)
	}
}

func TestReplaceViewTypingGoesToTheFocusedField(t *testing.T) {
	v := NewReplaceView(t.TempDir())
	v.Open()
	typeText(v, "foo")
	if v.find.String() != "foo" {
		t.Errorf("find = %q, want %q", v.find.String(), "foo")
	}

	v.HandleKey(layout.Key{Named: layout.KeyTab})
	typeText(v, "bar")
	if v.replace.String() != "bar" {
		t.Errorf("replace = %q, want %q", v.replace.String(), "bar")
	}
	if v.find.String() != "foo" {
		t.Errorf("find changed to %q, want it to stay %q", v.find.String(), "foo")
	}
}

func TestReplaceViewSpaceTypesALiteralSpaceInAField(t *testing.T) {
	v := NewReplaceView(t.TempDir())
	v.Open()
	typeText(v, "foo")
	v.HandleKey(layout.Key{Named: layout.KeySpace, Text: " "})
	if v.find.String() != "foo " {
		t.Errorf("find = %q, want %q (Space typed, not swallowed as an action)", v.find.String(), "foo ")
	}
}

func TestReplaceViewSpaceTogglesInResultsMode(t *testing.T) {
	v := NewReplaceView(t.TempDir())
	v.Open()
	v.rows = []replaceRow{{isFile: true, path: "a.go", checked: true}}
	v.focus = focusResults
	v.cursor = 0

	v.HandleKey(layout.Key{Named: layout.KeySpace, Text: " "})
	if v.rows[0].checked {
		t.Error("expected Space to toggle the row once focus is on the results list")
	}
}

func TestReplaceViewToggleFileTogglesAllItsOccurrences(t *testing.T) {
	dir := newReplaceTestRepo(t)
	v := NewReplaceView(dir)
	v.Open()
	typeText(v, "todo")
	v.focus = focusResults

	// Row 0 is a.go's file header; rows 1-2 are its two occurrences.
	if !v.rows[0].isFile {
		t.Fatal("expected row 0 to be a file header")
	}
	v.cursor = 0
	v.toggleCurrent()
	if v.rows[0].checked {
		t.Fatal("expected the file row to be unchecked after toggling")
	}
	if v.rows[1].checked || v.rows[2].checked {
		t.Errorf("expected both occurrences to be unchecked too, got %+v %+v", v.rows[1], v.rows[2])
	}
}

func TestReplaceViewUncheckingOneOccurrenceUnchecksItsFileHeader(t *testing.T) {
	dir := newReplaceTestRepo(t)
	v := NewReplaceView(dir)
	v.Open()
	typeText(v, "todo")
	v.focus = focusResults

	v.cursor = 1 // the first occurrence under a.go
	v.toggleCurrent()
	if v.rows[0].checked {
		t.Error("expected the file header to become unchecked once one of its occurrences is")
	}

	// Re-checking the same occurrence should restore the file header too.
	v.toggleCurrent()
	if !v.rows[0].checked {
		t.Error("expected the file header to become checked again once every occurrence is")
	}
}

func TestReplaceViewReplaceAllOnlySendsCheckedOccurrences(t *testing.T) {
	dir := newReplaceTestRepo(t)
	v := NewReplaceView(dir)
	v.Open()
	typeText(v, "todo")
	v.HandleKey(layout.Key{Named: layout.KeyTab})
	typeText(v, "DONE")
	v.focus = focusResults

	// Uncheck b.go's single occurrence (the file header at some index; find
	// it rather than hardcode, since row order is git grep's own path order).
	for i := range v.rows {
		if !v.rows[i].isFile && v.rows[i].path == "b.go" {
			v.cursor = i
			v.toggleCurrent()
		}
	}

	var gotSearch, gotReplacement string
	var gotOccs []editor.Occurrence
	v.OnReplaceAll = func(search, replacement string, occs []editor.Occurrence) {
		gotSearch, gotReplacement, gotOccs = search, replacement, occs
	}
	v.replaceAll()

	if gotSearch != "todo" || gotReplacement != "DONE" {
		t.Errorf("search=%q replacement=%q, want todo/DONE", gotSearch, gotReplacement)
	}
	if len(gotOccs) != 2 {
		t.Fatalf("got %d occurrences, want 2 (b.go's unchecked one excluded): %+v", len(gotOccs), gotOccs)
	}
	for _, o := range gotOccs {
		if strings.HasSuffix(o.AbsPath, "b.go") {
			t.Errorf("b.go should have been excluded, got %+v", gotOccs)
		}
	}
}

func TestReplaceViewReplaceCurrentSendsOnlyTheCursorRow(t *testing.T) {
	dir := newReplaceTestRepo(t)
	v := NewReplaceView(dir)
	v.Open()
	typeText(v, "todo")
	v.focus = focusResults
	v.cursor = 1 // a.go's first occurrence

	var gotOccs []editor.Occurrence
	v.OnReplaceAll = func(_, _ string, occs []editor.Occurrence) { gotOccs = occs }
	v.replaceCurrent()

	if len(gotOccs) != 1 {
		t.Fatalf("got %d occurrences, want exactly 1", len(gotOccs))
	}
	if gotOccs[0].Line != v.rows[1].line || gotOccs[0].Ordinal != v.rows[1].ordinal {
		t.Errorf("occurrence = %+v, want it to match row 1 (line=%d ordinal=%d)", gotOccs[0], v.rows[1].line, v.rows[1].ordinal)
	}
}

func TestReplaceViewReplaceCurrentOnFileRowIsNoOp(t *testing.T) {
	dir := newReplaceTestRepo(t)
	v := NewReplaceView(dir)
	v.Open()
	typeText(v, "todo")
	v.focus = focusResults
	v.cursor = 0 // a file header

	called := false
	v.OnReplaceAll = func(string, string, []editor.Occurrence) { called = true }
	v.replaceCurrent()
	if called {
		t.Error("expected replaceCurrent on a file header row to be a no-op")
	}
}

func TestReplaceViewEscClosesWhileTyping(t *testing.T) {
	v := NewReplaceView(t.TempDir())
	v.Open()
	closed := false
	v.OnClose = func() { closed = true }
	v.HandleKey(layout.Key{Named: layout.KeyEsc})
	if !closed {
		t.Error("expected Esc to close the overlay")
	}
}

func TestReplaceViewEscClosesFromResultSummary(t *testing.T) {
	v := NewReplaceView(t.TempDir())
	v.Open()
	v.ShowResult(editor.Result{Replaced: 3})

	closed := false
	v.OnClose = func() { closed = true }
	v.HandleKey(layout.Key{Named: layout.KeyEsc})
	if !closed {
		t.Error("expected Esc to close the overlay from the result summary too")
	}
}

func TestReplaceViewNonEscKeyIgnoredDuringResultSummary(t *testing.T) {
	v := NewReplaceView(t.TempDir())
	v.Open()
	v.ShowResult(editor.Result{Replaced: 1})

	closed := false
	v.OnClose = func() { closed = true }
	v.HandleKey(layout.Key{Text: "a"})
	if closed {
		t.Error("expected only Esc to close from the result summary")
	}
}

func TestReplaceViewRendersResultSummary(t *testing.T) {
	v := NewReplaceView(t.TempDir())
	v.Open()
	v.ShowResult(editor.Result{Replaced: 5, Skipped: []editor.Occurrence{{}}})

	w := newFakeWindow(80, 10)
	v.Render(w)
	joined := strings.Join(w.lines, "\n")
	if !strings.Contains(joined, "Replaced 5") {
		t.Errorf("expected the summary to report the replaced count, got:\n%s", joined)
	}
	if !strings.Contains(joined, "Skipped 1") {
		t.Errorf("expected the summary to report the skipped count, got:\n%s", joined)
	}
}

func TestReplaceViewImplementsScrollInterfaces(t *testing.T) {
	var v layout.View = NewReplaceView(t.TempDir())
	if _, ok := v.(layout.Scrollable); !ok {
		t.Error("ReplaceView should implement layout.Scrollable")
	}
	if _, ok := v.(layout.ScrollTarget); !ok {
		t.Error("ReplaceView should implement layout.ScrollTarget")
	}
}

func TestReplaceViewScrollStateEmptyWithNoResults(t *testing.T) {
	v := NewReplaceView(t.TempDir())
	v.Open()
	if got := v.ScrollState(); got != (layout.ScrollState{}) {
		t.Errorf("got %+v, want the zero value with no results", got)
	}
}

func TestReplaceViewScrollStateEmptyDuringResultSummary(t *testing.T) {
	dir := newReplaceTestRepo(t)
	v := NewReplaceView(dir)
	v.Open()
	typeText(v, "todo")
	v.lastListRows = 10
	v.ShowResult(editor.Result{Replaced: 1})

	if got := v.ScrollState(); got != (layout.ScrollState{}) {
		t.Errorf("got %+v, want the zero value while showing a result summary", got)
	}
}

func TestReplaceViewMoveCursorClampsToRowCount(t *testing.T) {
	dir := newReplaceTestRepo(t)
	v := NewReplaceView(dir)
	v.Open()
	typeText(v, "todo")
	v.focus = focusResults

	for range len(v.rows) + 5 {
		v.moveCursor(1)
	}
	if v.cursor != len(v.rows)-1 {
		t.Errorf("cursor = %d, want clamped to %d", v.cursor, len(v.rows)-1)
	}

	for range len(v.rows) + 5 {
		v.moveCursor(-1)
	}
	if v.cursor != 0 {
		t.Errorf("cursor = %d, want clamped to 0", v.cursor)
	}
}

func TestReplaceViewOpenResetsState(t *testing.T) {
	dir := newReplaceTestRepo(t)
	v := NewReplaceView(dir)
	v.Open()
	typeText(v, "todo")
	v.HandleKey(layout.Key{Named: layout.KeyTab})
	typeText(v, "DONE")
	v.ShowResult(editor.Result{Replaced: 1})

	v.Open()
	if v.find.String() != "" || v.replace.String() != "" {
		t.Errorf("find=%q replace=%q, want both cleared", v.find.String(), v.replace.String())
	}
	if v.focus != focusFind {
		t.Errorf("focus = %v, want reset to focusFind", v.focus)
	}
	if v.resultShown {
		t.Error("expected resultShown to reset to false")
	}
	if len(v.rows) != 0 {
		t.Errorf("rows = %+v, want cleared", v.rows)
	}
}
