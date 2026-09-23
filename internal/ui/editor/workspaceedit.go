package editor

import "github.com/bricejulia/nib/internal/lsp"

// This file applies an lsp.WorkspaceEdit — the multi-file edit shape
// rename and code actions both use — generalizing replace.go's
// Apply/findPane architecture from a fixed search/replacement pair to
// lsp.TextEdit's richer per-span shape. Unlike replace.go, every touched
// file ends up open in a tab (see View.OpenBackground), never rewritten on
// disk directly — so undo, diagnostics, and everything else that only
// makes sense against an open Buffer just works, for every file the edit
// touches.

// WorkspaceEditResult summarizes an ApplyWorkspaceEdit call — the
// WorkspaceEdit analogue of replace.go's Result. There's no Skipped: a
// TextEdit already carries an exact range, so there's no ordinal to go
// stale the way a text search's occurrence can.
type WorkspaceEditResult struct {
	FilesChanged int
	Failed       map[string]error // absolute path -> the error opening or editing it
}

// ApplyWorkspaceEdit applies edit's per-file TextEdits — through a file's
// shared open Buffer if findPane reports one open anywhere, or by opening
// it (see View.OpenBackground) in triggerView, the pane the rename or code
// action was actually invoked from, if it isn't open anywhere. Every
// touched file ends up a real, editable tab either way (via
// applyWorkspaceEditToOpenTab, as ONE undo entry each), tagged with one
// shared editGroup so a single undo/redo on any one of them reverts or
// reapplies the whole operation atomically — see View.undo. Every file is
// left dirty-until-manually-saved, exactly like any other edit — nothing
// here writes to disk on its own.
//
// One file's failure never aborts the rest, matching Apply.
func ApplyWorkspaceEdit(edit lsp.WorkspaceEdit, findPane func(absPath string) (*View, bool), triggerView *View) WorkspaceEditResult {
	res := WorkspaceEditResult{Failed: map[string]error{}}
	group := &editGroup{}
	for uri, edits := range edit.Changes {
		path := lsp.Location{URI: uri}.Path()
		if path == "" || len(edits) == 0 {
			continue
		}
		v, ok := findPane(path)
		if !ok {
			t := triggerView.OpenBackground(path)
			if t.err != nil {
				res.Failed[path] = t.err
				continue
			}
			v = triggerView
		}
		group.paths = append(group.paths, path)
		if v.applyWorkspaceEditToOpenTab(path, edits, group) {
			res.FilesChanged++
		}
	}
	return res
}

// applyWorkspaceEditToOpenTab applies edits to path's buffer if some tab in
// THIS pane has it open, returning whether one was found — the same
// find-the-matching-tab loop ReplaceLines uses, delegating the actual
// splice to applyTextEdits (format.go), which already does exactly "apply
// N TextEdits to one tab as one undo entry, bottom-to-top", tagged with
// group.
func (v *View) applyWorkspaceEditToOpenTab(path string, edits []lsp.TextEdit, group *editGroup) bool {
	for _, t := range v.tabs {
		if t.path != path || t.buf == nil {
			continue
		}
		v.applyTextEdits(t, edits, group)
		return true
	}
	return false
}
