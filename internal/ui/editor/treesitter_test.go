package editor

import (
	"os"
	"testing"

	"github.com/odvcencio/gotreesitter"

	"github.com/bricejulia/nib/internal/layout"
)

func TestComputeLineBoundsEmptySource(t *testing.T) {
	starts, ends := computeLineBounds([]byte(""))
	if len(starts) != 1 || starts[0] != 0 || ends[0] != 0 {
		t.Fatalf("got starts=%v ends=%v, want one empty line [0,0)", starts, ends)
	}
}

func TestComputeLineBoundsMatchesLinesForTrailingAndNoTrailingNewline(t *testing.T) {
	cases := []struct {
		name   string
		source string
		lines  []string
	}{
		{"no trailing newline", "ab\ncd\nef", []string{"ab", "cd", "ef"}},
		{"single line", "hello", []string{"hello"}},
	}
	for _, c := range cases {
		t.Run(c.name, func(t *testing.T) {
			starts, ends := computeLineBounds([]byte(c.source))
			if len(starts) != len(c.lines) {
				t.Fatalf("got %d lines, want %d", len(starts), len(c.lines))
			}
			for i, want := range c.lines {
				got := c.source[starts[i]:ends[i]]
				if got != want {
					t.Errorf("line %d = %q, want %q", i, got, want)
				}
			}
		})
	}
}

func TestSplitHighlightsByLineEmptyRangesFillsDefaultStyle(t *testing.T) {
	lines := splitHighlightsByLine([]byte("ab\ncd"), nil)
	if len(lines) != 2 {
		t.Fatalf("got %d lines, want 2", len(lines))
	}
	if segText(lines[0]) != "ab" || segText(lines[1]) != "cd" {
		t.Fatalf("got %+v", lines)
	}
	for _, line := range lines {
		for _, s := range line {
			if s.Style != (layout.Style{}) {
				t.Errorf("expected default style with no ranges, got %+v", s)
			}
		}
	}
}

func TestSplitHighlightsByLineEmptySource(t *testing.T) {
	lines := splitHighlightsByLine([]byte(""), nil)
	if len(lines) != 1 {
		t.Fatalf("got %d lines, want 1 (matching Buffer.Lines == [\"\"])", len(lines))
	}
	if len(lines[0]) != 0 {
		t.Errorf("expected no segments for an empty line, got %+v", lines[0])
	}
}

func TestSplitHighlightsByLineSplitsRangeSpanningMultipleLines(t *testing.T) {
	source := []byte("ab\ncd\nef")
	ranges := []gotreesitter.HighlightRange{
		{StartByte: 0, EndByte: uint32(len(source)), Capture: "string"},
	}
	lines := splitHighlightsByLine(source, ranges)
	if len(lines) != 3 {
		t.Fatalf("got %d lines, want 3", len(lines))
	}
	want := []string{"ab", "cd", "ef"}
	for i, w := range want {
		if segText(lines[i]) != w {
			t.Errorf("line %d = %q, want %q", i, segText(lines[i]), w)
		}
		for _, s := range lines[i] {
			if s.Text != "" && s.Style.Foreground != layout.ColorGreen {
				t.Errorf("line %d: expected string style throughout, got %+v", i, s)
			}
		}
		for _, s := range lines[i] {
			if r := []rune(s.Text); len(r) > 0 {
				for _, ch := range r {
					if ch == '\n' {
						t.Errorf("line %d: segment must never contain an embedded newline: %+v", i, s)
					}
				}
			}
		}
	}
}

func TestSplitHighlightsByLineGapsGetDefaultStyle(t *testing.T) {
	source := []byte("xkeywordy")
	ranges := []gotreesitter.HighlightRange{
		{StartByte: 1, EndByte: 8, Capture: "keyword"},
	}
	lines := splitHighlightsByLine(source, ranges)
	if len(lines) != 1 {
		t.Fatalf("got %d lines, want 1", len(lines))
	}
	segs := lines[0]
	if segText(segs) != "xkeywordy" {
		t.Fatalf("got %q, want %q", segText(segs), "xkeywordy")
	}
	if len(segs) != 3 {
		t.Fatalf("expected 3 segments (gap, keyword, gap), got %+v", segs)
	}
	if segs[0].Text != "x" || segs[0].Style != (layout.Style{}) {
		t.Errorf("segment 0 = %+v, want {x, default}", segs[0])
	}
	if segs[1].Text != "keyword" || segs[1].Style.Foreground != layout.ColorYellow {
		t.Errorf("segment 1 = %+v, want {keyword, yellow}", segs[1])
	}
	if segs[2].Text != "y" || segs[2].Style != (layout.Style{}) {
		t.Errorf("segment 2 = %+v, want {y, default}", segs[2])
	}
}

