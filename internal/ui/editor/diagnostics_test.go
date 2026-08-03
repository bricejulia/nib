package editor

import (
	"os"
	"path/filepath"
	"strings"
	"testing"

	"github.com/bricejulia/nib/internal/layout"
	"github.com/bricejulia/nib/internal/lsp"
)

func TestWorstSeverityPicksTheMostSevere(t *testing.T) {
	// LSP numbers Error as 1 and Hint as 4, so "worst" is the SMALLEST
	// number — the easy thing to get backwards.
	diags := []lsp.Diagnostic{
		{Severity: lsp.SeverityHint},
		{Severity: lsp.SeverityError},
		{Severity: lsp.SeverityWarning},
	}
	if got := worstSeverity(diags); got != lsp.SeverityError {
		t.Errorf("worstSeverity = %v, want SeverityError", got)
	}
}

func TestWorstSeverityEmptyIsUnranked(t *testing.T) {
	if got := worstSeverity(nil); got != 0 {
		t.Errorf("worstSeverity(nil) = %v, want 0", got)
	}
}

func TestWorstSeveritySkipsUnrankedEntries(t *testing.T) {
	// Severity is optional on the wire; an entry without one must not be
	// mistaken for severity 0 and win the "smallest number" comparison.
	diags := []lsp.Diagnostic{{Severity: 0}, {Severity: lsp.SeverityWarning}}
	if got := worstSeverity(diags); got != lsp.SeverityWarning {
		t.Errorf("worstSeverity = %v, want SeverityWarning", got)
	}
}

func TestDiagnosticMarkerAndStyleAreDistinctPerSeverity(t *testing.T) {
	severities := []lsp.DiagnosticSeverity{
		lsp.SeverityError, lsp.SeverityWarning, lsp.SeverityInformation, lsp.SeverityHint,
	}
	seenMarker := map[string]lsp.DiagnosticSeverity{}
	seenStyle := map[layout.Style]lsp.DiagnosticSeverity{}
	for _, s := range severities {
		m := diagnosticMarker(s)
		if m == " " {
			t.Errorf("diagnosticMarker(%v) is blank; only 'no diagnostic' should be", s)
		}
		if prev, ok := seenMarker[m]; ok {
			t.Errorf("diagnosticMarker(%v) collides with %v: both %q", s, prev, m)
		}
		seenMarker[m] = s

		st := diagnosticStyle(s)
		if st == (layout.Style{}) {
			t.Errorf("diagnosticStyle(%v) is the default style", s)
		}
		if prev, ok := seenStyle[st]; ok {
			t.Errorf("diagnosticStyle(%v) collides with %v", s, prev)
		}
		seenStyle[st] = s
	}
}

func TestDiagnosticMarkerBlankWhenNoDiagnostic(t *testing.T) {
	if got := diagnosticMarker(0); got != " " {
		t.Errorf("diagnosticMarker(0) = %q, want a space", got)
	}
	if got := diagnosticStyle(0); got != (layout.Style{}) {
		t.Errorf("diagnosticStyle(0) = %+v, want the default style", got)
	}
}

func TestDiagnosticsByLineGroupsOnStartLine(t *testing.T) {
	diags := []lsp.Diagnostic{
		{Range: lsp.Range{Start: lsp.Position{Line: 2}}, Message: "a"},
		{Range: lsp.Range{Start: lsp.Position{Line: 2}}, Message: "b"},
		// A multi-line diagnostic is recorded only against where it starts.
		{Range: lsp.Range{Start: lsp.Position{Line: 5}, End: lsp.Position{Line: 9}}, Message: "c"},
	}
	byLine := diagnosticsByLine(diags)

	if len(byLine[2]) != 2 {
		t.Errorf("line 2 has %d diagnostics, want 2", len(byLine[2]))
	}
	if len(byLine[5]) != 1 {
		t.Errorf("line 5 has %d diagnostics, want 1", len(byLine[5]))
	}
	if len(byLine[9]) != 0 {
		t.Errorf("line 9 (a multi-line diagnostic's END) should have none, got %d", len(byLine[9]))
	}
}

