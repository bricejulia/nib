package editor

import (
	"fmt"
	"sort"
	"strings"
	"unicode"

	gotreesitter "github.com/odvcencio/gotreesitter"

	"github.com/bricejulia/nib/internal/layout"
	"github.com/bricejulia/nib/internal/theme"
)

// Code folding: a fold collapses a block's lines into its first one, so a
// 400-line JSON object or a long function reads as a single row.
//
// What CAN fold is a property of the text, so it lives on the shared
// Buffer (Buffer.folds), computed alongside highlighting: from the
// tree-sitter tree when there is one (see foldRangesFromTree), else from a
// bracket/indentation scan (see foldRangesFallback). What IS folded is a
// property of the pane looking at it, so it lives on tab (tab.closedFolds),
// the same split as cursor vs. content — two panes on one file fold
// independently.
//
// A fold's header line stays visible; the lines after it, through its
// last line, are hidden. Nothing about the buffer changes: hidden lines
// are skipped by rendering, j/k and scrolling, and the moment the cursor
// lands on one by any other route (search, :N, go to definition, undo,
// ...) the folds hiding it open — see tab.revealLine, called from
// clampCursor, which every cursor move already goes through.

// foldRange is one foldable block: Start is its header line, End its last
// line (inclusive), always End > Start. Line indices are 0-based, into
// Buffer.Lines.
type foldRange struct {
	Start, End int
}

// maxFoldSplices bounds Buffer.splices. A tab that falls further behind
// than this (impossible in practice: every pane syncs on its next render)
// drops its folds rather than guess.
const maxFoldSplices = 256

// lineSplice is one structural edit, as Buffer.spliceHighlight sees it:
// removed lines at ln replaced by inserted ones. rev is the Buffer.rev the
// edit produced, so a tab knows which splices it has yet to apply to its
// own closed folds (see tab.syncFolds).
type lineSplice struct {
	rev                   uint64
	ln, removed, inserted int
}

// recordSplice logs a structural edit for every pane's closed folds and
// shifts the buffer's own foldable ranges to match, so folds keep their
// place while the background parse that will replace Buffer.folds is
// still running. Called just before resync bumps rev, hence rev+1.
func (b *Buffer) recordSplice(ln, removed, inserted int) {
	if len(b.splices) >= maxFoldSplices {
		b.splicesDropped = b.splices[0].rev
		b.splices = append(b.splices[:0], b.splices[1:]...)
	}
	b.splices = append(b.splices, lineSplice{rev: b.rev + 1, ln: ln, removed: removed, inserted: inserted})
	b.folds = shiftFoldRanges(b.folds, ln, removed, inserted)
	b.foldsGen++
}

// recordDiffSplice records a wholesale content replacement (undo/redo,
// reload) as the single splice covering the lines that differ between
// old and new — their common prefix and suffix are untouched, so folds
// outside the changed region survive an undo of an unrelated edit.
func (b *Buffer) recordDiffSplice(old, new []string) {
	prefix := 0
	for prefix < len(old) && prefix < len(new) && old[prefix] == new[prefix] {
		prefix++
	}
	suffix := 0
	for suffix < len(old)-prefix && suffix < len(new)-prefix &&
		old[len(old)-1-suffix] == new[len(new)-1-suffix] {
		suffix++
	}
	removed, inserted := len(old)-prefix-suffix, len(new)-prefix-suffix
	if removed == 0 && inserted == 0 {
		return
	}
	b.recordSplice(prefix, removed, inserted)
}

// shiftFoldRanges moves ranges through a splice of removed lines at ln
// replaced by inserted ones. A range whose header was replaced is dropped
// (its block may no longer exist); one the splice falls inside grows or
// shrinks; one entirely after it moves.
func shiftFoldRanges(ranges []foldRange, ln, removed, inserted int) []foldRange {
	if len(ranges) == 0 {
		return ranges
	}
	delta := inserted - removed
	editEnd := ln + removed // first line after the replaced ones
	out := ranges[:0]
	for _, r := range ranges {
		switch {
		case r.End < ln:
			// entirely before the edit
		case r.Start >= editEnd:
			r.Start += delta
			r.End += delta
		case removed == inserted:
			// lines rewritten in place: the structure is unchanged until
			// the next parse says otherwise
		case r.Start < ln:
			// the edit is inside the block: only its end moves
			if r.End >= editEnd {
				r.End += delta
			} else {
				r.End = ln + inserted - 1
			}
		default:
			continue // header replaced
		}
		if r.End > r.Start {
			out = append(out, r)
		}
	}
	return out
}

