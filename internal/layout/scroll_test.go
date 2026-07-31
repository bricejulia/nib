package layout

import "testing"

func TestThumbBoundsHiddenWhenEverythingFits(t *testing.T) {
	state := ScrollState{Top: 0, Viewport: 20, Total: 20}
	if _, _, show := ThumbBounds(state, 20); show {
		t.Error("expected no thumb when Total <= Viewport")
	}
}

func TestThumbBoundsHiddenWithNoUsableState(t *testing.T) {
	cases := []ScrollState{
		{Top: 0, Viewport: 0, Total: 100},
		{Top: 0, Viewport: 10, Total: 0},
	}
	for _, s := range cases {
		if _, _, show := ThumbBounds(s, 20); show {
			t.Errorf("expected no thumb for state %+v", s)
		}
	}
	if _, _, show := ThumbBounds(ScrollState{Viewport: 10, Total: 100}, 0); show {
		t.Error("expected no thumb for a zero-height track")
	}
}

func TestThumbBoundsExactAtTop(t *testing.T) {
	state := ScrollState{Top: 0, Viewport: 10, Total: 100}
	start, _, show := ThumbBounds(state, 40)
	if !show {
		t.Fatal("expected a thumb")
	}
	if start != 0 {
		t.Errorf("start = %d, want 0 at Top=0", start)
	}
}

func TestThumbBoundsExactAtBottom(t *testing.T) {
	state := ScrollState{Top: 90, Viewport: 10, Total: 100} // fully scrolled
	track := 40
	start, size, show := ThumbBounds(state, track)
	if !show {
		t.Fatal("expected a thumb")
	}
	if start+size != track {
		t.Errorf("start+size = %d, want exactly track (%d) when fully scrolled", start+size, track)
	}
}

func TestThumbBoundsMinimumSize(t *testing.T) {
	// A huge file relative to the track: proportional size would floor to 0.
	state := ScrollState{Top: 0, Viewport: 10, Total: 1_000_000}
	_, size, show := ThumbBounds(state, 20)
	if !show {
		t.Fatal("expected a thumb")
	}
	if size < 1 {
		t.Errorf("size = %d, want at least 1", size)
	}
}

func TestThumbBoundsMiddlePositionIsBetweenTheEnds(t *testing.T) {
	state := ScrollState{Top: 45, Viewport: 10, Total: 100} // halfway through the scrollable range
	track := 40
	start, size, show := ThumbBounds(state, track)
	if !show {
		t.Fatal("expected a thumb")
	}
	if start <= 0 || start+size >= track {
		t.Errorf("start=%d size=%d should sit strictly between the two ends of a %d-row track", start, size, track)
	}
}

func TestThumbBoundsClampsOutOfRangeTop(t *testing.T) {
	// A caller passing a Top beyond what's actually scrollable (e.g. stale
	// state) must not produce a start past the track.
	state := ScrollState{Top: 10_000, Viewport: 10, Total: 100}
	track := 40
	start, size, show := ThumbBounds(state, track)
	if !show {
		t.Fatal("expected a thumb")
	}
	if start+size != track {
		t.Errorf("start+size = %d, want exactly track (%d) when Top overshoots", start+size, track)
	}
}

func TestThumbBoundsDegenerateSingleRowTrack(t *testing.T) {
	// A track too short to show position at all: the thumb fills it
	// regardless of scroll position, which is the accepted degenerate case
	// rather than a bug — there is no room to draw anything else.
	state := ScrollState{Top: 50, Viewport: 10, Total: 100}
	start, size, show := ThumbBounds(state, 1)
	if !show {
		t.Fatal("expected a thumb even in the degenerate case")
	}
	if start != 0 || size != 1 {
		t.Errorf("start=%d size=%d, want start=0 size=1 for a 1-row track", start, size)
	}
}

func TestScrollTopForThumbStartIsThumbBoundsInverse(t *testing.T) {
	state := ScrollState{Viewport: 10, Total: 100}
	track := 40

	if got := ScrollTopForThumbStart(state, track, 0); got != 0 {
		t.Errorf("thumbStart=0 -> top=%d, want 0", got)
	}

	_, size, show := ThumbBounds(state, track)
	if !show {
		t.Fatal("expected a thumb")
	}
	maxStart := track - size
	if got, want := ScrollTopForThumbStart(state, track, maxStart), state.Total-state.Viewport; got != want {
		t.Errorf("thumbStart=maxStart -> top=%d, want %d (fully scrolled)", got, want)
	}
}

func TestScrollTopForThumbStartClampsOutOfRangeInput(t *testing.T) {
	state := ScrollState{Viewport: 10, Total: 100}
	track := 40
	_, size, _ := ThumbBounds(state, track)
	maxStart := track - size

	if got := ScrollTopForThumbStart(state, track, -5); got != 0 {
		t.Errorf("negative thumbStart -> top=%d, want 0", got)
	}
	if got, want := ScrollTopForThumbStart(state, track, track*10), state.Total-state.Viewport; got != want {
		t.Errorf("thumbStart way past track -> top=%d, want %d", got, want)
	}
	_ = maxStart
}

func TestScrollTopForThumbStartDegenerateReturnsZero(t *testing.T) {
	state := ScrollState{Top: 50, Viewport: 10, Total: 100}
	if got := ScrollTopForThumbStart(state, 1, 0); got != 0 {
		t.Errorf("degenerate 1-row track -> top=%d, want 0 (dragging can't do anything there)", got)
	}
}

func TestBucketMarksKeepsHighestPriorityPerBucket(t *testing.T) {
	// total=100, track=10 -> each bucket covers 10 lines. Lines 2 and 5 both
	// land in bucket 0.
	marks := []ScrollMark{
		{Line: 2, Priority: 1, Style: Style{Foreground: ColorGreen}},
		{Line: 5, Priority: 2, Style: Style{Foreground: ColorRed}},
	}
	buckets := BucketMarks(marks, 100, 10)
	got, ok := buckets[0]
	if !ok {
		t.Fatal("expected a mark in bucket 0")
	}
	if got.Priority != 2 || got.Style.Foreground != ColorRed {
		t.Errorf("got %+v, want the priority-2 (red) mark to win", got)
	}
}

func TestBucketMarksNeverDropsASingleLineChange(t *testing.T) {
	// A lone mark on a huge file must still surface in exactly one bucket,
	// not be silently sampled away.
	marks := []ScrollMark{{Line: 500_000, Priority: 1}}
	buckets := BucketMarks(marks, 1_000_000, 40)
	if len(buckets) != 1 {
		t.Fatalf("expected exactly one bucket populated, got %+v", buckets)
	}
}

func TestBucketMarksIgnoresOutOfRangeLines(t *testing.T) {
	marks := []ScrollMark{{Line: -1}, {Line: 100}, {Line: 50, Priority: 1}}
	buckets := BucketMarks(marks, 100, 10)
	if len(buckets) != 1 {
		t.Errorf("expected only the in-range mark to produce a bucket, got %+v", buckets)
	}
}

func TestBucketMarksEmptyInputsReturnNil(t *testing.T) {
	if got := BucketMarks(nil, 100, 10); got != nil {
		t.Errorf("no marks should return nil, got %+v", got)
	}
	if got := BucketMarks([]ScrollMark{{Line: 1}}, 0, 10); got != nil {
		t.Errorf("total=0 should return nil, got %+v", got)
	}
	if got := BucketMarks([]ScrollMark{{Line: 1}}, 100, 0); got != nil {
		t.Errorf("track=0 should return nil, got %+v", got)
	}
}
