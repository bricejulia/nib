package editor

import (
	"fmt"
	"strings"

	"github.com/bricejulia/nib/internal/debuglog"
	"github.com/bricejulia/nib/internal/layout"
	"github.com/bricejulia/nib/internal/ui/textfield"
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

// enterSearchMode opens the "/" prompt, remembering where the cursor and
// view were so cancelling can put them back. A no-op — logged, not silent —
// on a buffer over maxLiveSearchBytes: see that constant's doc comment for
// why search specifically, unlike rendering/navigation, isn't worth trying
// to bound instead of disabling outright.
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
	v.searchField = textfield.TextField{}
	v.searchOriginLn, v.searchOriginCol, v.searchOriginTop = t.cursorLn, t.cursorCol, t.topLine
	v.recomputeSearchMatches()
}

// handleSearchKey handles a key while the "/" prompt is open: Esc cancels
// (see cancelSearch), Enter commits (see commitSearch), and everything else
// — Backspace, Left/Right/Home/End (and their Alt-modified word-nav/
// word-delete variants), and printable text — is delegated to the shared
// TextField primitive, the same as finder/help/actionpopup's own query
// fields.
//
// Whenever the typed pattern actually changes, its matches are highlighted
// and the cursor jumps to the first one after where "/" was pressed —
// vim's "incsearch" — or back to that spot when nothing matches. Nothing
// is pushed onto the jump list until Enter.
func (v *View) handleSearchKey(k layout.Key) bool {
	switch v.keymap[k.String()] {
	case "normal_mode": // Esc
		v.cancelSearch()
		return true
	case "insert_newline": // Enter
		v.commitSearch()
		return true
	}

	before := v.searchField.String()
	consumed := v.searchField.HandleKey(k)
	if consumed && v.searchField.String() != before {
		v.searchTextChanged()
	}
	return consumed
}

// searchTextChanged re-highlights for the pattern typed (or pasted) so far
// and moves the cursor to its first match after the origin, or back to the
// origin when nothing matches.
func (v *View) searchTextChanged() {
	v.recomputeSearchMatches()
	v.restoreSearchOrigin()
	if len(v.searchMatches) > 0 {
		i, _ := v.matchIndexFromOrigin()
		v.placeCursorAtMatch(i)
	}
}

// searchText is the pattern whose matches are shown: the one being typed
// while the "/" prompt is open, the committed one otherwise.
func (v *View) searchText() string {
	if v.mode == modeSearch {
		return v.searchField.String()
	}
	return v.searchPattern
}

// recomputeSearchMatches finds searchText's matches in the active tab and
// records which tab and buffer revision they describe, so
// syncSearchMatches can tell when they've gone stale.
func (v *View) recomputeSearchMatches() {
	t := v.activeTab()
	if t == nil || t.buf == nil || len(t.buf.Source) > maxLiveSearchBytes {
		v.searchMatches, v.searchFor = nil, nil
		return
	}
	v.searchMatches = findMatches(t.buf, v.searchText())
	v.searchFor, v.searchForRev = t, t.buf.rev
}

// syncSearchMatches recomputes the matches if they were computed for
// another tab (a tab switch, or a file opened) or an older revision of the
// buffer (an edit, possibly from another pane sharing it). Cheap to call
// every render: a no-op unless something changed.
func (v *View) syncSearchMatches() {
	t := v.activeTab()
	if t == nil || t.buf == nil {
		v.searchMatches, v.searchFor = nil, nil
		return
	}
	if t != v.searchFor || t.buf.rev != v.searchForRev {
		v.recomputeSearchMatches()
	}
}

// visibleSearchMatches is what the body draws highlighted: nothing once
// Esc has hidden them (see HandleKey's "normal_mode" case), until the next
// search or n/N brings them back.
func (v *View) visibleSearchMatches() []searchMatch {
	if v.searchHidden && v.mode != modeSearch {
		return nil
	}
	return v.searchMatches
}

// restoreSearchOrigin puts the cursor and scroll back where they were when
// "/" was pressed.
func (v *View) restoreSearchOrigin() {
	t := v.activeTab()
	if t == nil {
		return
	}
	t.cursorLn, t.cursorCol, t.topLine = v.searchOriginLn, v.searchOriginCol, v.searchOriginTop
	v.clampToLastChar(t)
}

// cancelSearch abandons the prompt: the cursor and view go back to where
// they were, and the previously committed search (if any) is shown again,
// exactly as it was — n/N keep repeating it.
func (v *View) cancelSearch() {
	v.mode = modeNormal
	v.searchField = textfield.TextField{}
	v.restoreSearchOrigin()
	v.recomputeSearchMatches()
}