// maxSyncFoldRescanBytes bounds the files foldRanges rescans on the UI
// goroutine after every edit. Bigger ones are scanned once when first
// needed and then only shifted along with edits (see recordSplice) — the
// scan is linear, about 7ms a megabyte, which is fine once but not per
// keystroke.
const maxSyncFoldRescanBytes = 256 << 10

// foldRanges returns what can fold in b, sorted by Start, one range per
// header line. Files a grammar handles get theirs from the highlight pass
// (see Highlighter), kept shifted across edits in between; the rest are
// scanned here, lazily, once per content revision.
func (b *Buffer) foldRanges() []foldRange {
	if b == nil {
		return nil
	}
	stale := !b.foldsSet || (b.foldsRev != b.rev && len(b.Source) <= maxSyncFoldRescanBytes)
	if highlightGrammar(b.Path) == nil && stale {
		b.setFolds(foldRangesFallback(b.Lines))
	}
	return b.folds
}

// setFolds stores a fresh set of foldable ranges.
func (b *Buffer) setFolds(ranges []foldRange) {
	b.folds = ranges
	b.foldsRev = b.rev
	b.foldsSet = true
	b.foldsGen++
}

// foldAtHeader returns the foldable range whose header is ln.
func foldAtHeader(ranges []foldRange, ln int) (foldRange, bool) {
	i := sort.Search(len(ranges), func(i int) bool { return ranges[i].Start >= ln })
	if i < len(ranges) && ranges[i].Start == ln {
		return ranges[i], true
	}
	return foldRange{}, false
}

// innermostFoldContaining returns the foldable range with the latest
// header at or before ln that still reaches ln — the block ln sits in.
// below excludes ranges headed exactly at ln, for "the block around this
// one".
func innermostFoldContaining(ranges []foldRange, ln int, strictlyAbove bool) (foldRange, bool) {
	i := sort.Search(len(ranges), func(i int) bool { return ranges[i].Start > ln })
	for i--; i >= 0; i-- {
		r := ranges[i]
		if strictlyAbove && r.Start == ln {
			continue
		}
		if r.End >= ln {
			return r, true
		}
	}
	return foldRange{}, false
}

// foldRangesFromTree collects the foldable ranges of a parsed file: for
// each line, the outermost named node starting on it and ending on a
// later line. Generic on purpose — no per-language list of node types:
// JSON objects and arrays, functions, classes, blocks, multi-line calls
// and comments all come out of the same rule.
func foldRangesFromTree(tree *gotreesitter.Tree) []foldRange {
	if tree == nil {
		return nil
	}
	root := tree.RootNode()
	if root == nil {
		return nil
	}
	ends := map[int]int{}
	var walk func(n *gotreesitter.Node)
	walk = func(n *gotreesitter.Node) {
		for _, c := range n.Children() {
			if c == nil {
				continue
			}
			start, end := int(c.StartPoint().Row), int(c.EndPoint().Row)
			// A node ending at column 0 stopped at the line break before
			// that row (some grammars include the trailing newline).
			if c.EndPoint().Column == 0 && end > start {
				end--
			}
			if end <= start {
				continue // single-line: nothing below can span lines either
			}
			if c.IsNamed() && !c.IsError() && !c.IsMissing() && end > ends[start] {
				ends[start] = end
			}
			walk(c)
		}
	}
	walk(root)
	return sortedFoldRanges(ends)
}

