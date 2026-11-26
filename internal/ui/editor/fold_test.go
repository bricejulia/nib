package editor

import (
	"reflect"
	"strings"
	"testing"
)

const foldSampleJSON = `{
  "a": 1,
  "users": [
    {"n": 1},
    {
      "n": 2
    }
  ],
  "b": 2
}`

func foldsOf(t *testing.T, name, src string) []foldRange {
	t.Helper()
	_, folds, ok := analyzeSource(name, []byte(src))
	if !ok {
		t.Fatalf("analyzeSource(%s) gave up", name)
	}
	return folds
}

func hasFold(folds []foldRange, start, end int) bool {
	for _, f := range folds {
		if f.Start == start && f.End == end {
			return true
		}
	}
	return false
}

func TestFoldRangesFromTreeJSON(t *testing.T) {
	got := foldsOf(t, "x.json", foldSampleJSON)
	want := []foldRange{{0, 9}, {2, 7}, {4, 6}}
	if !reflect.DeepEqual(got, want) {
		t.Fatalf("folds = %v, want %v", got, want)
	}
}

func TestFoldRangesFromTreeCodeLanguages(t *testing.T) {
	cases := []struct {
		name, src  string
		start, end int
	}{
		{"x.go", "package main\n\nimport (\n\t\"fmt\"\n\t\"os\"\n)\n\nfunc main() {\n\tfmt.Println(os.Args)\n}\n", 7, 9},
		{"x.go", "package main\n\nimport (\n\t\"fmt\"\n\t\"os\"\n)\n\nfunc main() {\n\tfmt.Println(os.Args)\n}\n", 2, 5},
		{"x.php", "<?php\n\nfunction hello($n) {\n    if ($n) {\n        echo $n;\n    }\n}\n", 2, 6},
		{"x.php", "<?php\n\nfunction hello($n) {\n    if ($n) {\n        echo $n;\n    }\n}\n", 3, 5},
		{"x.c", "int main(void) {\n    int a[] = {\n        1,\n        2,\n    };\n    return 0;\n}\n", 0, 6},
		{"x.c", "int main(void) {\n    int a[] = {\n        1,\n        2,\n    };\n    return 0;\n}\n", 1, 4},
		{"x.go", "package main\n\n/*\nlong\ncomment\n*/\nvar x = 1\n", 2, 5},
	}
	for _, c := range cases {
		folds := foldsOf(t, c.name, c.src)
		if !hasFold(folds, c.start, c.end) {
			t.Errorf("%s: want fold %d-%d, got %v", c.name, c.start, c.end, folds)
		}
	}
}

func TestBracketFoldRangesSkipStringsAndComments(t *testing.T) {
	lines := []string{
		`a = {`,
		`  s: "}",`,
		`  // }`,
		`  /* {`,
		`  } */`,
		`  t: '{',`,
		`  list: [`,
		`    1,`,
		`  ]`,
		`}`,
	}
	got := foldRangesFallback(lines)
	want := []foldRange{{0, 9}, {6, 8}}
	if !reflect.DeepEqual(got, want) {
		t.Fatalf("folds = %v, want %v", got, want)
	}
}

func TestIndentFoldRangesWhenNoBrackets(t *testing.T) {
	lines := []string{
		"def f():",
		"    a = 1",
		"",
		"    if a:",
		"        b = 2",
		"",
		"x = 3",
	}
	got := foldRangesFallback(lines)
	want := []foldRange{{0, 4}, {3, 4}}
	if !reflect.DeepEqual(got, want) {
		t.Fatalf("folds = %v, want %v", got, want)
	}
}

func TestShiftFoldRanges(t *testing.T) {
	base := func() []foldRange { return []foldRange{{2, 5}, {8, 10}} }
	cases := []struct {
		name                  string
		ln, removed, inserted int
		want                  []foldRange
	}{
		{"insert above moves both", 0, 0, 2, []foldRange{{4, 7}, {10, 12}}},
		{"insert inside grows", 3, 0, 1, []foldRange{{2, 6}, {9, 11}}},
		{"delete inside shrinks", 3, 1, 0, []foldRange{{2, 4}, {7, 9}}},
		{"rewrite header keeps", 2, 1, 1, []foldRange{{2, 5}, {8, 10}}},
		{"delete header drops", 2, 1, 0, []foldRange{{7, 9}}},
		{"delete whole block", 2, 4, 0, []foldRange{{4, 6}}},
	}
	for _, c := range cases {
		got := shiftFoldRanges(base(), c.ln, c.removed, c.inserted)
		if !reflect.DeepEqual(got, c.want) {
			t.Errorf("%s: got %v, want %v", c.name, got, c.want)
		}
	}
}

