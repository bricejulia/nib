package editor

import (
	"strings"
	"testing"

	"github.com/bricejulia/nib/internal/layout"
)

func TestFindMatchesReportsRuneIndices(t *testing.T) {
	buf := &Buffer{Lines: []string{
		"foo bar foo",
		"nothing here",
		"héllo foo", // multi-byte before the match: rune != byte index
	}}

	got := findMatches(buf, "foo")

	want := []searchMatch{
		{ln: 0, start: 0, end: 3},
		{ln: 0, start: 8, end: 11},
		{ln: 2, start: 6, end: 9}, // "héllo " is 6 RUNES (7 bytes)
	}
	if len(got) != len(want) {
		t.Fatalf("got %+v, want %+v", got, want)
	}
	for i := range want {
		if got[i] != want[i] {
			t.Errorf("match %d = %+v, want %+v", i, got[i], want[i])
		}
	}
}

func TestFindMatchesEmptyPatternMatchesNothing(t *testing.T) {
	buf := &Buffer{Lines: []string{"anything"}}
	if got := findMatches(buf, ""); len(got) != 0 {
		t.Errorf("got %+v, want none", got)
	}
}

func TestFindMatchesIsCaseSensitive(t *testing.T) {
	buf := &Buffer{Lines: []string{"Foo foo FOO"}}
	if got := findMatches(buf, "foo"); len(got) != 1 || got[0].start != 4 {
		t.Errorf("got %+v, want only the exact-case match at rune 4", got)
	}
}

func TestFindMatchesFindsOverlappingOccurrences(t *testing.T) {
	// "aa" occurs at 0, 1 and 2 in "aaaa" — advancing by one rather than by
	// the match length is what catches the overlaps.
	buf := &Buffer{Lines: []string{"aaaa"}}
	if got := findMatches(buf, "aa"); len(got) != 3 {
		t.Errorf("got %d matches, want 3: %+v", len(got), got)
	}
}

func TestApplyHighlightRangesSplitsSegmentsAndKeepsOriginalStyle(t *testing.T) {
	// One segment, highlight the middle: expect three segments, the middle
	// one reverse-video, all keeping the original foreground.
	segs := []layout.Segment{
		{Text: "abcdef", Style: layout.Style{Foreground: layout.ColorGreen}},
	}

	got := applyHighlightRanges(segs, []runeRange{{start: 2, end: 4}}, searchHighlightStyle)

	if joinSegs(got) != "abcdef" {
		t.Fatalf("text changed: %q", joinSegs(got))
	}
	if len(got) != 3 {
		t.Fatalf("got %d segments, want 3: %+v", len(got), got)
	}
	if got[1].Text != "cd" || got[1].Style.Attr&layout.AttrReverse == 0 {
		t.Errorf("middle segment = %+v, want \"cd\" reverse-video", got[1])
	}
	for i, s := range got {
		if s.Style.Foreground != layout.ColorGreen {
			t.Errorf("segment %d lost its syntax colour: %+v", i, s)
		}
	}
	if got[0].Style.Attr&layout.AttrReverse != 0 || got[2].Style.Attr&layout.AttrReverse != 0 {
		t.Error("only the matched range should be highlighted")
	}
}

func TestApplyHighlightRangesSpansSegmentBoundaries(t *testing.T) {
	// A match straddling two differently-styled segments (e.g. a keyword
	// boundary) must highlight both halves without merging their styles.
	segs := []layout.Segment{
		{Text: "abc", Style: layout.Style{Foreground: layout.ColorYellow}},
		{Text: "def", Style: layout.Style{Foreground: layout.ColorBlue}},
	}

	got := applyHighlightRanges(segs, []runeRange{{start: 2, end: 4}}, searchHighlightStyle)

	if joinSegs(got) != "abcdef" {
		t.Fatalf("text changed: %q", joinSegs(got))
	}
	var highlighted string
	for _, s := range got {
		if s.Style.Attr&layout.AttrReverse != 0 {
			highlighted += s.Text
		}
	}
	if highlighted != "cd" {
		t.Errorf("highlighted %q, want %q", highlighted, "cd")
	}
}

