package editor

import (
	"sync"
	"time"

	"github.com/bricejulia/nib/internal/layout"
)

// highlightDebounce is how long the worker waits for typing to stop before
// spending a parse. Long enough that a burst of keystrokes collapses into
// one parse, short enough that a pause between words is already enough to
// get real colors back.
const highlightDebounce = 60 * time.Millisecond

// highlightMaxStall bounds how long continuous typing can hold the
// debounce open. Without it, someone typing steadily for a minute would
// see the heuristic fallback on every line they touched for that whole
// minute; with it, real colors land at least this often.
const highlightMaxStall = 300 * time.Millisecond

// Highlighter computes tree-sitter highlighting off the UI goroutine.
//
// A full re-parse is the entire cost of an editor keystroke — 236ms per
// key on an 1800-line Go file, and worse while the file is momentarily
// unparseable, which while typing it nearly always is (see
// Buffer.highlighted). Doing it inline is what made typing lag; this moves
// it behind a debounce, on its own goroutine, and lets the keystroke
// return immediately with the edited lines showing the cheap highlightLine
// heuristic until the real answer arrives.
//
// One per process, shared by every pane (like BufferStore and Register):
// results are stored on the shared Buffer, so a buffer open in two panes
// is highlighted once and both see it. Exactly one goroutine, deliberately
// — a gotreesitter.Highlighter owns a parser that is not safe for
// concurrent use, and highlighterCache is an unguarded map; keeping this
// the only caller of highlightSource is what makes both safe without a
// mutex.
//
// Only the LATEST submission per buffer is ever parsed. Anything
// superseded while the worker was busy is dropped, not queued: it
// describes text the user has already typed past.
type Highlighter struct {
	// post marshals a finished HighlightResult onto the UI event loop
	// (wired to ui.App.Post — the same field shape lsp.Manager.Post and
	// finder.View.Post use). A nil post makes results go nowhere, which is
	// what lets tests run a worker with no event loop.
	post func(ev interface{})

	// highlight does the actual parse. A field rather than a direct call
	// so tests can substitute a fake and assert on coalescing without
	// depending on how long a real parse takes. See highlightSource for
	// the contract, ok included.
	//
	// Set before the first Submit and not after: the worker reads it, and
	// the only thing making that safe without a lock is that the first
	// Submit's channel send orders the write ahead of every read.
	highlight func(path string, src []byte) ([][]layout.Segment, bool)

	// wake is signalled (never blocking) whenever pending changes, so a
	// submit costs a mutex and a non-blocking send — no allocation, no
	// timer, nothing that can stall the UI goroutine.
	wake chan struct{}
	quit chan struct{}
	done chan struct{}

	mu sync.Mutex
	// pending holds the newest unparsed snapshot per buffer, replaced
	// rather than appended to. firstPending records when the oldest
	// still-unparsed snapshot arrived, for highlightMaxStall.
	pending      map[*Buffer]highlightJob
	firstPending time.Time
	// now is time.Now, indirected for tests.
	now func() time.Time
}

// highlightJob is one buffer's text at one revision, captured on the UI
// goroutine at submit time. It carries the bytes and path by value: the
// worker must never read through buf, which the UI goroutine is free to be
// mutating — the pointer travels purely as the identity the result is
// matched back to. Buffer.Source is always replaced wholesale by resync,
// never written in place, so the slice itself is safe to hand over.
type highlightJob struct {
	buf  *Buffer
	path string
	src  []byte
	rev  uint64
}

// HighlightResult is a finished highlight on its way back to the UI
// goroutine, where ApplyHighlightResult stores it. It crosses the event
// loop like lsp.DiagnosticsEvent does — see cmd/nib/main.go's handler.
type HighlightResult struct {
	buf   *Buffer
	rev   uint64
	lines [][]layout.Segment
}

// NewHighlighter starts a highlight worker that delivers its results
// through post. Call Close to stop it.
func NewHighlighter(post func(ev interface{})) *Highlighter {
	h := &Highlighter{
		post:      post,
		highlight: highlightSource,
		wake:      make(chan struct{}, 1),
		quit:      make(chan struct{}),
		done:      make(chan struct{}),
		pending:   map[*Buffer]highlightJob{},
		now:       time.Now,
	}
	go h.run()
	return h
}

// Submit queues buf's current contents to be highlighted after the
// debounce, superseding any snapshot of the same buffer not yet parsed.
// Cheap enough to call on every keystroke — that is the whole point — and
// safe to call on a buffer no grammar handles, which costs two map lookups
// (see highlightGrammar) and never wakes the worker.
func (h *Highlighter) Submit(buf *Buffer) {
	h.submit(buf, false)
}

// SubmitNow is Submit without the debounce, for the one-off re-highlights
// that aren't part of a burst — opening a file, renaming one. The parse
// still happens on the worker, so the caller doesn't block on it.
func (h *Highlighter) SubmitNow(buf *Buffer) {
	h.submit(buf, true)
}

