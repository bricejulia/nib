package editor

import (
	"github.com/odvcencio/gotreesitter"

	"github.com/bricejulia/nib/internal/layout"
	"github.com/bricejulia/nib/internal/lsp"
)

// diagnosticMarker and diagnosticStyle are the editor gutter's
// presentation for a language-server diagnostic, shaped deliberately like
// gitstyle.LineMarker/LineStyle. Kept local to this package rather than
// factored into a shared style package the way gitstyle was: git status
// shows up in three places (file tree, finder, editor gutter) and so
// genuinely needed sharing, whereas diagnostics only ever render here.

// diagnosticMarker is the single-character gutter glyph for a severity.
func diagnosticMarker(s lsp.DiagnosticSeverity) string {
	switch s {
	case lsp.SeverityError:
		return "E"
	case lsp.SeverityWarning:
		return "W"
	case lsp.SeverityInformation:
		return "I"
	case lsp.SeverityHint:
		return "H"
	default:
		return " "
	}
}

// diagnosticStyle is the color/attribute for a severity's gutter glyph.
func diagnosticStyle(s lsp.DiagnosticSeverity) layout.Style {
	switch s {
	case lsp.SeverityError:
		return layout.Style{Foreground: layout.ColorRed, Attr: layout.AttrBold}
	case lsp.SeverityWarning:
		return layout.Style{Foreground: layout.ColorYellow}
	case lsp.SeverityInformation:
		return layout.Style{Foreground: layout.ColorBlue}
	case lsp.SeverityHint:
		return layout.Style{Attr: layout.AttrDim}
	default:
		return layout.Style{}
	}
}

// worstSeverity returns the most severe severity among diags, or 0 (no
// severity, rendering as a blank gutter) for an empty slice. A line can
// carry several diagnostics at once and the gutter has room for exactly
// one glyph, so the most urgent one wins.
//
// Note the inverted comparison: LSP numbers severities with Error as 1 and
// Hint as 4, so "more severe" means numerically smaller.
func worstSeverity(diags []lsp.Diagnostic) lsp.DiagnosticSeverity {
	worst := lsp.DiagnosticSeverity(0)
	for _, d := range diags {
		if d.Severity == 0 {
			continue // severity is optional on the wire; treat as unranked
		}
		if worst == 0 || d.Severity < worst {
			worst = d.Severity
		}
	}
	return worst
}

// diagnosticsByLine groups a server's flat diagnostic list by the 0-based
// buffer line each one starts on, which is how the gutter renderer needs
// them. A diagnostic spanning several lines is recorded only against its
// first line: the gutter marks where a problem begins rather than shading
// its whole extent.
func diagnosticsByLine(diags []lsp.Diagnostic) map[int][]lsp.Diagnostic {
	if len(diags) == 0 {
		return nil
	}
	byLine := make(map[int][]lsp.Diagnostic, len(diags))
	for _, d := range diags {
		byLine[d.Range.Start.Line] = append(byLine[d.Range.Start.Line], d)
	}
	return byLine
}

// maxDiagnosticPopupRows bounds the details popup so a line carrying many
// long diagnostics can't cover the whole pane.
const maxDiagnosticPopupRows = 8

// diagnosticPopupLines renders the diagnostics on line ln into display
// lines for the details popup, wrapped to width and prefixed with a
// severity marker so several messages on one line stay tellable apart.
// Returns nil when the line is clean, which is what suppresses the popup.
func diagnosticPopupLines(diags []lsp.Diagnostic, width int) []string {
	if len(diags) == 0 || width <= 0 {
		return nil
	}
	var out []string
	for _, d := range diags {
		text := diagnosticMarker(d.Severity) + " " + d.Message
		if d.Source != "" {
			text += " [" + d.Source + "]"
		}
		for _, line := range wrapText(text, width) {
			if len(out) >= maxDiagnosticPopupRows {
				return out
			}
			out = append(out, line)
		}
	}
	return out
}

// maxSyntaxDiagnostics caps how many parse errors are reported for one
// buffer. A badly mangled file can produce a great many ERROR nodes, and
// past the first handful they're noise — the useful signal is where the
// trouble starts.
const maxSyntaxDiagnostics = 20

// syntaxDiagnosticSource labels these as parse errors, matching the
// "source" real language servers use for the same thing (gopls reports its
// own parse errors as source "syntax"), so the two read alike.
const syntaxDiagnosticSource = "syntax"

