package editor

import (
	"sort"
	"strings"

	"github.com/odvcencio/gotreesitter"
	"github.com/odvcencio/gotreesitter/grammars"

	"github.com/bricejulia/nib/internal/debuglog"
	"github.com/bricejulia/nib/internal/layout"
	"github.com/bricejulia/nib/internal/theme"
)

// languageFor returns the language name for path, or "" if no grammar
// recognizes it. The single source of truth for "what language is this
// file" — shared by syntax highlighting, the on-demand parse used by
// go-to-parent/definition, and the LSP server registry (see
// internal/lsp.DefaultServers, keyed on exactly these names), so those
// three can never disagree about a file's language.
func languageFor(path string) string {
	entry := grammars.DetectLanguage(path)
	if entry == nil {
		return ""
	}
	return entry.Name
}

// nonCodeGrammars names grammars whose highlighting isn't worth a parse
// for the files they claim. The registry maps ".txt" to the vimdoc
// grammar, so without this every large plain-text file pays a full parse
// (measured at 14ms per keystroke on an 1800-line file) to produce
// markup-directive colors for prose that has none.
//
// A deny list rather than an allow list, unlike syntaxCheckedLanguages
// next door in diagnostics.go: 206 of the registry's grammars ship a
// highlight query, so an allow list would silently drop highlighting for
// most languages nib supports, whereas that one guards correctness
// (invented error markers) and so has to fail closed. Both are data, not
// code — one more line covers one more language.
var nonCodeGrammars = map[string]bool{
	"vimdoc": true,
}

// highlightTimeoutMicros bounds a single parse. The parser's error
// recovery costs 8-12x a clean parse, and mid-edit code is nearly always
// momentarily unparseable, so a big enough file in a bad enough state can
// otherwise occupy the highlight worker for a very long time. Hitting this
// leaves the previous highlight in place (see highlightSource) — stale
// colors on a few lines, which is what the heuristic fallback already
// looks like, rather than a stalled worker.
// A var only so a test can wind it down far enough to actually trip; treat
// it as a constant otherwise. Note it is baked into each cached
// highlighter at construction (see highlightSource), so changing it later
// only affects languages not yet compiled.
var highlightTimeoutMicros uint64 = 250_000 // 250ms

// maxTreeSitterBytes bounds how much source highlightSource and parseTree
// will hand to tree-sitter at all. highlightTimeoutMicros already bounds
// how long a single parse can run, but a pathological input — most notably
// a "file" that's actually one multi-million-character line, the shape of
// e.g. a serialized PHP framework cache — can still cost real time and
// memory on the way to timing out, over and over, on every keystroke and
// on the UI goroutine itself for parseTree (see its own doc comment). Above
// this size tree-sitter has nothing worth the attempt anyway (highlighting
// a file this large is not something anyone reads a screenful at a time),
// so both bail out the same way they already do for "no grammar matches" —
// fail soft, not an error.
// A var, like highlightTimeoutMicros, only so tests can shrink it.
var maxTreeSitterBytes = 4 << 20 // 4MB

// highlightGrammar returns the grammar to highlight path with, or nil if
// tree-sitter has nothing useful to offer for it: no grammar claims the
// extension, the one that does is prose (see nonCodeGrammars), or it ships
// no highlight query. The single place that decision is made, so the
// worker's "is this worth waking up for" check (see Highlighter.submit)
// and the parse itself can't disagree about which files get highlighted.
func highlightGrammar(path string) *grammars.LangEntry {
	entry := grammars.DetectLanguage(path)
	if entry == nil || nonCodeGrammars[entry.Name] || strings.TrimSpace(entry.HighlightQuery) == "" {
		return nil
	}
	return entry
}

