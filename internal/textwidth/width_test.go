package textwidth

import (
	"testing"

	"github.com/bricejulia/kiwi/internal/layout"
)

func segText(segs []layout.Segment) string {
	s := ""
	for _, seg := range segs {
		s += seg.Text
	}
	return s
}

func TestExpandTabsAlignsToTabStopsFromLineStart(t *testing.T) {
	cases := []struct {
		name     string
		line     string
		tabWidth int
		want     string
	}{
		{"tab at start", "\tx", 4, "    x"},
		{"tab mid-line aligns to next stop", "ab\tx", 4, "ab  x"},
		{"tab exactly at stop still advances a full stop", "abcd\tx", 4, "abcd    x"},
		{"multiple tabs", "a\tb\tc", 4, "a   b   c"},
		{"tab width 8", "ab\tx", 8, "ab      x"},
	}
	for _, c := range cases {
		t.Run(c.name, func(t *testing.T) {
			got := ExpandTabs(c.line, c.tabWidth)
			if got != c.want {
				t.Errorf("ExpandTabs(%q, %d) = %q, want %q", c.line, c.tabWidth, got, c.want)
			}
		})
	}
}

func TestExpandTabsSegmentsThreadsColumnAcrossSegmentBoundary(t *testing.T) {
	// A tab straddling a segment boundary must still land on the tab stop
	// determined by the WHOLE line's column, not reset per segment.
	segs := []layout.Segment{
		{Text: "a\t", Style: layout.Style{Foreground: layout.ColorRed}},
		{Text: "\tb", Style: layout.Style{Foreground: layout.ColorGreen}},
	}
	got := ExpandTabsSegments(segs, 4)
	joined := segText(got)
	want := segText([]layout.Segment{{Text: ExpandTabs("a\t\tb", 4)}})
	if joined != want {
		t.Fatalf("got %q, want %q (must match ExpandTabs on the concatenated plain string)", joined, want)
	}
	if len(got) != 2 {
		t.Fatalf("expected 2 segments preserved, got %d: %+v", len(got), got)
	}
	if got[0].Style.Foreground != layout.ColorRed || got[1].Style.Foreground != layout.ColorGreen {
		t.Errorf("styles must be preserved per segment, got %+v", got)
	}
}

func TestExpandTabsSegmentsAtLineStart(t *testing.T) {
	got := ExpandTabsSegments([]layout.Segment{{Text: "\tx"}}, 4)
	if segText(got) != "    x" {
		t.Errorf("got %q, want %q", segText(got), "    x")
	}
}

func TestExpandTabsSegmentsNoTabsPassesThroughUnchanged(t *testing.T) {
	segs := []layout.Segment{{Text: "abc", Style: layout.Style{Foreground: layout.ColorBlue}}}
	got := ExpandTabsSegments(segs, 4)
	if len(got) != 1 || got[0].Text != "abc" || got[0].Style.Foreground != layout.ColorBlue {
		t.Errorf("got %+v, want unchanged input", got)
	}
}

func TestExpandTabsSegmentsCJKAdvancesColumnByTwo(t *testing.T) {
	// "日" is width 2; a following tab must land on the stop as if two
	// columns were consumed, matching ExpandTabs's own CJK handling.
	segs := []layout.Segment{
		{Text: "日"},
		{Text: "\tx"},
	}
	got := ExpandTabsSegments(segs, 4)
	want := ExpandTabs("日\tx", 4)
	if segText(got) != want {
		t.Errorf("got %q, want %q", segText(got), want)
	}
}

func TestExpandTabsSegmentsEmptyInput(t *testing.T) {
	got := ExpandTabsSegments(nil, 4)
	if len(got) != 0 {
		t.Errorf("expected empty output for empty input, got %+v", got)
	}
}

func TestExpandTabsSegmentsZeroTabWidthDefaultsToEight(t *testing.T) {
	got := ExpandTabsSegments([]layout.Segment{{Text: "\tx"}}, 0)
	want := ExpandTabs("\tx", 0)
	if segText(got) != want {
		t.Errorf("got %q, want %q", segText(got), want)
	}
}

func TestDisplayWidthASCII(t *testing.T) {
	if w := DisplayWidth("hello"); w != 5 {
		t.Errorf("got %d, want 5", w)
	}
}

func TestDisplayWidthCJKIsDoubleWidth(t *testing.T) {
	// 3 CJK characters, each width 2 -> total 6.
	if w := DisplayWidth("日本語"); w != 6 {
		t.Errorf("got %d, want 6", w)
	}
}

func TestSliceByDisplayColumnBasicASCII(t *testing.T) {
	got := SliceByDisplayColumn("abcdefgh", 2, 3)
	if got != "cde" {
		t.Errorf("got %q, want %q", got, "cde")
	}
}

