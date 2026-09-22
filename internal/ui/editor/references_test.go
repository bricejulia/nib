package editor

import (
	"testing"

	"github.com/bricejulia/nib/internal/lsp"
)

func TestFindReferencesDispatchesWhenReady(t *testing.T) {
	fake := &fakeLSP{ready: true, refOK: true, refLocs: []lsp.Location{
		{URI: pathURIForTest("/a.go")},
		{URI: pathURIForTest("/b.go")},
	}}
	v := NewView()
	v.lsp = fake
	v.Open(fixturePath(t, "highlight_sample.go"))
	tb := v.activeTab()
	tb.cursorLn, tb.cursorCol = 3, 6 // inside "greet" on "func greet(...)"

	var gotLocs []lsp.Location
	var gotOK bool
	if !v.FindReferences(func(locs []lsp.Location, ok bool) { gotLocs, gotOK = locs, ok }) {
		t.Fatal("expected FindReferences to dispatch when the server is ready")
	}
	if !fake.refDispatched {
		t.Fatal("expected a references request sent to the server")
	}
	if fake.refLine != 3 {
		t.Errorf("requested line = %d, want 3", fake.refLine)
	}
	fake.deliver(t)

	if !gotOK || len(gotLocs) != 2 {
		t.Errorf("callback got (%+v, %v), want 2 locations and ok", gotLocs, gotOK)
	}
}

func TestFindReferencesReturnsFalseWithoutServer(t *testing.T) {
	fake := &fakeLSP{ready: false}
	v := NewView()
	v.lsp = fake
	v.Open(fixturePath(t, "highlight_sample.go"))
	tb := v.activeTab()
	tb.cursorLn, tb.cursorCol = 3, 6

	called := false
	if v.FindReferences(func([]lsp.Location, bool) { called = true }) {
		t.Fatal("expected FindReferences to report it could not dispatch")
	}
	if called {
		t.Fatal("callback must not run when nothing was dispatched")
	}
}

func TestFindReferencesReturnsFalseWithNoActiveTab(t *testing.T) {
	v := NewView()
	v.lsp = &fakeLSP{ready: true}

	if v.FindReferences(func([]lsp.Location, bool) {}) {
		t.Fatal("expected FindReferences to report it could not dispatch with no active tab")
	}
}