// highlighterCache holds one compiled *gotreesitter.Highlighter per
// language name — constructing one compiles a tree-sitter Query, so it's
// built once and reused across every tab/Open() of the same language, not
// rebuilt per file. A nil value (present key, nil pointer) records "this
// language's highlighter failed to construct", so a broken query doesn't
// get retried on every Open of that language.
//
// NOT synchronized, and a *gotreesitter.Highlighter owns a parser that is
// itself not safe for concurrent use. Everything here is therefore reached
// from exactly one goroutine at a time: the Highlighter worker's, once a
// View has one, and otherwise the UI goroutine's (see View.submitHighlight,
// which never straddles the two). Note this is a different cache from
// parserCache below, whose parsers stay on the UI goroutine — separate
// instances, and the library's own global state is all sync.Pool/sync.Map,
// so the two coexist safely.
var highlighterCache = map[string]*gotreesitter.Highlighter{}

// highlightBuffer returns per-line, tab-unexpanded, styled segments for
// buf's entire contents. See highlightSource, which it defers to; a parse
// that stops early leaves the buffer's existing highlight in place rather
// than blanking it.
func highlightBuffer(buf *Buffer) [][]layout.Segment {
	if buf == nil {
		return nil
	}
	lines, ok := highlightSource(buf.Path, buf.Source)
	if !ok {
		return buf.highlighted
	}
	return lines
}

// highlightSource returns per-line, tab-unexpanded, styled segments for
// src, parsed with the grammar path's extension selects.
//
// ok reports whether the answer is final. (nil, true) is a definitive "no
// tree-sitter highlighting for this file" — no grammar matches, the
// grammar is prose (see nonCodeGrammars), or its query won't compile — and
// callers should show the heuristic highlightLine fallback. (nil, false)
// means the parse gave up early (see highlightTimeoutMicros) and whatever
// highlight the buffer already has should be kept.
//
// Takes the path and bytes rather than a *Buffer precisely so it can run
// on the highlight worker's goroutine, which must not touch a Buffer the
// UI goroutine is free to be mutating (see Highlighter).
func highlightSource(path string, src []byte) ([][]layout.Segment, bool) {
	entry := highlightGrammar(path)
	if entry == nil || len(src) > maxTreeSitterBytes {
		return nil, true
	}

	hl, cached := highlighterCache[entry.Name]
	if !cached {
		var err error
		hl, err = gotreesitter.NewHighlighter(entry.Language(), entry.HighlightQuery,
			gotreesitter.WithHighlighterTimeoutMicros(highlightTimeoutMicros))
		if err != nil {
			hl = nil // cache the failure too: don't retry every Open of this language
		}
		highlighterCache[entry.Name] = hl
	}
	if hl == nil {
		return nil, true
	}

	// The Strict variant purely to learn whether the parse finished:
	// Highlight() reports a timed-out parse as an ordinary (but wrong,
	// half-colored) result. It hands the tree back instead of releasing it
	// internally, so releasing it is this function's job.
	ranges, tree, err := hl.HighlightIncrementalStrict(src, nil)
	if tree != nil {
		defer tree.Release()
	}
	if err != nil {
		debuglog.Debug("highlight %s: %v", path, err)
		return nil, false
	}
	return splitHighlightsByLine(src, ranges), true
}

// parserCache holds one *gotreesitter.Parser per language name, mirroring
// highlighterCache above — Parser construction is cheap relative to a
// Query compile, but reused all the same for consistency and to avoid any
// repeated setup cost across calls.
var parserCache = map[string]*gotreesitter.Parser{}

