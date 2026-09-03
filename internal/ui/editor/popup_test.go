package editor

import (
	"strings"
	"testing"

	"github.com/bricejulia/nib/internal/layout"
	"github.com/bricejulia/nib/internal/lsp"
)

func TestWrapTextBreaksOnWhitespace(t *testing.T) {
	got := wrapText("the quick brown fox jumps", 11)
	want := []string{"the quick", "brown fox", "jumps"}
	if len(got) != len(want) {
		t.Fatalf("got %q, want %q", got, want)
	}
	for i := range want {
		if got[i] != want[i] {
			t.Errorf("line %d = %q, want %q", i, got[i], want[i])
		}
	}
}

func TestWrapTextHardSplitsAnOverlongWord(t *testing.T) {
	// Losing the tail of a long identifier would be worse than an ugly break.
	got := wrapText("aVeryLongIdentifierName", 8)
	if len(got) < 2 {
		t.Fatalf("expected the word split across rows, got %q", got)
	}
	if joined := strings.Join(got, ""); joined != "aVeryLongIdentifierName" {
		t.Errorf("rejoined = %q, want the whole word preserved", joined)
	}
	for _, l := range got {
		if len(l) > 8 {
			t.Errorf("line %q exceeds width 8", l)
		}
	}
}

func TestWrapTextEmptyAndZeroWidth(t *testing.T) {
	if got := wrapText("", 10); got != nil {
		t.Errorf("empty input gave %q, want nil", got)
	}
	if got := wrapText("something", 0); got != nil {
		t.Errorf("zero width gave %q, want nil", got)
	}
}

func TestClampToWidthDoesNotSplitWideRunes(t *testing.T) {
	// Each CJK rune is 2 display columns wide; clamping to 3 must keep only
	// one rather than emit half a glyph.
	got := clampToWidth("日本語", 3)
	if got != "日" {
		t.Errorf("clampToWidth = %q, want %q", got, "日")
	}
}

func TestDiagnosticPopupLinesIncludesSeverityAndSource(t *testing.T) {
	diags := []lsp.Diagnostic{
		{Severity: lsp.SeverityError, Message: "undefined: foo", Source: "compiler"},
	}
	got := diagnosticPopupLines(diags, 60)
	if len(got) == 0 {
		t.Fatal("expected popup content")
	}
	joined := strings.Join(got, " ")
	if !strings.Contains(joined, "undefined: foo") {
		t.Errorf("missing the message: %q", joined)
	}
	if !strings.Contains(joined, diagnosticMarker(lsp.SeverityError)) {
		t.Errorf("missing the severity marker: %q", joined)
	}
	if !strings.Contains(joined, "compiler") {
		t.Errorf("missing the source: %q", joined)
	}
}

func TestDiagnosticPopupLinesEmptyForCleanLine(t *testing.T) {
	if got := diagnosticPopupLines(nil, 60); got != nil {
		t.Errorf("got %q, want nil for a line with no diagnostics", got)
	}
}

func TestDiagnosticPopupLinesCapsRows(t *testing.T) {
	var diags []lsp.Diagnostic
	for i := 0; i < 40; i++ {
		diags = append(diags, lsp.Diagnostic{Severity: lsp.SeverityError, Message: "a problem"})
	}
	if got := diagnosticPopupLines(diags, 60); len(got) > maxDiagnosticPopupRows {
		t.Errorf("got %d rows, want at most %d", len(got), maxDiagnosticPopupRows)
	}
}

// diagPopupView builds a pane whose line 0 carries one diagnostic.
func diagPopupView(t *testing.T) *View {
	t.Helper()
	v := NewView()
	v.tabs = []*tab{{path: "t.txt", buf: &Buffer{Path: "t.txt", Lines: []string{"broken line", "fine line"}}}}
	v.active = 0
	v.activeTab().diagnostics = map[int][]lsp.Diagnostic{
		0: {{Severity: lsp.SeverityError, Message: "something is quite wrong here", Source: "syntax"}},
	}
	return v
}

func TestKShowsDiagnosticDetailsPopup(t *testing.T) {
	v := diagPopupView(t)

	if !v.HandleKey(layout.Key{Text: "K"}) {
		t.Fatal("expected 'K' to be consumed")
	}
	if !v.showDiagnostics {
		t.Fatal("expected the popup to open on a flagged line")
	}

	w := newFakeWindow(60, 10)
	v.Render(w)

	joined := strings.Join(w.lines, "\n")
	if !strings.Contains(joined, "something is quite wrong") {
		t.Errorf("expected the message rendered, got:\n%s", joined)
	}
}

