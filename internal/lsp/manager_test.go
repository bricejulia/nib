package lsp

import (
	"errors"
	"testing"
)

// fakeClient records what the Manager sent it, so the Manager's own logic
// (refcounting, versioning, dispatch) is testable without spawning a real
// language server.
type fakeClient struct {
	opened    []string
	changed   []string
	closed    []string
	versions  []int
	defLoc    Location
	defFound  bool
	defErr    error
	defCalls  int
	closeCall int

	completionItems []CompletionItem
	completionErr   error
	completionCalls int
}

func (f *fakeClient) didOpen(path, _, _ string, version int) error {
	f.opened = append(f.opened, path)
	f.versions = append(f.versions, version)
	return nil
}

func (f *fakeClient) didChange(path, _ string, version int) error {
	f.changed = append(f.changed, path)
	f.versions = append(f.versions, version)
	return nil
}

func (f *fakeClient) didClose(path string) error {
	f.closed = append(f.closed, path)
	return nil
}

func (f *fakeClient) definition(string, int, int) (Location, bool, error) {
	f.defCalls++
	return f.defLoc, f.defFound, f.defErr
}

func (f *fakeClient) completion(string, int, int) ([]CompletionItem, error) {
	f.completionCalls++
	return f.completionItems, f.completionErr
}

func (f *fakeClient) Close() error {
	f.closeCall++
	return nil
}

// newTestManager returns a Manager with fake pre-installed as the "already
// spawned" client for language, bypassing subprocess spawning entirely.
func newTestManager(language string, fake serverClient) *Manager {
	m := NewManager("/project")
	m.clients[language] = fake
	return m
}

func TestOpenSendsDidOpenOnce(t *testing.T) {
	fake := &fakeClient{}
	m := newTestManager("go", fake)

	m.Open("/project/a.go", "go", "package main")

	if len(fake.opened) != 1 || fake.opened[0] != "/project/a.go" {
		t.Fatalf("opened = %v, want one entry for a.go", fake.opened)
	}
}

func TestOpenTwiceRefcountsWithoutResendingDidOpen(t *testing.T) {
	fake := &fakeClient{}
	m := newTestManager("go", fake)

	m.Open("/project/a.go", "go", "package main") // e.g. one split pane
	m.Open("/project/a.go", "go", "package main") // the same file in a second pane

	if len(fake.opened) != 1 {
		t.Fatalf("opened %d times, want 1 (a file open in two panes is ONE document to the server)", len(fake.opened))
	}
}

func TestCloseOnlySendsDidCloseWhenLastReferenceGoes(t *testing.T) {
	fake := &fakeClient{}
	m := newTestManager("go", fake)
	m.Open("/project/a.go", "go", "x")
	m.Open("/project/a.go", "go", "x")

	m.Close("/project/a.go")
	if len(fake.closed) != 0 {
		t.Fatalf("closed too early: %v (one pane still has it open)", fake.closed)
	}

	m.Close("/project/a.go")
	if len(fake.closed) != 1 || fake.closed[0] != "/project/a.go" {
		t.Fatalf("closed = %v, want one entry for a.go", fake.closed)
	}
}

func TestChangeBumpsVersionEachTime(t *testing.T) {
	fake := &fakeClient{}
	m := newTestManager("go", fake)
	m.Open("/project/a.go", "go", "v1")

	m.Change("/project/a.go", "v2")
	m.Change("/project/a.go", "v3")

	if len(fake.changed) != 2 {
		t.Fatalf("changed = %v, want 2 entries", fake.changed)
	}
	// versions records didOpen's version then each didChange's; LSP
	// requires this to strictly increase.
	for i := 1; i < len(fake.versions); i++ {
		if fake.versions[i] <= fake.versions[i-1] {
			t.Fatalf("versions must strictly increase, got %v", fake.versions)
		}
	}
}

func TestChangeOnUnopenedPathIsNoop(t *testing.T) {
	fake := &fakeClient{}
	m := newTestManager("go", fake)

	m.Change("/project/never-opened.go", "x")

	if len(fake.changed) != 0 {
		t.Fatalf("changed = %v, want none", fake.changed)
	}
}

