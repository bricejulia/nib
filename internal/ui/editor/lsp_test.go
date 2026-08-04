package editor

import (
	"os"
	"path/filepath"
	"strings"
	"testing"

	"github.com/bricejulia/nib/internal/layout"
	"github.com/bricejulia/nib/internal/lsp"
)

// fakeLSP stands in for *lsp.Manager (see View.lsp / languageServer) so the
// editor's LSP wiring is testable without spawning a real subprocess.
// pendingApply holds the callback a Definition call would have delivered
// asynchronously, so tests can run it deterministically instead of racing
// a goroutine.
type fakeLSP struct {
	ready bool
	// notReadyStatus is what Status reports when ready is false, so tests
	// can distinguish "no server configured" from "configured but down".
	notReadyStatus lsp.ServerStatus

	opened      []string
	openedLangs []string
	changed     []string
	closed      []string

	defDispatched bool
	defLoc        lsp.Location
	defFound      bool
	defLine       int
	defChar       int
	pendingApply  func()

	completionDispatched bool
	completionItems      []lsp.CompletionItem
	completionOK         bool
	completionLine       int
	completionChar       int

	hoverDispatched bool
	hoverText       string
	hoverOK         bool
	hoverLine       int
	hoverChar       int

	sigHelpDispatched bool
	sigHelpAnswer     lsp.SignatureHelp
	sigHelpOK         bool
	sigHelpLine       int
	sigHelpChar       int

	formatDispatched   bool
	formatEdits        []lsp.TextEdit
	formatOK           bool
	formatTabWidth     int
	formatInsertSpaces bool
}

func (f *fakeLSP) Ready(string) bool { return f.ready }

func (f *fakeLSP) Status(string) lsp.ServerStatus {
	if f.ready {
		return lsp.StatusRunning
	}
	return f.notReadyStatus // zero value is StatusNone
}

func (f *fakeLSP) Open(path, language, _ string) {
	f.opened = append(f.opened, path)
	f.openedLangs = append(f.openedLangs, language)
}

func (f *fakeLSP) Change(path, _ string) { f.changed = append(f.changed, path) }

func (f *fakeLSP) Close(path string) { f.closed = append(f.closed, path) }

func (f *fakeLSP) Definition(_, _ string, line, character int, apply func(lsp.Location, bool)) bool {
	if !f.ready {
		return false
	}
	f.defDispatched = true
	f.defLine, f.defChar = line, character
	loc, found := f.defLoc, f.defFound
	f.pendingApply = func() { apply(loc, found) }
	return true
}

func (f *fakeLSP) Completion(_, _ string, line, character int, apply func([]lsp.CompletionItem, bool)) bool {
	if !f.ready {
		return false
	}
	f.completionDispatched = true
	f.completionLine, f.completionChar = line, character
	items, ok := f.completionItems, f.completionOK
	f.pendingApply = func() { apply(items, ok) }
	return true
}

func (f *fakeLSP) Hover(_, _ string, line, character int, apply func(string, bool)) bool {
	if !f.ready {
		return false
	}
	f.hoverDispatched = true
	f.hoverLine, f.hoverChar = line, character
	text, ok := f.hoverText, f.hoverOK
	f.pendingApply = func() { apply(text, ok) }
	return true
}

func (f *fakeLSP) SignatureHelp(_, _ string, line, character int, apply func(lsp.SignatureHelp, bool)) bool {
	if !f.ready {
		return false
	}
	f.sigHelpDispatched = true
	f.sigHelpLine, f.sigHelpChar = line, character
	sh, ok := f.sigHelpAnswer, f.sigHelpOK
	f.pendingApply = func() { apply(sh, ok) }
	return true
}

func (f *fakeLSP) Formatting(_, _ string, tabWidth int, insertSpaces bool, apply func([]lsp.TextEdit, bool)) bool {
	if !f.ready {
		return false
	}
	f.formatDispatched = true
	f.formatTabWidth = tabWidth
	f.formatInsertSpaces = insertSpaces
	edits, ok := f.formatEdits, f.formatOK
	f.pendingApply = func() { apply(edits, ok) }
	return true
}

// deliver runs the callback a Definition call captured, standing in for the
// event loop dispatching lsp.AsyncResult.
func (f *fakeLSP) deliver(t *testing.T) {
	t.Helper()
	if f.pendingApply == nil {
		t.Fatal("no pending Definition callback to deliver")
	}
	apply := f.pendingApply
	f.pendingApply = nil
	apply()
}