func TestSplitHighlightsByLineDefensivelyHandlesUnsortedOverlappingRanges(t *testing.T) {
	source := []byte("abcdef")
	// Deliberately unsorted and overlapping (real Highlight() output never
	// looks like this — this is testing the defensive clipping, not real
	// library behavior).
	ranges := []gotreesitter.HighlightRange{
		{StartByte: 3, EndByte: 6, Capture: "string"},
		{StartByte: 0, EndByte: 4, Capture: "keyword"}, // overlaps the first
	}
	lines := splitHighlightsByLine(source, ranges)
	if len(lines) != 1 {
		t.Fatalf("got %d lines, want 1", len(lines))
	}
	if segText(lines[0]) != "abcdef" {
		t.Fatalf("got %q, want all text preserved despite overlap: %q", segText(lines[0]), "abcdef")
	}
	// Must not panic and must not duplicate/drop any byte — that's the
	// actual contract under test; exact style attribution at the overlap
	// point is not guaranteed to be at nanosecond dev-a nitpick level.
}

func TestCaptureStyleExactMatch(t *testing.T) {
	if got := captureStyle("comment"); got.Foreground != layout.ColorBrightBlack || got.Attr&layout.AttrDim == 0 {
		t.Errorf("got %+v, want dim bright-black", got)
	}
}

func TestCaptureStyleHierarchicalFallback(t *testing.T) {
	// "function.builtin" has no exact entry; must fall back to "function".
	got := captureStyle("function.builtin")
	want := captureStyle("function")
	if got != want {
		t.Errorf("function.builtin = %+v, want to fall back to function's style %+v", got, want)
	}
}

func TestCaptureStyleDeepFallback(t *testing.T) {
	// Strips more than one level if needed: "variable.parameter.builtin"
	// has no entry, nor does "variable.parameter", but "variable" does.
	got := captureStyle("variable.parameter.builtin")
	want := captureStyle("variable")
	if got != want {
		t.Errorf("got %+v, want variable's style %+v", got, want)
	}
}

func TestCaptureStyleUnknownReturnsDefault(t *testing.T) {
	if got := captureStyle("totally-unknown-capture-name"); got != (layout.Style{}) {
		t.Errorf("got %+v, want default style", got)
	}
}

func TestCaptureStyleEmptyNameReturnsDefault(t *testing.T) {
	if got := captureStyle(""); got != (layout.Style{}) {
		t.Errorf("got %+v, want default style", got)
	}
}

// --- Real gotreesitter integration tests (no mocking) ---

func TestHighlightBufferRealGoSnippet(t *testing.T) {
	// No trailing newline in Source — matching what Buffer.Load actually
	// produces after TrimSuffix (see buffer.go); a trailing '\n' here
	// would manufacture a phantom 6th empty line that Lines doesn't have.
	src := "package main\n\nfunc main() {\n\tx := 42 // comment\n}"
	buf := &Buffer{Path: "test.go", Source: []byte(src), Lines: []string{
		"package main", "", "func main() {", "\tx := 42 // comment", "}",
	}}
	lines := highlightBuffer(buf)
	if lines == nil {
		t.Fatal("expected non-nil highlighting for a recognized .go file")
	}
	if len(lines) != len(buf.Lines) {
		t.Fatalf("got %d highlighted lines, want %d (matching Buffer.Lines)", len(lines), len(buf.Lines))
	}

	if !containsStyledText(lines[0], "package", layout.ColorYellow) {
		t.Errorf("expected \"package\" styled as a keyword on line 0, got %+v", lines[0])
	}
	if !containsStyledText(lines[3], "// comment", layout.ColorBrightBlack) {
		t.Errorf("expected a dim/bright-black comment on line 3, got %+v", lines[3])
	}
	if !containsStyledText(lines[3], "42", layout.ColorMagenta) {
		t.Errorf("expected \"42\" styled as a number on line 3, got %+v", lines[3])
	}
}

func TestHighlightBufferRealPhpSnippet(t *testing.T) {
	// No trailing newline — see the .go test above for why that matters.
	src := "<?php\n// comment\n$x = 42;\necho \"hi $x\";"
	wantLines := []string{"<?php", "// comment", "$x = 42;", "echo \"hi $x\";"}
	buf := &Buffer{Path: "test.php", Source: []byte(src), Lines: wantLines}
	lines := highlightBuffer(buf)
	if lines == nil {
		t.Fatal("expected non-nil highlighting for a recognized .php file")
	}
	if len(lines) != len(wantLines) {
		t.Fatalf("got %d highlighted lines, want %d (matching Buffer.Lines)", len(lines), len(wantLines))
	}
	if !containsStyledText(lines[1], "// comment", layout.ColorBrightBlack) {
		t.Errorf("expected a comment on line 1, got %+v", lines[1])
	}
	if !containsStyledText(lines[2], "42", layout.ColorMagenta) {
		t.Errorf("expected \"42\" styled as a number on line 2, got %+v", lines[2])
	}
}

func TestHighlightBufferBladeAndTwigDetected(t *testing.T) {
	for _, path := range []string{"view.blade.php", "template.twig"} {
		buf := &Buffer{Path: path, Source: []byte("hello"), Lines: []string{"hello"}}
		if highlightBuffer(buf) == nil {
			t.Errorf("expected %q to be recognized (Blade/Twig ship bundled grammars)", path)
		}
	}
}

