// Package memwatch periodically samples nib's own memory use and reports
// when it crosses a threshold, so a caller can offer to do something about
// it (e.g. cmd/nib/main.go's prompt to close the largest open file) instead
// of the app just silently getting slower or eventually running out of
// memory on a pathologically large working set.
package memwatch

import (
	"fmt"
	"runtime"
	"sync"
	"sync/atomic"
	"time"
)

// Watcher periodically samples nib's own Go heap size and calls
// OnThreshold once per "episode" — the transition from below Threshold to
// at-or-above it — rather than on every tick while it stays high, so a
// caller (e.g. a prompt offering to close the largest open file) isn't
// re-triggered every Interval while the user is still deciding what to do
// about the last warning. A fresh episode (a sample drops back below
// Threshold, then rises above it again) re-arms and can fire again.
//
// Uses runtime.MemStats.HeapAlloc rather than OS-level RSS/CPU%: it's pure
// stdlib (no new dependency, no platform-specific code), and Go heap size
// is exactly what a large buffer, its undo history, or its highlight
// cache would show up as.
type Watcher struct {
	Threshold   uint64 // bytes; a sample at or above this is "over"
	Interval    time.Duration
	OnThreshold func(heapBytes uint64)

	// OnSample, if set, is called with every sample, before the threshold
	// logic — what lets a caller (cmd/nib/main.go's status bar) show the
	// current figure, not only react to crossings. Runs on the watcher's
	// goroutine, so like OnThreshold it must only post to the UI.
	OnSample func(heapBytes uint64)

	last atomic.Uint64 // the latest sample, for Last

	// sample reads the current heap size. A field, not a direct call, so
	// tests can substitute a fake and drive Check deterministically,
	// without waiting on a real Interval or actually growing the heap.
	sample func() uint64

	mu    sync.Mutex
	armed bool // false once OnThreshold has fired for the current episode

	quit chan struct{}
	done chan struct{}
}

// New returns a Watcher that isn't running yet — call Start. onThreshold
// may be nil (Check still updates the armed/re-armed state, it just has
// no one to tell).
func New(threshold uint64, interval time.Duration, onThreshold func(heapBytes uint64)) *Watcher {
	return &Watcher{
		Threshold:   threshold,
		Interval:    interval,
		OnThreshold: onThreshold,
		sample:      defaultSample,
		armed:       true,
		quit:        make(chan struct{}),
		done:        make(chan struct{}),
	}
}

func defaultSample() uint64 {
	var m runtime.MemStats
	runtime.ReadMemStats(&m)
	return m.HeapAlloc
}

// Start takes a first sample right away, so Last isn't 0 until the first
// Interval elapses, then runs the periodic check on its own goroutine.
// Call Close to stop it; Start must not be called more than once per
// Watcher.
func (w *Watcher) Start() {
	w.Check()
	go w.run()
}

// Last returns the most recent sample, or 0 before the first one. Safe to
// call from any goroutine.
func (w *Watcher) Last() uint64 { return w.last.Load() }

// Format renders a byte count compactly for the status bar: whole MiB
// below 1GiB ("84M"), GiB with one decimal from there ("1.2G").
func Format(bytes uint64) string {
	if bytes < 1<<30 {
		return fmt.Sprintf("%dM", bytes>>20)
	}
	return fmt.Sprintf("%.1fG", float64(bytes)/(1<<30))
}

// Close stops the watcher and waits for its goroutine to exit. Idempotent,
// like Highlighter.Close, which this mirrors.
func (w *Watcher) Close() {
	select {
	case <-w.quit:
		return
	default:
	}
	close(w.quit)
	<-w.done
}

func (w *Watcher) run() {
	defer close(w.done)
	ticker := time.NewTicker(w.Interval)
	defer ticker.Stop()
	for {
		select {
		case <-w.quit:
			return
		case <-ticker.C:
			w.Check()
		}
	}
}

// Check runs one sample-and-compare cycle immediately, outside the
// ticker. This is what the background loop calls on every tick, and what
// tests call directly to exercise the threshold/episode logic without
// waiting on a real Interval.
func (w *Watcher) Check() {
	heap := w.sample()
	w.last.Store(heap)
	if w.OnSample != nil {
		w.OnSample(heap)
	}

	w.mu.Lock()
	defer w.mu.Unlock()

	if heap < w.Threshold {
		w.armed = true // dropped back below: a later rise is a fresh episode
		return
	}
	if !w.armed {
		return // already warned for this episode; stay quiet until it re-arms
	}
	w.armed = false
	if w.OnThreshold != nil {
		w.OnThreshold(heap)
	}
}