// foldView opens foldSampleJSON in a fresh View (inline highlighting, so
// fold ranges are there at once) sized w×h.
func foldView(t *testing.T, w, h int) (*View, *tab) {
	t.Helper()
	p := writeTemp(t, t.TempDir(), "data.json", foldSampleJSON)
	v := NewView()
	v.Open(p)
	v.lastWidth, v.lastHeight = w, h
	tb := v.activeTab()
	if tb == nil || tb.buf == nil {
		t.Fatal("open failed")
	}
	return v, tb
}

func render(v *View, w, h int) *fakeWindow {
	win := newFakeWindow(w, h)
	v.Render(win)
	return win
}

func TestFoldCloseRendersPlaceholderAndHidesLines(t *testing.T) {
	v, tb := foldView(t, 60, 20)
	tb.cursorLn = 3
	pressKeys(v, "zc")

	if tb.cursorLn != 2 {
		t.Fatalf("cursor = %d, want 2 (the fold's header)", tb.cursorLn)
	}
	win := render(v, 60, 20)
	if !strings.Contains(win.lines[3], `"users": [ ⋯ 5 lines ⋯ ],`) {
		t.Fatalf("row 3 = %q, want the folded header with its placeholder", win.lines[3])
	}
	if !strings.Contains(win.lines[3], "▸") {
		t.Errorf("row 3 = %q, want a closed-fold marker", win.lines[3])
	}
	if !strings.Contains(win.lines[1], "▾") {
		t.Errorf("row 1 = %q, want an open-fold marker on the outer object", win.lines[1])
	}
	if !strings.Contains(win.lines[4], `"b": 2`) {
		t.Fatalf("row 4 = %q, want the line after the fold", win.lines[4])
	}
	if strings.Contains(strings.Join(win.lines, "\n"), `"n": 2`) {
		t.Fatal("hidden lines are still drawn")
	}
}

func TestJKStepOverAClosedFold(t *testing.T) {
	v, tb := foldView(t, 60, 20)
	tb.cursorLn = 2
	pressKeys(v, "zc")
	pressKeys(v, "j")
	if tb.cursorLn != 8 {
		t.Fatalf("j from folded header: cursor = %d, want 8", tb.cursorLn)
	}
	pressKeys(v, "k")
	if tb.cursorLn != 2 {
		t.Fatalf("k back: cursor = %d, want 2", tb.cursorLn)
	}
	if !tb.closedFolds[2] {
		t.Fatal("moving over the fold opened it")
	}
}

func TestDDOnAFoldedHeaderDeletesTheWholeBlock(t *testing.T) {
	v, tb := foldView(t, 60, 20)
	tb.cursorLn = 2
	pressKeys(v, "zcdd")
	got := strings.Join(tb.buf.Lines, "|")
	want := `{|  "a": 1,|  "b": 2|}`
	if got != want {
		t.Fatalf("Lines = %q, want %q", got, want)
	}
}

func TestCursorLandingInAFoldOpensIt(t *testing.T) {
	v, tb := foldView(t, 60, 20)
	tb.cursorLn = 2
	pressKeys(v, "zc")
	tb.cursorLn = 5 // as a search match or :6 would
	v.clamp(tb)
	if len(tb.closedFolds) != 0 || tb.isHidden(5) {
		t.Fatalf("fold still closed over the cursor: %v", tb.closedFolds)
	}
}

func TestReopeningAnOuterFoldKeepsInnerOnesClosed(t *testing.T) {
	v, tb := foldView(t, 60, 20)
	tb.cursorLn = 4
	pressKeys(v, "zc") // inner object
	tb.cursorLn = 2
	pressKeys(v, "zc") // users array
	pressKeys(v, "zo")
	if !tb.closedFolds[4] || tb.closedFolds[2] {
		t.Fatalf("closedFolds = %v, want only the inner one (4)", tb.closedFolds)
	}
	if !tb.isHidden(5) || tb.isHidden(3) {
		t.Fatal("wrong lines hidden after reopening the outer fold")
	}
}

func TestZcOnAClosedFoldClosesTheOneAroundIt(t *testing.T) {
	v, tb := foldView(t, 60, 20)
	tb.cursorLn = 4
	pressKeys(v, "zczc")
	if !tb.closedFolds[2] || tb.cursorLn != 2 {
		t.Fatalf("closedFolds = %v, cursor %d; want the users array closed and the cursor on it", tb.closedFolds, tb.cursorLn)
	}
}

