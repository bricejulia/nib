package editor

import (
	"testing"

	"github.com/bricejulia/nib/internal/layout"
	"github.com/bricejulia/nib/internal/lsp"
)

func TestTriggerCodeActionDispatchesWithLineDiagnostics(t *testing.T) {
	fake := &fakeLSP{ready: true, codeActionOK: true, codeActionResults: []lsp.CodeAction{
		{Title: "Remove unused import", Edit: &lsp.WorkspaceEdit{Changes: map[string][]lsp.TextEdit{"file:///a.go": {{}}}}},
	}}
	v := NewView()
	v.lsp = fake
	v.Open(fixturePath(t, "highlight_sample.go"))
	tb := v.activeTab()
	tb.cursorLn = 3
	tb.diagnostics = map[int][]lsp.Diagnostic{3: {{Message: "unused import"}}}

	var gotActions []lsp.CodeAction
	v.OnCodeActions = func(actions []lsp.CodeAction) { gotActions = actions }

	if !v.HandleKey(layout.Key{Text: "A"}) {
		t.Fatal("expected 'A' to be consumed")
	}
	if !fake.codeActionDispatched {
		t.Fatal("expected a codeAction request when the server is ready")
	}
	if fake.codeActionLine != 3 {
		t.Errorf("requested line = %d, want 3", fake.codeActionLine)
	}
	if len(fake.codeActionDiagnostics) != 1 || fake.codeActionDiagnostics[0].Message != "unused import" {
		t.Errorf("diagnostics sent as context = %+v, want the line's diagnostic", fake.codeActionDiagnostics)
	}
	fake.deliver(t)

	if len(gotActions) != 1 || gotActions[0].Title != "Remove unused import" {
		t.Errorf("OnCodeActions got %+v", gotActions)
	}
}

func TestTriggerCodeActionNoopWhenServerNotReady(t *testing.T) {
	fake := &fakeLSP{ready: false}
	v := NewView()
	v.lsp = fake
	v.Open(fixturePath(t, "highlight_sample.go"))

	called := false
	v.OnCodeActions = func([]lsp.CodeAction) { called = true }

	v.HandleKey(layout.Key{Text: "A"})

	if fake.codeActionDispatched {
		t.Fatal("expected no codeAction request when the server isn't ready")
	}
	if called {
		t.Fatal("OnCodeActions must not run when nothing was dispatched")
	}
}

func TestTriggerCodeActionStaleResponseIgnoredAfterTabSwitch(t *testing.T) {
	fake := &fakeLSP{ready: true, codeActionOK: true, codeActionResults: []lsp.CodeAction{{Title: "STALE"}}}
	v := NewView()
	v.lsp = fake
	v.Open(fixturePath(t, "highlight_sample.go"))

	called := false
	v.OnCodeActions = func([]lsp.CodeAction) { called = true }

	v.HandleKey(layout.Key{Text: "A"})
	if !fake.codeActionDispatched {
		t.Fatal("expected a codeAction request dispatched")
	}

	// The user opens another file before the server answers.
	v.Open(fixturePath(t, "no_trailing_newline.txt"))
	fake.deliver(t)

	if called {
		t.Error("stale code-action response was applied after switching tabs")
	}
}
