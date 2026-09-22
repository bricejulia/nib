package editor

import (
	"github.com/bricejulia/nib/internal/debuglog"
	"github.com/bricejulia/nib/internal/lsp"
)

// triggerCodeAction implements "A": asks the language server what fixes
// or refactors are available at the cursor, feeding any diagnostics
// already known for this line as context so the server can bias its
// suggestions toward them. No tree-sitter fallback — nib has no
// syntax-only substitute for "what can fix this", same reasoning as
// triggerFormat. Returns true if a request was dispatched.
func (v *View) triggerCodeAction(t *tab) bool {
	if t == nil || t.buf == nil || v.lsp == nil {
		return false
	}
	lang := languageFor(t.path)
	if lang == "" || !v.lsp.Ready(lang) {
		return false
	}
	raw := rawIndexForExpandedCol(t.buf.Lines[t.cursorLn], t.cursorCol, tabWidthOf(t))
	diags := t.diagnostics[t.cursorLn]
	return v.lsp.CodeAction(t.path, lang, t.cursorLn, raw, diags, func(actions []lsp.CodeAction, ok bool) {
		if v.activeTab() != t {
			return // the user moved on while the server was thinking
		}
		if !ok || len(actions) == 0 {
			debuglog.Warn("code actions: none available")
			return
		}
		if v.OnCodeActions != nil {
			v.OnCodeActions(actions)
		}
	})
}
