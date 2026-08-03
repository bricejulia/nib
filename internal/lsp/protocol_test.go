package lsp

import (
	"encoding/json"
	"path/filepath"
	"testing"
)

func TestPathURIRoundTrip(t *testing.T) {
	cases := []string{
		"/tmp/simple.go",
		"/tmp/with space/main.go",
		"/tmp/with#hash/main.go",
		"/tmp/ünïcode/main.go",
	}
	for _, path := range cases {
		t.Run(path, func(t *testing.T) {
			uri := pathToURI(path)
			if got := uriToPath(uri); got != path {
				t.Errorf("round-trip: %q -> %q -> %q", path, uri, got)
			}
		})
	}
}

func TestPathToURIHasFileScheme(t *testing.T) {
	if got := pathToURI("/tmp/x.go"); got != "file:///tmp/x.go" {
		t.Errorf("pathToURI = %q, want %q", got, "file:///tmp/x.go")
	}
}

func TestURIToPathRejectsNonFileURIs(t *testing.T) {
	for _, uri := range []string{"http://example.com/x.go", "", "::not a uri::"} {
		if got := uriToPath(uri); got != "" {
			t.Errorf("uriToPath(%q) = %q, want empty", uri, got)
		}
	}
}

func TestLocationPathConvertsURI(t *testing.T) {
	loc := Location{URI: pathToURI("/tmp/x.go")}
	if got := loc.Path(); got != filepath.FromSlash("/tmp/x.go") {
		t.Errorf("Path() = %q, want %q", got, "/tmp/x.go")
	}
}

func TestDiagnosticJSONRoundTrip(t *testing.T) {
	want := PublishDiagnosticsParams{
		URI: "file:///tmp/x.go",
		Diagnostics: []Diagnostic{{
			Range:    Range{Start: Position{Line: 3, Character: 7}, End: Position{Line: 3, Character: 12}},
			Severity: SeverityError,
			Source:   "compiler",
			Message:  "undefined: foo",
		}},
	}

	data, err := json.Marshal(want)
	if err != nil {
		t.Fatalf("Marshal: %v", err)
	}
	var got PublishDiagnosticsParams
	if err := json.Unmarshal(data, &got); err != nil {
		t.Fatalf("Unmarshal: %v", err)
	}

	if got.URI != want.URI || len(got.Diagnostics) != 1 {
		t.Fatalf("got %+v, want %+v", got, want)
	}
	d := got.Diagnostics[0]
	if d.Message != "undefined: foo" || d.Severity != SeverityError || d.Range.Start.Line != 3 || d.Range.Start.Character != 7 {
		t.Errorf("diagnostic round-trip lost data: %+v", d)
	}
}

// TestDiagnosticDecodesRealServerShape uses the exact wire format a server
// sends (rather than something this package marshaled itself), so a wrong
// json tag can't pass by being wrong symmetrically in both directions.
func TestDiagnosticDecodesRealServerShape(t *testing.T) {
	const raw = `{
		"uri": "file:///tmp/x.go",
		"diagnostics": [
			{"range":{"start":{"line":10,"character":1},"end":{"line":10,"character":5}},
			 "severity":2,"source":"gopls","message":"declared and not used: x"}
		]
	}`
	var got PublishDiagnosticsParams
	if err := json.Unmarshal([]byte(raw), &got); err != nil {
		t.Fatalf("Unmarshal: %v", err)
	}
	if len(got.Diagnostics) != 1 {
		t.Fatalf("got %d diagnostics, want 1", len(got.Diagnostics))
	}
	d := got.Diagnostics[0]
	if d.Severity != SeverityWarning {
		t.Errorf("Severity = %v, want SeverityWarning", d.Severity)
	}
	if d.Range.Start.Line != 10 || d.Range.End.Character != 5 {
		t.Errorf("Range = %+v", d.Range)
	}
	if d.Message != "declared and not used: x" {
		t.Errorf("Message = %q", d.Message)
	}
}

func TestFirstLocationAcceptsArrayShape(t *testing.T) {
	raw := json.RawMessage(`[
		{"uri":"file:///tmp/a.go","range":{"start":{"line":1,"character":2},"end":{"line":1,"character":3}}},
		{"uri":"file:///tmp/b.go","range":{"start":{"line":9,"character":0},"end":{"line":9,"character":1}}}
	]`)
	loc, ok, err := firstLocation(raw)
	if err != nil || !ok {
		t.Fatalf("firstLocation: ok=%v err=%v", ok, err)
	}
	if loc.URI != "file:///tmp/a.go" || loc.Range.Start.Line != 1 {
		t.Errorf("got %+v, want the FIRST location", loc)
	}
}

