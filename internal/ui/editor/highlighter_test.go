package editor

import (
	"os"
	"path/filepath"
	"strings"
	"sync"
	"testing"
	"time"

	"github.com/bricejulia/kiwi/internal/layout"
)

// goBuffer returns a Go buffer holding lines, already highlighted, so a
// test can watch what an edit does to an EXISTING highlight cache.
func goBuffer(t *testing.T, lines ...string) *Buffer {
	t.Helper()
	b := &Buffer{Lines: lines, Path: "probe.go", Source: []byte(strings.Join(lines, "\n"))}
	b.highlighted = highlightBuffer(b)
	if b.highlighted == nil {
		t.Fatal("a .go buffer should highlight")
	}
	if len(b.highlighted) != len(b.Lines) {
		t.Fatalf("highlighted has %d lines, Lines has %d", len(b.highlighted), len(b.Lines))
	}
	return b
}

// The invariant every keystroke depends on: highlighted stays index-aligned
// with Lines, with only the touched lines cleared. Misalignment would paint
// one line's colors onto another's text (renderBody indexes both by the
// same ln), which is worse than no colors at all.
func TestBufferEditsKeepHighlightAligned(t *testing.T) {
	tests := []struct {
		name     string
		edit     func(b *Buffer)
		wantLine int  // the line expected to be cleared, -1 for none in particular
		wantNil  bool // whether the whole cache is expected to be dropped
	}{
		{
			name:     "InsertText clears just that line",
			edit:     func(b *Buffer) { b.InsertText(1, 0, "x") },
			wantLine: 1,
		},
		{
			name:     "SplitLine clears both halves",
			edit:     func(b *Buffer) { b.SplitLine(2, 4) },
			wantLine: 2,
		},
		{
			name:     "DeleteBackward within a line clears that line",
			edit:     func(b *Buffer) { b.DeleteBackward(2, 3) },
			wantLine: 2,
		},
		{
			name:     "DeleteBackward joining lines clears the survivor",
			edit:     func(b *Buffer) { b.DeleteBackward(2, 0) },
			wantLine: 1,
		},
		{
			name:     "DeleteLine drops that line's entry",
			edit:     func(b *Buffer) { b.DeleteLine(1) },
			wantLine: -1,
		},
		{
			name:     "InsertLines makes room for the new lines",
			edit:     func(b *Buffer) { b.InsertLines(1, []string{"\tx := 1", "\ty := 2"}) },
			wantLine: 1,
		},
		{
			name:     "Restore drops the whole cache",
			edit:     func(b *Buffer) { b.Restore([]string{"package other"}) },
			wantNil:  true,
			wantLine: -1,
		},
	}

	for _, tc := range tests {
		t.Run(tc.name, func(t *testing.T) {
			b := goBuffer(t, "package main", "", "func main() {}", "")
			before := b.rev
			tc.edit(b)

			if b.rev <= before {
				t.Errorf("rev = %d, want > %d: an edit must invalidate results in flight", b.rev, before)
			}
			if tc.wantNil {
				if b.highlighted != nil {
					t.Error("highlighted should have been dropped entirely")
				}
				return
			}
			if len(b.highlighted) != len(b.Lines) {
				t.Fatalf("highlighted has %d entries, Lines has %d — they must stay 1:1", len(b.highlighted), len(b.Lines))
			}
			if tc.wantLine >= 0 && b.highlighted[tc.wantLine] != nil {
				t.Errorf("line %d should have been cleared, so it renders with the heuristic", tc.wantLine)
			}
			// The whole point of splicing rather than dropping: lines the
			// edit didn't touch keep their real colors. No case here edits
			// line 0, so it is the canary.
			if b.highlighted[0] == nil {
				t.Error("line 0 was untouched and should have kept its highlighting")
			}
		})
	}
}

// An edit that leaves the buffer with exactly one line still has to leave a
// one-entry cache behind, not a two-entry one.
func TestDeleteLineOnLastLineKeepsAlignment(t *testing.T) {
	b := goBuffer(t, "package main")
	if got := b.DeleteLine(0); got != "package main" {
		t.Fatalf("DeleteLine returned %q", got)
	}
	if len(b.Lines) != 1 {
		t.Fatalf("Lines = %d, want 1", len(b.Lines))
	}
	if len(b.highlighted) != 1 {
		t.Errorf("highlighted has %d entries, want 1", len(b.highlighted))
	}
}

// A caller whose splice disagrees with the buffer's would leave the two
// permanently misaligned; dropping the cache is the safe way to lose.
func TestSpliceHighlightOutOfRangeDropsTheCache(t *testing.T) {
	b := goBuffer(t, "package main", "")
	b.spliceHighlight(5, 1, 1)
	if b.highlighted != nil {
		t.Error("an out-of-range splice should drop the cache rather than mangle it")
	}
}