func TestOpenRegistersFileWithDetectedLanguage(t *testing.T) {
	fake := &fakeLSP{}
	v := NewView()
	v.lsp = fake

	v.Open(fixturePath(t, "highlight_sample.go"))

	if len(fake.opened) != 1 {
		t.Fatalf("opened = %v, want one entry", fake.opened)
	}
	// The language name must be the same one the LSP server registry is
	// keyed on (see lsp.DefaultServers), which is exactly what languageFor
	// exists to guarantee.
	if fake.openedLangs[0] != "go" {
		t.Errorf("registered language = %q, want %q", fake.openedLangs[0], "go")
	}
}

func TestOpenSkipsLanguageServerForUnrecognizedExtension(t *testing.T) {
	// Note: an unrecognized extension is rarer than it sounds — the grammar
	// registry covers ~186 languages and even claims ".txt" (as "vimdoc"),
	// so this uses a deliberately made-up extension. Filtering unsupported
	// languages is the Manager's job (it finds no server and no-ops); the
	// View only skips when there's no language NAME to look up at all.
	dir := t.TempDir()
	path := filepath.Join(dir, "data.zzzznotalanguage")
	if err := os.WriteFile(path, []byte("hello"), 0o644); err != nil {
		t.Fatal(err)
	}

	fake := &fakeLSP{}
	v := NewView()
	v.lsp = fake
	v.Open(path)

	if len(fake.opened) != 0 {
		t.Fatalf("opened = %v, want none when no grammar recognizes the file", fake.opened)
	}
}

func TestClosingTabUnregistersFromLanguageServer(t *testing.T) {
	fake := &fakeLSP{}
	v := NewView()
	v.lsp = fake
	path := fixturePath(t, "highlight_sample.go")
	v.Open(path)

	v.CloseTab()

	if len(fake.closed) != 1 || fake.closed[0] != path {
		t.Fatalf("closed = %v, want one entry for %s", fake.closed, path)
	}
}

func TestEditingNotifiesLanguageServer(t *testing.T) {
	fake := &fakeLSP{}
	v := NewView()
	v.lsp = fake
	v.Open(fixturePath(t, "highlight_sample.go"))

	v.HandleKey(layout.Key{Text: "i"})
	v.HandleKey(layout.Key{Text: "x"})

	if len(fake.changed) == 0 {
		t.Fatal("expected an edit to push the new contents to the server")
	}
}

func TestEditingStillRehighlightsAlongsideNotifying(t *testing.T) {
	// onBufferEdited does two things; make sure adding the LSP notification
	// didn't displace the re-highlight it already did.
	fake := &fakeLSP{}
	v := NewView()
	v.lsp = fake
	v.Open(fixturePath(t, "highlight_sample.go"))
	tb := v.activeTab()

	v.HandleKey(layout.Key{Text: "i"})
	v.HandleKey(layout.Key{Text: "x"})

	if tb.buf.highlighted == nil {
		t.Fatal("expected highlighting to be recomputed after an edit")
	}
	if len(tb.buf.highlighted) != len(tb.buf.Lines) {
		t.Errorf("highlighted has %d lines, buffer has %d", len(tb.buf.highlighted), len(tb.buf.Lines))
	}
}

func TestGoToDefinitionPrefersLSPWhenReady(t *testing.T) {
	fake := &fakeLSP{ready: true, defFound: false}
	v := NewView()
	v.lsp = fake
	v.Open(fixturePath(t, "highlight_sample.go"))
	tb := v.activeTab()
	tb.cursorLn, tb.cursorCol = 3, 6 // inside "greet" on "func greet(...)"

	v.HandleKey(ctrlKey("]"))

	if !fake.defDispatched {
		t.Fatal("expected the LSP definition request to be used when a server is ready")
	}
	if fake.defLine != 3 {
		t.Errorf("requested line = %d, want 3", fake.defLine)
	}
}