// parseTree parses buf's current Source fresh with its detected grammar,
// for on-demand navigation queries (go to parent/definition — see
// navigate.go). Returns ok=false if the language isn't recognized or
// parsing fails.
//
// Deliberately not cached on Buffer: these actions only ever fire on an
// explicit keypress, so a fresh parse each time needs no invalidation —
// notably simpler now that a Buffer can be shown in more than one pane at
// once (see BufferStore), where a cached *Tree would need to account for
// edits made from any of them.
//
// Nor is the tree reused across edits via Tree.Edit + the parser's
// incremental path, which looks like the obvious optimization and is not:
// measured on an 1800-line Go file, one mid-file character costs 33.5ms
// incrementally against 19.7ms for a parse from scratch, so in
// gotreesitter v0.47.1 "incremental" is 1.7x slower than starting over.
// The fix for parse cost is to keep it off the keystroke path (see
// Highlighter), not to try to make each parse smaller.
//
// Runs on the UI goroutine, using parserCache's own parsers — never the
// highlight worker's (see highlighterCache).
//
// Bounded by the same highlightTimeoutMicros budget as the background
// highlighter (see highlightSource): this runs synchronously on the UI
// goroutine, on every Open and every completed Normal-mode edit (see
// refreshSyntaxDiagnostics), so an unbounded parse here can freeze the
// whole app on a single pathological file — e.g. a cache file with one
// multi-million-character line, which some parsers pathologically slow
// down on. ParseStrict (rather than Parse) is what turns a timeout into
// the same ok=false "give up" result an unparseable file already
// produces, exactly like highlightSource's own Strict call.
func parseTree(buf *Buffer) (*gotreesitter.Tree, bool) {
	if buf == nil {
		return nil, false
	}
	entry := grammars.DetectLanguage(buf.Path)
	if entry == nil || len(buf.Source) > maxTreeSitterBytes {
		return nil, false
	}

	p, cached := parserCache[entry.Name]
	if !cached {
		p = gotreesitter.NewParser(entry.Language())
		p.SetTimeoutMicros(highlightTimeoutMicros)
		parserCache[entry.Name] = p
	}
	// Mirrors highlightSource's own nil check on hl: NewParser itself never
	// returns nil, but a nil parserCache entry (a previous caller's or
	// test's stale/failed write to this unsynchronized, process-lifetime
	// map) should degrade to "no parse" here, not crash on a nil receiver.
	if p == nil {
		return nil, false
	}

	tree, err := p.ParseStrict(buf.Source)
	if err != nil || tree == nil {
		return nil, false
	}
	return tree, true
}

// computeLineBounds returns, for each line i (0-indexed), the half-open
// byte range [starts[i], ends[i]) of that line's content within source —
// ends[i] excludes the terminating '\n' (there is none for the last
// line). For any source string this produces exactly the same count and
// content as strings.Split(string(source), "\n") would, including
// source == "" (one empty line) — which is exactly how Buffer.Lines is
// derived from Buffer.Source in Load, so the two stay aligned 1:1.
func computeLineBounds(source []byte) (starts, ends []uint32) {
	start := uint32(0)
	for i, b := range source {
		if b == '\n' {
			starts = append(starts, start)
			ends = append(ends, uint32(i))
			start = uint32(i + 1)
		}
	}
	starts = append(starts, start)
	ends = append(ends, uint32(len(source)))
	return starts, ends
}

// splitHighlightsByLine converts gotreesitter's flat, whole-buffer,
// byte-offset-range output into one []layout.Segment per physical line of
// source, aligned 1:1 with Buffer.Lines. Gaps between ranges (and any
// leading/trailing unstyled buffer, including the entire buffer when
// ranges is empty) render as default-style plain text. A range spanning
// multiple lines is split at every line boundary it crosses; no returned
// segment ever contains an embedded '\n'. Segments are raw — not
// tab-expanded — matching what the old highlightLine produced per line;
// see textwidth.ExpandTabsSegments for that step, applied at render time.
//
// ranges is expected sorted by StartByte and non-overlapping (that is
// what gotreesitter.Highlighter.Highlight guarantees), but this
// defensively re-sorts and clips overlaps anyway: cheap insurance against
// a library change or a future caller that hand-merges ranges from more
// than one Highlight() call without re-resolving them.
func splitHighlightsByLine(source []byte, ranges []gotreesitter.HighlightRange) [][]layout.Segment {
	starts, ends := computeLineBounds(source)
	lines := make([][]layout.Segment, len(starts))

	sorted := append([]gotreesitter.HighlightRange(nil), ranges...)
	sort.Slice(sorted, func(i, j int) bool { return sorted[i].StartByte < sorted[j].StartByte })

	li := 0 // current line index; only ever moves forward
	advanceTo := func(bytePos uint32) {
		for li+1 < len(starts) && bytePos >= starts[li+1] {
			li++
		}
	}

	// emit appends source[from:to) with style, splitting at every line
	// boundary crossed. Requires 0 <= from <= to <= len(source).
	emit := func(from, to uint32, style layout.Style) {
		for from < to {
			advanceTo(from)
			lineEnd := ends[li]
			stop := to
			if stop > lineEnd {
				stop = lineEnd
			}
			if stop > from {
				pushSeg(&lines[li], string(source[from:stop]), style)
			}
			if stop == lineEnd && stop < to {
				from = stop + 1 // skip the '\n' separator
			} else {
				from = stop
			}
		}
	}

	pos := uint32(0)
	total := uint32(len(source))
	for _, r := range sorted {
		start, end := r.StartByte, r.EndByte
		if end > total {
			end = total
		}
		if start < pos {
			start = pos // clip overlap with the previously emitted range
		}
		if start >= end {
			continue // empty, inverted, or fully-overlapped: skip
		}
		if start > pos {
			emit(pos, start, layout.Style{}) // gap: default style
		}
		emit(start, end, captureStyle(r.Capture))
		pos = end
	}
	if pos < total {
		emit(pos, total, layout.Style{})
	}

	return lines
}