// A result computed before the user's latest keystroke describes text that
// no longer exists — applying it would paint stale colors over lines the
// edit already cleared.
func TestApplyHighlightResultDropsStaleResults(t *testing.T) {
	b := goBuffer(t, "package main", "")
	fresh := [][]layout.Segment{{{Text: "package main"}}, nil}

	inFlight := HighlightResult{buf: b, rev: b.rev, lines: fresh}
	b.InsertText(0, 0, "x") // the user types while the parse is running

	ApplyHighlightResult(inFlight)
	if len(b.highlighted) > 0 && b.highlighted[0] != nil {
		t.Error("a result from before the edit should have been dropped")
	}

	current := HighlightResult{buf: b, rev: b.rev, lines: fresh}
	ApplyHighlightResult(current)
	if len(b.highlighted) == 0 || b.highlighted[0] == nil {
		t.Error("a result matching the buffer's current revision should have been stored")
	}
}

// fakeHighlighter is a Highlighter whose parses are counted and controlled,
// so coalescing can be asserted without depending on how long a real parse
// takes.
type fakeHighlighter struct {
	*Highlighter
	mu      sync.Mutex
	calls   int
	lastSrc string
	results chan HighlightResult
}

func newFakeHighlighter(t *testing.T) *fakeHighlighter {
	t.Helper()
	f := &fakeHighlighter{results: make(chan HighlightResult, 64)}
	f.Highlighter = NewHighlighter(func(ev interface{}) {
		if r, ok := ev.(HighlightResult); ok {
			f.results <- r
		}
	})
	f.highlight = func(path string, src []byte) ([][]layout.Segment, bool) {
		f.mu.Lock()
		f.calls++
		f.lastSrc = string(src)
		f.mu.Unlock()
		return [][]layout.Segment{{{Text: string(src)}}}, true
	}
	t.Cleanup(f.Close)
	return f
}

func (f *fakeHighlighter) seen() (int, string) {
	f.mu.Lock()
	defer f.mu.Unlock()
	return f.calls, f.lastSrc
}

// The reason the worker debounces at all: a burst of keystrokes must cost a
// small number of parses, not one per key, and the one that does run has to
// see the final text.
func TestHighlighterCoalescesABurstOfEdits(t *testing.T) {
	f := newFakeHighlighter(t)
	b := goBuffer(t, "package main")

	const keys = 50
	for i := 0; i < keys; i++ {
		b.InsertText(0, 0, "x")
		f.Submit(b)
	}

	var (
		calls int
		src   string
	)
	deadline := time.Now().Add(5 * time.Second)
	for time.Now().Before(deadline) {
		if calls, src = f.seen(); calls > 0 {
			break
		}
		time.Sleep(2 * time.Millisecond)
	}

	if calls == 0 {
		t.Fatal("the worker never parsed anything")
	}
	if calls >= keys {
		t.Errorf("parsed %d times for %d keystrokes: the burst was not coalesced", calls, keys)
	}
	if want := string(b.Source); src != want {
		t.Errorf("parsed %q, want the newest text %q", src, want)
	}
}

// Files no grammar wants (or that only a prose grammar claims) must not
// wake the worker at all — that check is the whole reason a big .txt file
// stopped costing a parse per keystroke.
func TestHighlighterSkipsFilesWithNothingToHighlight(t *testing.T) {
	f := newFakeHighlighter(t)
	for _, path := range []string{"notes.txt", "mystery.zzz"} {
		b := &Buffer{Lines: []string{"hello"}, Path: path, Source: []byte("hello")}
		f.SubmitNow(b)
	}

	select {
	case r := <-f.results:
		t.Fatalf("worker produced a result for %q", r.buf.Path)
	case <-time.After(highlightDebounce + 200*time.Millisecond):
	}
	if calls, _ := f.seen(); calls != 0 {
		t.Errorf("parsed %d times, want 0", calls)
	}
}

// SubmitNow exists for the edits that aren't part of a burst (open, rename)
// — they shouldn't sit out a debounce meant for typing.
func TestHighlighterSubmitNowSkipsTheDebounce(t *testing.T) {
	f := newFakeHighlighter(t)
	b := goBuffer(t, "package main")

	start := time.Now()
	f.SubmitNow(b)
	select {
	case r := <-f.results:
		if r.rev != b.rev {
			t.Errorf("result rev = %d, want %d", r.rev, b.rev)
		}
	case <-time.After(5 * time.Second):
		t.Fatal("no result")
	}
	if elapsed := time.Since(start); elapsed >= highlightDebounce {
		t.Errorf("took %v: SubmitNow should not wait out the %v debounce", elapsed, highlightDebounce)
	}
}

// A parse that gives up (see highlightTimeoutMicros) must leave the
// previous highlight alone rather than posting an empty one, which would
// blank a whole file's colors because it was briefly too slow to parse.
func TestHighlighterKeepsPreviousHighlightWhenTheParseGivesUp(t *testing.T) {
	f := newFakeHighlighter(t)
	f.highlight = func(path string, src []byte) ([][]layout.Segment, bool) {
		return nil, false
	}
	b := goBuffer(t, "package main")
	before := b.highlighted

	f.SubmitNow(b)
	select {
	case <-f.results:
		t.Fatal("a parse that stopped early should not post a result")
	case <-time.After(highlightDebounce + 200*time.Millisecond):
	}
	if b.highlighted == nil || len(b.highlighted) != len(before) {
		t.Error("the previous highlight should have been left in place")
	}
}