func (h *Highlighter) submit(buf *Buffer, immediate bool) {
	if h == nil || buf == nil || highlightGrammar(buf.Path) == nil {
		return
	}

	h.mu.Lock()
	h.pending[buf] = highlightJob{buf: buf, path: buf.Path, src: buf.Source, rev: buf.rev}
	if h.firstPending.IsZero() || immediate {
		// An immediate submission backdates the stall clock past
		// highlightMaxStall, so the worker's next look skips the debounce
		// for everything currently pending.
		h.firstPending = h.now()
		if immediate {
			h.firstPending = h.firstPending.Add(-highlightMaxStall)
		}
	}
	h.mu.Unlock()

	select {
	case h.wake <- struct{}{}:
	default: // already awake or about to be; nothing to add
	}
}

// Close stops the worker and waits for an in-flight parse to finish, so
// nothing is still running once nib exits. Idempotent.
func (h *Highlighter) Close() {
	if h == nil {
		return
	}
	select {
	case <-h.quit: // already closed
		return
	default:
	}
	close(h.quit)
	<-h.done
}

// run is the worker goroutine: wait for work, let it settle, parse the
// newest snapshot of each buffer, post the results, repeat.
func (h *Highlighter) run() {
	defer close(h.done)

	timer := time.NewTimer(highlightDebounce)
	if !timer.Stop() {
		<-timer.C
	}
	defer timer.Stop()

	for {
		select {
		case <-h.quit:
			return
		case <-h.wake:
		}

		// Settle: keep restarting the debounce while keystrokes keep
		// arriving, but not past highlightMaxStall since the oldest
		// unparsed snapshot — otherwise uninterrupted typing would defer
		// real colors indefinitely.
		for h.waitOut(timer) {
		}

		for _, job := range h.takePending() {
			select {
			case <-h.quit:
				return
			default:
			}
			h.process(job)
		}
	}
}

// waitOut sleeps out one debounce window, reporting whether another
// submission arrived during it and the deadline still allows waiting
// again. Returns false when it's time to parse — because typing paused, or
// because highlightMaxStall has run out, or because the worker is
// shutting down.
func (h *Highlighter) waitOut(timer *time.Timer) bool {
	if h.stalled() {
		return false
	}
	timer.Reset(highlightDebounce)
	select {
	case <-h.quit:
		timer.Stop() // not drained: run returns without consulting it again
		return false
	case <-timer.C:
		return false // typing paused: parse what's pending
	case <-h.wake:
		return true // another edit landed; wait again
	}
}

// stalled reports whether the oldest unparsed snapshot has been waiting
// longer than highlightMaxStall.
func (h *Highlighter) stalled() bool {
	h.mu.Lock()
	defer h.mu.Unlock()
	return !h.firstPending.IsZero() && h.now().Sub(h.firstPending) >= highlightMaxStall
}

// takePending removes and returns every pending job, resetting the stall
// clock. Anything submitted from here on is a new batch.
func (h *Highlighter) takePending() []highlightJob {
	h.mu.Lock()
	defer h.mu.Unlock()

	if len(h.pending) == 0 {
		return nil
	}
	jobs := make([]highlightJob, 0, len(h.pending))
	for buf, job := range h.pending {
		jobs = append(jobs, job)
		delete(h.pending, buf)
	}
	h.firstPending = time.Time{}
	return jobs
}

// process parses one snapshot and posts the result. A snapshot already
// superseded while it sat in the queue is skipped rather than parsed: the
// newer one is right behind it, and the answer would be discarded on
// arrival anyway (see ApplyHighlightResult).
func (h *Highlighter) process(job highlightJob) {
	if h.superseded(job) {
		return
	}
	lines, ok := h.highlight(job.path, job.src)
	if !ok {
		return // parse gave up: leave the previous highlight in place
	}
	if h.post == nil {
		return
	}
	h.post(HighlightResult{buf: job.buf, rev: job.rev, lines: lines})
}

// superseded reports whether a newer snapshot of job's buffer is already
// waiting. Reads only the queue, never job.buf, which belongs to the UI
// goroutine.
func (h *Highlighter) superseded(job highlightJob) bool {
	h.mu.Lock()
	defer h.mu.Unlock()
	newer, ok := h.pending[job.buf]
	return ok && newer.rev > job.rev
}

// ApplyHighlightResult stores a finished highlight on its buffer, unless
// the buffer has changed since — the edit that changed it already nil'd
// the lines it touched (see Buffer.spliceHighlight) and submitted a fresh
// snapshot, so a stale result would only paint the wrong colors for a
// moment before being replaced.
//
// Must be called on the UI goroutine; cmd/nib/main.go's event loop is
// where that happens. A result for a buffer no pane holds anymore (closed
// while the parse was running) writes to an object nothing will read and
// is harmless.
func ApplyHighlightResult(r HighlightResult) {
	if r.buf == nil || r.buf.rev != r.rev {
		return
	}
	r.buf.highlighted = r.lines
}
