package memwatch

import (
	"testing"
	"time"
)

// newFakeWatcher returns a Watcher whose sample is controlled by the
// returned setter, so Check's threshold/episode logic can be exercised
// deterministically without waiting on a real Interval or actually
// growing the heap.
func newFakeWatcher(threshold uint64, onThreshold func(uint64)) (*Watcher, func(uint64)) {
	w := New(threshold, time.Hour /* never actually ticks in these tests */, onThreshold)
	var current uint64
	w.sample = func() uint64 { return current }
	return w, func(v uint64) { current = v }
}

func TestCheckFiresOnceWhenCrossingTheThreshold(t *testing.T) {
	var got []uint64
	w, setHeap := newFakeWatcher(100, func(heap uint64) { got = append(got, heap) })

	setHeap(50)
	w.Check() // below threshold: no fire

	setHeap(150)
	w.Check() // crosses: fires

	if len(got) != 1 || got[0] != 150 {
		t.Fatalf("got %v, want a single fire with heap=150", got)
	}
}

func TestCheckDoesNotRefireWhileStayingAboveTheThreshold(t *testing.T) {
	fires := 0
	w, setHeap := newFakeWatcher(100, func(uint64) { fires++ })

	setHeap(150)
	w.Check()
	w.Check()
	w.Check()

	if fires != 1 {
		t.Fatalf("fires = %d, want exactly 1 despite three checks above threshold", fires)
	}
}

func TestCheckRefiresAfterDroppingBelowAndCrossingAgain(t *testing.T) {
	fires := 0
	w, setHeap := newFakeWatcher(100, func(uint64) { fires++ })

	setHeap(150)
	w.Check() // episode 1: fires
	setHeap(50)
	w.Check() // drops below: re-arms
	setHeap(150)
	w.Check() // episode 2: fires again

	if fires != 2 {
		t.Fatalf("fires = %d, want 2 (one per episode)", fires)
	}
}

func TestCheckNeverFiresBelowTheThreshold(t *testing.T) {
	fires := 0
	w, setHeap := newFakeWatcher(100, func(uint64) { fires++ })

	setHeap(0)
	w.Check()
	setHeap(99)
	w.Check()

	if fires != 0 {
		t.Fatalf("fires = %d, want 0 below threshold", fires)
	}
}

func TestCheckAtExactlyTheThresholdFires(t *testing.T) {
	fires := 0
	w, setHeap := newFakeWatcher(100, func(uint64) { fires++ })

	setHeap(100)
	w.Check()

	if fires != 1 {
		t.Fatalf("fires = %d, want 1 at exactly the threshold (documented as \"at or above\")", fires)
	}
}

func TestCheckToleratesANilOnThreshold(t *testing.T) {
	w, setHeap := newFakeWatcher(100, nil)
	setHeap(150)
	w.Check() // must not panic
}

// Start/Close smoke test: confirms the background goroutine actually
// ticks and can be stopped cleanly, using a real (short) Interval — the
// threshold/episode logic itself is covered above via Check directly.
func TestStartRunsPeriodicallyAndCloseStopsIt(t *testing.T) {
	fired := make(chan uint64, 1)
	w := New(100, 10*time.Millisecond, func(heap uint64) {
		select {
		case fired <- heap:
		default:
		}
	})
	w.sample = func() uint64 { return 150 }
	w.Start()
	defer w.Close()

	select {
	case heap := <-fired:
		if heap != 150 {
			t.Errorf("fired with heap=%d, want 150", heap)
		}
	case <-time.After(2 * time.Second):
		t.Fatal("Start did not fire OnThreshold within 2s of a 10ms Interval")
	}
}

func TestCloseIsIdempotent(t *testing.T) {
	w := New(100, time.Hour, nil)
	w.Start()
	w.Close()
	w.Close()
}
