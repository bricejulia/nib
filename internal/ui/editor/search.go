package editor

import (
	"strings"

	"github.com/bricejulia/nib/internal/debuglog"
	"github.com/bricejulia/nib/internal/layout"
)

// searchHighlightStyle marks every match of the active pattern. Reverse
// video rather than a background colour because layout.Style has no
// background field — and reverse video is the conventional terminal way to
// show a match anyway.
var searchHighlightStyle = layout.Style{Attr: layout.AttrReverse}

// maxLiveSearchBytes bounds how large a buffer's Source can be before
// search is disabled outright, rather than trying to bound findMatches/
// matchRangesOnLine/jumpToMatch individually. Deliberately its own,
// smaller threshold than maxRenderLineRunes/Buffer.HasLongLine (view.go,
// buffer.go): those exist because even ~20,000 characters is too much to
// reprocess every render frame, but searching 20,000 characters is
// instant — the actual cost driver here is total scanned bytes, live, on
// every keystroke, which stays cheap into the low tens of megabytes.
// Sized well under a reported 12.7MB single-line file, since a generic
// size like maxLoadableFileSize (buffer.go) would let exactly that
// through un-guarded even after findMatches's O(n²) fix.
const maxLiveSearchBytes = 8 << 20 // 8MB

// searchMatch is one occurrence of the active pattern: a line plus the
// half-open rune range [start, end) within that line's RAW (un-tab-expanded)
// text, matching how cursor edits address positions elsewhere.
type searchMatch struct {
	ln         int
	start, end int
}

// runeRange is a half-open [start, end) span of rune indices within a
// single line.
type runeRange struct {
	start, end int
}

// findMatches returns every occurrence of pattern in buf, in file order.
// Case-sensitive plain-substring matching, deliberately: it's what vim does
// by default, and it's predictable. Regex is a deferred follow-up — Go's
// dialect differs from vim's in ways that would quietly mislead anyone
// typing a familiar vim pattern.
func findMatches(buf *Buffer, pattern string) []searchMatch {
	if buf == nil || pattern == "" {
		return nil
	}
	patternRuneLen := len([]rune(pattern))
	var matches []searchMatch
	for ln, line := range buf.Lines {
		// Byte offsets from strings.Index have to become rune indices,
		// since every position elsewhere in the editor is rune-based.
		// runeOffset/countedByte track that conversion incrementally —
		// only ever counting the runes in the span since the last match,
		// never rescanning from the start of the line — so this whole
		// loop costs O(line length) overall, not O(line length) PER
		// MATCH: a line with many matches (e.g. a common single
		// character searched across a multi-million-character line)
		// used to make re-deriving runeStart from scratch every time
		// degenerate toward O(n²).
		byteOffset, runeOffset, countedByte := 0, 0, 0
		for {
			i := strings.Index(line[byteOffset:], pattern)
			if i < 0 {
				break
			}
			byteStart := byteOffset + i
			runeOffset += len([]rune(line[countedByte:byteStart]))
			countedByte = byteStart
			matches = append(matches, searchMatch{
				ln:    ln,
				start: runeOffset,
				end:   runeOffset + patternRuneLen,
			})
			// Advance past this match's start (not its end) so overlapping
			// occurrences of a self-overlapping pattern are all found.
			byteOffset = byteStart + 1
			if byteOffset >= len(line) {
				break
			}
		}
	}
	return matches
}

// matchRangesOnLine returns the rune ranges to highlight on line ln.
func matchRangesOnLine(matches []searchMatch, ln int) []runeRange {
	var ranges []runeRange
	for _, m := range matches {
		if m.ln == ln {
			ranges = append(ranges, runeRange{start: m.start, end: m.end})
		}
	}
	return ranges
}