// sortedFoldRanges turns a header->end map into foldRanges sorted by
// header.
func sortedFoldRanges(ends map[int]int) []foldRange {
	if len(ends) == 0 {
		return nil
	}
	out := make([]foldRange, 0, len(ends))
	for s, e := range ends {
		if e > s {
			out = append(out, foldRange{Start: s, End: e})
		}
	}
	sort.Slice(out, func(i, j int) bool { return out[i].Start < out[j].Start })
	return out
}

// foldRangesFallback finds foldable ranges without a syntax tree — files
// no grammar handles, or too big to parse: bracket pairs spanning lines
// first, and indentation if the file has none.
func foldRangesFallback(lines []string) []foldRange {
	if r := bracketFoldRanges(lines); len(r) > 0 {
		return r
	}
	return indentFoldRanges(lines)
}

// bracketFoldRanges matches {} and [] pairs across lines, skipping
// brackets inside "..." / '...' / `...` strings (which end at the line's
// end, so a stray apostrophe in prose can't swallow the rest of the file)
// and // and /* */ comments.
func bracketFoldRanges(lines []string) []foldRange {
	type open struct {
		ch   byte
		line int
	}
	var stack []open
	ends := map[int]int{}
	inBlockComment := false
	for ln, line := range lines {
		var quote byte
		for i := 0; i < len(line); i++ {
			c := line[i]
			switch {
			case inBlockComment:
				if c == '*' && i+1 < len(line) && line[i+1] == '/' {
					inBlockComment = false
					i++
				}
			case quote != 0:
				switch c {
				case '\\':
					i++
				case quote:
					quote = 0
				}
			case c == '"' || c == '\'' || c == '`':
				quote = c
			case c == '/' && i+1 < len(line) && line[i+1] == '/':
				i = len(line)
			case c == '/' && i+1 < len(line) && line[i+1] == '*':
				inBlockComment = true
				i++
			case c == '{' || c == '[':
				stack = append(stack, open{ch: c, line: ln})
			case c == '}' || c == ']':
				want := byte('{')
				if c == ']' {
					want = '['
				}
				if n := len(stack); n > 0 && stack[n-1].ch == want {
					o := stack[n-1]
					stack = stack[:n-1]
					if ln > o.line && ln > ends[o.line] {
						ends[o.line] = ln
					}
				}
			}
		}
	}
	return sortedFoldRanges(ends)
}

// indentFoldRanges folds each non-blank line over the lines after it that
// are indented deeper (blank lines in between included, trailing ones
// not) — Python, YAML, Markdown lists and outlines.
func indentFoldRanges(lines []string) []foldRange {
	indent := func(s string) (int, bool) {
		n := 0
		for _, r := range s {
			switch r {
			case ' ':
				n++
			case '\t':
				n += 4
			default:
				return n, true
			}
		}
		return 0, false // blank
	}
	var out []foldRange
	for i, line := range lines {
		base, ok := indent(line)
		if !ok {
			continue
		}
		end := i
		for j := i + 1; j < len(lines); j++ {
			d, ok := indent(lines[j])
			if !ok {
				continue
			}
			if d <= base {
				break
			}
			end = j
		}
		if end > i {
			out = append(out, foldRange{Start: i, End: end})
		}
	}
	return out
}

// --- per-pane fold state ---

// syncFolds brings t.closedFolds up to date with every structural edit
// made to its buffer since it last looked — from this pane or any other
// sharing the buffer. An edit that rewrites a closed fold's header line
// opens that fold, like editing the line you folded on does in vim.
func (t *tab) syncFolds() {
	if t.buf == nil || t.foldRev == t.buf.rev {
		return
	}
	defer func() { t.foldRev = t.buf.rev }()
	if len(t.closedFolds) == 0 {
		return
	}
	if t.foldRev < t.buf.splicesDropped {
		t.closedFolds = nil
		t.foldGen++
		return
	}
	for _, s := range t.buf.splices {
		if s.rev <= t.foldRev {
			continue
		}
		next := make(map[int]bool, len(t.closedFolds))
		for start := range t.closedFolds {
			switch {
			case start < s.ln:
				next[start] = true
			case start >= s.ln+s.removed:
				next[start+s.inserted-s.removed] = true
			}
		}
		t.closedFolds = next
	}
	t.foldGen++
}

