package editor

import (
	"strings"
	"testing"

	"github.com/bricejulia/nib/internal/layout"
)

func segsText(segs []layout.Segment) string {
	var b strings.Builder
	for _, s := range segs {
		b.WriteString(s.Text)
	}
	return b.String()
}

func TestExpandTabsForDisplayReplacesSpacesWithDots(t *testing.T) {
	segs := []layout.Segment{{Text: "a b  c"}}
	got := expandTabsForDisplay(segs, 4)
	if want := "a·b··c"; segsText(got) != want {
		t.Errorf("expandTabsForDisplay text = %q, want %q", segsText(got), want)
	}
	// Rune count must be preserved: every position downstream (cursor math,
	// viewport slicing) depends on it.
	if len([]rune(segsText(got))) != len([]rune("a b  c")) {
		t.Errorf("expandTabsForDisplay changed the rune count")
	}
}

func TestExpandTabsForDisplayMarksASingleTabWithOneArrow(t *testing.T) {
	segs := []layout.Segment{{Text: "ab\tcd"}}
	got := expandTabsForDisplay(segs, 4)
	if want := "ab→ cd"; segsText(got) != want { // col 2 -> next stop at 4: arrow + 1 pad space
		t.Errorf("expandTabsForDisplay text = %q, want %q", segsText(got), want)
	}
}

func TestExpandTabsForDisplayMarksEachOfSeveralChainedTabsSeparately(t *testing.T) {
	// The bug this guards against: expanding tabs to spaces FIRST and then
	// guessing arrow placement from "runs of spaces" in the output can't
	// tell three adjacent tabs apart from one long run of tab-fill, and
	// draws only one arrow for all of them combined. Marking each tab as
	// it's expanded (not after) is what makes three tabs draw three
	// arrows.
	segs := []layout.Segment{{Text: "\t\t\tx"}}
	got := expandTabsForDisplay(segs, 4)
	want := "→   →   →   x" // three 4-column tab stops, each with its own arrow
	if segsText(got) != want {
		t.Errorf("expandTabsForDisplay text = %q, want %q", segsText(got), want)
	}
	if n := strings.Count(segsText(got), "→"); n != 3 {
		t.Errorf("got %d arrows, want 3 (one per tab character)", n)
	}
}

func TestExpandTabsForDisplayTabStopDependsOnStartingColumn(t *testing.T) {
	// A tab starting at column 2 (width 4) only needs 2 fill columns, not
	// a full 4 — tab-stop width depends on where the tab starts, exactly
	// like plain ExpandTabs/ExpandTabsSegments.
	segs := []layout.Segment{{Text: "ab\tc"}}
	got := expandTabsForDisplay(segs, 4)
	if want := "ab→ c"; segsText(got) != want {
		t.Errorf("expandTabsForDisplay text = %q, want %q", segsText(got), want)
	}
}

func TestRenderShowsWhitespaceGlyphsWhenEnabled(t *testing.T) {
	v := NewView()
	v.SetShowWhitespace(true)
	v.Open(fixturePath(t, "editor_sample.txt"))
	w := newFakeWindow(40, 10)
	v.Render(w)

	// Row 1 is "line one": the space between the words becomes a dot.
	if !strings.Contains(w.lines[1], "line·one") {
		t.Errorf("row 1 = %q, want the space rendered as a dot", w.lines[1])
	}
	// Row 2 is "\ttabbed line": the leading tab's expansion should show an
	// arrow, and the space between "tabbed"/"line" a dot.
	if !strings.Contains(w.lines[2], "→") {
		t.Errorf("row 2 = %q, want the expanded tab to show an arrow", w.lines[2])
	}
	if !strings.Contains(w.lines[2], "tabbed·line") {
		t.Errorf("row 2 = %q, want the space rendered as a dot", w.lines[2])
	}
}

func TestRenderLeavesWhitespaceLiteralWhenDisabled(t *testing.T) {
	v := NewView() // showWhitespace defaults to false
	v.Open(fixturePath(t, "editor_sample.txt"))
	w := newFakeWindow(40, 10)
	v.Render(w)

	if strings.ContainsAny(w.lines[1], "·→") {
		t.Errorf("row 1 = %q, want no whitespace glyphs when disabled", w.lines[1])
	}
	if !strings.Contains(w.lines[1], "line one") {
		t.Errorf("row 1 = %q, want the literal space preserved", w.lines[1])
	}
}