func TestApplyDiagnosticsRendersGutterMarker(t *testing.T) {
	v := NewView()
	v.Open(fixturePath(t, "editor_sample.txt"))

	v.ApplyDiagnostics(fixturePath(t, "editor_sample.txt"), []lsp.Diagnostic{
		{Range: lsp.Range{Start: lsp.Position{Line: 0}}, Severity: lsp.SeverityError, Message: "boom"},
	})

	w := newFakeWindow(60, 10)
	v.Render(w)

	// Buffer line 0 renders on row 1 (row 0 is the tab bar).
	if !strings.Contains(w.lines[1], "E") {
		t.Errorf("expected an error marker in row 1's gutter, got %q", w.lines[1])
	}
	if !rowHasStyle(w, 1, func(s layout.Style) bool { return s.Foreground == layout.ColorRed }) {
		t.Errorf("expected a red-styled segment on row 1, got %+v", w.segs[1])
	}
	// Line 1 has no diagnostic, so no marker.
	if strings.Contains(w.lines[2], "E") {
		t.Errorf("row 2 has no diagnostic; expected no marker, got %q", w.lines[2])
	}
}

func TestApplyDiagnosticsReplacesPreviousSet(t *testing.T) {
	path := fixturePath(t, "editor_sample.txt")
	v := NewView()
	v.Open(path)
	v.ApplyDiagnostics(path, []lsp.Diagnostic{
		{Range: lsp.Range{Start: lsp.Position{Line: 0}}, Severity: lsp.SeverityError},
	})

	// An empty set means "clean now", not "nothing to report".
	v.ApplyDiagnostics(path, nil)

	w := newFakeWindow(60, 10)
	v.Render(w)
	if strings.Contains(w.lines[1], "E") {
		t.Errorf("expected markers cleared by an empty diagnostic set, got %q", w.lines[1])
	}
}

// goBuf builds an in-memory Go buffer (parseTree needs Path's extension to
// pick a grammar, and Source rather than Lines to parse).
func syntaxBuf(path string, lines ...string) *Buffer {
	src := strings.Join(lines, "\n")
	return &Buffer{Path: path, Lines: lines, Source: []byte(src)}
}

func TestSyntaxDiagnosticsFindsRealParseErrors(t *testing.T) {
	cases := []struct {
		name string
		buf  *Buffer
		want bool
		atLn int
	}{
		{
			// The marker lands on line 2, where the unclosed "{" is — the
			// error's START, not the later line that trips the parser.
			name: "broken go",
			buf:  syntaxBuf("t.go", "package main", "", "func main() {", "\tx := foo(", ""),
			want: true,
			atLn: 2,
		},
		{
			name: "valid go",
			buf:  syntaxBuf("t.go", "package main", "", "func main() {}", ""),
			want: false,
		},
		{
			name: "broken php",
			buf:  syntaxBuf("t.php", "<?php", "function f( {", ""),
			want: true,
			atLn: 0,
		},
		{
			// Regression: a broken function inside otherwise-valid PHP
			// yields ONLY a zero-width MISSING ")" node — no ERROR node at
			// all. An earlier version filtered MISSING out and silently
			// reported nothing here, which the minimal case above did not
			// catch because it happened to produce an ERROR node.
			name: "broken php amid valid code",
			buf:  syntaxBuf("t.php", "<?php", "", "function ok($n) {", "    echo 1;", "}", "", "function broken( {", "    echo 2;", "}", ""),
			want: true,
			atLn: 6,
		},
		{
			name: "valid php",
			buf:  syntaxBuf("t.php", "<?php", "function f() {", "  echo 1;", "}", ""),
			want: false,
		},
	}
	for _, c := range cases {
		t.Run(c.name, func(t *testing.T) {
			diags := syntaxDiagnostics(c.buf)
			if got := len(diags) > 0; got != c.want {
				t.Fatalf("found %d diagnostics, want any=%v: %+v", len(diags), c.want, diags)
			}
			if !c.want {
				return
			}
			onExpectedLine := false
			for _, d := range diags {
				if d.Severity != lsp.SeverityError {
					t.Errorf("severity = %v, want SeverityError", d.Severity)
				}
				if d.Source != syntaxDiagnosticSource {
					t.Errorf("source = %q, want %q", d.Source, syntaxDiagnosticSource)
				}
				if d.Range.Start.Line == c.atLn {
					onExpectedLine = true
				}
			}
			// Pointing at the wrong line is as unhelpful as not reporting.
			if !onExpectedLine {
				t.Errorf("no diagnostic on line %d; got %+v", c.atLn, diags)
			}
		})
	}
}