// syntaxCheckedLanguages is the set of languages whose parse errors are
// trustworthy enough to show in the gutter.
//
// A curated list, not a heuristic, because syntax diagnostics need a higher
// confidence bar than syntax highlighting does: a wrong grammar guess just
// makes highlighting bland, but it makes diagnostics *fabricate errors*.
// The motivating case is ".txt", which the grammar registry maps to
// "vimdoc" — that grammar then rejects ordinary prose as one giant parse
// error. Two heuristics were tried against real files and both failed
// (see syntaxDiagnostics for the measurements), so the honest fix is to
// only trust grammars whose file extensions are unambiguous.
//
// Entries are the names the registry reports (see languageFor). Adding a
// language is a one-line change, same as internal/lsp.DefaultServers, and
// omitting one fails safe: no syntax markers, exactly as before this
// existed.
var syntaxCheckedLanguages = map[string]bool{
	"go":         true,
	"php":        true,
	"python":     true,
	"javascript": true,
	"typescript": true,
	"tsx":        true,
	"rust":       true,
	"c":          true,
	"cpp":        true,
	"c_sharp":    true,
	"java":       true,
	"ruby":       true,
	"lua":        true,
	"json":       true,
	"css":        true,
}

// syntaxDiagnostics finds parse errors in buf using tree-sitter, expressed
// as lsp.Diagnostics so the gutter renders them exactly like a language
// server's. Returns nil if no grammar recognizes the file or it parses
// cleanly.
//
// This is what gives every one of tree-sitter's ~186 languages instant
// syntax feedback, rather than only the handful with a configured language
// server. Tree-sitter is error-tolerant by design: it recovers and keeps
// parsing, so unlike a compiler that stops at the first problem, it can
// point at several distinct trouble spots in one pass.
//
// Reusing lsp.Diagnostic for a non-LSP source is deliberate: it's the
// shape the gutter already speaks, and inventing a parallel type plus a
// conversion layer would buy nothing but churn.
//
// Both ERROR nodes ("there is text here I can't fit the grammar to") and
// MISSING nodes ("a token I needed isn't here") count. MISSING carries real
// signal that nothing else provides: a PHP file with "function broken( {"
// inside otherwise valid code yields ONLY a MISSING ")" — no ERROR node at
// all — so dropping it would miss that entire class of mistake.
//
// Restricting to syntaxCheckedLanguages, rather than filtering node shapes,
// is what keeps false positives out. Two alternatives were measured against
// real files and rejected:
//
//   - Trusting every grammar. ".txt" resolves to the "vimdoc" grammar,
//     which flags a whole file of ordinary prose as one 253-byte parse
//     error — errors invented out of nothing.
//   - Suppressing files whose errors span most of the content, on the
//     theory that the grammar must be the wrong one. Actively harmful: a
//     genuinely broken PHP file measured 95% error-covered (starting at
//     byte 0, exactly like the bogus vimdoc case) while a genuinely broken
//     Go file measured only 30%. Extent does not separate "wrong grammar"
//     from "badly broken file".
//
// Zero-width ERROR nodes are skipped as degenerate — they give the gutter
// nothing to point at. MISSING nodes are always zero-width by nature, so
// they're exempt from that check.
func syntaxDiagnostics(buf *Buffer) []lsp.Diagnostic {
	if !syntaxCheckedLanguages[languageFor(buf.Path)] {
		return nil
	}
	tree, ok := parseTree(buf)
	if !ok {
		return nil
	}
	root := tree.RootNode()
	if root == nil || !root.HasError() {
		return nil // the common case: parses cleanly, nothing to walk
	}

	var diags []lsp.Diagnostic
	lang := tree.Language()
	gotreesitter.Walk(root, func(n *gotreesitter.Node, _ int) gotreesitter.WalkAction {
		if len(diags) >= maxSyntaxDiagnostics {
			return gotreesitter.WalkStop
		}

		missing := n.IsMissing()
		if !missing && (!n.IsError() || n.EndByte() <= n.StartByte()) {
			return gotreesitter.WalkContinue
		}

		// A MISSING node names the token the parser wanted, which makes for
		// a genuinely useful message; an ERROR node has no such detail.
		message := "syntax error"
		if missing {
			message = "syntax error: missing " + n.Type(lang)
		}

		startLn, startCol := positionForByteOffset(buf, n.StartByte())
		endLn, endCol := positionForByteOffset(buf, n.EndByte())
		diags = append(diags, lsp.Diagnostic{
			Range: lsp.Range{
				Start: lsp.Position{Line: startLn, Character: startCol},
				End:   lsp.Position{Line: endLn, Character: endCol},
			},
			Severity: lsp.SeverityError,
			Source:   syntaxDiagnosticSource,
			Message:  message,
		})
		return gotreesitter.WalkContinue
	})
	return diags
}