func TestGoToDefinitionFallsBackToTreeSitterWhenNotReady(t *testing.T) {
	lines := []string{
		"package main",
		"",
		"func helper() {",
		"}",
		"",
		"func main() {",
		"\thelper()",
		"}",
	}
	fake := &fakeLSP{ready: false} // server not running for this language
	v := NewView()
	v.lsp = fake
	v.tabs = []*tab{{
		path: "test.go",
		buf:  &Buffer{Path: "test.go", Lines: lines, Source: []byte(strings.Join(lines, "\n"))},
	}}
	v.active = 0
	tb := v.activeTab()
	line := lines[6]
	tb.cursorLn = 6
	tb.cursorCol = expandedColForRawIndex(line, strings.Index(line, "helper")+1, tabWidthOf(tb))

	v.HandleKey(ctrlKey("]"))

	if fake.defDispatched {
		t.Fatal("expected no LSP request when the server isn't ready")
	}
	// The tree-sitter fallback should have jumped to the declaration.
	if tb.cursorLn != 2 {
		t.Fatalf("cursorLn = %d, want 2 (tree-sitter fallback found the declaration)", tb.cursorLn)
	}
}

func TestGoToDefinitionLSPMovesCursorWithinSameFile(t *testing.T) {
	path := fixturePath(t, "highlight_sample.go")
	fake := &fakeLSP{
		ready:    true,
		defFound: true,
		defLoc:   lsp.Location{URI: pathURIForTest(path), Range: lsp.Range{Start: lsp.Position{Line: 3, Character: 5}}},
	}
	v := NewView()
	v.lsp = fake
	v.Open(path)
	tb := v.activeTab()
	tb.cursorLn, tb.cursorCol = 7, 4
	startLn, startCol := tb.cursorLn, tb.cursorCol

	v.HandleKey(ctrlKey("]"))
	fake.deliver(t)

	if tb.cursorLn != 3 {
		t.Errorf("cursorLn = %d, want 3 (the location the server returned)", tb.cursorLn)
	}
	if len(v.jumpStack) != 1 {
		t.Fatalf("expected one jump pushed, got %d", len(v.jumpStack))
	}
	if v.jumpStack[0].ln != startLn || v.jumpStack[0].col != startCol {
		t.Errorf("pushed jump = %+v, want (%d,%d) so Ctrl+b returns there", v.jumpStack[0], startLn, startCol)
	}
}

func TestGoToDefinitionLSPOpensCrossFileTarget(t *testing.T) {
	fake := &fakeLSP{
		ready:    true,
		defFound: true,
		// A different file than the one we start in — the thing the
		// tree-sitter fallback fundamentally can't do.
		defLoc: lsp.Location{
			URI:   pathURIForTest(fixturePath(t, "no_trailing_newline.txt")),
			Range: lsp.Range{Start: lsp.Position{Line: 0, Character: 3}},
		},
	}
	v := NewView()
	v.lsp = fake
	v.Open(fixturePath(t, "highlight_sample.go"))
	tb := v.activeTab()
	tb.cursorLn, tb.cursorCol = 3, 6

	v.HandleKey(ctrlKey("]"))
	fake.deliver(t)

	if len(v.tabs) != 2 {
		t.Fatalf("expected the target file opened in a new tab, got %d tabs", len(v.tabs))
	}
	if got := v.ActivePath(); got != fixturePath(t, "no_trailing_newline.txt") {
		t.Errorf("active path = %q, want the definition's file", got)
	}
	if nt := v.activeTab(); nt.cursorLn != 0 {
		t.Errorf("cursorLn = %d, want 0", nt.cursorLn)
	}
}

// TestJumpBackReturnsAcrossFilesAfterCrossFileDefinition is a regression
// test for a bug found during the smoke test: the jump stack originally
// lived on the tab, so a cross-file go-to-definition pushed the return
// location onto the SOURCE tab while Ctrl+b then operated on the (empty)
// destination tab's stack — jumping back across files silently did nothing.
func TestJumpBackReturnsAcrossFilesAfterCrossFileDefinition(t *testing.T) {
	source := fixturePath(t, "highlight_sample.go")
	target := fixturePath(t, "no_trailing_newline.txt")
	fake := &fakeLSP{
		ready:    true,
		defFound: true,
		defLoc:   lsp.Location{URI: pathURIForTest(target), Range: lsp.Range{Start: lsp.Position{Line: 0}}},
	}
	v := NewView()
	v.lsp = fake
	v.Open(source)
	tb := v.activeTab()
	tb.cursorLn, tb.cursorCol = 3, 6
	startLn, startCol := tb.cursorLn, tb.cursorCol

	v.HandleKey(ctrlKey("]"))
	fake.deliver(t)
	if v.ActivePath() != target {
		t.Fatalf("setup: expected to be in %q, got %q", target, v.ActivePath())
	}

	if !v.HandleKey(ctrlKey("b")) {
		t.Fatal("expected Ctrl+b to be consumed")
	}

	if got := v.ActivePath(); got != source {
		t.Fatalf("active path after jump back = %q, want %q (back across the file boundary)", got, source)
	}
	back := v.activeTab()
	if back.cursorLn != startLn || back.cursorCol != startCol {
		t.Errorf("cursor = (%d,%d), want (%d,%d)", back.cursorLn, back.cursorCol, startLn, startCol)
	}
}

