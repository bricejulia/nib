package statusbar

import (
	"strings"
	"testing"

	"github.com/bricejulia/nib/internal/layout"
	"github.com/bricejulia/nib/internal/textwidth"
)

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

func TestViewRendersTextRightAligned(t *testing.T) {
	v := New()
	v.TextFunc = func() string { return "Ln 1, Col 1" }

	w := newFakeWindow(20, 1)
	v.Render(w)

	if !strings.HasSuffix(w.lines[0], "Ln 1, Col 1") {
		t.Fatalf("expected text right-aligned at the end of the line, got %q", w.lines[0])
	}
	if len(w.lines[0]) != 20 {
		t.Fatalf("expected the line padded to the full width (20), got len=%d: %q", len(w.lines[0]), w.lines[0])
	}
}

func TestViewNilTextFuncRendersEmpty(t *testing.T) {
	v := New()
	w := newFakeWindow(20, 1)
	v.Render(w)
	if w.lines[0] != "" {
		t.Fatalf("expected empty output with no TextFunc, got %q", w.lines[0])
	}
}

func TestViewTruncatesTextWiderThanWindow(t *testing.T) {
	v := New()
	v.TextFunc = func() string { return "this text is way too long to fit" }
	w := newFakeWindow(10, 1)
	v.Render(w) // must not panic
	if len(w.lines[0]) > 10 {
		t.Fatalf("expected truncation to window width, got len=%d", len(w.lines[0]))
	}
}

// TestViewTruncationDoesNotSplitMultiByteGlyphs is a regression test: the
// bar used to do its arithmetic in bytes, so truncating a hint containing
// "·" (2 bytes, 1 column) sliced a UTF-8 sequence in half and the terminal
// drew a replacement character. Multi-byte glyphs are routine here — the
// hint's separators, and the editor's language-server "●" marker.
func TestViewTruncationDoesNotSplitMultiByteGlyphs(t *testing.T) {
	v := New()
	v.Hint = "Tab Switch · Ctrl+P Finder · Ctrl+D Debug · ? Help"
	v.TextFunc = func() string { return "Ln 1, Col 1   go ●   nib dev" }

	// Sweep widths so the truncation point lands inside a multi-byte glyph
	// at some of them.
	for cols := 4; cols <= 60; cols++ {
		w := newFakeWindow(cols, 1)
		v.Render(w)
		if strings.ContainsRune(w.lines[0], '�') {
			t.Fatalf("width %d produced a replacement character: %q", cols, w.lines[0])
		}
		if got := textwidth.DisplayWidth(w.lines[0]); got > cols {
			t.Fatalf("width %d rendered %d display columns: %q", cols, got, w.lines[0])
		}
	}
}

func TestViewKeepsRightTextWhenHintMustBeDropped(t *testing.T) {
	// The right side is the load-bearing one, so it survives a narrow bar.
	v := New()
	v.Hint = "a very long hint that cannot possibly fit"
	v.TextFunc = func() string { return "go ●" }

	w := newFakeWindow(6, 1)
	v.Render(w)

	if !strings.Contains(w.lines[0], "●") {
		t.Errorf("expected the right-hand text preserved, got %q", w.lines[0])
	}
}

func TestViewIsUnfocusableAndConsumesNoKeys(t *testing.T) {
	v := New()
	if !v.Unfocusable() {
		t.Fatal("statusbar.View must report Unfocusable() == true")
	}
	if v.HandleKey(layout.Key{Text: "j"}) {
		t.Fatal("statusbar.View must never consume a key")
	}
}

func TestViewRendersHintLeftAlignedAndTextRightAligned(t *testing.T) {
	v := New()
	v.Hint = "Ctrl+P Finder"
	v.TextFunc = func() string { return "Ln 1, Col 1" }

	w := newFakeWindow(40, 1)
	v.Render(w)

	line := w.lines[0]
	if !strings.HasPrefix(line, "Ctrl+P Finder") {
		t.Fatalf("expected hint left-aligned, got %q", line)
	}
	if !strings.HasSuffix(line, "Ln 1, Col 1") {
		t.Fatalf("expected text right-aligned, got %q", line)
	}
	if len(line) != 40 {
		t.Fatalf("expected the line padded to the full width (40), got len=%d: %q", len(line), line)
	}
}

func TestViewHintAloneRendersEmptyRight(t *testing.T) {
	v := New()
	v.Hint = "Ctrl+P Finder"

	w := newFakeWindow(20, 1)
	v.Render(w)

	if w.lines[0] != "Ctrl+P Finder       " {
		t.Fatalf("got %q", w.lines[0])
	}
}

func TestViewNarrowWindowDropsHintBeforeText(t *testing.T) {
	v := New()
	v.Hint = "Ctrl+P Finder"
	v.TextFunc = func() string { return "Ln 100, Col 42" }

	// Not enough room for both; the right-aligned text must still show
	// in full and the hint must not corrupt it.
	w := newFakeWindow(len("Ln 100, Col 42"), 1)
	v.Render(w)

	if w.lines[0] != "Ln 100, Col 42" {
		t.Fatalf("expected text to win over hint on a narrow window, got %q", w.lines[0])
	}
}