// hiddenSpans returns the line ranges t's closed folds hide, merged and
// sorted: each span is [Start, End] inclusive of lines NOT drawn. A
// closed fold whose header no longer heads a foldable range (the block
// was edited away) is forgotten here.
func (t *tab) hiddenSpans() []foldRange {
	if t.buf == nil || len(t.closedFolds) == 0 {
		return nil
	}
	t.syncFolds()
	ranges := t.buf.foldRanges()
	key := foldCacheKey{rev: t.buf.rev, bufGen: t.buf.foldsGen, tabGen: t.foldGen}
	if t.hiddenKey == key && t.hiddenValid {
		return t.hidden
	}

	var spans []foldRange
	for start := range t.closedFolds {
		r, ok := foldAtHeader(ranges, start)
		if !ok {
			delete(t.closedFolds, start)
			continue
		}
		spans = append(spans, foldRange{Start: r.Start + 1, End: r.End})
	}
	sort.Slice(spans, func(i, j int) bool { return spans[i].Start < spans[j].Start })
	merged := spans[:0]
	for _, s := range spans {
		if n := len(merged); n > 0 && s.Start <= merged[n-1].End+1 {
			if s.End > merged[n-1].End {
				merged[n-1].End = s.End
			}
			continue
		}
		merged = append(merged, s)
	}
	t.hidden, t.hiddenKey, t.hiddenValid = merged, key, true
	return merged
}

// foldCacheKey identifies the inputs hiddenSpans was computed from.
type foldCacheKey struct {
	rev            uint64
	bufGen, tabGen uint64
}

// hiddenSpanAt returns the hidden span containing ln.
func (t *tab) hiddenSpanAt(ln int) (foldRange, bool) {
	spans := t.hiddenSpans()
	i := sort.Search(len(spans), func(i int) bool { return spans[i].End >= ln })
	if i < len(spans) && spans[i].Start <= ln {
		return spans[i], true
	}
	return foldRange{}, false
}

// isHidden reports whether a closed fold hides line ln.
func (t *tab) isHidden(ln int) bool {
	_, ok := t.hiddenSpanAt(ln)
	return ok
}

// foldedBelow returns the hidden span right after header line ln when ln
// is the visible first line of a closed fold — what renderBody draws the
// placeholder for.
func (t *tab) foldedBelow(ln int) (foldRange, bool) {
	if len(t.closedFolds) == 0 {
		return foldRange{}, false
	}
	return t.hiddenSpanAt(ln + 1)
}

// nextVisible returns the first visible line after ln, or len(Lines) when
// there is none.
func (t *tab) nextVisible(ln int) int {
	ln++
	if s, ok := t.hiddenSpanAt(ln); ok {
		ln = s.End + 1
	}
	return ln
}

// prevVisible returns the last visible line before ln, or -1.
func (t *tab) prevVisible(ln int) int {
	ln--
	if s, ok := t.hiddenSpanAt(ln); ok {
		ln = s.Start - 1
	}
	return ln
}

// stepVisible moves n visible lines from ln (negative for up), stopping at
// the first or last visible line.
func (t *tab) stepVisible(ln, n int) int {
	if t.buf == nil {
		return ln + n
	}
	last := len(t.buf.Lines)
	for ; n > 0; n-- {
		next := t.nextVisible(ln)
		if next >= last {
			break
		}
		ln = next
	}
	for ; n < 0; n++ {
		prev := t.prevVisible(ln)
		if prev < 0 {
			break
		}
		ln = prev
	}
	return ln
}

// visibleAtOrBefore returns ln itself when visible, else the header of the
// fold hiding it.
func (t *tab) visibleAtOrBefore(ln int) int {
	if s, ok := t.hiddenSpanAt(ln); ok {
		return s.Start - 1
	}
	return ln
}