func TestHighlightBufferUnsupportedExtensionReturnsNil(t *testing.T) {
	// gotreesitter's registry is large (200+ grammars with linguist-based
	// extension matching) and surprisingly maps even ".txt" to a real
	// "vimdoc" grammar (confirmed directly) — so this uses a made-up
	// extension no real registry would ever claim, rather than assuming
	// a plain-looking one is actually unrecognized.
	buf := &Buffer{Path: "notes.definitely-not-a-real-extension-xyz123", Source: []byte("plain text"), Lines: []string{"plain text"}}
	if got := highlightBuffer(buf); got != nil {
		t.Errorf("expected nil for an unrecognized extension, got %+v", got)
	}
}

func TestHighlightBufferNoPathReturnsNil(t *testing.T) {
	// In-memory buffers with no path (as some existing tests construct
	// directly) must fall back to the heuristic, not panic.
	buf := &Buffer{Source: []byte("x"), Lines: []string{"x"}}
	if got := highlightBuffer(buf); got != nil {
		t.Errorf("expected nil with no path/extension to match, got %+v", got)
	}
}

func TestHighlightBufferNilBufferReturnsNil(t *testing.T) {
	if got := highlightBuffer(nil); got != nil {
		t.Errorf("expected nil for a nil buffer, got %+v", got)
	}
}

// parseTree runs synchronously on the UI goroutine, on every Open and every
// completed Normal-mode edit (see refreshSyntaxDiagnostics) — unlike
// highlightSource, which the background Highlighter already bounds (see
// TestHighlightSourceReportsAParseThatRanOutOfTime, which this mirrors). A
// timeout that only capped the background path and left this one unbounded
// would still let a single pathological file freeze the app.
func TestParseTreeReportsAParseThatRanOutOfTime(t *testing.T) {
	src, err := os.ReadFile("view.go")
	if err != nil {
		t.Skip(err)
	}

	// The timeout is baked into the cached parser, so both it and the cache
	// entry have to be swapped out and put back. Restoring via a plain
	// "parserCache[\"go\"] = restoreCached" would be wrong (and was, until
	// -shuffle=on ordering this test before anything else ever populated
	// the cache caught it): a single-value map read of an absent key
	// returns the same nil a present-but-failed entry would, so restoring
	// unconditionally can plant an explicit nil under "go" — poisoning
	// every later test's parseTree call in this process with a permanent
	// "this language's parser failed" cache hit for the rest of the run
	// (parseTree, unlike highlightSource, has no nil guard before using
	// the cached value, so that poisoning crashes instead of degrading).
	restoreTimeout := highlightTimeoutMicros
	restoreCached, hadCached := parserCache["go"]
	t.Cleanup(func() {
		highlightTimeoutMicros = restoreTimeout
		if hadCached {
			parserCache["go"] = restoreCached
		} else {
			delete(parserCache, "go")
		}
	})
	highlightTimeoutMicros = 1 // 1µs: no real parse finishes in that
	delete(parserCache, "go")

	buf := &Buffer{Path: "probe.go", Source: src}
	tree, ok := parseTree(buf)
	if ok {
		t.Fatal("a 1µs budget should not have been enough to finish parsing 1800 lines")
	}
	if tree != nil {
		t.Error("an unfinished parse should report ok=false, not hand back a partial tree")
	}
}

// Above maxTreeSitterBytes, both tree-sitter entry points must bail out the
// same way they already do for an unrecognized grammar — fail soft, not
// spend a bounded-but-still-real parse on content this large (see that
// constant's doc comment for why: a pathological single-line file can cost
// real time/memory on the way to timing out, over and over).
func TestHighlightSourceSkipsPathologicallyLargeSource(t *testing.T) {
	restore := maxTreeSitterBytes
	t.Cleanup(func() { maxTreeSitterBytes = restore })
	maxTreeSitterBytes = 10 // smaller than any real source below

	lines, ok := highlightSource("probe.go", []byte("package main\n\nfunc main() {}\n"))
	if !ok {
		t.Fatal("skipping for size should report ok=true (definitive no-highlight), like an unrecognized grammar")
	}
	if lines != nil {
		t.Errorf("expected nil lines, got %+v", lines)
	}
}

func TestParseTreeSkipsPathologicallyLargeSource(t *testing.T) {
	restore := maxTreeSitterBytes
	t.Cleanup(func() { maxTreeSitterBytes = restore })
	maxTreeSitterBytes = 10 // smaller than any real source below

	buf := &Buffer{Path: "probe.go", Source: []byte("package main\n\nfunc main() {}\n")}
	if _, ok := parseTree(buf); ok {
		t.Fatal("expected parseTree to skip source over maxTreeSitterBytes")
	}
}

func containsStyledText(segs []layout.Segment, text string, color layout.Color) bool {
	for _, s := range segs {
		if s.Text == text && s.Style.Foreground == color {
			return true
		}
	}
	return false
}
