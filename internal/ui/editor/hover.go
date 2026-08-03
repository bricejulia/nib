package editor

// maxHoverPopupRows bounds the hover popup, like maxDiagnosticPopupRows —
// slightly larger, since hover text (a doc comment) tends to run longer
// than one diagnostic message.
const maxHoverPopupRows = 12

// triggerHover implements "I": asks the language server for documentation/
// type info at the cursor and shows it as a transient popup, dismissed by
// the next keypress — same lifetime as the diagnostic-details popup ("K").
// No tree-sitter fallback: nib has no syntax-only substitute for "what is
// the type/doc of this symbol", so this is simply a no-op without a ready
// server.
//
// The server's answer is routinely LSP MarkupContent (gopls always sends
// markdown) but this popup is plain text with no renderer, so
// stripMarkdown cleans up the syntax that would otherwise show up
// literally (code-fence delimiters, "---" rules, "[label](url)" links).
func (v *View) triggerHover(t *tab) {
	if t == nil || t.buf == nil || v.lsp == nil {
		return
	}
	lang := languageFor(t.path)
	if lang == "" || !v.lsp.Ready(lang) {
		return
	}
	raw := rawIndexForExpandedCol(t.buf.Lines[t.cursorLn], t.cursorCol, v.tabWidth)
	v.lsp.Hover(t.path, lang, t.cursorLn, raw, func(text string, ok bool) {
		if v.activeTab() != t {
			return // the user moved on while the server was thinking
		}
		if ok {
			v.hoverText = stripMarkdown(text)
		}
	})
}