func TestApplyHighlightRangesNoRangesIsUnchanged(t *testing.T) {
	segs := []layout.Segment{{Text: "abc"}}
	got := applyHighlightRanges(segs, nil, searchHighlightStyle)
	if len(got) != 1 || got[0].Text != "abc" {
		t.Errorf("got %+v, want the input untouched", got)
	}
}

func joinSegs(segs []layout.Segment) string {
	var b strings.Builder
	for _, s := range segs {
		b.WriteString(s.Text)
	}
	return b.String()
}

// searchView builds a pane over a small buffer for driving the "/" prompt.
func searchView() (*View, *tab) {
	v := NewView()
	v.tabs = []*tab{{path: "t.txt", buf: &Buffer{Path: "t.txt", Lines: []string{
		"alpha one",
		"beta two",
		"alpha three",
	}}}}
	v.active = 0
	return v, v.activeTab()
}

func TestSlashPromptShowsInStatusBarAndJumpsOnEnter(t *testing.T) {
	v, tb := searchView()

	if !v.HandleKey(layout.Key{Text: "/"}) {
		t.Fatal("expected '/' to be consumed")
	}
	if v.mode != modeSearch {
		t.Fatal("expected search mode")
	}
	for _, r := range "beta" {
		v.HandleKey(layout.Key{Text: string(r)})
	}
	if got := v.StatusText(); got != "/beta" {
		t.Errorf("StatusText = %q, want %q", got, "/beta")
	}

	v.HandleKey(layout.Key{Named: layout.KeyEnter})

	if v.mode != modeNormal {
		t.Error("expected Enter to leave search mode")
	}
	if tb.cursorLn != 1 {
		t.Errorf("cursorLn = %d, want 1 (the \"beta two\" line)", tb.cursorLn)
	}
}

func TestSearchEscRestoresCursorAndClearsHighlights(t *testing.T) {
	v, tb := searchView()
	tb.cursorLn, tb.cursorCol = 2, 3
	startLn, startCol := tb.cursorLn, tb.cursorCol

	v.HandleKey(layout.Key{Text: "/"})
	for _, r := range "alpha" {
		v.HandleKey(layout.Key{Text: string(r)})
	}
	if len(v.searchMatches) == 0 {
		t.Fatal("expected matches highlighted while typing")
	}

	v.HandleKey(layout.Key{Named: layout.KeyEsc})

	if v.mode != modeNormal {
		t.Error("expected Esc to leave search mode")
	}
	if tb.cursorLn != startLn || tb.cursorCol != startCol {
		t.Errorf("cursor = (%d,%d), want the pre-search (%d,%d)", tb.cursorLn, tb.cursorCol, startLn, startCol)
	}
	if len(v.searchMatches) != 0 {
		t.Error("expected highlights cleared on cancel")
	}
}

func TestSearchHighlightsUpdateWhileTypingWithoutMovingCursor(t *testing.T) {
	v, tb := searchView()
	startLn, startCol := tb.cursorLn, tb.cursorCol

	v.HandleKey(layout.Key{Text: "/"})
	v.HandleKey(layout.Key{Text: "a"})
	if len(v.searchMatches) == 0 {
		t.Fatal("expected highlights while typing")
	}
	if tb.cursorLn != startLn || tb.cursorCol != startCol {
		t.Error("the cursor must not move until Enter")
	}

	// Narrowing the pattern narrows the match set.
	before := len(v.searchMatches)
	v.HandleKey(layout.Key{Text: "l"}) // "al"
	if len(v.searchMatches) >= before {
		t.Errorf("expected fewer matches for a longer pattern, got %d then %d", before, len(v.searchMatches))
	}
}