func TestCloseOnUnopenedPathIsNoop(t *testing.T) {
	fake := &fakeClient{}
	m := newTestManager("go", fake)

	m.Close("/project/never-opened.go") // must not panic

	if len(fake.closed) != 0 {
		t.Fatalf("closed = %v, want none", fake.closed)
	}
}

func TestOpenWithNoServerForLanguageIsNoop(t *testing.T) {
	m := NewManager("/project")
	m.SetServers(map[string][]string{}) // no server for anything

	m.Open("/project/a.php", "php", "<?php")

	if m.Ready("php") {
		t.Fatal("expected php not to be Ready with no server configured")
	}
	// The attempt must be remembered, so a missing/unconfigured server isn't
	// retried on every single file open.
	if !m.tried["php"] {
		t.Fatal("expected the attempt for php to be remembered")
	}
	if m.Status("php") != StatusNone {
		t.Errorf("Status = %v, want StatusNone for an unconfigured language", m.Status("php"))
	}
}

func TestStatusDistinguishesUnconfiguredFromNotRunning(t *testing.T) {
	// The whole point of ServerStatus: "kiwi has no server for this
	// language" and "the configured server isn't running" look identical
	// internally (no client), but mean very different things to a user
	// wondering why they get no diagnostics.
	m := NewManager("/project")
	m.SetServers(map[string][]string{"go": {"gopls"}})

	if got := m.Status("go"); got != StatusNotRunning {
		t.Errorf("Status(go) = %v, want StatusNotRunning (configured, not started yet)", got)
	}
	if got := m.Status("php"); got != StatusNone {
		t.Errorf("Status(php) = %v, want StatusNone (no server configured)", got)
	}
	if got := m.Status(""); got != StatusNone {
		t.Errorf("Status(\"\") = %v, want StatusNone (unrecognized language)", got)
	}

	m.clients["go"] = &fakeClient{}
	if got := m.Status("go"); got != StatusRunning {
		t.Errorf("Status(go) = %v, want StatusRunning once a client exists", got)
	}
}

func TestStatusDoesNotSpawnAnything(t *testing.T) {
	m := NewManager("/project")
	m.SetServers(map[string][]string{"go": {"definitely-not-a-real-binary"}})

	_ = m.Status("go")

	if len(m.clients) != 0 || len(m.tried) != 0 {
		t.Fatalf("Status must not attempt a spawn: clients=%v tried=%v", m.clients, m.tried)
	}
}

func TestReadyReflectsRunningClientWithoutSpawning(t *testing.T) {
	m := NewManager("/project")
	// Ready must not spawn anything as a side effect of being asked.
	if m.Ready("go") {
		t.Fatal("expected not Ready before any file is opened")
	}
	if len(m.clients) != 0 {
		t.Fatalf("Ready spawned/recorded a client as a side effect: %v", m.clients)
	}

	m.clients["go"] = &fakeClient{}
	if !m.Ready("go") {
		t.Fatal("expected Ready once a client exists")
	}
}

func TestDiagnosticsArePostedWithFilesystemPath(t *testing.T) {
	m := NewManager("/project")
	var got any
	m.Post = func(ev interface{}) { got = ev }

	m.handleDiagnostics(PublishDiagnosticsParams{
		URI:         pathToURI("/project/a.go"),
		Diagnostics: []Diagnostic{{Message: "boom", Severity: SeverityError}},
	})

	ev, ok := got.(DiagnosticsEvent)
	if !ok {
		t.Fatalf("posted %T, want DiagnosticsEvent", got)
	}
	if ev.Path != "/project/a.go" {
		t.Errorf("Path = %q, want a filesystem path (not a URI)", ev.Path)
	}
	if len(ev.Diagnostics) != 1 || ev.Diagnostics[0].Message != "boom" {
		t.Errorf("Diagnostics = %+v", ev.Diagnostics)
	}
}

func TestEmptyDiagnosticsArePostedToo(t *testing.T) {
	// An empty set means "this file is clean now" and must reach the editor
	// so it can clear old markers — not be filtered out as uninteresting.
	m := NewManager("/project")
	posted := 0
	m.Post = func(interface{}) { posted++ }

	m.handleDiagnostics(PublishDiagnosticsParams{URI: pathToURI("/project/a.go")})

	if posted != 1 {
		t.Fatalf("posted %d events, want 1", posted)
	}
}

