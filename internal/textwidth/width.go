// Package textwidth is the shared display-column math for anything that
// renders text into a fixed-width terminal cell grid: tab expansion,
// rune-width-aware slicing (so a double-width CJK/emoji glyph is never
// split in half at a viewport boundary), and clamping a horizontal scroll
// offset to what a given line of text actually needs. Used by both the
// editor pane (source lines) and any list-style pane showing long,
// possibly-truncated single lines (the file tree, the fuzzy finder).
package textwidth

import (
	"strings"

	"github.com/mattn/go-runewidth"

	"github.com/bricejulia/nib/internal/layout"
)

// ExpandTabs replaces each tab in line with spaces up to the next tab stop,
// where tab stops are aligned to tabWidth-column multiples measured from
// the start of the line — not a fixed N-space replacement, since a tab's
// rendered width depends on the column it starts at.
func ExpandTabs(line string, tabWidth int) string {
	if tabWidth <= 0 {
		tabWidth = 8
	}
	var b []rune
	col := 0
	for _, r := range line {
		if r == '\t' {
			spaces := tabWidth - (col % tabWidth)
			for i := 0; i < spaces; i++ {
				b = append(b, ' ')
			}
			col += spaces
			continue
		}
		b = append(b, r)
		col += runewidth.RuneWidth(r)
	}
	return string(b)
}

// ExpandTabsSegments is ExpandTabs's counterpart for styled text: it
// expands every '\t' across segs into spaces up to the next tabWidth-column
// stop, threading ONE running display column across all segments in order —
// tab-stop width depends on the column a tab starts at, which is a property
// of the whole line, not of any single segment, so a tab straddling a
// segment boundary must see the column left by the segment before it.
// Given the same underlying text and tabWidth, this produces byte-for-byte
// the same expansion ExpandTabs would on the concatenated plain string,
// just with segment boundaries and Style preserved. Non-tab runes advance
// the column via go-runewidth, exactly like ExpandTabs, so double-width
// runes (CJK, most emoji) are handled identically. Output has the same
// length and order as segs — this never merges or reorders segments, only
// rewrites each Text in place.
func ExpandTabsSegments(segs []layout.Segment, tabWidth int) []layout.Segment {
	if tabWidth <= 0 {
		tabWidth = 8
	}
	if len(segs) == 0 {
		return segs
	}

	out := make([]layout.Segment, len(segs))
	col := 0
	for i, seg := range segs {
		if !strings.ContainsRune(seg.Text, '\t') {
			// Fast path: nothing to expand, just advance col so a tab in
			// a later segment lands on the right stop.
			out[i] = seg
			for _, r := range seg.Text {
				col += runewidth.RuneWidth(r)
			}
			continue
		}
		var b strings.Builder
		b.Grow(len(seg.Text))
		for _, r := range seg.Text {
			if r == '\t' {
				spaces := tabWidth - (col % tabWidth)
				for n := 0; n < spaces; n++ {
					b.WriteByte(' ')
				}
				col += spaces
				continue
			}
			b.WriteRune(r)
			col += runewidth.RuneWidth(r)
		}
		out[i] = layout.Segment{Text: b.String(), Style: seg.Style}
	}
	return out
}

// DisplayWidth returns the total terminal-column width of s, accounting for
// double-width (CJK, many emoji) runes.
func DisplayWidth(s string) int {
	w := 0
	for _, r := range s {
		w += runewidth.RuneWidth(r)
	}
	return w
}

// RuneIndexForDisplayColumn is DisplayWidth's inverse: given a display
// column, it returns the index of the rune occupying that column in s, which
// is assumed to already have tabs expanded (ExpandTabs). The result is
// clamped to [0, rune count], so a column past the end of the line lands
// just after the last rune — the same "one past the end" position a cursor
// legitimately sits at.
//
// A column falling on the SECOND cell of a double-width rune returns that
// rune's own index rather than the next one: the two cells are one character,
// and clicking either half must select the same character. This is why the
// conversion can't be done by counting runes.
//
// Its reason for existing is mouse input — a click arrives as a display
// column, while every text position in the editor is a rune index — so it is
// the counterpart of SliceByDisplayColumn (which answers "what is visible
// from this column") for a single position.
func RuneIndexForDisplayColumn(s string, col int) int {
	if col <= 0 {
		return 0
	}
	current := 0
	for i, r := range []rune(s) {
		w := runewidth.RuneWidth(r)
		if w == 0 {
			w = 1
		}
		// col lands anywhere inside this rune's cells — including the
		// trailing cell of a wide rune — so this is the rune it names.
		if col < current+w {
			return i
		}
		current += w
	}
	return len([]rune(s))
}