func TestGoToDefinitionLSPNotFoundLeavesCursorAlone(t *testing.T) {
	fake := &fakeLSP{ready: true, defFound: false}
	v := NewView()
	v.lsp = fake
	v.Open(fixturePath(t, "highlight_sample.go"))
	tb := v.activeTab()
	tb.cursorLn, tb.cursorCol = 3, 6
	startLn, startCol := tb.cursorLn, tb.cursorCol

	v.HandleKey(ctrlKey("]"))
	fake.deliver(t)

	if tb.cursorLn != startLn || tb.cursorCol != startCol {
		t.Errorf("cursor moved to (%d,%d) on a not-found answer", tb.cursorLn, tb.cursorCol)
	}
	if len(v.jumpStack) != 0 {
		t.Error("expected no jump pushed when nothing was found")
	}
}

func TestLanguageStatusShowsLanguageAndServerState(t *testing.T) {
	path := fixturePath(t, "highlight_sample.go")

	cases := []struct {
		name string
		fake *fakeLSP
		want string
	}{
		{"server running", &fakeLSP{ready: true}, "go " + lspRunningGlyph},
		{"configured but down", &fakeLSP{notReadyStatus: lsp.StatusNotRunning}, "go " + lspNotRunningGlyph},
		{"no server for this language", &fakeLSP{notReadyStatus: lsp.StatusNone}, "go"},
	}
	for _, c := range cases {
		t.Run(c.name, func(t *testing.T) {
			v := NewView()
			v.lsp = c.fake
			v.Open(path)
			if got := v.LanguageStatus(); got != c.want {
				t.Errorf("LanguageStatus = %q, want %q", got, c.want)
			}
		})
	}
}

func TestLanguageStatusWithoutManagerIsJustTheLanguage(t *testing.T) {
	v := NewView() // no LSP manager at all
	v.Open(fixturePath(t, "highlight_sample.go"))
	if got := v.LanguageStatus(); got != "go" {
		t.Errorf("LanguageStatus = %q, want %q", got, "go")
	}
}

func TestLanguageStatusEmptyWithNoFileOpen(t *testing.T) {
	v := NewView()
	if got := v.LanguageStatus(); got != "" {
		t.Errorf("LanguageStatus = %q, want empty with no file open", got)
	}
}

// completionFixture builds a Go tab whose last line is "\tobj." with the
// cursor just past the dot — the member-access case that has no partial word
// to filter on.
func completionFixture(t *testing.T, fake *fakeLSP) (*View, *tab) {
	t.Helper()
	lines := []string{
		"package main",
		"",
		"func main() {",
		"\tobj := Thing{}",
		"\tobj.",
		"}",
	}
	v := NewView()
	if fake != nil {
		v.lsp = fake
	}
	v.tabs = []*tab{{
		path: "test.go",
		buf:  &Buffer{Path: "test.go", Lines: lines, Source: []byte(strings.Join(lines, "\n"))},
	}}
	v.active = 0
	tb := v.activeTab()
	tb.cursorLn = 4
	tb.cursorCol = expandedColForRawIndex(lines[4], len([]rune(lines[4])), tabWidthOf(tb)) // just past "."
	v.HandleKey(layout.Key{Text: "i"})                                                     // Insert mode
	return v, tb
}

func TestAutocompleteAfterDotUsesLSPMembers(t *testing.T) {
	fake := &fakeLSP{
		ready:        true,
		completionOK: true,
		completionItems: []lsp.CompletionItem{
			{Label: "Alpha", SortText: "00"},
			{Label: "Beta(x int)", InsertText: "Beta", SortText: "01"},
		},
	}
	v, _ := completionFixture(t, fake)

	v.HandleKey(ctrlSpace())
	if !fake.completionDispatched {
		t.Fatal("expected a server completion request after a dot")
	}
	fake.deliver(t)

	if v.completion == nil {
		t.Fatal("expected the popup to open with the server's members")
	}
	want := []string{"Alpha", "Beta"} // InsertText wins over the signature-bearing label
	if len(v.completion.candidates) != len(want) {
		t.Fatalf("candidates = %v, want %v", v.completion.candidates, want)
	}
	for i, w := range want {
		if v.completion.candidates[i] != w {
			t.Errorf("candidates[%d] = %q, want %q", i, v.completion.candidates[i], w)
		}
	}
	// Nothing was typed after the dot, so accepting must not delete anything.
	if v.completion.prefixLen != 0 {
		t.Errorf("prefixLen = %d, want 0 right after a dot", v.completion.prefixLen)
	}
}

