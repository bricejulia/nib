package editor

import (
	"github.com/bricejulia/nib/internal/debuglog"
	"github.com/bricejulia/nib/internal/layout"
	"github.com/bricejulia/nib/internal/lsp"
	"github.com/bricejulia/nib/internal/ui/textfield"
)

// startRename implements "R": opens the rename prompt (modeRename),
// prefilled with the symbol under the cursor, if a language server is
// ready for this file. No tree-sitter fallback — like triggerFormat, a
// purely syntax-level "rename" (blind text substitution across the file)
// is exactly the wrong answer for a feature whose entire value is
// scope-correctness, so this is simply unavailable without a server.
func (v *View) startRename(t *tab) {
	if t == nil || t.buf == nil || v.lsp == nil {
		return
	}
	lang := languageFor(t.path)
	if lang == "" || !v.lsp.Ready(lang) {
		return
	}
	word := wordUnderCursor(t, tabWidthOf(t))
	if word == "" {
		return
	}
	v.renameField = textfield.New(word)
	v.mode = modeRename
}

// handleRenameKey handles a key while the rename prompt is open — same
// shape as handleCommandKey: Enter commits (commitRename), Esc cancels
// back to Normal mode, everything else delegates to the shared TextField.
func (v *View) handleRenameKey(k layout.Key) bool {
	switch v.keymap[k.String()] {
	case "normal_mode":
		v.mode = modeNormal
		v.renameField = textfield.TextField{}
		return true
	case "insert_newline": // Enter
		v.commitRename()
		return true
	}
	return v.renameField.HandleKey(k)
}

// commitRename fires the actual LSP rename request for the active tab's
// symbol, using v.renameField's current text as the new name. Always
// closes the prompt back to Normal mode first, matching commitCommand —
// an empty new name is a silent no-op, the same "no error UI" precedent
// commitCommand's own blank-input case sets.
func (v *View) commitRename() {
	newName := v.renameField.String()
	v.renameField = textfield.TextField{}
	v.mode = modeNormal
	if newName == "" {
		return
	}

	t := v.activeTab()
	if t == nil || t.buf == nil || v.lsp == nil {
		return
	}
	lang := languageFor(t.path)
	if lang == "" || !v.lsp.Ready(lang) {
		return
	}
	raw := rawIndexForExpandedCol(t.buf.Lines[t.cursorLn], t.cursorCol, tabWidthOf(t))
	v.lsp.Rename(t.path, lang, t.cursorLn, raw, newName, func(edit lsp.WorkspaceEdit, ok bool) {
		if !ok || len(edit.Changes) == 0 {
			debuglog.Warn("rename: no edits returned")
			return
		}
		if v.OnApplyWorkspaceEdit != nil {
			v.OnApplyWorkspaceEdit(edit)
		}
	})
}
