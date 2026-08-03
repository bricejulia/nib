package editor

import (
	"strings"
	"testing"

	"github.com/bricejulia/kiwi/internal/layout"
	"github.com/bricejulia/kiwi/internal/lsp"
)

// formatFixture builds a Go tab with the two lines: "package main" and
// "func x(){}" (deliberately unformatted).
func formatFixture(t *testing.T, fake *fakeLSP) (*View, *tab) {
	t.Helper()
	lines := []string{"package main", "func x(){}"}
	v := NewView()
	v.lsp = fake
	v.tabs = []*tab{{
		path: "test.go",
		buf:  &Buffer{Path: "test.go", Lines: lines, Source: []byte(strings.Join(lines, "\n"))},
	}}
	v.active = 0
	return v, v.activeTab()
}

func TestApplyTextEditsSingleWholeDocumentEdit(t *testing.T) {
	// The common gofmt case: one edit spanning the whole file.
	fake := &fakeLSP{ready: true, formatOK: true, formatEdits: []lsp.TextEdit{
		{
			Range:   lsp.Range{Start: lsp.Position{Line: 0, Character: 0}, End: lsp.Position{Line: 1, Character: len("func x(){}")}},
			NewText: "package main\n\nfunc x() {}",
		},
	}}
	v, tb := formatFixture(t, fake)

	if !v.HandleKey(layout.Key{Text: "F"}) {
		t.Fatal("expected 'F' to be consumed")
	}
	if !fake.formatDispatched {
		t.Fatal("expected a formatting request when the server is ready")
	}
	fake.deliver(t)

	want := []string{"package main", "", "func x() {}"}
	if len(tb.buf.Lines) != len(want) {
		t.Fatalf("Lines = %v, want %v", tb.buf.Lines, want)
	}
	for i := range want {
		if tb.buf.Lines[i] != want[i] {
			t.Errorf("Lines[%d] = %q, want %q", i, tb.buf.Lines[i], want[i])
		}
	}
}

func TestApplyTextEditsAppliesMultipleEditsInReverseOrderWithoutCorruption(t *testing.T) {
	lines := []string{"aaaa", "bbbb", "cccc"}
	// Two edits, both expressed in the ORIGINAL document's coordinates.
	// Applying the line-0 edit first would not shift line 2's indices (edits
	// are same-line replacements here), but this constructs a case where a
	// naive forward pass corrupts the SECOND edit: the first edit changes
	// line 0's line COUNT (adds a line via an embedded newline), which
	// shifts every subsequent line index by one — so an edit meant for the
	// original line 2 would land on the wrong line if applied forward.
	edits := []lsp.TextEdit{
		{
			Range:   lsp.Range{Start: lsp.Position{Line: 0, Character: 0}, End: lsp.Position{Line: 0, Character: 4}},
			NewText: "AAAA\nEXTRA",
		},
		{
			Range:   lsp.Range{Start: lsp.Position{Line: 2, Character: 0}, End: lsp.Position{Line: 2, Character: 4}},
			NewText: "CCCC",
		},
	}
	fake := &fakeLSP{ready: true, formatOK: true, formatEdits: edits}
	v, tb := formatFixture(t, fake)
	tb.buf.Lines = append([]string(nil), lines...)
	tb.buf.Source = []byte(strings.Join(lines, "\n"))

	v.HandleKey(layout.Key{Text: "F"})
	fake.deliver(t)

	want := []string{"AAAA", "EXTRA", "bbbb", "CCCC"}
	if len(tb.buf.Lines) != len(want) {
		t.Fatalf("Lines = %v, want %v", tb.buf.Lines, want)
	}
	for i := range want {
		if tb.buf.Lines[i] != want[i] {
			t.Errorf("Lines[%d] = %q, want %q", i, tb.buf.Lines[i], want[i])
		}
	}
}

func TestApplyTextEditsIsOneUndoEntry(t *testing.T) {
	fake := &fakeLSP{ready: true, formatOK: true, formatEdits: []lsp.TextEdit{
		{
			Range:   lsp.Range{Start: lsp.Position{Line: 0, Character: 0}, End: lsp.Position{Line: 1, Character: len("func x(){}")}},
			NewText: "package main\n\nfunc x() {}",
		},
	}}
	v, tb := formatFixture(t, fake)
	original := append([]string(nil), tb.buf.Lines...)

	v.HandleKey(layout.Key{Text: "F"})
	fake.deliver(t)
	if linesEqual(tb.buf.Lines, original) {
		t.Fatal("setup: expected formatting to have changed the buffer")
	}

	v.HandleKey(layout.Key{Text: "u"}) // undo

	if !linesEqual(tb.buf.Lines, original) {
		t.Errorf("one undo left Lines = %v, want the original %v", tb.buf.Lines, original)
	}
}

func TestFormatNoopWhenServerNotReady(t *testing.T) {
	fake := &fakeLSP{ready: false}
	v, tb := formatFixture(t, fake)
	original := append([]string(nil), tb.buf.Lines...)

	v.HandleKey(layout.Key{Text: "F"})

	if fake.formatDispatched {
		t.Fatal("expected no formatting request when the server isn't ready")
	}
	if !linesEqual(tb.buf.Lines, original) {
		t.Errorf("Lines changed with no ready server: %v", tb.buf.Lines)
	}
}

func TestFormatStaleResponseIgnoredAfterTabSwitch(t *testing.T) {
	fake := &fakeLSP{ready: true, formatOK: true, formatEdits: []lsp.TextEdit{
		{Range: lsp.Range{End: lsp.Position{Line: 1, Character: len("func x(){}")}}, NewText: "STALE"},
	}}
	v, _ := formatFixture(t, fake)

	v.HandleKey(layout.Key{Text: "F"})
	if !fake.formatDispatched {
		t.Fatal("expected a formatting request dispatched")
	}

	// The user opens another file before the server answers.
	v.Open(fixturePath(t, "highlight_sample.go"))
	otherPath := v.ActivePath()
	fake.deliver(t)

	if v.ActivePath() != otherPath {
		t.Fatalf("active path changed unexpectedly: %q", v.ActivePath())
	}
	nt := v.activeTab()
	if len(nt.buf.Lines) == 1 && nt.buf.Lines[0] == "STALE" {
		t.Error("stale formatting response was applied to the wrong tab")
	}
}