// commitSearch accepts the typed pattern as the active search, shows its
// highlights, and jumps to the first match after where the prompt was
// opened. An empty pattern repeats the last search, as in vim. The pattern
// is remembered for n/N.
func (v *View) commitSearch() {
	pattern := v.searchField.String()
	v.mode = modeNormal
	v.searchField = textfield.TextField{}
	if pattern == "" {
		pattern = v.searchPattern
	}
	// Back to the origin first, undoing the live jump, so the jump below
	// records where the search started, for Ctrl+b.
	v.restoreSearchOrigin()
	if pattern == "" {
		v.recomputeSearchMatches()
		return
	}

	v.searchPattern = pattern
	v.searchHidden = false
	v.recomputeSearchMatches()
	if len(v.searchMatches) == 0 {
		v.noMatch()
		return
	}
	i, wrapped := v.matchIndexFromOrigin()
	v.jumpToMatch(i)
	if wrapped {
		v.searchNotice = "Search wrapped"
	}
}

// noMatch reports that searchPattern matches nothing in this file.
func (v *View) noMatch() {
	v.searchNotice = "No match: " + v.searchPattern
	debuglog.Warn("search: no match for %q", v.searchPattern)
}

// matchIndexFromOrigin is matchIndexFrom relative to where "/" was pressed.
func (v *View) matchIndexFromOrigin() (int, bool) {
	t := v.activeTab()
	raw := v.searchOriginCol
	if t != nil && t.buf != nil && v.searchOriginLn < len(t.buf.Lines) {
		raw = rawIndexForExpandedCol(t.buf.Lines[v.searchOriginLn], v.searchOriginCol, tabWidthOf(t))
	}
	return v.matchIndexFrom(v.searchOriginLn, raw, true)
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
	v.searchHidden = false
	// Recompute rather than trusting a cached set: the buffer may have been
	// edited (or changed under us by another pane) since the last search.
	v.recomputeSearchMatches()
	if len(v.searchMatches) == 0 {
		v.noMatch()
		return
	}
	raw := rawIndexForExpandedCol(t.buf.Lines[t.cursorLn], t.cursorCol, tabWidthOf(t))
	i, wrapped := v.matchIndexFrom(t.cursorLn, raw, forward)
	v.jumpToMatch(i)
	if wrapped {
		v.searchNotice = "Search wrapped"
	}
}

// matchIndexFrom picks which match to jump to relative to (ln, rawCol),
// wrapping around the ends of the file — vim's behaviour, and what makes
// n/N usable without thinking about where you are. wrapped reports that it
// had to.
func (v *View) matchIndexFrom(ln, rawCol int, forward bool) (i int, wrapped bool) {
	if forward {
		for i, m := range v.searchMatches {
			if m.ln > ln || (m.ln == ln && m.start > rawCol) {
				return i, false
			}
		}
		return 0, true
	}
	for i := len(v.searchMatches) - 1; i >= 0; i-- {
		m := v.searchMatches[i]
		if m.ln < ln || (m.ln == ln && m.start < rawCol) {
			return i, false
		}
	}
	return len(v.searchMatches) - 1, true
}

// jumpToMatch moves the cursor to the match at index i, pushing the
// pre-move position onto the jump stack first (see pushJump) — the single
// choke point both commitSearch (Enter) and searchNext/searchPrev (n/N)
// jump through, so this alone gives Ctrl+b coverage for both without
// duplicating the push at each call site. The live jump while typing uses
// placeCursorAtMatch instead, so it pushes nothing.
func (v *View) jumpToMatch(i int) {
	t := v.activeTab()
	if t == nil || i < 0 || i >= len(v.searchMatches) {
		return
	}
	v.pushJump(t)
	v.placeCursorAtMatch(i)
}

// placeCursorAtMatch moves the cursor to the start of match i.
func (v *View) placeCursorAtMatch(i int) {
	t := v.activeTab()
	if t == nil || i < 0 || i >= len(v.searchMatches) {
		return
	}
	m := v.searchMatches[i]
	t.cursorLn = m.ln
	t.cursorCol = expandedColForRawIndexIn(t.buf, m.ln, m.start, tabWidthOf(t))
	v.clampToLastChar(t)
}

// searchStatus is the status-bar summary of the active search: the
// pattern and which match the cursor is on out of how many ("/foo [3/12]"),
// or just how many when it isn't on one — empty when no highlights are
// showing. Followed by searchNotice, if any.
func (v *View) searchStatus() string {
	var out string
	if t := v.activeTab(); t != nil && t.buf != nil && v.searchPattern != "" && !v.searchHidden && len(v.searchMatches) > 0 {
		raw := 0
		if t.cursorLn < len(t.buf.Lines) {
			raw = rawIndexForExpandedCol(t.buf.Lines[t.cursorLn], t.cursorCol, tabWidthOf(t))
		}
		count := fmt.Sprintf("[%d]", len(v.searchMatches))
		for i, m := range v.searchMatches {
			if m.ln == t.cursorLn && raw >= m.start && raw < m.end {
				count = fmt.Sprintf("[%d/%d]", i+1, len(v.searchMatches))
				break
			}
		}
		out = "/" + v.searchPattern + " " + count
	}
	if v.searchNotice != "" {
		if out != "" {
			out += "  "
		}
		out += v.searchNotice
	}
	return out
}