// pushSeg appends text styled with style onto *line, coalescing with the
// previous segment if it shares the same style — same convention as
// SliceSegmentsByDisplayColumn's appendText and highlightLine's push.
func pushSeg(line *[]layout.Segment, text string, style layout.Style) {
	if text == "" {
		return
	}
	if n := len(*line); n > 0 && (*line)[n-1].Style == style {
		(*line)[n-1].Text += text
		return
	}
	*line = append(*line, layout.Segment{Text: text, Style: style})
}

// captureStyle maps a tree-sitter highlight capture name (e.g. "keyword",
// "function.builtin", "variable.parameter") to a display Style. Capture
// names follow the tree-sitter highlights.scm convention of dotted,
// increasingly specific categories; grammars vary in how deep they go, so
// this tries the full name first and, on a miss, strips the trailing
// ".xxx" segment and retries until it matches or runs out of segments,
// falling back to the default (unstyled) Style. This starter palette is
// deliberately small and easy to retune — it is not meant to be final.
func captureStyle(name string) layout.Style {
	for name != "" {
		if spec, ok := captureSpecs[name]; ok {
			return layout.Style{Foreground: theme.Get(spec.role), Attr: spec.attr}
		}
		i := strings.LastIndexByte(name, '.')
		if i < 0 {
			break
		}
		name = name[:i]
	}
	return layout.Style{}
}

// captureSpec is a capture's themed color role plus whatever fixed
// attribute (never themed) it carries, e.g. comment's dim.
type captureSpec struct {
	role theme.Role
	attr layout.AttrMask
}

var captureSpecs = map[string]captureSpec{
	"comment":          {role: theme.SyntaxComment, attr: layout.AttrDim},
	"string":           {role: theme.SyntaxString},
	"number":           {role: theme.SyntaxConstant},
	"constant":         {role: theme.SyntaxConstant},
	"boolean":          {role: theme.SyntaxConstant},
	"escape":           {role: theme.SyntaxConstant},
	"keyword":          {role: theme.SyntaxKeyword},
	"operator":         {role: theme.SyntaxKeyword},
	"label":            {role: theme.SyntaxKeyword},
	"tag":              {role: theme.SyntaxKeyword},
	"function":         {role: theme.SyntaxFunction},
	"constructor":      {role: theme.SyntaxFunction},
	"type":             {role: theme.SyntaxType},
	"variable.builtin": {role: theme.SyntaxType},
	"property":         {role: theme.SyntaxType},
	"attribute":        {role: theme.SyntaxType},
	"namespace":        {role: theme.SyntaxType},
	"module":           {role: theme.SyntaxType},
	// "variable" and "punctuation" are absent on purpose: unstyled today,
	// unstyled regardless of theme — see captureStyle's fallback.
}