func TestDefinitionReturnsFalseWithNoServer(t *testing.T) {
	m := NewManager("/project")
	called := false

	dispatched := m.Definition("/project/a.go", "go", 1, 2, func(Location, bool) { called = true })

	if dispatched {
		t.Fatal("expected Definition to report it could not dispatch")
	}
	if called {
		t.Fatal("callback must not run when nothing was dispatched")
	}
}

func TestDefinitionDeliversResultThroughPost(t *testing.T) {
	want := Location{URI: pathToURI("/project/b.go"), Range: Range{Start: Position{Line: 41}}}
	fake := &fakeClient{defLoc: want, defFound: true}
	m := newTestManager("go", fake)

	// The request runs on its own goroutine and hands the result back via
	// Post; collect it synchronously here.
	results := make(chan AsyncResult, 1)
	m.Post = func(ev interface{}) { results <- ev.(AsyncResult) }

	var gotLoc Location
	var gotOK bool
	if !m.Definition("/project/a.go", "go", 3, 4, func(loc Location, ok bool) { gotLoc, gotOK = loc, ok }) {
		t.Fatal("expected Definition to dispatch")
	}
	(<-results).Apply()

	if !gotOK || gotLoc.URI != want.URI || gotLoc.Range.Start.Line != 41 {
		t.Errorf("callback got (%+v, %v), want (%+v, true)", gotLoc, gotOK, want)
	}
}

func TestDefinitionErrorIsReportedAsNotFound(t *testing.T) {
	fake := &fakeClient{defErr: errors.New("server exploded"), defFound: true}
	m := newTestManager("go", fake)
	results := make(chan AsyncResult, 1)
	m.Post = func(ev interface{}) { results <- ev.(AsyncResult) }

	var gotOK bool
	m.Definition("/project/a.go", "go", 0, 0, func(_ Location, ok bool) { gotOK = ok })
	(<-results).Apply()

	if gotOK {
		t.Error("expected an errored request to surface as not-found, not as a bogus location")
	}
}

func TestCompletionDeliversItemsThroughPost(t *testing.T) {
	fake := &fakeClient{completionItems: []CompletionItem{
		{Label: "Field"},
		{Label: "Method(x int)", InsertText: "Method"},
	}}
	m := newTestManager("go", fake)
	results := make(chan AsyncResult, 1)
	m.Post = func(ev interface{}) { results <- ev.(AsyncResult) }

	var got []CompletionItem
	var gotOK bool
	if !m.Completion("/project/a.go", "go", 3, 4, func(items []CompletionItem, ok bool) {
		got, gotOK = items, ok
	}) {
		t.Fatal("expected Completion to dispatch")
	}
	(<-results).Apply()

	if !gotOK || len(got) != 2 {
		t.Fatalf("callback got (%+v, %v), want 2 items and ok", got, gotOK)
	}
	// InsertText wins over Label when present — a function's label carries
	// its signature, which must not be inserted literally.
	if got[1].Text() != "Method" {
		t.Errorf("Text() = %q, want %q", got[1].Text(), "Method")
	}
}

func TestCompletionReturnsFalseWithNoServer(t *testing.T) {
	m := NewManager("/project")
	called := false

	if m.Completion("/project/a.go", "go", 1, 2, func([]CompletionItem, bool) { called = true }) {
		t.Fatal("expected Completion to report it could not dispatch")
	}
	if called {
		t.Fatal("callback must not run when nothing was dispatched")
	}
}

func TestShutdownClosesEveryClient(t *testing.T) {
	goFake, phpFake := &fakeClient{}, &fakeClient{}
	m := NewManager("/project")
	m.clients["go"] = goFake
	m.clients["php"] = phpFake
	m.clients["rust"] = nil // a remembered failure: must not panic

	m.Shutdown()

	if goFake.closeCall != 1 || phpFake.closeCall != 1 {
		t.Errorf("expected both clients closed, got go=%d php=%d", goFake.closeCall, phpFake.closeCall)
	}
	if len(m.clients) != 0 {
		t.Errorf("expected client state cleared, got %v", m.clients)
	}
}