// applyHighlightRanges overlays style onto the given rune ranges of a line
// that has already been split into styled segments, splitting segments at
// range boundaries as needed. Each segment keeps its original style
// otherwise, so a match highlight lands on top of syntax highlighting
// rather than replacing it.
//
// Operates on RAW (pre-tab-expansion) segments, because that's the only
// stage where a rune index means the same thing as it does to the cursor
// and to findMatches; tab expansion happens after this.
func applyHighlightRanges(segs []layout.Segment, ranges []runeRange, style layout.Style) []layout.Segment {
	if len(ranges) == 0 || len(segs) == 0 {
		return segs
	}

	out := make([]layout.Segment, 0, len(segs)+2*len(ranges))
	pos := 0 // rune index of the start of the current segment
	for _, seg := range segs {
		runes := []rune(seg.Text)
		// Walk this segment rune by rune, grouping consecutive runes that
		// share the same highlighted/not-highlighted verdict.
		runStart := 0
		runHighlighted := len(runes) > 0 && inAnyRange(ranges, pos)
		for i := 1; i <= len(runes); i++ {
			atEnd := i == len(runes)
			var highlighted bool
			if !atEnd {
				highlighted = inAnyRange(ranges, pos+i)
			}
			if atEnd || highlighted != runHighlighted {
				text := string(runes[runStart:i])
				segStyle := seg.Style
				if runHighlighted {
					segStyle.Attr |= style.Attr
					if style.Foreground != layout.ColorDefault {
						segStyle.Foreground = style.Foreground
					}
					// Background composes the same way Foreground does, so
					// overlaying a selection (which uses one — see
					// selectionStyle) leaves the syntax foreground colour
					// underneath intact, and a search match inside a
					// selection keeps its own reverse video on top.
					if style.Background != layout.ColorDefault {
						segStyle.Background = style.Background
					}
				}
				out = append(out, layout.Segment{Text: text, Style: segStyle})
				runStart = i
				runHighlighted = highlighted
			}
		}
		pos += len(runes)
	}
	return out
}

// inAnyRange reports whether rune index i falls in any of ranges.
func inAnyRange(ranges []runeRange, i int) bool {
	for _, r := range ranges {
		if i >= r.start && i < r.end {
			return true
		}
	}
	return false
}

// enterSearchMode opens the "/" prompt, remembering where the cursor was so
// cancelling can put it back. A no-op — logged, not silent — on a buffer
// over maxLiveSearchBytes: see that constant's doc comment for why search
// specifically, unlike rendering/navigation, isn't worth trying to bound
// instead of disabling outright.
func (v *View) enterSearchMode() {
	t := v.activeTab()
	if t == nil || t.buf == nil {
		return
	}
	if len(t.buf.Source) > maxLiveSearchBytes {
		debuglog.Warn("search: disabled for %s (%d bytes exceeds the %d-byte live-search limit)",
			t.path, len(t.buf.Source), maxLiveSearchBytes)
		return
	}
	v.mode = modeSearch
	v.searchBuf = ""
	v.searchOriginLn, v.searchOriginCol = t.cursorLn, t.cursorCol
}

// handleSearchKey handles a key while the "/" prompt is open: Esc cancels
// (restoring the cursor and clearing highlights), Enter commits and jumps to
// the first match after the origin, Backspace edits, and printable
// characters extend the pattern.
//
// Matches are re-highlighted on every keystroke so you can see what you're
// converging on, but the cursor doesn't move until Enter — vim's
// jump-as-you-type "incsearch" is a deferred nicety, and not moving means
// cancelling is genuinely lossless.
func (v *View) handleSearchKey(k layout.Key) bool {
	switch v.keymap[k.String()] {
	case "normal_mode": // Esc
		v.cancelSearch()
		return true
	case "insert_newline": // Enter
		v.commitSearch()
		return true
	case "insert_backspace": // Backspace
		if n := len(v.searchBuf); n > 0 {
			v.searchBuf = v.searchBuf[:n-1]
			v.refreshSearchHighlights()
		}
		return true
	}

	if k.Text != "" && k.Mods&(layout.ModCtrl|layout.ModAlt|layout.ModSuper) == 0 {
		v.searchBuf += k.Text
		v.refreshSearchHighlights()
	}
	return true
}

// refreshSearchHighlights recomputes the highlighted matches for the
// pattern typed so far, without moving the cursor.
func (v *View) refreshSearchHighlights() {
	t := v.activeTab()
	if t == nil {
		return
	}
	v.searchMatches = findMatches(t.buf, v.searchBuf)
}

// cancelSearch abandons the prompt, restoring the cursor and clearing the
// highlights — so an abandoned search leaves no trace.
func (v *View) cancelSearch() {
	v.mode = modeNormal
	v.searchBuf = ""
	v.searchMatches = nil
	if t := v.activeTab(); t != nil {
		t.cursorLn, t.cursorCol = v.searchOriginLn, v.searchOriginCol
		v.clampToLastChar(t)
	}
}

