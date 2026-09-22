package editor

import "github.com/bricejulia/nib/internal/lsp"

// FindReferences asks the language server for every usage of the symbol
// under the active tab's cursor, delivering the answer to apply on the UI
// goroutine. Returns false if no request could be made at all (no server
// for this file's language), so the caller can fall back to a text search
// immediately rather than waiting for a response that will never come.
//
// apply's ok is false both when the server found nothing and when there's
// no server — the caller only needs one signal, "fall back", either way
// (see Manager.References).
func (v *View) FindReferences(apply func(locs []lsp.Location, ok bool)) bool {
	t := v.activeTab()
	if t == nil || t.buf == nil || v.lsp == nil {
		return false
	}
	lang := languageFor(t.path)
	if lang == "" || !v.lsp.Ready(lang) {
		return false
	}
	raw := rawIndexForExpandedCol(t.buf.Lines[t.cursorLn], t.cursorCol, tabWidthOf(t))
	return v.lsp.References(t.path, lang, t.cursorLn, raw, apply)
}