// visibleLinesBetween counts the visible lines in [from, to), giving up
// past limit. Negative when to is before from.
func (t *tab) visibleLinesBetween(from, to, limit int) int {
	if to < from {
		return -1
	}
	if len(t.closedFolds) == 0 {
		return to - from
	}
	n := 0
	for ln := from; ln < to && n <= limit; ln = t.nextVisible(ln) {
		if !t.isHidden(ln) {
			n++
		}
	}
	return n
}

// visibleLineCount is how many lines the pane can show — the scrollbar's
// total.
func (t *tab) visibleLineCount() int {
	total := len(t.buf.Lines)
	for _, s := range t.hiddenSpans() {
		total -= s.End - s.Start + 1
	}
	return total
}

// visibleIndex is ln's position counting visible lines only — the
// scrollbar's coordinate.
func (t *tab) visibleIndex(ln int) int {
	idx := ln
	for _, s := range t.hiddenSpans() {
		if s.Start > ln {
			break
		}
		idx -= min(s.End, ln) - s.Start + 1
	}
	return idx
}

// lineAtVisibleIndex is visibleIndex's inverse.
func (t *tab) lineAtVisibleIndex(idx int) int {
	ln := idx
	for _, s := range t.hiddenSpans() {
		if s.Start > ln {
			break
		}
		ln += s.End - s.Start + 1
	}
	return min(ln, len(t.buf.Lines)-1)
}

// revealLine opens every closed fold hiding ln. Reports whether any did.
func (t *tab) revealLine(ln int) bool {
	if !t.isHidden(ln) {
		return false
	}
	ranges := t.buf.foldRanges()
	for start := range t.closedFolds {
		if r, ok := foldAtHeader(ranges, start); ok && r.Start < ln && ln <= r.End {
			delete(t.closedFolds, start)
		}
	}
	t.foldGen++
	return true
}

// closeFoldAt closes the fold ln heads, or else the innermost one around
// it — and when ln already heads a closed fold, the one around that,
// climbing a level per press like vim's zc. Reports whether anything
// closed; the caller moves a cursor the fold now hides (see
// keepCursorVisible).
func (t *tab) closeFoldAt(ln int) bool {
	if t.buf == nil {
		return false
	}
	t.syncFolds()
	ranges := t.buf.foldRanges()
	r, ok := foldAtHeader(ranges, ln)
	if !ok || t.closedFolds[ln] {
		r, ok = innermostFoldContaining(ranges, ln, true)
	}
	if !ok {
		return false
	}
	if t.closedFolds == nil {
		t.closedFolds = map[int]bool{}
	}
	t.closedFolds[r.Start] = true
	t.foldGen++
	return true
}

// openFoldAt opens the closed fold headed at ln. Folds nested inside it
// keep their own state, so reopening an outer block shows inner ones as
// they were.
func (t *tab) openFoldAt(ln int) bool {
	if t.buf == nil {
		return false
	}
	t.syncFolds()
	if !t.closedFolds[ln] {
		return false
	}
	delete(t.closedFolds, ln)
	t.foldGen++
	return true
}

// toggleFoldAt opens a closed fold headed at ln, else closes the one ln
// heads or sits in.
func (t *tab) toggleFoldAt(ln int) bool {
	if t.openFoldAt(ln) {
		return true
	}
	return t.closeFoldAt(ln)
}

// foldAll closes every foldable range.
func (t *tab) foldAll() bool {
	if t.buf == nil {
		return false
	}
	t.syncFolds()
	ranges := t.buf.foldRanges()
	if len(ranges) == 0 {
		return false
	}
	t.closedFolds = make(map[int]bool, len(ranges))
	for _, r := range ranges {
		t.closedFolds[r.Start] = true
	}
	t.foldGen++
	return true
}

// keepCursorVisible moves a cursor that a fold just closed over up to that
// fold's header — the only line of it still showing — instead of letting
// clampCursor's reveal reopen the fold.
func (t *tab) keepCursorVisible() {
	if t.isHidden(t.cursorLn) {
		t.cursorLn = t.visibleAtOrBefore(t.cursorLn)
	}
}

// unfoldAll opens every fold.
func (t *tab) unfoldAll() bool {
	if len(t.closedFolds) == 0 {
		return false
	}
	t.closedFolds = nil
	t.foldGen++
	return true
}

