package finder

import (
	"sync"
	"testing"
	"time"

	"github.com/bricejulia/kiwi/internal/layout"
)

// withFastDebounce shrinks contentSearchDebounce for the duration of a
// test so it doesn't have to wait a real 150ms per case.
func withFastDebounce(t *testing.T) {
	t.Helper()
	orig := contentSearchDebounce
	contentSearchDebounce = 10 * time.Millisecond
	t.Cleanup(func() { contentSearchDebounce = orig })
}

// postCollector is a goroutine-safe sink for View.Post, standing in for
// ui.App.Post in tests.
type postCollector struct {
	mu     sync.Mutex
	events []interface{}
}

func (c *postCollector) post(ev interface{}) {
	c.mu.Lock()
	defer c.mu.Unlock()
	c.events = append(c.events, ev)
}

func (c *postCollector) count() int {
	c.mu.Lock()
	defer c.mu.Unlock()
	return len(c.events)
}

func (c *postCollector) last() (SearchResult, bool) {
	c.mu.Lock()
	defer c.mu.Unlock()
	if len(c.events) == 0 {
		return SearchResult{}, false
	}
	r, ok := c.events[len(c.events)-1].(SearchResult)
	return r, ok
}

func waitUntil(t *testing.T, timeout time.Duration, cond func() bool) {
	t.Helper()
	deadline := time.Now().Add(timeout)
	for !cond() {
		if time.Now().After(deadline) {
			t.Fatalf("condition not met within %v", timeout)
		}
		time.Sleep(2 * time.Millisecond)
	}
}

func TestRefilterContentDoesNotBlockWhenPostIsSet(t *testing.T) {
	withFastDebounce(t)
	dir := newContentSearchRepo(t)
	v := New(dir)
	c := &postCollector{}
	v.Post = c.post
	v.mode = modeContent

	// 2 chars: at minContentQueryLen, so this actually starts a search
	// (a 1-char query stays in the "type more" state and never reaches
	// the async path at all).
	v.HandleKey(layout.Key{Text: "n"})
	v.HandleKey(layout.Key{Text: "e"})
	// HandleKey must return immediately (well under the debounce delay,
	// let alone however long git grep takes) — proving refilterContent
	// did not run the search inline on this goroutine.
	if v.searching != true {
		t.Fatal("expected searching=true immediately after a query change")
	}
	if v.contentMatches != nil {
		t.Fatal("expected contentMatches to be cleared immediately, not populated synchronously")
	}
}

func TestContentSearchDeliversResultViaPost(t *testing.T) {
	withFastDebounce(t)
	dir := newContentSearchRepo(t)
	v := New(dir)
	c := &postCollector{}
	v.Post = c.post
	v.mode = modeContent

	for _, r := range "needle" {
		v.HandleKey(layout.Key{Text: string(r)})
	}

	waitUntil(t, time.Second, func() bool { return c.count() > 0 })

	result, ok := c.last()
	if !ok {
		t.Fatal("expected a SearchResult to be posted")
	}
	v.ApplyContentResult(result)

	if v.searching {
		t.Error("expected searching=false after the result is applied")
	}
	if len(v.contentMatches) != 1 || v.contentMatches[0].path != "haystack.go" {
		t.Errorf("expected haystack.go in contentMatches, got %+v", v.contentMatches)
	}
}

func TestContentSearchDebounceCollapsesRapidKeystrokesIntoOneSearch(t *testing.T) {
	withFastDebounce(t)
	dir := newContentSearchRepo(t)
	v := New(dir)
	c := &postCollector{}
	v.Post = c.post
	v.mode = modeContent

	// Type all 6 characters faster than the (already-shrunk) debounce
	// window, as a real fast typist would relative to a 150ms debounce.
	for _, r := range "needle" {
		v.HandleKey(layout.Key{Text: string(r)})
	}

	// Give the debounce window plus a margin to fire exactly once.
	time.Sleep(contentSearchDebounce*3 + 20*time.Millisecond)

	if got := c.count(); got != 1 {
		t.Errorf("expected exactly 1 posted search result for 6 rapid keystrokes, got %d", got)
	}
}

func TestContentSearchStaleResultIsDiscarded(t *testing.T) {
	withFastDebounce(t)
	dir := newContentSearchRepo(t)
	v := New(dir)
	c := &postCollector{}
	v.Post = c.post
	v.mode = modeContent

	v.HandleKey(layout.Key{Text: "n"})
	staleGen := v.searchGen

	// Supersede it before the debounce fires.
	v.HandleKey(layout.Key{Text: "e"})

	// Manually deliver a result carrying the OLD (now-stale) generation,
	// as if a slow first search finally finished after being superseded.
	v.ApplyContentResult(SearchResult{gen: staleGen, matches: []contentMatch{{path: "should-not-apply.go", line: 1}}})

	if len(v.contentMatches) != 0 {
		t.Errorf("expected the stale result to be discarded, got %+v", v.contentMatches)
	}
}

func TestCancelPendingSearchStopsDebounceTimer(t *testing.T) {
	withFastDebounce(t)
	dir := newContentSearchRepo(t)
	v := New(dir)
	c := &postCollector{}
	v.Post = c.post
	v.mode = modeContent

	// 2 chars: at minContentQueryLen, so a real debounce timer actually
	// gets armed (a 1-char query never reaches that point, which would
	// make this test pass for the wrong reason — no timer to cancel).
	v.HandleKey(layout.Key{Text: "n"})
	v.HandleKey(layout.Key{Text: "e"})
	if v.debounceTimer == nil {
		t.Fatal("expected a debounce timer to be armed before testing that it gets canceled")
	}
	v.Open() // closes/reopens the finder before the debounce fires

	time.Sleep(contentSearchDebounce * 3)

	if got := c.count(); got != 0 {
		t.Errorf("expected the canceled search to never post a result, got %d posts", got)
	}
}

func TestSearchingIndicatorShowsWhilePending(t *testing.T) {
	withFastDebounce(t)
	dir := newContentSearchRepo(t)
	v := New(dir)
	c := &postCollector{}
	v.Post = c.post
	v.mode = modeContent

	for _, r := range "needle" {
		v.HandleKey(layout.Key{Text: string(r)})
	}

	w := newFakeWindow(60, 10)
	v.Render(w)
	if !containsSearching(w.lines) {
		t.Errorf("expected a \"searching…\" indicator while the result is pending, got %v", w.lines)
	}

	waitUntil(t, time.Second, func() bool { return c.count() > 0 })
	result, _ := c.last()
	v.ApplyContentResult(result)

	v.Render(w)
	if containsSearching(w.lines) {
		t.Error("expected the \"searching…\" indicator to disappear once results arrive")
	}
}

func containsSearching(lines []string) bool {
	for _, l := range lines {
		if l == "searching…" {
			return true
		}
	}
	return false
}