// TestSyntaxDiagnosticsIgnoresProseMisdetectedAsCode is a regression test
// for a false positive found while building this: ".txt" resolves to the
// "vimdoc" grammar, which rejects a file of ordinary prose as one giant
// parse error. Reporting that would invent errors in every plain text file.
func TestSyntaxDiagnosticsIgnoresProseMisdetectedAsCode(t *testing.T) {
	buf, err := Load(fixturePath(t, "editor_sample.txt"))
	if err != nil {
		t.Fatal(err)
	}
	// Guard the premise: if .txt stops resolving to a strict grammar, this
	// test silently stops testing anything.
	if lang := languageFor(buf.Path); lang == "" || syntaxCheckedLanguages[lang] {
		t.Skipf("premise changed: .txt now resolves to %q", lang)
	}

	if diags := syntaxDiagnostics(buf); len(diags) != 0 {
		t.Fatalf("expected no syntax diagnostics for prose, got %+v", diags)
	}
}

func TestSyntaxDiagnosticsSkipsUnlistedLanguages(t *testing.T) {
	// Markdown isn't on the trusted list, so even genuinely odd content
	// must produce nothing rather than a guess.
	buf := syntaxBuf("t.md", "# Title", "", "```go", "func broken( {", "```", "")
	if diags := syntaxDiagnostics(buf); len(diags) != 0 {
		t.Fatalf("expected no diagnostics for an unlisted language, got %+v", diags)
	}
}

func TestSyntaxDiagnosticsShowInGutterOnOpen(t *testing.T) {
	dir := t.TempDir()
	path := filepath.Join(dir, "broken.go")
	if err := os.WriteFile(path, []byte("package main\n\nfunc main() {\n\tx := foo(\n"), 0o644); err != nil {
		t.Fatal(err)
	}

	v := NewView() // no LSP manager at all: tree-sitter is the only source
	v.Open(path)

	w := newFakeWindow(60, 10)
	v.Render(w)

	joined := strings.Join(w.lines, "\n")
	if !strings.Contains(joined, "E") {
		t.Fatalf("expected a syntax-error marker in the gutter, got:\n%s", joined)
	}
}

func TestSyntaxDiagnosticsSuppressedWhileALanguageServerIsRunning(t *testing.T) {
	// A server reports syntax errors too (with better messages), so showing
	// both would double every marker. The server owns the field when live.
	dir := t.TempDir()
	path := filepath.Join(dir, "broken.go")
	if err := os.WriteFile(path, []byte("package main\n\nfunc main() {\n\tx := foo(\n"), 0o644); err != nil {
		t.Fatal(err)
	}

	v := NewView()
	v.lsp = &fakeLSP{ready: true}
	v.Open(path)

	if tb := v.activeTab(); len(tb.diagnostics) != 0 {
		t.Fatalf("expected tree-sitter diagnostics suppressed with a live server, got %+v", tb.diagnostics)
	}
}

func TestSyntaxDiagnosticsRefreshAfterInsertSessionNotDuringIt(t *testing.T) {
	// Mid-typing code is nearly always momentarily unparseable, so markers
	// must not appear on every keystroke — only once the session ends.
	lines := []string{"package main", "", "func main() {}", ""}
	v := NewView()
	v.tabs = []*tab{{path: "t.go", buf: syntaxBuf("t.go", lines...)}}
	v.active = 0
	tb := v.activeTab()
	tb.cursorLn, tb.cursorCol = 2, len("func main() {}")

	v.HandleKey(layout.Key{Text: "i"})
	v.HandleKey(layout.Key{Text: "("}) // breaks the file mid-session
	if len(tb.diagnostics) != 0 {
		t.Fatalf("expected no markers while still typing, got %+v", tb.diagnostics)
	}

	v.HandleKey(layout.Key{Named: layout.KeyEsc})
	if len(tb.diagnostics) == 0 {
		t.Fatal("expected markers once the Insert session ended")
	}
}

func TestApplyDiagnosticsIgnoresUnopenedPath(t *testing.T) {
	v := NewView()
	v.Open(fixturePath(t, "editor_sample.txt"))

	// Every server notification is fanned out to every pane, so a path this
	// pane doesn't have open must be silently ignored, not panic.
	v.ApplyDiagnostics("/some/other/file.go", []lsp.Diagnostic{
		{Range: lsp.Range{Start: lsp.Position{Line: 0}}, Severity: lsp.SeverityError},
	})

	w := newFakeWindow(60, 10)
	v.Render(w)
	if strings.Contains(w.lines[1], "E") {
		t.Errorf("expected no marker for a different file's diagnostics, got %q", w.lines[1])
	}
}
