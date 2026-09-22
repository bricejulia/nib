package editor

import "github.com/bricejulia/nib/internal/lsp"

// This file applies an lsp.WorkspaceEdit — the multi-file edit shape
// rename and code actions both use — generalizing replace.go's
// Apply/findPane/RewriteFile architecture from a fixed search/replacement
// pair to lsp.TextEdit's richer per-span shape. See replace.go's own doc
// comment for the open-buffer-vs-disk split this mirrors.

// WorkspaceEditResult summarizes an ApplyWorkspaceEdit call — the
// WorkspaceEdit analogue of replace.go's Result. There's no Skipped: a
// TextEdit already carries an exact range, so there's no ordinal to go
// stale the way a text search's occurrence can.
type WorkspaceEditResult struct {
	FilesChanged int
	Failed       map[string]error // absolute path -> the error writing/rewriting it
}

// ApplyWorkspaceEdit applies edit's per-file TextEdits — through a file's
// shared open Buffer if findPane reports one open anywhere (via the
// existing applyTextEdits, as ONE undo entry), straight to disk otherwise.
// One file's failure never aborts the rest, matching Apply.
func ApplyWorkspaceEdit(edit lsp.WorkspaceEdit, findPane func(absPath string) (*View, bool)) WorkspaceEditResult {
	res := WorkspaceEditResult{Failed: map[string]error{}}
	for uri, edits := range edit.Changes {
		path := lsp.Location{URI: uri}.Path()
		if path == "" || len(edits) == 0 {
			continue
		}
		if v, ok := findPane(path); ok && v.applyWorkspaceEditToOpenTab(path, edits) {
			res.FilesChanged++
			continue
		}
		if err := rewriteFileWithTextEdits(path, edits); err != nil {
			res.Failed[path] = err
			continue
		}
		res.FilesChanged++
	}
	return res
}

// applyWorkspaceEditToOpenTab applies edits to path's buffer if some tab in
// THIS pane has it open, returning whether one was found — the same
// find-the-matching-tab loop ReplaceLines uses, delegating the actual
// splice to applyTextEdits (format.go), which already does exactly "apply
// N TextEdits to one tab as one undo entry, bottom-to-top".
func (v *View) applyWorkspaceEditToOpenTab(path string, edits []lsp.TextEdit) bool {
	for _, t := range v.tabs {
		if t.path != path || t.buf == nil {
			continue
		}
		v.applyTextEdits(t, edits)
		return true
	}
	return false
}

// rewriteFileWithTextEdits applies edits straight to disk, for a path with
// no pane open anywhere — the WorkspaceEdit analogue of RewriteFile
// (replace.go), using a throwaway Buffer never registered with any
// BufferStore.
func rewriteFileWithTextEdits(path string, edits []lsp.TextEdit) error {
	buf, err := Load(path)
	if err != nil {
		return err
	}
	lines := append([]string(nil), buf.Lines...)
	for _, e := range sortEditsDescending(edits) {
		lines = applyTextEdit(lines, e)
	}
	buf.Restore(lines)
	if !buf.Dirty {
		return nil // the edits were all no-ops against this file's current content
	}
	return buf.Save()
}