func TestAutocompleteAfterDotAcceptsWithoutEatingTheDot(t *testing.T) {
	fake := &fakeLSP{
		ready:           true,
		completionOK:    true,
		completionItems: []lsp.CompletionItem{{Label: "Alpha"}},
	}
	v, tb := completionFixture(t, fake)

	v.HandleKey(ctrlSpace())
	fake.deliver(t)
	v.HandleKey(layout.Key{Named: layout.KeyEnter}) // accept

	if got := tb.buf.Lines[4]; got != "\tobj.Alpha" {
		t.Fatalf("Lines[4] = %q, want %q", got, "\tobj.Alpha")
	}
}

func TestLSPCompletionFiltersByTypedPrefix(t *testing.T) {
	// A server asked at "obj.Al" typically still returns every member, so
	// the client has to filter.
	fake := &fakeLSP{
		ready:        true,
		completionOK: true,
		completionItems: []lsp.CompletionItem{
			{Label: "Alpha"}, {Label: "Beta"}, {Label: "Almond"},
		},
	}
	lines := []string{"package main", "", "var obj = 1", "obj.Al"}
	v := NewView()
	v.lsp = fake
	v.tabs = []*tab{{
		path: "test.go",
		buf:  &Buffer{Path: "test.go", Lines: lines, Source: []byte(strings.Join(lines, "\n"))},
	}}
	v.active = 0
	tb := v.activeTab()
	tb.cursorLn, tb.cursorCol = 3, len("obj.Al")
	v.HandleKey(layout.Key{Text: "i"})

	v.HandleKey(ctrlSpace())
	fake.deliver(t)

	if v.completion == nil {
		t.Fatal("expected a popup")
	}
	for _, c := range v.completion.candidates {
		if !strings.HasPrefix(c, "Al") {
			t.Errorf("candidate %q does not match the typed prefix %q", c, "Al")
		}
	}
	if len(v.completion.candidates) != 2 {
		t.Errorf("candidates = %v, want the two Al* members", v.completion.candidates)
	}
	if v.completion.prefixLen != 2 {
		t.Errorf("prefixLen = %d, want 2 so accepting replaces the typed \"Al\"", v.completion.prefixLen)
	}
}

func TestAutocompleteFallsBackToBufferWordsWithoutServer(t *testing.T) {
	// No LSP at all: an empty prefix after the dot must still offer the
	// buffer's words rather than silently doing nothing.
	v, _ := completionFixture(t, nil)

	v.HandleKey(ctrlSpace())

	if v.completion == nil {
		t.Fatal("expected buffer-word candidates with no server available")
	}
}

func TestLSPCompletionEmptyAnswerFallsBackToBufferWords(t *testing.T) {
	// A server that declines to answer shouldn't leave the user worse off
	// than having no server at all.
	fake := &fakeLSP{ready: true, completionOK: true, completionItems: nil}
	v, _ := completionFixture(t, fake)

	v.HandleKey(ctrlSpace())
	fake.deliver(t)

	if v.completion == nil {
		t.Fatal("expected a fallback to buffer words when the server returned nothing")
	}
}

func TestNilLSPManagerLeavesEverythingWorking(t *testing.T) {
	// The default: no manager at all. Every LSP-touching path must be inert
	// rather than panicking.
	v := NewView()
	v.Open(fixturePath(t, "highlight_sample.go"))
	v.HandleKey(layout.Key{Text: "i"})
	v.HandleKey(layout.Key{Text: "x"})
	v.HandleKey(layout.Key{Named: layout.KeyEsc})
	v.HandleKey(ctrlKey("]"))
	v.CloseTab()
	// Reaching here without a panic is the assertion.
}

// pathURIForTest builds the file:// URI form the lsp package would produce,
// without exporting its internal helper just for tests.
func pathURIForTest(path string) string {
	return "file://" + path
}