func TestFirstLocationAcceptsSingleObjectShape(t *testing.T) {
	raw := json.RawMessage(`{"uri":"file:///tmp/a.go","range":{"start":{"line":4,"character":1},"end":{"line":4,"character":2}}}`)
	loc, ok, err := firstLocation(raw)
	if err != nil || !ok {
		t.Fatalf("firstLocation: ok=%v err=%v", ok, err)
	}
	if loc.Range.Start.Line != 4 {
		t.Errorf("got %+v", loc)
	}
}

func TestCompletionItemsAcceptsListObjectShape(t *testing.T) {
	raw := json.RawMessage(`{"isIncomplete":false,"items":[
		{"label":"Field","detail":"int"},
		{"label":"Method(x int)","insertText":"Method","sortText":"00"}
	]}`)
	items, err := completionItems(raw)
	if err != nil {
		t.Fatalf("completionItems: %v", err)
	}
	if len(items) != 2 {
		t.Fatalf("got %d items, want 2", len(items))
	}
	if items[0].Text() != "Field" {
		t.Errorf("Text() = %q, want %q (falls back to label)", items[0].Text(), "Field")
	}
	if items[1].Text() != "Method" {
		t.Errorf("Text() = %q, want %q (insertText wins)", items[1].Text(), "Method")
	}
	if items[1].Order() != "00" {
		t.Errorf("Order() = %q, want %q (sortText wins)", items[1].Order(), "00")
	}
	if items[0].Order() != "Field" {
		t.Errorf("Order() = %q, want the label when sortText is absent", items[0].Order())
	}
}

func TestCompletionItemsAcceptsBareArrayShape(t *testing.T) {
	raw := json.RawMessage(`[{"label":"Alpha"},{"label":"Beta"}]`)
	items, err := completionItems(raw)
	if err != nil {
		t.Fatalf("completionItems: %v", err)
	}
	if len(items) != 2 || items[0].Label != "Alpha" {
		t.Errorf("got %+v", items)
	}
}

func TestCompletionItemsHandlesNullAndEmpty(t *testing.T) {
	for _, raw := range []json.RawMessage{json.RawMessage(`null`), json.RawMessage(``)} {
		items, err := completionItems(raw)
		if err != nil {
			t.Errorf("completionItems(%q): unexpected error %v", raw, err)
		}
		if len(items) != 0 {
			t.Errorf("completionItems(%q) = %+v, want none", raw, items)
		}
	}
}

func TestFirstLocationHandlesNullAndEmpty(t *testing.T) {
	for _, raw := range []json.RawMessage{json.RawMessage(`null`), json.RawMessage(``), json.RawMessage(`[]`)} {
		loc, ok, err := firstLocation(raw)
		if err != nil {
			t.Errorf("firstLocation(%q): unexpected error %v", raw, err)
		}
		if ok {
			t.Errorf("firstLocation(%q) = %+v, want ok=false (no definition is a normal answer)", raw, loc)
		}
	}
}

func TestDecodeDocumentationAcceptsPlainString(t *testing.T) {
	got := decodeDocumentation(json.RawMessage(`"a plain doc comment"`))
	if got != "a plain doc comment" {
		t.Errorf("decodeDocumentation = %q", got)
	}
}

func TestDecodeDocumentationAcceptsMarkupContentObject(t *testing.T) {
	got := decodeDocumentation(json.RawMessage(`{"kind":"markdown","value":"**bold** doc"}`))
	if got != "**bold** doc" {
		t.Errorf("decodeDocumentation = %q, want the value field", got)
	}
}

func TestDecodeDocumentationAcceptsMarkedStringArray(t *testing.T) {
	raw := json.RawMessage(`[{"language":"go","value":"func Foo()"}, "plain trailer"]`)
	got := decodeDocumentation(raw)
	if got != "func Foo()\n\nplain trailer" {
		t.Errorf("decodeDocumentation = %q", got)
	}
}

func TestDecodeDocumentationHandlesNullAndEmpty(t *testing.T) {
	for _, raw := range []json.RawMessage{json.RawMessage(`null`), json.RawMessage(``)} {
		if got := decodeDocumentation(raw); got != "" {
			t.Errorf("decodeDocumentation(%q) = %q, want empty", raw, got)
		}
	}
}