// linewiseFoldEnd returns the last line a linewise operator covers when
// it reaches visible line ln: a folded header carries its whole hidden
// block with it, as in vim.
func (t *tab) linewiseFoldEnd(ln int) int {
	if s, ok := t.foldedBelow(ln); ok {
		return s.End
	}
	return ln
}

// --- rendering ---

// foldColumnWidth is the fold gutter's width: one marker column, present
// only when the buffer has something to fold, so plain text keeps its
// gutter as it was.
func foldColumnWidth(t *tab) int {
	if t == nil || t.buf == nil || len(t.buf.foldRanges()) == 0 {
		return 0
	}
	return 1
}

// foldClosedStyle marks what a closed fold draws — its gutter ▸ and the
// placeholder — in the theme's accent, read at render time so a theme
// switch applies at once.
func foldClosedStyle() layout.Style {
	return layout.Style{Foreground: theme.Get(theme.UIFocusBorder), Attr: layout.AttrBold}
}

var foldOpenMarkerStyle = layout.Style{Attr: layout.AttrDim}

// foldMarkerSegment is line ln's fold gutter cell: ▸ on a closed fold, ▾
// on an open foldable header, blank otherwise.
func foldMarkerSegment(t *tab, ln int) layout.Segment {
	if _, ok := t.foldedBelow(ln); ok {
		return layout.Segment{Text: "▸", Style: foldClosedStyle()}
	}
	if _, ok := foldAtHeader(t.buf.foldRanges(), ln); ok {
		return layout.Segment{Text: "▾", Style: foldOpenMarkerStyle}
	}
	return layout.Segment{Text: " "}
}

// foldPlaceholder is the text drawn after a closed fold's header: the
// hidden line count, then the block's closing line when it is just a
// closing bracket — so a folded `"users": [` reads `"users": [ ⋯ 42 lines ⋯ ],`.
func foldPlaceholder(t *tab, span foldRange) string {
	n := span.End - span.Start + 1
	unit := "lines"
	if n == 1 {
		unit = "line"
	}
	text := fmt.Sprintf(" ⋯ %d %s ⋯", n, unit)
	if span.End < len(t.buf.Lines) {
		closing := strings.TrimSpace(t.buf.Lines[span.End])
		if closing != "" && strings.ContainsRune("}])", rune(closing[0])) && len(closing) <= 24 &&
			!strings.ContainsFunc(closing, unicode.IsLetter) {
			text += " " + closing
		}
	}
	return text
}

// --- actions ---

// foldKeyActions maps the key after "z" to its fold action, vim's letters.
var foldKeyActions = map[string]string{
	"a": "fold_toggle",
	"c": "fold_close",
	"o": "fold_open",
	"M": "fold_all",
	"R": "unfold_all",
}

// handleFoldKey reads the key after "z". Anything that isn't a fold
// command just cancels, consumed, the way an unknown "r<key>" is.
func (v *View) handleFoldKey(k layout.Key) bool {
	v.awaitingFoldKey = false
	v.count = 0
	if k.Named != "" || k.Mods&(layout.ModCtrl|layout.ModAlt|layout.ModSuper) != 0 {
		return true
	}
	if action, ok := foldKeyActions[k.Text]; ok {
		v.runAction(action, 1)
	}
	return true
}

// runFoldAction runs one of the fold actions on t, reporting whether
// action was one.
func (v *View) runFoldAction(t *tab, action string) bool {
	switch action {
	case "fold":
		v.awaitingFoldKey = true
		return true
	case "fold_toggle":
		t.toggleFoldAt(t.cursorLn)
	case "fold_close":
		t.closeFoldAt(t.cursorLn)
	case "fold_open":
		t.openFoldAt(t.cursorLn)
	case "fold_all":
		t.foldAll()
	case "unfold_all":
		t.unfoldAll()
	default:
		return false
	}
	if t.buf != nil {
		t.keepCursorVisible()
		t.clearSelection()
	}
	return true
}
