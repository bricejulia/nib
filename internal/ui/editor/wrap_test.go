package editor

import (
	"slices"
	"strings"
	"testing"
)

func TestWrapStarts(t *testing.T) {
	cases := []struct {
		name  string
		text  string
		width int
		want  []int
	}{
		{"empty line", "", 5, []int{0}},
		{"fits", "abc", 5, []int{0}},
		{"exactly fits", "abcde", 5, []int{0}},
		{"word wrap breaks after the last space", "ab cd ef", 5, []int{0, 3}},
		{"hard break without a space", "abcdefgh", 3, []int{0, 3, 6}},
		{"wide rune at the edge moves down whole", "ab中", 3, []int{0, 2}},
		{"rune wider than the row gets its own row", "中中", 1, []int{0, 2}},
		{"zero width", "abc", 0, []int{0}},
	}
	for _, c := range cases {
		if got := wrapStarts(c.text, c.width); !slices.Equal(got, c.want) {
			t.Errorf("%s: wrapStarts(%q, %d) = %v, want %v", c.name, c.text, c.width, got, c.want)
		}
	}
}

func TestRowOf(t *testing.T) {
	starts := []int{0, 5, 10}
	for col, want := range map[int]int{0: 0, 4: 0, 5: 1, 9: 1, 10: 2, 50: 2} {
		if got := rowOf(starts, col); got != want {
			t.Errorf("rowOf(%v, %d) = %d, want %d", starts, col, got, want)
		}
	}
}

// wrapView is a wrapped pane 20 columns wide over lines — 4 columns of
// gutter for a buffer of under 10 lines, so 16 of text.
func wrapView(rows int, lines ...string) (*View, *tab, *fakeWindow) {
	v, tb := selectionView(lines...)
	v.SetWrap(true)
	return v, tb, newFakeWindow(20, rows)
}

func TestWrapRendersLongLineOnSeveralRows(t *testing.T) {
	v, _, w := wrapView(10, "aaaa bbbb cccc dddd eeee", "second")
	v.Render(w)

	if !strings.HasSuffix(w.lines[1], "1 aaaa bbbb cccc ") {
		t.Errorf("row 1 = %q, want the line's first 15 columns, broken after a space", w.lines[1])
	}
	if want := "    dddd eeee"; w.lines[2] != want {
		t.Errorf("row 2 = %q, want %q: a blank gutter, then the rest", w.lines[2], want)
	}
	if !strings.HasSuffix(w.lines[3], "2 second") {
		t.Errorf("row 3 = %q, want line 2 numbered as such", w.lines[3])
	}
}

func TestWrapOffKeepsOneRowPerLine(t *testing.T) {
	v, _, w := wrapView(10, "aaaa bbbb cccc dddd eeee", "second")
	v.SetWrap(false)
	v.Render(w)
	if !strings.HasSuffix(w.lines[2], "2 second") {
		t.Errorf("row 2 = %q, want line 2 right below line 1", w.lines[2])
	}
}

func TestWrapCursorPositionOnContinuationRow(t *testing.T) {
	v, tb, w := wrapView(10, "aaaa bbbb cccc dddd eeee")
	tb.cursorCol = 20 // the second "e"
	v.Render(w)

	col, row, ok := v.CursorPosition()
	if !ok || col != 4+20-15 || row != 2 {
		t.Errorf("CursorPosition = (%d, %d, %v), want (9, 2, true)", col, row, ok)
	}
}

func TestWrapScrollsThroughLineTallerThanPane(t *testing.T) {
	// 100 columns at 16 a row is 7 rows, in a pane with room for 3.
	v, tb, w := wrapView(4, strings.Repeat("x", 100))
	tb.cursorCol = 99
	v.Render(w)

	if tb.topLine != 0 || tb.topSub != 4 {
		t.Errorf("top = (%d, %d), want (0, 4): the line's last three rows in view", tb.topLine, tb.topSub)
	}
	if _, row, _ := v.CursorPosition(); row != 3 {
		t.Errorf("cursor row = %d, want 3, the pane's last", row)
	}
	if !strings.HasPrefix(w.lines[1], "    x") {
		t.Errorf("row 1 = %q, want a continuation row (blank gutter)", w.lines[1])
	}

	tb.cursorCol = 0
	v.Render(w)
	if tb.topSub != 0 {
		t.Errorf("topSub = %d after returning to the line start, want 0", tb.topSub)
	}
}

func TestWrapScrollsToCursorLinesBelow(t *testing.T) {
	long := strings.Repeat("y", 40) // 3 rows each
	v, tb, w := wrapView(5, long, long, long, "last")
	tb.cursorLn = 3
	v.Render(w)

	if _, row, _ := v.CursorPosition(); row != 4 {
		t.Errorf("cursor row = %d, want 4 (bottom of a 4-row body)", row)
	}
	if !strings.HasSuffix(w.lines[4], "4 last") {
		t.Errorf("bottom row = %q, want line 4", w.lines[4])
	}
}

func TestWrapClickOnContinuationRow(t *testing.T) {
	v, tb, w := wrapView(10, "aaaa bbbb cccc dddd eeee", "second")
	v.Render(w)

	v.HandleMouse(press(4+2, 2, 1))
	if tb.cursorLn != 0 || tb.cursorCol != 17 {
		t.Errorf("cursor = (%d, %d), want (0, 17): the third column of the second row", tb.cursorLn, tb.cursorCol)
	}

	// Past the end of a row that isn't the line's last: its last character.
	v.HandleMouse(press(19, 1, 1))
	if tb.cursorLn != 0 || tb.cursorCol != 14 {
		t.Errorf("cursor = (%d, %d), want (0, 14)", tb.cursorLn, tb.cursorCol)
	}

	v.HandleMouse(press(4+1, 3, 1))
	if tb.cursorLn != 1 || tb.cursorCol != 1 {
		t.Errorf("cursor = (%d, %d), want (1, 1) on the row after the wrapped line", tb.cursorLn, tb.cursorCol)
	}
}

func TestWrapDragSelectsAcrossRows(t *testing.T) {
	v, tb, w := wrapView(10, "aaaa bbbb cccc dddd eeee")
	v.Render(w)

	v.HandleMouse(press(4+5, 1, 1))
	v.HandleMouse(motion(4+3, 2))
	v.HandleMouse(release(4+3, 2))
	start, end, ok := tb.selectionSpan()
	if !ok || start.ln != 0 || start.col != 5 || end.ln != 0 || end.col != 18 {
		t.Errorf("selection = %+v..%+v (%v), want (0,5)..(0,18)", start, end, ok)
	}
}

func TestSetWrapResetsSidewaysScroll(t *testing.T) {
	v, tb, _ := wrapView(10, "x")
	tb.leftCol, tb.topSub = 7, 2
	v.SetWrap(false)
	if tb.leftCol != 0 || tb.topSub != 0 {
		t.Errorf("leftCol, topSub = %d, %d, want 0, 0", tb.leftCol, tb.topSub)
	}
}
