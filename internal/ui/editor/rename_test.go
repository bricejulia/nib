package editor

import (
	"testing"

	"github.com/bricejulia/nib/internal/layout"
	"github.com/bricejulia/nib/internal/lsp"
)

func TestStartRenamePrefillsTheSymbolUnderTheCursor(t *testing.T) {
	fake := &fakeLSP{ready: true}
	v := NewView()
	v.lsp = fake
	v.Open(fixturePath(t, "highlight_sample.go"))
	tb := v.activeTab()
	tb.cursorLn, tb.cursorCol = 3, 6 // inside "greet" on "func greet(...)"

	if !v.HandleKey(layout.Key{Text: "R"}) {
		t.Fatal("expected 'R' to be consumed")
	}
	if v.mode != modeRename {
		t.Fatalf("mode = %v, want modeRename", v.mode)
	}
	if got := v.renameField.String(); got != "greet" {
		t.Errorf("renameField = %q, want prefilled with %q", got, "greet")
	}
}

func TestStartRenameNoopWhenServerNotReady(t *testing.T) {
	fake := &fakeLSP{ready: false}
	v := NewView()
	v.lsp = fake
	v.Open(fixturePath(t, "highlight_sample.go"))
	tb := v.activeTab()
	tb.cursorLn, tb.cursorCol = 3, 6

	v.HandleKey(layout.Key{Text: "R"})

	if v.mode != modeNormal {
		t.Fatalf("mode = %v, want modeNormal (no server, no prompt)", v.mode)
	}
}

func TestRenameEscCancelsWithoutDispatching(t *testing.T) {
	fake := &fakeLSP{ready: true}
	v := NewView()
	v.lsp = fake
	v.Open(fixturePath(t, "highlight_sample.go"))
	tb := v.activeTab()
	tb.cursorLn, tb.cursorCol = 3, 6

	v.HandleKey(layout.Key{Text: "R"})
	v.HandleKey(layout.Key{Named: layout.KeyEsc})

	if v.mode != modeNormal {
		t.Fatalf("mode = %v, want modeNormal after Esc", v.mode)
	}
	if fake.renameDispatched {
		t.Error("expected Esc to cancel without firing a rename request")
	}
}

func TestCommitRenameDispatchesWithTypedName(t *testing.T) {
	fake := &fakeLSP{ready: true, renameOK: true, renameEdit: lsp.WorkspaceEdit{
		Changes: map[string][]lsp.TextEdit{pathURIForTest("/a.go"): {{NewText: "newName"}}},
	}}
	v := NewView()
	v.lsp = fake
	v.Open(fixturePath(t, "highlight_sample.go"))
	tb := v.activeTab()
	tb.cursorLn, tb.cursorCol = 3, 6

	var gotEdit lsp.WorkspaceEdit
	applied := false
	v.OnApplyWorkspaceEdit = func(edit lsp.WorkspaceEdit) { gotEdit, applied = edit, true }

	v.HandleKey(layout.Key{Text: "R"})
	// Replace the prefilled "greet" with "newName".
	for range "greet" {
		v.HandleKey(layout.Key{Named: layout.KeyBackspace})
	}
	for _, r := range "newName" {
		v.HandleKey(layout.Key{Text: string(r)})
	}
	v.HandleKey(layout.Key{Named: layout.KeyEnter})

	if v.mode != modeNormal {
		t.Fatalf("mode = %v, want modeNormal after commit", v.mode)
	}
	if !fake.renameDispatched {
		t.Fatal("expected a rename request sent to the server")
	}
	if fake.renameNewName != "newName" {
		t.Errorf("newName sent = %q, want %q", fake.renameNewName, "newName")
	}
	if fake.renameLine != 3 {
		t.Errorf("requested line = %d, want 3", fake.renameLine)
	}
	fake.deliver(t)

	if !applied || len(gotEdit.Changes) != 1 {
		t.Errorf("OnApplyWorkspaceEdit got applied=%v edit=%+v", applied, gotEdit)
	}
}

func TestCommitRenameEmptyNameIsNoop(t *testing.T) {
	fake := &fakeLSP{ready: true}
	v := NewView()
	v.lsp = fake
	v.Open(fixturePath(t, "highlight_sample.go"))
	tb := v.activeTab()
	tb.cursorLn, tb.cursorCol = 3, 6

	v.HandleKey(layout.Key{Text: "R"})
	for range "greet" {
		v.HandleKey(layout.Key{Named: layout.KeyBackspace})
	}
	v.HandleKey(layout.Key{Named: layout.KeyEnter})

	if v.mode != modeNormal {
		t.Fatalf("mode = %v, want modeNormal", v.mode)
	}
	if fake.renameDispatched {
		t.Error("expected an empty new name to be a silent no-op, no request sent")
	}
}

func TestRenameNoWordUnderCursorIsNoop(t *testing.T) {
	fake := &fakeLSP{ready: true}
	v := NewView()
	v.lsp = fake
	lines := []string{"   "}
	v.tabs = []*tab{{path: "test.go", buf: &Buffer{Path: "test.go", Lines: lines}}}
	v.active = 0
	tb := v.activeTab()
	tb.cursorLn, tb.cursorCol = 0, 1

	v.HandleKey(layout.Key{Text: "R"})

	if v.mode != modeNormal {
		t.Fatalf("mode = %v, want modeNormal with no word under the cursor", v.mode)
	}
}