// TestHoverResultDecodesRealServerShape uses gopls's actual wire shape.
func TestHoverResultDecodesRealServerShape(t *testing.T) {
	const raw = `{
		"contents": {"kind":"markdown","value":"` + `func Foo(x int) string` + `"},
		"range": {"start":{"line":1,"character":0},"end":{"line":1,"character":3}}
	}`
	var got Hover
	if err := json.Unmarshal([]byte(raw), &got); err != nil {
		t.Fatalf("Unmarshal: %v", err)
	}
	if text := decodeDocumentation(got.Contents); text != "func Foo(x int) string" {
		t.Errorf("decodeDocumentation(Contents) = %q", text)
	}
}

func TestHoverResultHandlesNullAndEmpty(t *testing.T) {
	for _, raw := range []string{`null`, ``} {
		text, ok, err := hoverText(json.RawMessage(raw))
		if err != nil {
			t.Errorf("hoverText(%q): unexpected error %v", raw, err)
		}
		if ok || text != "" {
			t.Errorf("hoverText(%q) = (%q, %v), want (\"\", false)", raw, text, ok)
		}
	}
}

func TestSignatureHelpResultDecodesObjectShape(t *testing.T) {
	const raw = `{
		"signatures": [{
			"label": "Foo(x int, y string) bool",
			"documentation": "does the foo thing",
			"parameters": [{"label":"x int"},{"label":"y string"}]
		}],
		"activeSignature": 0,
		"activeParameter": 1
	}`
	sh, ok, err := signatureHelpResult(json.RawMessage(raw))
	if err != nil {
		t.Fatalf("signatureHelpResult: %v", err)
	}
	if !ok || len(sh.Signatures) != 1 {
		t.Fatalf("got ok=%v signatures=%+v", ok, sh.Signatures)
	}
	sig := sh.Signatures[0]
	if sig.Label != "Foo(x int, y string) bool" {
		t.Errorf("Label = %q", sig.Label)
	}
	if sig.DocText() != "does the foo thing" {
		t.Errorf("DocText() = %q", sig.DocText())
	}
	if len(sig.Parameters) != 2 || sig.Parameters[1].Label != "y string" {
		t.Errorf("Parameters = %+v", sig.Parameters)
	}
	if sh.ActiveParameter != 1 {
		t.Errorf("ActiveParameter = %d, want 1", sh.ActiveParameter)
	}
}

func TestSignatureHelpResultHandlesNullAndEmpty(t *testing.T) {
	for _, raw := range []string{`null`, ``, `{"signatures":[]}`} {
		sh, ok, err := signatureHelpResult(json.RawMessage(raw))
		if err != nil {
			t.Errorf("signatureHelpResult(%q): unexpected error %v", raw, err)
		}
		if ok || len(sh.Signatures) != 0 {
			t.Errorf("signatureHelpResult(%q) = (%+v, %v), want no signatures and ok=false", raw, sh, ok)
		}
	}
}

func TestTextEditsDecodesArrayShape(t *testing.T) {
	const raw = `[
		{"range":{"start":{"line":0,"character":0},"end":{"line":0,"character":0}},"newText":"package main\n"},
		{"range":{"start":{"line":5,"character":2},"end":{"line":5,"character":4}},"newText":""}
	]`
	edits, err := textEdits(json.RawMessage(raw))
	if err != nil {
		t.Fatalf("textEdits: %v", err)
	}
	if len(edits) != 2 {
		t.Fatalf("got %d edits, want 2", len(edits))
	}
	if edits[0].NewText != "package main\n" {
		t.Errorf("edits[0].NewText = %q", edits[0].NewText)
	}
	if edits[1].Range.Start.Line != 5 || edits[1].Range.End.Character != 4 {
		t.Errorf("edits[1].Range = %+v", edits[1].Range)
	}
}

func TestTextEditsHandlesNullAndEmpty(t *testing.T) {
	for _, raw := range []string{`null`, ``} {
		edits, err := textEdits(json.RawMessage(raw))
		if err != nil {
			t.Errorf("textEdits(%q): unexpected error %v", raw, err)
		}
		if len(edits) != 0 {
			t.Errorf("textEdits(%q) = %+v, want none", raw, edits)
		}
	}
}