// SliceByDisplayColumn returns the portion of s visible in a viewport that
// starts at display column fromCol and is maxCols wide. s is assumed to
// already have tabs expanded (ExpandTabs) — this function works purely in
// display columns and rune widths.
//
// A double-width rune straddling either viewport edge is dropped whole
// (never split in half) and the gap is padded with a single space, matching
// how terminal editors avoid corrupting wide glyphs at scroll boundaries.
func SliceByDisplayColumn(s string, fromCol, maxCols int) string {
	if maxCols <= 0 {
		return ""
	}

	var out []rune
	col := 0     // display column of the rune about to be examined
	written := 0 // display columns written to out so far

	for _, r := range s {
		w := runewidth.RuneWidth(r)
		if w == 0 {
			w = 1
		}
		runeStart := col
		runeEnd := col + w
		col = runeEnd

		if runeEnd <= fromCol {
			continue // entirely before the viewport
		}
		if runeStart < fromCol {
			// Straddles the left edge: drop it, pad instead.
			pad := runeEnd - fromCol
			if pad > maxCols-written {
				pad = maxCols - written
			}
			for i := 0; i < pad; i++ {
				out = append(out, ' ')
			}
			written += pad
			continue
		}
		if written+w > maxCols {
			// Straddles the right edge: drop it, pad to fill, then stop.
			for written < maxCols {
				out = append(out, ' ')
				written++
			}
			break
		}

		out = append(out, r)
		written += w

		if written >= maxCols {
			break
		}
	}

	return string(out)
}

// SliceSegmentsByDisplayColumn is SliceByDisplayColumn's counterpart for
// styled text: it applies the identical viewport-clipping and
// wide-rune-at-the-boundary rules, but operates over a sequence of styled
// segments instead of a plain string, so per-segment styling (e.g. syntax
// colors) survives horizontal scrolling. Consecutive output runes that end
// up with the same style are coalesced into one segment.
func SliceSegmentsByDisplayColumn(segs []layout.Segment, fromCol, maxCols int) []layout.Segment {
	if maxCols <= 0 {
		return nil
	}

	var out []layout.Segment
	col := 0
	written := 0

	appendText := func(text string, style layout.Style) {
		if text == "" {
			return
		}
		if n := len(out); n > 0 && out[n-1].Style == style {
			out[n-1].Text += text
			return
		}
		out = append(out, layout.Segment{Text: text, Style: style})
	}

outer:
	for _, seg := range segs {
		for _, r := range seg.Text {
			w := runewidth.RuneWidth(r)
			if w == 0 {
				w = 1
			}
			runeStart := col
			runeEnd := col + w
			col = runeEnd

			if runeEnd <= fromCol {
				continue // entirely before the viewport
			}
			if runeStart < fromCol {
				// Straddles the left edge: drop it, pad instead.
				pad := runeEnd - fromCol
				if pad > maxCols-written {
					pad = maxCols - written
				}
				appendText(strings.Repeat(" ", pad), layout.Style{})
				written += pad
				continue
			}
			if written+w > maxCols {
				// Straddles the right edge: drop it, pad to fill, then stop.
				appendText(strings.Repeat(" ", maxCols-written), layout.Style{})
				break outer
			}

			appendText(string(r), seg.Style)
			written += w

			if written >= maxCols {
				break outer
			}
		}
	}

	return out
}

// ClampScroll bounds a horizontal scroll offset to what's actually needed
// to reveal the end of a line textWidth display-columns wide within a
// viewport availableCols wide — never negative, and never further right
// than the point where the last column of text lines up with the right
// edge of the viewport. Used to keep a manual "peek right to see the rest
// of this long line" scroll (the file tree, the fuzzy finder) from
// scrolling into empty space past the actual content.
func ClampScroll(scroll, textWidth, availableCols int) int {
	max := textWidth - availableCols
	if max < 0 {
		max = 0
	}
	if scroll > max {
		scroll = max
	}
	if scroll < 0 {
		scroll = 0
	}
	return scroll
}
