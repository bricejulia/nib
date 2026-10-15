package editor

import (
	"github.com/mattn/go-runewidth"

	"github.com/bricejulia/nib/internal/textwidth"
)

// Soft wrap: with View.wrap on, a line wider than the pane is drawn across
// several screen rows ("visual rows") instead of scrolling horizontally.
// Nothing about the buffer or the cursor changes — cursorLn/cursorCol stay
// in file-line terms and j/k still move by file line — so wrapping is
// purely a matter of mapping between a (line, display column) and a screen
// (row, column). Everything that does that mapping (renderBody, cursorAnchor,
// positionAt) goes through wrapStarts, so the three agree on where every
// row breaks.

// wrapStarts returns the display column at which each visual row of text
// begins when it is wrapped to width columns; the first is always 0, and a
// line that fits returns just that. text is tab-expanded already.
//
// Rows break after the last space that still fits (word wrap), or at the
// width itself when a row has no space to break at. A double-width rune
// that would straddle the edge moves whole onto the next row, so
// SliceSegmentsByDisplayColumn never has to drop it. Zero-width runes count
// as one column, matching the slicers in internal/textwidth.
func wrapStarts(text string, width int) []int {
	starts := []int{0}
	if width <= 0 {
		return starts
	}
	rowStart := 0
	// afterSpace is the display column just past the most recent space in
	// the current row — where the row would break for word wrap — or -1
	// when the row has none yet.
	afterSpace := -1
	col := 0
	for _, r := range text {
		w := runewidth.RuneWidth(r)
		if w == 0 {
			w = 1
		}
		for col+w-rowStart > width {
			if col == rowStart {
				// A rune wider than the whole row, at the start of one: it
				// gets that row to itself rather than an empty row above.
				break
			}
			if afterSpace > rowStart && afterSpace <= col {
				rowStart = afterSpace
			} else {
				rowStart = col
			}
			starts = append(starts, rowStart)
			afterSpace = -1
		}
		col += w
		if r == ' ' {
			afterSpace = col
		}
	}
	return starts
}

// rowOf returns which visual row of a line (as wrapStarts split it) holds
// display column col: the last row starting at or before it. A column
// exactly at a break belongs to the row it starts.
func rowOf(starts []int, col int) int {
	row := 0
	for i, s := range starts {
		if s > col {
			break
		}
		row = i
	}
	return row
}

// visualLineText is buffer line ln as wrapping sees it: clipped like renderBody
// clips it, tab-expanded, and with the truncation marker when renderBody
// would draw one — so its breaks are exactly the ones renderBody draws.
func visualLineText(t *tab, ln, tabWidth int) string {
	if t.buf == nil || ln < 0 || ln >= len(t.buf.Lines) {
		return ""
	}
	line, truncated := clipLineForRender(t.buf.Lines[ln])
	text := textwidth.ExpandTabs(line, tabWidth)
	if truncated {
		text += renderTruncationMarker.Text
	}
	return text
}

// lineWrapStarts is wrapStarts for buffer line ln of t at content width
// width.
func lineWrapStarts(t *tab, ln, tabWidth, width int) []int {
	return wrapStarts(visualLineText(t, ln, tabWidth), width)
}

// wrapWidth is how many columns of text a pane width cols leaves beside
// t's gutter — the width rows wrap at.
func wrapWidth(t *tab, cols int) int {
	return max(0, cols-gutterWidthFor(t))
}

// cursorWrapRow is the visual row of the cursor's own line that the cursor
// is on.
func cursorWrapRow(t *tab, tabWidth, width int) int {
	return rowOf(lineWrapStarts(t, t.cursorLn, tabWidth, width), cursorDisplayColumn(t, tabWidth))
}

// stepVisualRows moves n visual rows (negative for up) from row sub of line
// ln, stopping at the first row of the buffer or the last row of its last
// line.
func stepVisualRows(t *tab, ln, sub, n, tabWidth, width int) (int, int) {
	last := len(t.buf.Lines) - 1
	for ; n > 0; n-- {
		if sub+1 < len(lineWrapStarts(t, ln, tabWidth, width)) {
			sub++
		} else if ln < last {
			ln, sub = ln+1, 0
		} else {
			break
		}
	}
	for ; n < 0; n++ {
		if sub > 0 {
			sub--
		} else if ln > 0 {
			ln--
			sub = len(lineWrapStarts(t, ln, tabWidth, width)) - 1
		} else {
			break
		}
	}
	return ln, sub
}

// visualRowsBetween counts the visual rows from (ln, sub) forward to
// (toLn, toSub), giving up at limit — the caller only ever needs to know
// whether the target is within a viewport, and lines between could be
// arbitrarily many. Returns a negative count when the target is before the
// start.
func visualRowsBetween(t *tab, ln, sub, toLn, toSub, limit, tabWidth, width int) int {
	if toLn < ln || (toLn == ln && toSub < sub) {
		return -1
	}
	n := -sub
	for ; ln < toLn; ln++ {
		n += len(lineWrapStarts(t, ln, tabWidth, width))
		if n > limit {
			return n
		}
	}
	return n + toSub
}

// scrollWrapped is renderBody's keep-the-cursor-visible step for wrap mode:
// the same rule as the unwrapped one — move the viewport only as far as it
// takes — counted in visual rows, with topSub letting the viewport start
// partway through a line taller than the pane.
func scrollWrapped(t *tab, tabWidth, width, rows int) {
	t.leftCol = 0
	t.topLine = max(0, min(t.topLine, len(t.buf.Lines)-1))
	t.topSub = max(0, min(t.topSub, len(lineWrapStarts(t, t.topLine, tabWidth, width))-1))

	curSub := cursorWrapRow(t, tabWidth, width)
	dist := visualRowsBetween(t, t.topLine, t.topSub, t.cursorLn, curSub, rows, tabWidth, width)
	switch {
	case dist < 0:
		t.topLine, t.topSub = t.cursorLn, curSub
	case dist >= rows:
		t.topLine, t.topSub = stepVisualRows(t, t.cursorLn, curSub, -(rows - 1), tabWidth, width)
	}
}
