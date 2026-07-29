package editor

import (
	"sort"
	"strings"

	"github.com/odvcencio/gotreesitter"
	"github.com/odvcencio/gotreesitter/grammars"

	"github.com/bricejulia/kiwi/internal/layout"
)

// highlighterCache holds one compiled *gotreesitter.Highlighter per
// language name — constructing one compiles a tree-sitter Query, so it's
// built once and reused across every tab/Open() of the same language, not
// rebuilt per file. A nil value (present key, nil pointer) records "this
// language's highlighter failed to construct", so a broken query doesn't
// get retried on every Open of that language.
var highlighterCache = map[string]*gotreesitter.Highlighter{}

// highlightBuffer returns per-line, tab-unexpanded, styled segments for
// buf's entire contents, or nil if no grammar matches buf.Path or the
// highlighter fails to construct — callers fall back to the heuristic
// highlightLine in that case. Computed once in Open and recomputed after
// every edit (see View.reHighlight); the result is cached on
// Buffer.highlighted, not per-tab — see its doc comment for why.
func highlightBuffer(buf *Buffer) [][]layout.Segment {
	if buf == nil {
		return nil
	}
	entry := grammars.DetectLanguage(buf.Path)
	if entry == nil || strings.TrimSpace(entry.HighlightQuery) == "" {
		return nil
	}

	hl, cached := highlighterCache[entry.Name]
	if !cached {
		var err error
		hl, err = gotreesitter.NewHighlighter(entry.Language(), entry.HighlightQuery)
		if err != nil {
			hl = nil // cache the failure too: don't retry every Open of this language
		}
		highlighterCache[entry.Name] = hl
	}
	if hl == nil {
		return nil
	}

	return splitHighlightsByLine(buf.Source, hl.Highlight(buf.Source))
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
// Deliberately not cached on Buffer: unlike highlighting, which is
// recomputed on every keystroke via reHighlight, these actions only ever
// fire on an explicit keypress, so a fresh parse each time is cheap and
// needs no invalidation — notably simpler now that a Buffer can be shown
// in more than one pane at once (see BufferStore), where a cached *Tree
// would need to account for edits made from any of them.
func parseTree(buf *Buffer) (*gotreesitter.Tree, bool) {
	if buf == nil {
		return nil, false
	}
	entry := grammars.DetectLanguage(buf.Path)
	if entry == nil {
		return nil, false
	}

	p, cached := parserCache[entry.Name]
	if !cached {
		p = gotreesitter.NewParser(entry.Language())
		parserCache[entry.Name] = p
	}

	tree, err := p.Parse(buf.Source)
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
		if style, ok := captureStyles[name]; ok {
			return style
		}
		i := strings.LastIndexByte(name, '.')
		if i < 0 {
			break
		}
		name = name[:i]
	}
	return layout.Style{}
}

var captureStyles = map[string]layout.Style{
	"comment":          {Attr: layout.AttrDim, Foreground: layout.ColorBrightBlack},
	"string":           {Foreground: layout.ColorGreen},
	"number":           {Foreground: layout.ColorMagenta},
	"constant":         {Foreground: layout.ColorMagenta},
	"boolean":          {Foreground: layout.ColorMagenta},
	"keyword":          {Foreground: layout.ColorYellow},
	"operator":         {Foreground: layout.ColorYellow},
	"function":         {Foreground: layout.ColorBlue},
	"constructor":      {Foreground: layout.ColorBlue},
	"type":             {Foreground: layout.ColorCyan},
	"variable":         {}, // most identifiers stay unstyled
	"variable.builtin": {Foreground: layout.ColorCyan},
	"property":         {Foreground: layout.ColorCyan},
	"tag":              {Foreground: layout.ColorYellow},
	"attribute":        {Foreground: layout.ColorCyan},
	"punctuation":      {},
	"label":            {Foreground: layout.ColorYellow},
	"escape":           {Foreground: layout.ColorMagenta},
	"namespace":        {Foreground: layout.ColorCyan},
	"module":           {Foreground: layout.ColorCyan},
}
