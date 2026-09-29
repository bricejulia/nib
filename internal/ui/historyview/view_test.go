package historyview

import (
	"fmt"
	"strings"
	"testing"

	"github.com/bricejulia/nib/internal/layout"
)

// fakeWindow is an in-memory layout.Window double, the same minimal
// Println-into-a-slice fake the other overlay packages use (see
// internal/ui/refview/view_test.go). It also records whether each row was
// drawn reversed, which is how the selected entry shows.
type fakeWindow struct {
	cols, rows int
	lines      []string
	reversed   []bool
}

func newFakeWindow(cols, rows int) *fakeWindow {
	return &fakeWindow{cols: cols, rows: rows, lines: make([]string, rows), reversed: make([]bool, rows)}
}

func (w *fakeWindow) Size() (int, int) { return w.cols, w.rows }
func (w *fakeWindow) Println(row int, segs ...layout.Segment) {
	if row < 0 || row >= len(w.lines) {
		return
	}
	text := ""
	rev := len(segs) > 0
	for _, s := range segs {
		text += s.Text
		rev = rev && s.Style.Attr&layout.AttrReverse != 0
	}
	w.lines[row] = text
	w.reversed[row] = rev
}
func (w *fakeWindow) Clear() {
	for i := range w.lines {
		w.lines[i] = ""
		w.reversed[i] = false
	}
}

func key(s string) layout.Key {
	switch s {
	case "Esc", "Down", "Up", "Home", "End", "PageDown", "PageUp", "Left", "Right":
		return layout.Key{Named: s}
	}
	return layout.Key{Text: s}
}

func entries(n int) []Entry {
	out := make([]Entry, n)
	for i := range out {
		out[i] = Entry{ID: fmt.Sprintf("c%06d", i), When: "2026-01-01", Author: "ann", Summary: fmt.Sprintf("commit %d", i)}
	}
	return out
}

// diffLines is a long, per-entry-distinct diff, so tests can tell which
// entry's diff is showing and scroll through it.
func diffLines(i int) []string {
	lines := []string{fmt.Sprintf("@@ entry %d @@", i)}
	for j := 0; j < 100; j++ {
		lines = append(lines, fmt.Sprintf("+line %d.%d", i, j))
	}
	return lines
}

func openView(n int) (*View, *[]int) {
	v := New()
	var calls []int
	v.DiffFunc = func(i int) []string {
		calls = append(calls, i)
		return diffLines(i)
	}
	v.Open("f.go", entries(n))
	return v, &calls
}

func contains(w *fakeWindow, s string) bool {
	for _, l := range w.lines {
		if strings.Contains(l, s) {
			return true
		}
	}
	return false
}

func TestTitleNamesFile(t *testing.T) {
	v, _ := openView(1)
	if got := v.Title(); got != "History: f.go" {
		t.Errorf("got %q", got)
	}
}

func TestOpenLoadsFirstEntrysDiff(t *testing.T) {
	v, calls := openView(3)
	if len(*calls) != 1 || (*calls)[0] != 0 {
		t.Fatalf("DiffFunc calls = %v, want [0]", *calls)
	}
	w := newFakeWindow(80, 30)
	v.Render(w)
	if !strings.HasPrefix(w.lines[0], "3 commits") {
		t.Errorf("header = %q", w.lines[0])
	}
	if !strings.Contains(w.lines[1], "c000000") || !w.reversed[1] {
		t.Errorf("first row should be the selected first entry: %q reversed=%v", w.lines[1], w.reversed[1])
	}
	if w.reversed[2] {
		t.Error("second row should not be selected")
	}
	if !contains(w, "@@ entry 0 @@") {
		t.Error("preview should show entry 0's diff")
	}
}

func TestDownSelectsNextAndLoadsItsDiff(t *testing.T) {
	v, calls := openView(3)
	v.HandleKey(key("Down"))
	w := newFakeWindow(80, 30)
	v.Render(w)
	if !w.reversed[2] || w.reversed[1] {
		t.Errorf("second row should be selected: %v", w.reversed[:4])
	}
	if !contains(w, "@@ entry 1 @@") || contains(w, "@@ entry 0 @@") {
		t.Error("preview should show entry 1's diff only")
	}
	if fmt.Sprint(*calls) != "[0 1]" {
		t.Errorf("calls = %v", *calls)
	}
}

