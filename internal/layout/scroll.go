package layout

// ScrollState is a pane's current scroll position and content extent,
// reported fresh whenever queried — mirrors CursorProvider.CursorPosition's
// contract: meaningful only after Render has run, since Viewport/Total are
// derived from whatever Render last saw (e.g. the editor's lastHeight and
// its active tab's line count).
type ScrollState struct {
	Top      int // first visible content line (0-based)
	Viewport int // visible content lines, excluding any header row
	Total    int // total content lines

	// RowOffset is how many header rows sit above the scrollable region
	// within the pane's own content window (the editor's tab bar is 1;
	// everything else is 0) — it's where ui.App starts drawing the
	// scrollbar track, so the bar lines up with the text it represents
	// rather than the pane's header.
	RowOffset int
}

// Scrollable is implemented by a pane that wants a scrollbar drawn in its
// rightmost content column. A pane reporting Total <= 0 (e.g. the editor
// with no file open) gets no bar and keeps its full pane width — see
// ui.App's render helpers.
type Scrollable interface {
	ScrollState() ScrollState
}

// ScrollTarget is implemented by a pane whose scrollbar can be clicked and
// dragged. Kept separate from Scrollable so a pane can render a static bar
// (e.g. an overlay pane, which receives no mouse events at all today)
// without being draggable.
type ScrollTarget interface {
	// ScrollTo asks the pane to scroll so Top becomes top; the pane clamps
	// it to its own valid range, since that range differs per pane (and,
	// for the editor, moving Top also has to keep the cursor inside the new
	// viewport — see editor.View.ScrollTo).
	ScrollTo(top int)
}

// ScrollMark flags one content line worth calling out in the scrollbar
// track — currently git change status. Priority breaks ties when several
// marked lines land in the same track row (see BucketMarks): higher wins.
type ScrollMark struct {
	Line     int
	Priority int
	Style    Style
}

// ScrollMarker is implemented by a pane that wants marks drawn in its
// scrollbar track, layered underneath the thumb.
type ScrollMarker interface {
	ScrollMarks() []ScrollMark
}

// minThumbSize is the smallest a thumb is ever drawn, so it never
// disappears to nothing on a very long file.
const minThumbSize = 1

// ThumbBounds returns the thumb's start row and height within a scrollbar
// track of `track` rows. show is false when there's nothing to scroll
// (Total <= Viewport) or the pane has reported no usable state (Viewport,
// Total, or track <= 0) — callers should draw no thumb (but may still draw
// the bare track and any marks) in that case.
//
// Exact at both ends by construction: top=0 always yields start=0, and
// top=Total-Viewport (fully scrolled) always yields start=track-size
// exactly — the maximum position needs no rounding, since
// top*maxStart/maxTop == maxStart when top == maxTop.
func ThumbBounds(state ScrollState, track int) (start, size int, show bool) {
	if state.Viewport <= 0 || state.Total <= 0 || track <= 0 {
		return 0, 0, false
	}
	if state.Total <= state.Viewport {
		return 0, 0, false
	}

	size = state.Viewport * track / state.Total
	if size < minThumbSize {
		size = minThumbSize
	}
	// A thumb spanning the whole track can't show position — there'd be
	// nowhere left for it to move to — so clamp it under track whenever
	// there's more than one row to work with. With track == 1 there is
	// only one cell to draw, full stop: the thumb fills it and dragging
	// can't do anything (see ScrollTopForThumbStart's maxStart == 0 case).
	if track > 1 && size > track-1 {
		size = track - 1
	}
	if size > track {
		size = track
	}

	maxTop := state.Total - state.Viewport
	maxStart := track - size
	top := clampInt(state.Top, 0, maxTop)
	if maxStart == 0 {
		start = 0
	} else {
		start = top * maxStart / maxTop
	}
	return start, size, true
}

// ScrollTopForThumbStart is ThumbBounds' inverse, used while dragging: given
// where the thumb's top row should now be, returns the `top` to pass to
// ScrollTo. thumbStart is clamped to [0, track-size] here, so callers don't
// each need to.
func ScrollTopForThumbStart(state ScrollState, track, thumbStart int) int {
	_, size, show := ThumbBounds(state, track)
	if !show {
		return state.Top
	}
	maxStart := track - size
	maxTop := state.Total - state.Viewport
	if maxStart <= 0 || maxTop <= 0 {
		return 0
	}
	thumbStart = clampInt(thumbStart, 0, maxStart)
	return thumbStart * maxTop / maxStart
}

// BucketMarks maps each mark onto its track row — bucket(line) =
// line*track/total, clamped to track-1 — and keeps only the
// highest-Priority mark per row (a tie keeps whichever was seen first).
// O(len(marks)): every mark's line maps directly to exactly one bucket, so
// no per-row scan over marks is needed, and no mark is ever silently
// dropped — at worst two marks share a bucket and the lower-priority one is
// hidden by the higher, never both discarded.
func BucketMarks(marks []ScrollMark, total, track int) map[int]ScrollMark {
	if total <= 0 || track <= 0 || len(marks) == 0 {
		return nil
	}
	buckets := make(map[int]ScrollMark, len(marks))
	for _, m := range marks {
		if m.Line < 0 || m.Line >= total {
			continue
		}
		row := m.Line * track / total
		if row >= track {
			row = track - 1
		}
		if existing, ok := buckets[row]; !ok || m.Priority > existing.Priority {
			buckets[row] = m
		}
	}
	return buckets
}

func clampInt(v, lo, hi int) int {
	if v < lo {
		return lo
	}
	if v > hi {
		return hi
	}
	return v
}