func TestSearchBackspaceEditsPattern(t *testing.T) {
	v, _ := searchView()
	v.HandleKey(layout.Key{Text: "/"})
	v.HandleKey(layout.Key{Text: "a"})
	v.HandleKey(layout.Key{Text: "z"})
	v.HandleKey(layout.Key{Named: layout.KeyBackspace})

	if got := v.StatusText(); got != "/a" {
		t.Errorf("StatusText = %q, want %q", got, "/a")
	}
}

func TestSearchNextAndPrevWrapAround(t *testing.T) {
	v, tb := searchView() // "alpha" on lines 0 and 2
	v.HandleKey(layout.Key{Text: "/"})
	for _, r := range "alpha" {
		v.HandleKey(layout.Key{Text: string(r)})
	}
	v.HandleKey(layout.Key{Named: layout.KeyEnter})
	if tb.cursorLn != 2 {
		t.Fatalf("setup: first jump went to line %d, want 2 (next match after line 0)", tb.cursorLn)
	}

	// n from the last match wraps to the first.
	v.HandleKey(layout.Key{Text: "n"})
	if tb.cursorLn != 0 {
		t.Errorf("after n: cursorLn = %d, want 0 (wrapped)", tb.cursorLn)
	}
	// N from the first match wraps back to the last.
	v.HandleKey(layout.Key{Text: "N"})
	if tb.cursorLn != 2 {
		t.Errorf("after N: cursorLn = %d, want 2 (wrapped back)", tb.cursorLn)
	}
}

func TestSearchNextWithoutAnActiveSearchIsNoop(t *testing.T) {
	v, tb := searchView()
	startLn := tb.cursorLn

	if !v.HandleKey(layout.Key{Text: "n"}) {
		t.Fatal("expected 'n' to still be consumed")
	}
	if tb.cursorLn != startLn {
		t.Errorf("cursor moved with no active search: %d", tb.cursorLn)
	}
}

func TestSearchWithNoMatchLeavesCursorAlone(t *testing.T) {
	v, tb := searchView()
	tb.cursorLn = 1
	v.HandleKey(layout.Key{Text: "/"})
	for _, r := range "zzznope" {
		v.HandleKey(layout.Key{Text: string(r)})
	}
	v.HandleKey(layout.Key{Named: layout.KeyEnter})

	if tb.cursorLn != 1 {
		t.Errorf("cursorLn = %d, want 1 (unchanged when nothing matched)", tb.cursorLn)
	}
}

func TestSearchMatchesRenderHighlighted(t *testing.T) {
	v, _ := searchView()
	v.HandleKey(layout.Key{Text: "/"})
	for _, r := range "alpha" {
		v.HandleKey(layout.Key{Text: string(r)})
	}
	v.HandleKey(layout.Key{Named: layout.KeyEnter})

	w := newFakeWindow(40, 10)
	v.Render(w)

	// Buffer line 0 ("alpha one") renders on row 1 and contains a match.
	if !rowHasStyle(w, 1, func(s layout.Style) bool { return s.Attr&layout.AttrReverse != 0 }) {
		t.Errorf("expected a reverse-video match on row 1, got %+v", w.segs[1])
	}
	// Line 1 ("beta two") has no match, so nothing highlighted.
	if rowHasStyle(w, 2, func(s layout.Style) bool { return s.Attr&layout.AttrReverse != 0 }) {
		t.Errorf("row 2 has no match; expected no highlight, got %+v", w.segs[2])
	}
}

func TestSearchModeIsNotAffectedByNormalModeLetters(t *testing.T) {
	// Letters bound to Normal-mode actions ("n", "j", "x"...) must be typed
	// into the pattern while the prompt is open, not re-trigger their action.
	v, tb := searchView()
	startLn := tb.cursorLn

	v.HandleKey(layout.Key{Text: "/"})
	v.HandleKey(layout.Key{Text: "n"})
	v.HandleKey(layout.Key{Text: "j"})

	if got := v.StatusText(); got != "/nj" {
		t.Errorf("StatusText = %q, want %q", got, "/nj")
	}
	if tb.cursorLn != startLn {
		t.Errorf("cursor moved to %d; letters must be literal in the prompt", tb.cursorLn)
	}
}