func TestKOnCleanLineShowsNothing(t *testing.T) {
	v := diagPopupView(t)
	v.activeTab().cursorLn = 1 // the line with no diagnostics

	v.HandleKey(layout.Key{Text: "K"})

	if v.showDiagnostics {
		t.Error("expected no popup on a line with no diagnostics")
	}
}

func TestDiagnosticPopupDismissedByNextKeypress(t *testing.T) {
	v := diagPopupView(t)
	v.HandleKey(layout.Key{Text: "K"})
	if !v.showDiagnostics {
		t.Fatal("setup: expected the popup open")
	}

	// It's a tooltip, not a mode: the next key dismisses it AND does its
	// normal job.
	v.HandleKey(layout.Key{Text: "j"})

	if v.showDiagnostics {
		t.Error("expected the popup dismissed by the next keypress")
	}
	if v.activeTab().cursorLn != 1 {
		t.Errorf("cursorLn = %d, want 1 — the dismissing key should still move", v.activeTab().cursorLn)
	}
}

func TestRenderPopupClampsToRemainingRows(t *testing.T) {
	// Anchored near the bottom edge: fewer rows drawn, and no panic.
	w := newFakeWindow(40, 6)
	renderPopup(w, 40, 6, 2, 4, []string{"one", "two", "three", "four"}, -1)

	// Only row 5 is available below anchorRow=4 in a 6-row window.
	if strings.TrimSpace(w.lines[5]) != "one" {
		t.Errorf("row 5 = %q, want the first popup line", w.lines[5])
	}
}

func TestRenderPopupNoRoomBelowFlipsUpward(t *testing.T) {
	w := newFakeWindow(40, 5)
	renderPopup(w, 40, 5, 0, 4, []string{"content"}, -1) // anchored on the last row

	if !strings.Contains(w.lines[3], "content") {
		t.Errorf("row 3 = %q, want the popup flipped to draw just above the anchor row", w.lines[3])
	}
	if strings.Contains(w.lines[4], "content") {
		t.Errorf("row 4 (the anchor row) unexpectedly drew popup content: %q", w.lines[4])
	}
}

func TestRenderPopupNoRoomEitherDirectionDrawsNothing(t *testing.T) {
	// A single-row window: anchored on row 0, nothing above (row 0 is
	// always the tab bar) and nothing below either.
	w := newFakeWindow(40, 1)
	renderPopup(w, 40, 1, 0, 0, []string{"content"}, -1)

	for i, l := range w.lines {
		if strings.Contains(l, "content") {
			t.Fatalf("row %d unexpectedly drew popup content: %q", i, l)
		}
	}
}

func TestRenderPopupFlipsUpwardWhenBelowInsufficientButAboveFits(t *testing.T) {
	w := newFakeWindow(40, 10)
	renderPopup(w, 40, 10, 0, 8, []string{"one", "two", "three"}, -1) // below=1, above=7

	for i, want := range []string{"one", "two", "three"} {
		row := 5 + i // anchorRow(8) - total(3) + i
		if !strings.Contains(w.lines[row], want) {
			t.Errorf("row %d = %q, want to contain %q", row, w.lines[row], want)
		}
	}
	if strings.Contains(w.lines[8], "one") || strings.Contains(w.lines[8], "two") || strings.Contains(w.lines[8], "three") {
		t.Errorf("anchor row 8 unexpectedly drew popup content: %q", w.lines[8])
	}
}

func TestRenderPopupPrefersBelowWhenBothFit(t *testing.T) {
	w := newFakeWindow(40, 20)
	renderPopup(w, 40, 20, 0, 10, []string{"one", "two", "three"}, -1) // below=9, above=9: both fit

	for i, want := range []string{"one", "two", "three"} {
		row := 11 + i
		if !strings.Contains(w.lines[row], want) {
			t.Errorf("row %d = %q, want to contain %q (still prefers drawing below)", row, w.lines[row], want)
		}
	}
}

func TestRenderPopupPadsFullWidthToCoverContentBeneath(t *testing.T) {
	// The padding matters: vaxis only writes the cells a segment covers, so
	// a short row would let file content show through the popup.
	w := newFakeWindow(30, 10)
	renderPopup(w, 30, 10, 3, 1, []string{"hi"}, -1)

	if got := len(w.lines[2]); got != 30 {
		t.Errorf("popup row width = %d, want the full window width 30 (%q)", got, w.lines[2])
	}
}