func TestSliceByDisplayColumnDropsWideRuneAtRightEdgeRatherThanSplitting(t *testing.T) {
	// "a日b": a(1) 日(2,cols1-2) b(1,col3). Window [0,2) should show "a"
	// then a padded space instead of half of 日.
	got := SliceByDisplayColumn("a日b", 0, 2)
	if len([]rune(got)) != 2 {
		t.Fatalf("expected 2-rune-wide result (a + pad), got %q (runes=%d)", got, len([]rune(got)))
	}
	if []rune(got)[0] != 'a' {
		t.Errorf("first rune should be 'a', got %q", got)
	}
	if []rune(got)[1] != ' ' {
		t.Errorf("second cell should be a padding space (日 dropped whole), got %q", got)
	}
}

func TestSliceByDisplayColumnDropsWideRuneAtLeftEdgeRatherThanSplitting(t *testing.T) {
	// "a日b": a occupies col 0, 日 occupies cols 1-2, b occupies col 3.
	// Scrolling to start at column 2 lands mid-日 (its second cell).
	got := SliceByDisplayColumn("a日b", 2, 3)
	runes := []rune(got)
	if len(runes) == 0 {
		t.Fatalf("expected non-empty output")
	}
	if runes[0] != ' ' {
		t.Errorf("expected leading pad space for the straddled wide rune, got %q", got)
	}
	// The wide rune must not appear split — it should be entirely absent,
	// replaced by padding, with 'b' following.
	for _, r := range runes {
		if r == '日' {
			t.Errorf("wide rune should have been dropped whole, found it in output %q", got)
		}
	}
}

func TestSliceByDisplayColumnEmptyWindow(t *testing.T) {
	if got := SliceByDisplayColumn("abc", 0, 0); got != "" {
		t.Errorf("got %q, want empty string", got)
	}
}

func TestSliceByDisplayColumnBeyondEndOfLine(t *testing.T) {
	if got := SliceByDisplayColumn("abc", 10, 5); got != "" {
		t.Errorf("got %q, want empty string for a viewport past the end of the line", got)
	}
}

func oneSeg(text string, style layout.Style) []layout.Segment {
	return []layout.Segment{{Text: text, Style: style}}
}

func TestSliceSegmentsByDisplayColumnBasicASCII(t *testing.T) {
	got := SliceSegmentsByDisplayColumn(oneSeg("abcdefgh", layout.Style{}), 2, 3)
	if text := segText(got); text != "cde" {
		t.Errorf("got %q, want %q", text, "cde")
	}
}

func TestSliceSegmentsByDisplayColumnPreservesStyleAcrossMultipleSegments(t *testing.T) {
	red := layout.Style{Foreground: layout.ColorRed}
	green := layout.Style{Foreground: layout.ColorGreen}
	segs := []layout.Segment{
		{Text: "abc", Style: red},
		{Text: "def", Style: green},
	}
	got := SliceSegmentsByDisplayColumn(segs, 2, 3)
	if text := segText(got); text != "cde" {
		t.Fatalf("got %q, want %q", text, "cde")
	}
	// "c" (red) and "de" (green) must survive as two distinctly-styled
	// output segments, not merged or restyled.
	if len(got) != 2 {
		t.Fatalf("expected 2 segments (styles must not merge across the split), got %+v", got)
	}
	if got[0].Text != "c" || got[0].Style != red {
		t.Errorf("segment 0 = %+v, want {c, red}", got[0])
	}
	if got[1].Text != "de" || got[1].Style != green {
		t.Errorf("segment 1 = %+v, want {de, green}", got[1])
	}
}

func TestSliceSegmentsByDisplayColumnDropsWideRuneAtBoundary(t *testing.T) {
	got := SliceSegmentsByDisplayColumn(oneSeg("a日b", layout.Style{}), 0, 2)
	text := segText(got)
	runes := []rune(text)
	if len(runes) != 2 || runes[0] != 'a' || runes[1] != ' ' {
		t.Fatalf("expected \"a\" + pad space (日 dropped whole), got %q", text)
	}
}

func TestSliceSegmentsByDisplayColumnEmptyWindow(t *testing.T) {
	got := SliceSegmentsByDisplayColumn(oneSeg("abc", layout.Style{}), 0, 0)
	if len(got) != 0 {
		t.Errorf("got %+v, want no segments", got)
	}
}

func TestClampScrollWithinRangeIsUnchanged(t *testing.T) {
	if got := ClampScroll(3, 20, 10); got != 3 {
		t.Errorf("got %d, want 3 (already valid, should pass through)", got)
	}
}

func TestClampScrollNegativeClampsToZero(t *testing.T) {
	if got := ClampScroll(-5, 20, 10); got != 0 {
		t.Errorf("got %d, want 0", got)
	}
}

func TestClampScrollBeyondEndOfTextClampsToMax(t *testing.T) {
	// 20-wide text in a 10-wide viewport: max useful scroll is 10 (so the
	// last column of text lines up with the viewport's right edge).
	if got := ClampScroll(100, 20, 10); got != 10 {
		t.Errorf("got %d, want 10", got)
	}
}

func TestClampScrollTextShorterThanViewportClampsToZero(t *testing.T) {
	// Text already fits entirely: no scrolling is meaningful at all.
	if got := ClampScroll(5, 8, 20); got != 0 {
		t.Errorf("got %d, want 0", got)
	}
}