// Close must not leave a goroutine behind, and must be safe to call twice
// (main.go defers it; a test may call it explicitly first).
func TestHighlighterCloseIsIdempotent(t *testing.T) {
	h := NewHighlighter(nil)
	b := goBuffer(t, "package main")
	h.Submit(b)
	h.Close()
	h.Close()
}

// The two tree-sitter entry points run on different goroutines — the worker
// highlights while go-to-definition parses on the UI goroutine — so they
// must not share a parser. Run with -race, this is what proves it.
func TestHighlightWorkerAndParseTreeRunConcurrently(t *testing.T) {
	h := NewHighlighter(nil)
	defer h.Close()

	b := goBuffer(t, "package main", "", "func main() {}", "")
	var wg sync.WaitGroup
	wg.Add(1)
	go func() {
		defer wg.Done()
		for i := 0; i < 20; i++ {
			h.SubmitNow(b)
		}
	}()
	for i := 0; i < 20; i++ {
		local := &Buffer{Lines: b.Lines, Path: b.Path, Source: b.Source}
		if tree, ok := parseTree(local); ok {
			tree.Release()
		}
	}
	wg.Wait()
}

// The tail-latency guard: a parse that runs out of time must report itself
// as unfinished rather than returning the half-colored tree it got to. What
// callers do with that is tested above — this is the part that has to come
// from the parser.
func TestHighlightSourceReportsAParseThatRanOutOfTime(t *testing.T) {
	src, err := os.ReadFile("view.go")
	if err != nil {
		t.Skip(err)
	}

	// The timeout is baked into the cached highlighter, so both it and the
	// cache entry have to be swapped out and put back.
	restoreTimeout, restoreCached := highlightTimeoutMicros, highlighterCache["go"]
	t.Cleanup(func() {
		highlightTimeoutMicros = restoreTimeout
		highlighterCache["go"] = restoreCached
	})
	highlightTimeoutMicros = 1 // 1µs: no real parse finishes in that
	delete(highlighterCache, "go")

	lines, ok := highlightSource("probe.go", src)
	if ok {
		t.Fatal("a 1µs budget should not have been enough to finish parsing 1800 lines")
	}
	if lines != nil {
		t.Error("an unfinished parse should return no lines, so the caller keeps the previous highlight")
	}
}

// End to end, the way the real app is wired: a pane with a worker, results
// coming back through a Post that stands in for cmd/kiwi's event loop. What
// a user experiences is that typing never waits, and a moment after they
// stop, real colors are back on the line they were editing.
func TestTypingThenPausingRestoresRealHighlighting(t *testing.T) {
	dir := t.TempDir()
	path := filepath.Join(dir, "main.go")
	if err := os.WriteFile(path, []byte("package main\n\nfunc main() {\n\tprintln(\"hi\")\n}\n"), 0o644); err != nil {
		t.Fatal(err)
	}

	results := make(chan HighlightResult, 64)
	h := NewHighlighter(func(ev interface{}) {
		if r, ok := ev.(HighlightResult); ok {
			results <- r
		}
	})
	defer h.Close()

	v := NewView()
	v.SetHighlighter(h)
	v.Open(path)
	drainInto(t, results, 300*time.Millisecond) // the open's own highlight

	// Type a word at the top of the body. Each keystroke must leave that
	// line uncolored (the heuristic renders it) rather than blocking.
	v.HandleKey(layout.Key{Named: "down"})
	v.HandleKey(layout.Key{Named: "down"})
	v.HandleKey(layout.Key{Text: "i"})
	for _, r := range "const x = 1" {
		v.HandleKey(layout.Key{Text: string(r)})
	}
	tb := v.activeTab()
	if len(tb.buf.highlighted) > tb.cursorLn && tb.buf.highlighted[tb.cursorLn] != nil {
		t.Error("the line being typed on should be cleared, not re-highlighted inline")
	}

	// Now stop typing, and let the worker catch up.
	deadline := time.Now().Add(5 * time.Second)
	for time.Now().Before(deadline) {
		drainInto(t, results, highlightDebounce+300*time.Millisecond)
		if len(tb.buf.highlighted) > tb.cursorLn && tb.buf.highlighted[tb.cursorLn] != nil {
			return
		}
	}
	t.Error("after a pause, the edited line should have real tree-sitter colors again")
}

// drainInto applies every highlight result that arrives within d, the way
// cmd/kiwi's event loop does. Returns once the channel has been quiet for d.
func drainInto(t *testing.T, results <-chan HighlightResult, d time.Duration) {
	t.Helper()
	for {
		select {
		case r := <-results:
			ApplyHighlightResult(r)
		case <-time.After(d):
			return
		}
	}
}