// commitSearch accepts the typed pattern as the active search, keeps its
// highlights up, and jumps to the first match at or after where the prompt
// was opened. The pattern is remembered for n/N.
func (v *View) commitSearch() {
	v.mode = modeNormal
	pattern := v.searchBuf
	v.searchBuf = ""
	if pattern == "" {
		v.searchMatches = nil
		return
	}

	v.searchPattern = pattern
	v.refreshSearchMatchesForPattern()
	if len(v.searchMatches) == 0 {
		debuglog.Warn("search: no match for %q", pattern)
		return
	}
	v.jumpToMatch(v.matchIndexFrom(v.searchOriginLn, v.searchOriginCol, true))
}

// refreshSearchMatchesForPattern recomputes matches for the committed
// pattern — called after a commit and whenever n/N runs, so matches stay
// correct after the buffer has been edited.
func (v *View) refreshSearchMatchesForPattern() {
	t := v.activeTab()
	if t == nil {
		v.searchMatches = nil
		return
	}
	v.searchMatches = findMatches(t.buf, v.searchPattern)
}

// searchNext moves to the next match after the cursor, wrapping at the end
// of the file. A no-op with no active search.
func (v *View) searchNext() { v.stepSearch(true) }

// searchPrev moves to the previous match before the cursor, wrapping at the
// start of the file.
func (v *View) searchPrev() { v.stepSearch(false) }

func (v *View) stepSearch(forward bool) {
	if v.searchPattern == "" {
		return
	}
	t := v.activeTab()
	if t == nil || t.buf == nil {
		return
	}
	// Defensive, mirroring enterSearchMode's guard: a pattern committed
	// while this buffer was smaller (or before a paste grew it) must not
	// let n/N keep re-running findMatches over a buffer that's since
	// crossed maxLiveSearchBytes.
	if len(t.buf.Source) > maxLiveSearchBytes {
		debuglog.Warn("search: disabled for %s (%d bytes exceeds the %d-byte live-search limit)",
			t.path, len(t.buf.Source), maxLiveSearchBytes)
		return
	}
	// Recompute rather than trusting a cached set: the buffer may have been
	// edited (or changed under us by another pane) since the last search.
	v.refreshSearchMatchesForPattern()
	if len(v.searchMatches) == 0 {
		debuglog.Warn("search: no match for %q", v.searchPattern)
		return
	}
	raw := rawIndexForExpandedCol(t.buf.Lines[t.cursorLn], t.cursorCol, tabWidthOf(t))
	v.jumpToMatch(v.matchIndexFrom(t.cursorLn, raw, forward))
}

// matchIndexFrom picks which match to jump to relative to (ln, rawCol),
// wrapping around the ends of the file — vim's behaviour, and what makes
// n/N usable without thinking about where you are.
func (v *View) matchIndexFrom(ln, rawCol int, forward bool) int {
	if forward {
		for i, m := range v.searchMatches {
			if m.ln > ln || (m.ln == ln && m.start > rawCol) {
				return i
			}
		}
		return 0 // wrapped past the end
	}
	for i := len(v.searchMatches) - 1; i >= 0; i-- {
		m := v.searchMatches[i]
		if m.ln < ln || (m.ln == ln && m.start < rawCol) {
			return i
		}
	}
	return len(v.searchMatches) - 1 // wrapped past the start
}

// jumpToMatch moves the cursor to the match at index i, pushing the
// pre-move position onto the jump stack first (see pushJump) — the single
// choke point both commitSearch (Enter) and searchNext/searchPrev (n/N)
// jump through, so this alone gives Ctrl+b coverage for both without
// duplicating the push at each call site. Safe to push unconditionally
// here specifically because incremental typing of the pattern
// (refreshSearchHighlights) never calls this function — only an actual
// cursor-moving commit or step does.
func (v *View) jumpToMatch(i int) {
	t := v.activeTab()
	if t == nil || i < 0 || i >= len(v.searchMatches) {
		return
	}
	v.pushJump(t)
	m := v.searchMatches[i]
	t.cursorLn = m.ln
	t.cursorCol = expandedColForRawIndexIn(t.buf, m.ln, m.start, tabWidthOf(t))
	v.clampToLastChar(t)
}
