package editor

import "github.com/bricejulia/nib/internal/lsp"

// maxSignatureHelpPopupRows bounds the signature-help popup, the same
// "don't let it cover the whole pane" rule as maxDiagnosticPopupRows.
const maxSignatureHelpPopupRows = 8

// triggerSignatureHelp implements Ctrl+a: asks the language server for the
// enclosing call's parameter list and shows it as a transient popup, same
// lifetime as hover/diagnostics (dismissed by the next keypress). Reachable
// from both Normal and Insert mode (see view.go's dispatch), since it's
// useful both mid-typing a call's arguments and parked on one in Normal
// mode. No tree-sitter fallback: nib has no syntax-only substitute for a
// call's signature.
func (v *View) triggerSignatureHelp() {
	t := v.activeTab()
	if t == nil || t.buf == nil || v.lsp == nil {
		return
	}
	lang := languageFor(t.path)
	if lang == "" || !v.lsp.Ready(lang) {
		return
	}
	raw := rawIndexForExpandedCol(t.buf.Lines[t.cursorLn], t.cursorCol, tabWidthOf(t))
	v.lsp.SignatureHelp(t.path, lang, t.cursorLn, raw, func(sh lsp.SignatureHelp, ok bool) {
		if v.activeTab() != t {
			return // the user moved on while the server was thinking
		}
		if ok {
			v.signatureHelp = &sh
		}
	})
}

// signatureHelpLines renders sh's active signature — its label, then its
// documentation if any — as popup lines, wrapped/capped like any other
// popup (see wrapText, maxSignatureHelpPopupRows).
func signatureHelpLines(sh lsp.SignatureHelp, width int) []string {
	if len(sh.Signatures) == 0 || width <= 0 {
		return nil
	}
	idx := sh.ActiveSignature
	if idx < 0 || idx >= len(sh.Signatures) {
		idx = 0
	}
	sig := sh.Signatures[idx]

	var out []string
	appendWrapped := func(text string) bool {
		for _, line := range wrapText(text, width) {
			if len(out) >= maxSignatureHelpPopupRows {
				return false
			}
			out = append(out, line)
		}
		return true
	}
	if !appendWrapped(sig.Label) {
		return out
	}
	if doc := stripMarkdown(sig.DocText()); doc != "" {
		appendWrapped(doc)
	}
	return out
}
