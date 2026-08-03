package editor

import (
	"sort"
	"strings"

	"github.com/bricejulia/kiwi/internal/lsp"
)

// triggerFormat implements "F": asks the language server to reformat the
// active tab's whole document and applies the edits it returns as ONE
// undoable change. No tree-sitter fallback — kiwi has no syntax-only
// reformatter — so this is a no-op without a ready server.
//
// Format-on-save is deliberately out of scope: saveActive/saveTab are
// synchronous today, and folding an async LSP round-trip into Save
// correctly is more design work than this covers. Formatting stays a
// manually-triggered action only.
func (v *View) triggerFormat() {
	t := v.activeTab()
	if t == nil || t.buf == nil || v.lsp == nil {
		return
	}
	lang := languageFor(t.path)
	if lang == "" || !v.lsp.Ready(lang) {
		return
	}
	v.lsp.Formatting(t.path, lang, v.tabWidth, func(edits []lsp.TextEdit, ok bool) {
		if v.activeTab() != t {
			return // the user moved on while the server was thinking
		}
		if !ok || len(edits) == 0 {
			return
		}
		v.applyTextEdits(t, edits)
	})
}

// applyTextEdits rewrites t.buf's Lines by applying every edit in edits,
// landing the whole reformat as ONE undo entry — one "u" undoes it all,
// exactly like a "dd" or an Insert session. Reuses the same
// copy-build-Restore-pushUndoIfChanged-onBufferEdited sequence
// ReplaceLines (replace.go) already uses for a whole-buffer rewrite, rather
// than a new Buffer method.
//
// edits are applied in REVERSE document order (bottom of the file first):
// each TextEdit's Range is expressed in the ORIGINAL document's
// coordinates per the LSP spec, so applying one earlier in the file first
// would shift the line/column offsets an edit further down was computed
// against. Processing back-to-front means every edit still-to-apply sits
// entirely above the edits already spliced in, so its recorded coordinates
// stay valid throughout.
func (v *View) applyTextEdits(t *tab, edits []lsp.TextEdit) {
	before := snapshotTab(t)
	lines := append([]string(nil), t.buf.Lines...)

	sorted := append([]lsp.TextEdit(nil), edits...)
	sort.Slice(sorted, func(i, j int) bool {
		a, b := sorted[i].Range.Start, sorted[j].Range.Start
		if a.Line != b.Line {
			return a.Line > b.Line
		}
		return a.Character > b.Character
	})
	for _, e := range sorted {
		lines = applyTextEdit(lines, e)
	}

	t.buf.Restore(lines)
	v.pushUndoIfChanged(t, before)
	v.onBufferEdited(t)
	v.clamp(t)
}

// applyTextEdit replaces the span from e.Range.Start to e.Range.End — RAW
// rune positions, the same convention Buffer.TextBetween uses — with
// e.NewText, which may itself contain embedded newlines (a whole-document
// reformat's replacement usually is the entire new file as one string).
// Rebuilt as a fresh slice, the same "rebuild rather than shift in place"
// style Buffer.SplitLine/InsertLines use. Arguments in the wrong order or
// out of range are normalized/clamped exactly as TextBetween's are.
func applyTextEdit(lines []string, e lsp.TextEdit) []string {
	if len(lines) == 0 {
		return lines
	}
	startLn, startCol := e.Range.Start.Line, e.Range.Start.Character
	endLn, endCol := e.Range.End.Line, e.Range.End.Character
	if endLn < startLn || (endLn == startLn && endCol < startCol) {
		startLn, startCol, endLn, endCol = endLn, endCol, startLn, startCol
	}
	startLn = clampIndex(startLn, len(lines)-1)
	endLn = clampIndex(endLn, len(lines)-1)

	startRunes := []rune(lines[startLn])
	endRunes := []rune(lines[endLn])
	startCol = clampIndex(startCol, len(startRunes))
	endCol = clampIndex(endCol, len(endRunes))

	prefix := string(startRunes[:startCol])
	suffix := string(endRunes[endCol:])

	replacement := strings.Split(e.NewText, "\n")
	replacement[0] = prefix + replacement[0]
	replacement[len(replacement)-1] += suffix

	out := make([]string, 0, len(lines)-(endLn-startLn)+len(replacement))
	out = append(out, lines[:startLn]...)
	out = append(out, replacement...)
	out = append(out, lines[endLn+1:]...)
	return out
}