func TestDiffsAreCached(t *testing.T) {
	v, calls := openView(3)
	v.HandleKey(key("j"))
	v.HandleKey(key("k"))
	v.HandleKey(key("j"))
	if len(*calls) != 2 {
		t.Errorf("DiffFunc calls = %v, want each entry fetched once", *calls)
	}
}

func TestSelectionStopsAtEnds(t *testing.T) {
	v, _ := openView(2)
	v.HandleKey(key("Up"))
	if v.cursor != 0 {
		t.Errorf("cursor = %d after Up at top", v.cursor)
	}
	v.HandleKey(key("End"))
	v.HandleKey(key("Down"))
	if v.cursor != 1 {
		t.Errorf("cursor = %d after Down at bottom", v.cursor)
	}
	v.HandleKey(key("Home"))
	if v.cursor != 0 {
		t.Errorf("cursor = %d after Home", v.cursor)
	}
}

func TestPageDownScrollsPreviewNotSelection(t *testing.T) {
	v, _ := openView(3)
	w := newFakeWindow(80, 30)
	v.Render(w)
	v.HandleKey(key("PageDown"))
	v.Render(w)
	if v.cursor != 0 {
		t.Errorf("cursor moved to %d", v.cursor)
	}
	if contains(w, "@@ entry 0 @@") || !contains(w, "+line 0.") {
		t.Error("preview should have scrolled past the hunk header")
	}
}

func TestListScrollsToKeepSelectionVisible(t *testing.T) {
	v, _ := openView(50)
	w := newFakeWindow(80, 30) // list gets 10 rows
	v.Render(w)
	for i := 0; i < 20; i++ {
		v.HandleKey(key("j"))
	}
	v.Render(w)
	found := false
	for row := range w.lines {
		if w.reversed[row] && strings.Contains(w.lines[row], "c000020") {
			found = true
		}
	}
	if !found {
		t.Errorf("selected entry 20 not visible:\n%s", strings.Join(w.lines, "\n"))
	}
}

func TestPreviewNeverDrawsOverList(t *testing.T) {
	v, _ := openView(3)
	w := newFakeWindow(80, 8)
	v.Render(w)
	for row := 1; row <= 3; row++ {
		if !strings.Contains(w.lines[row], fmt.Sprintf("c%06d", row-1)) {
			t.Errorf("row %d = %q, want entry %d", row, w.lines[row], row-1)
		}
	}
	if !strings.HasPrefix(w.lines[4], "─") {
		t.Errorf("row 4 should be the divider, got %q", w.lines[4])
	}
	if !strings.Contains(w.lines[5], "@@ entry 0 @@") {
		t.Errorf("row 5 should start the preview, got %q", w.lines[5])
	}
}

func TestEscCloses(t *testing.T) {
	v, _ := openView(1)
	closed := false
	v.OnClose = func() { closed = true }
	v.HandleKey(key("Esc"))
	if !closed {
		t.Error("Esc should call OnClose")
	}
}

func TestEmptyHistory(t *testing.T) {
	v := New()
	called := false
	v.DiffFunc = func(int) []string { called = true; return nil }
	v.Open("f.go", nil)
	if called {
		t.Error("DiffFunc should not be called for an empty history")
	}
	w := newFakeWindow(80, 10)
	v.Render(w)
	if !strings.Contains(w.lines[0], "no history") {
		t.Errorf("got %q", w.lines[0])
	}
	v.HandleKey(key("Down")) // must not panic
	v.HandleKey(key("End"))
}

func TestEmptyCommitDiffSaysSo(t *testing.T) {
	v := New()
	v.DiffFunc = func(int) []string { return nil }
	v.Open("f.go", entries(1))
	w := newFakeWindow(80, 10)
	v.Render(w)
	if !contains(w, "no changes to this file in this commit") {
		t.Errorf("got:\n%s", strings.Join(w.lines, "\n"))
	}
}

func TestTruncateLongAuthor(t *testing.T) {
	if got := truncate("abcdefghij", 5); got != "abcd…" {
		t.Errorf("got %q", got)
	}
	if got := truncate("abc", 5); got != "abc" {
		t.Errorf("got %q", got)
	}
}
