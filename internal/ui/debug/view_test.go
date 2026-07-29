package debug

import (
	"fmt"
	"strings"
	"testing"
	"time"

	"github.com/bricejulia/kiwi/internal/debuglog"
	"github.com/bricejulia/kiwi/internal/layout"
)

// fakeWindow is an in-memory layout.Window double so View.Render is
// testable without a live terminal.
type fakeWindow struct {
	cols, rows int
	lines      []string
}

func newFakeWindow(cols, rows int) *fakeWindow {
	return &fakeWindow{cols: cols, rows: rows, lines: make([]string, rows)}
}

func (w *fakeWindow) Size() (int, int) { return w.cols, w.rows }
func (w *fakeWindow) Println(row int, segs ...layout.Segment) {
	if row < 0 || row >= len(w.lines) {
		return
	}
	text := ""
	for _, s := range segs {
		text += s.Text
	}
	w.lines[row] = text
}
func (w *fakeWindow) Clear() {
	for i := range w.lines {
		w.lines[i] = ""
	}
}

// entriesAt builds n synthetic Info-level entries named "msg 0".."msg
// n-1", oldest first, so tests don't depend on debuglog's package-level
// state.
func entriesAt(n int) func() []debuglog.Entry {
	es := make([]debuglog.Entry, n)
	for i := range es {
		es[i] = debuglog.Entry{Time: time.Time{}, Level: debuglog.LevelInfo, Text: fmt.Sprintf("msg %d", i)}
	}
	return func() []debuglog.Entry { return es }
}

// mixedLevelEntries builds one entry per level, oldest (Debug) first.
func mixedLevelEntries() func() []debuglog.Entry {
	es := []debuglog.Entry{
		{Level: debuglog.LevelDebug, Text: "debug msg"},
		{Level: debuglog.LevelInfo, Text: "info msg"},
		{Level: debuglog.LevelWarn, Text: "warn msg"},
		{Level: debuglog.LevelError, Text: "error msg"},
	}
	return func() []debuglog.Entry { return es }
}

func TestRenderShowsEmptyState(t *testing.T) {
	v := New()
	v.EntriesFunc = entriesAt(0)
	w := newFakeWindow(40, 5)
	v.Render(w)

	if !strings.Contains(w.lines[0], "no log messages") {
		t.Errorf("expected an empty-state message, got %q", w.lines[0])
	}
}

func TestRenderShowsNewestEntriesByDefault(t *testing.T) {
	v := New()
	v.EntriesFunc = entriesAt(10)
	w := newFakeWindow(40, 3)
	v.Render(w)

	joined := strings.Join(w.lines, "\n")
	for _, want := range []string{"msg 7", "msg 8", "msg 9"} {
		if !strings.Contains(joined, want) {
			t.Errorf("expected newest entries to be visible, missing %q:\n%s", want, joined)
		}
	}
	if strings.Contains(joined, "msg 6") {
		t.Errorf("expected only the last 3 entries to fit, but found msg 6:\n%s", joined)
	}
}

func TestUpScrollsToOlderEntries(t *testing.T) {
	v := New()
	v.EntriesFunc = entriesAt(10)
	w := newFakeWindow(40, 3)
	v.Render(w)

	v.HandleKey(layout.Key{Named: layout.KeyUp})
	v.HandleKey(layout.Key{Named: layout.KeyUp})
	v.HandleKey(layout.Key{Named: layout.KeyUp})
	v.Render(w)

	joined := strings.Join(w.lines, "\n")
	if !strings.Contains(joined, "msg 6") {
		t.Errorf("expected scrolling up to reveal msg 6, got:\n%s", joined)
	}
	if strings.Contains(joined, "msg 9") {
		t.Errorf("expected scrolling up to move away from the newest entry, got:\n%s", joined)
	}
}

func TestDownScrollsBackToNewest(t *testing.T) {
	v := New()
	v.EntriesFunc = entriesAt(10)
	w := newFakeWindow(40, 3)
	v.Render(w)

	for i := 0; i < 5; i++ {
		v.HandleKey(layout.Key{Named: layout.KeyUp})
	}
	for i := 0; i < 5; i++ {
		v.HandleKey(layout.Key{Named: layout.KeyDown})
	}
	v.Render(w)

	joined := strings.Join(w.lines, "\n")
	if !strings.Contains(joined, "msg 9") {
		t.Errorf("expected scrolling back down to reach the newest entry, got:\n%s", joined)
	}
}

func TestEscClosesTheOverlay(t *testing.T) {
	v := New()
	v.EntriesFunc = entriesAt(1)
	closed := false
	v.OnClose = func() { closed = true }

	v.HandleKey(layout.Key{Named: layout.KeyEsc})

	if !closed {
		t.Error("expected Esc to call OnClose")
	}
}

func TestHandleKeyAlwaysConsumesTheKey(t *testing.T) {
	v := New()
	v.EntriesFunc = entriesAt(1)

	if !v.HandleKey(layout.Key{Text: "x"}) {
		t.Error("expected an unrecognized key to still be reported as consumed (modal input)")
	}
}

func TestRenderShowsAllLevelsByDefault(t *testing.T) {
	v := New()
	v.EntriesFunc = mixedLevelEntries()
	w := newFakeWindow(40, 10)
	v.Render(w)

	joined := strings.Join(w.lines, "\n")
	for _, want := range []string{"DEBUG", "debug msg", "INFO", "info msg", "WARN", "warn msg", "ERROR", "error msg"} {
		if !strings.Contains(joined, want) {
			t.Errorf("expected default (unfiltered) view to include %q:\n%s", want, joined)
		}
	}
}

func TestTabCyclesMinLevelFilter(t *testing.T) {
	v := New()
	v.EntriesFunc = mixedLevelEntries()
	w := newFakeWindow(40, 10)

	v.HandleKey(layout.Key{Named: layout.KeyTab}) // Debug -> Info
	v.Render(w)
	joined := strings.Join(w.lines, "\n")
	if strings.Contains(joined, "debug msg") {
		t.Errorf("expected Debug entries to be filtered out at the Info level:\n%s", joined)
	}
	if !strings.Contains(joined, "info msg") || !strings.Contains(joined, "warn msg") || !strings.Contains(joined, "error msg") {
		t.Errorf("expected Info and above to still be visible:\n%s", joined)
	}
	if !strings.Contains(v.Title(), "INFO") {
		t.Errorf("expected Title to reflect the active filter, got %q", v.Title())
	}

	v.HandleKey(layout.Key{Named: layout.KeyTab}) // Info -> Warn
	v.HandleKey(layout.Key{Named: layout.KeyTab}) // Warn -> Error
	v.Render(w)
	joined = strings.Join(w.lines, "\n")
	if strings.Contains(joined, "warn msg") {
		t.Errorf("expected only Error entries to be visible at the Error level:\n%s", joined)
	}
	if !strings.Contains(joined, "error msg") {
		t.Errorf("expected the Error entry to remain visible:\n%s", joined)
	}

	v.HandleKey(layout.Key{Named: layout.KeyTab}) // Error -> wraps back to Debug
	if v.Title() != "Debug Log" {
		t.Errorf("expected Tab to wrap back to showing everything, got title %q", v.Title())
	}
}
