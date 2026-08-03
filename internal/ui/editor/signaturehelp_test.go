package editor

import (
	"strings"
	"testing"

	"github.com/bricejulia/nib/internal/layout"
	"github.com/bricejulia/nib/internal/lsp"
)

func TestSignatureHelpLinesFormatsActiveSignature(t *testing.T) {
	sh := lsp.SignatureHelp{
		Signatures: []lsp.SignatureInformation{
			{Label: "Foo(x int) bool"},
			{Label: "Foo(x int, y string) bool", Documentation: rawDoc("does the foo thing")},
		},
		ActiveSignature: 1,
	}
	got := signatureHelpLines(sh, 60)
	joined := strings.Join(got, " ")
	if !strings.Contains(joined, "Foo(x int, y string) bool") {
		t.Errorf("expected the ACTIVE signature's label, got %q", joined)
	}
	if !strings.Contains(joined, "does the foo thing") {
		t.Errorf("expected the documentation included, got %q", joined)
	}
}

func TestSignatureHelpLinesWrapsLongDocumentation(t *testing.T) {
	sh := lsp.SignatureHelp{
		Signatures: []lsp.SignatureInformation{
			{Label: "Foo()", Documentation: rawDoc("a very long description that should wrap across more than one row of the popup")},
		},
	}
	got := signatureHelpLines(sh, 20)
	if len(got) < 2 {
		t.Fatalf("expected wrapping across multiple rows, got %v", got)
	}
	if len(got) > maxSignatureHelpPopupRows {
		t.Errorf("got %d rows, want at most %d", len(got), maxSignatureHelpPopupRows)
	}
}

func TestSignatureHelpLinesEmptyWithNoSignatures(t *testing.T) {
	if got := signatureHelpLines(lsp.SignatureHelp{}, 60); got != nil {
		t.Errorf("got %v, want nil", got)
	}
}

// rawDoc builds the json.RawMessage form of a plain-string documentation
// field, matching the wire shape lsp.SignatureInformation.Documentation
// decodes (see lsp.decodeDocumentation).
func rawDoc(s string) []byte {
	return []byte(`"` + s + `"`)
}

func TestCtrlAOpensSignatureHelpInInsertMode(t *testing.T) {
	fake := &fakeLSP{ready: true, sigHelpOK: true, sigHelpAnswer: lsp.SignatureHelp{
		Signatures: []lsp.SignatureInformation{{Label: "Foo(x int)"}},
	}}
	v := NewView()
	v.lsp = fake
	v.Open(fixturePath(t, "highlight_sample.go"))
	v.HandleKey(layout.Key{Text: "i"}) // Insert mode

	if !v.HandleKey(ctrlKey("a")) {
		t.Fatal("expected Ctrl+a to be consumed")
	}
	if !fake.sigHelpDispatched {
		t.Fatal("expected a signature-help request when the server is ready")
	}
	fake.deliver(t)

	if v.signatureHelp == nil {
		t.Fatal("expected the signature-help popup to open")
	}
}

func TestSignatureHelpNoopWhenServerNotReady(t *testing.T) {
	fake := &fakeLSP{ready: false}
	v := NewView()
	v.lsp = fake
	v.Open(fixturePath(t, "highlight_sample.go"))

	v.HandleKey(ctrlKey("a"))

	if fake.sigHelpDispatched {
		t.Fatal("expected no signature-help request when the server isn't ready")
	}
	if v.signatureHelp != nil {
		t.Error("expected no popup when the server isn't ready")
	}
}

func TestSignatureHelpStaleResponseIgnoredAfterTabSwitch(t *testing.T) {
	fake := &fakeLSP{ready: true, sigHelpOK: true, sigHelpAnswer: lsp.SignatureHelp{
		Signatures: []lsp.SignatureInformation{{Label: "Stale(x int)"}},
	}}
	v := NewView()
	v.lsp = fake
	v.Open(fixturePath(t, "highlight_sample.go"))

	v.HandleKey(ctrlKey("a"))
	if !fake.sigHelpDispatched {
		t.Fatal("expected a signature-help request dispatched")
	}

	v.Open(fixturePath(t, "no_trailing_newline.txt"))
	fake.deliver(t)

	if v.signatureHelp != nil {
		t.Error("expected the stale response to be ignored after the tab switched")
	}
}