func TestFoldAllAndUnfoldAll(t *testing.T) {
	v, tb := foldView(t, 60, 20)
	tb.cursorLn = 5
	pressKeys(v, "zM")
	if tb.cursorLn != 0 {
		t.Fatalf("after zM cursor = %d, want 0 (outermost header)", tb.cursorLn)
	}
	win := render(v, 60, 20)
	if !strings.Contains(win.lines[1], "{ ⋯ 9 lines ⋯ }") || win.lines[2] != "" {
		t.Fatalf("zM render: %q / %q", win.lines[1], win.lines[2])
	}
	if s := v.ScrollState(); s.Total != 1 {
		t.Fatalf("scroll total = %d, want 1 visible line", s.Total)
	}
	pressKeys(v, "zR")
	if len(tb.closedFolds) != 0 {
		t.Fatalf("zR left %v closed", tb.closedFolds)
	}
}

func TestEditsAboveAFoldMoveIt(t *testing.T) {
	v, tb := foldView(t, 60, 20)
	tb.cursorLn = 2
	pressKeys(v, "zc")
	tb.buf.InsertLines(1, []string{`  "new": 0,`})
	if !tb.isHidden(4) || tb.isHidden(3) || !tb.closedFolds[3] {
		t.Fatalf("fold didn't follow the edit: %v", tb.closedFolds)
	}
}

func TestEditingAFoldsHeaderOpensIt(t *testing.T) {
	v, tb := foldView(t, 60, 20)
	tb.cursorLn = 2
	pressKeys(v, "zc")
	tb.buf.InsertText(2, 0, " ")
	if tb.isHidden(3) {
		t.Fatal("fold survived an edit of its header line")
	}
}

func TestUndoOfAnUnrelatedEditKeepsFolds(t *testing.T) {
	v, tb := foldView(t, 60, 20)
	tb.cursorLn = 2
	pressKeys(v, "zc")
	tb.cursorLn = 8
	pressKeys(v, "x")
	pressKeys(v, "u")
	if !tb.closedFolds[2] || !tb.isHidden(3) {
		t.Fatalf("undo dropped the fold: %v", tb.closedFolds)
	}
}

func TestClickingTheFoldMarkerTogglesIt(t *testing.T) {
	v, tb := foldView(t, 60, 20)
	render(v, 60, 20)
	marker := gutterWidthFor(tb) - 2
	v.HandleMouse(press(marker, 3, 1)) // row 3 = line 2
	if !tb.closedFolds[2] {
		t.Fatal("click on ▾ didn't close the fold")
	}
	if tb.hasSel {
		t.Fatal("fold click started a selection")
	}
	win := render(v, 60, 20)
	v.HandleMouse(press(gutterWidthFor(tb)+len(`  "users": [`)+3, 3, 1)) // on the placeholder
	if tb.closedFolds[2] {
		t.Fatalf("click on the placeholder didn't open the fold (row: %q)", win.lines[3])
	}
}

func TestFoldsWithSoftWrap(t *testing.T) {
	v, tb := foldView(t, 60, 20)
	v.wrap = true
	tb.cursorLn = 2
	pressKeys(v, "zc")
	win := render(v, 60, 20)
	if !strings.Contains(win.lines[3], "⋯ 5 lines ⋯") || !strings.Contains(win.lines[4], `"b": 2`) {
		t.Fatalf("wrapped render: %q / %q", win.lines[3], win.lines[4])
	}
	pressKeys(v, "j")
	if tb.cursorLn != 8 {
		t.Fatalf("j in wrap mode: cursor = %d, want 8", tb.cursorLn)
	}
	if _, row := v.cursorAnchor(tb); row != 4 {
		t.Fatalf("cursor row = %d, want 4", row)
	}
}

func TestPlainTextWithoutFoldsKeepsItsGutter(t *testing.T) {
	v := NewView()
	v.Open(writeTemp(t, t.TempDir(), "notes.txt", "one\ntwo\n"))
	if foldColumnWidth(v.activeTab()) != 0 {
		t.Fatal("a file with nothing to fold grew a fold column")
	}
}

func TestFoldKeyCancelsOnUnknownKey(t *testing.T) {
	v, tb := foldView(t, 60, 20)
	tb.cursorLn = 1
	pressKeys(v, "zx")
	if tb.buf.Lines[1] != `  "a": 1,` {
		t.Fatal(`"x" after "z" was run as delete`)
	}
	pressKeys(v, "j")
	if tb.cursorLn != 2 {
		t.Fatal("key after a cancelled z was swallowed")
	}
}

func TestPlainTextFoldsOnBrackets(t *testing.T) {
	b := &Buffer{Path: "notes.txt", Lines: []string{"a {", "  b", "}"}}
	if got := b.foldRanges(); !reflect.DeepEqual(got, []foldRange{{0, 2}}) {
		t.Fatalf("folds = %v, want [{0 2}]", got)
	}
}
