package editor

import (
	"sort"
	"strings"

	"github.com/bricejulia/kiwi/internal/layout"
)

// maxCompletionCandidates caps how many autocomplete candidates are kept
// (and shown), the same "bound it, don't let it grow forever" instinct as
// maxUndoEntries elsewhere in this package.
const maxCompletionCandidates = 10

// completionState is the in-progress autocomplete popup (Ctrl+Space),
// kept on View (not tab) since only one pane is ever mid-Insert-session at
// a time (see ExitEditingModes) — the same reasoning commandBuf already
// relies on.
type completionState struct {
	candidates []string // prefix-filtered, sorted, capped at maxCompletionCandidates
	selected   int
	prefixLen  int // runes of the in-progress word already typed, before the cursor
}

// bufferWords returns every distinct identifier-like run of runes (see
// isIdentRune, already used by the heuristic highlighter) across buf's
// lines, sorted. This is autocomplete's only candidate source — the same
// simple whole-buffer keyword-completion approach vim's own builtin
// Ctrl+n/Ctrl+p uses, deliberately not tree-sitter-based (a declarations-
// only source would miss plain local variables/params, and a completion
// *list* benefits from being permissive where a "go to X" jump would not).
func bufferWords(buf *Buffer) []string {
	seen := map[string]bool{}
	var words []string
	for _, line := range buf.Lines {
		runes := []rune(line)
		i := 0
		for i < len(runes) {
			if !isIdentRune(runes[i]) {
				i++
				continue
			}
			j := i + 1
			for j < len(runes) && isIdentRune(runes[j]) {
				j++
			}
			w := string(runes[i:j])
			if !seen[w] {
				seen[w] = true
				words = append(words, w)
			}
			i = j
		}
	}
	sort.Strings(words)
	return words
}

// wordBeforeCursor returns the identifier-like run of runes immediately
// before t's cursor — the partial word being typed, unlike
// wordUnderCursor (navigate.go) which also looks forward — plus its
// length in runes, for autocomplete's prefix filter and accept/backspace
// bookkeeping.
func wordBeforeCursor(t *tab, tabWidth int) (string, int) {
	line := t.buf.Lines[t.cursorLn]
	runes := []rune(line)
	raw := rawIndexForExpandedCol(line, t.cursorCol, tabWidth)
	if raw > len(runes) {
		raw = len(runes)
	}
	start := raw
	for start > 0 && isIdentRune(runes[start-1]) {
		start--
	}
	return string(runes[start:raw]), raw - start
}

// computeCompletionCandidates builds a fresh completionState for t's
// current cursor position, or nil if there's no buffer, no partial word
// before the cursor, or nothing matches it.
func computeCompletionCandidates(t *tab, tabWidth int) *completionState {
	if t == nil || t.buf == nil {
		return nil
	}
	prefix, prefixLen := wordBeforeCursor(t, tabWidth)
	if prefix == "" {
		return nil
	}

	var candidates []string
	for _, w := range bufferWords(t.buf) {
		if len(candidates) >= maxCompletionCandidates {
			break
		}
		if w != prefix && strings.HasPrefix(w, prefix) {
			candidates = append(candidates, w)
		}
	}
	if len(candidates) == 0 {
		return nil
	}
	return &completionState{candidates: candidates, prefixLen: prefixLen}
}

// triggerAutocomplete implements Ctrl+Space: opens the popup for the
// partial word before the cursor, if any and if it matches something.
func (v *View) triggerAutocomplete() {
	v.completion = computeCompletionCandidates(v.activeTab(), v.tabWidth)
}

// refilterCompletion re-runs the candidate filter after a keystroke while
// the popup is open (typing on, or backspacing), closing it once nothing
// matches anymore rather than leave a stale/empty menu up — at that point
// typing just continues as plain Insert-mode editing.
func (v *View) refilterCompletion() {
	v.completion = computeCompletionCandidates(v.activeTab(), v.tabWidth)
}

// acceptCompletion inserts the selected candidate in place of the typed
// prefix and closes the popup. Reuses the existing deleteBackward/
// insertText primitives (looping deleteBackward once per prefix rune,
// then a single insertText of the full candidate) rather than a new
// Buffer method, so the edit is folded into whichever Insert session is
// already in progress — undoable as part of it on the next Esc, like any
// other keystroke, with no special-casing needed.
func (v *View) acceptCompletion() {
	comp := v.completion
	v.completion = nil
	if comp == nil || len(comp.candidates) == 0 {
		return
	}
	for i := 0; i < comp.prefixLen; i++ {
		v.deleteBackward()
	}
	v.insertText(comp.candidates[comp.selected])
}

// handleCompletionKey handles a key while the autocomplete popup is open,
// called before handleInsertKey's normal dispatch. Returns true if it
// fully handled the key; false means "not for me, continue as normal" —
// used for Backspace and printable characters, which still need their
// usual Insert-mode effect (delete/insert), with the caller responsible
// for re-filtering afterward (see handleInsertKey).
func (v *View) handleCompletionKey(k layout.Key) bool {
	switch v.keymap[k.String()] {
	case "normal_mode": // Esc closes the popup only, staying in Insert mode
		v.completion = nil
		return true
	case "insert_newline", "insert_tab": // Enter or Tab accepts, like most completion menus
		v.acceptCompletion()
		return true
	case "move_up":
		v.completion.selected--
		if v.completion.selected < 0 {
			v.completion.selected = len(v.completion.candidates) - 1
		}
		return true
	case "move_down":
		v.completion.selected = (v.completion.selected + 1) % len(v.completion.candidates)
		return true
	}
	return false
}

// renderCompletionPopup draws the open popup as extra rows directly below
// the cursor's current screen position (cursorCol, cursorRow, in this
// View's own window coordinates — see CursorPosition), one candidate per
// row with the selected one reverse-video highlighted. Clamped to however
// many rows actually fit before the pane's bottom edge (fewer candidates
// shown if it doesn't fit; no flip-above-cursor if there's no room below,
// a deferred nicety). Every row is padded to the window's full width:
// vaxis's Println only writes the cells its segments cover, so anything
// short of that would otherwise leave stale glyphs from the body content
// already drawn underneath. Assumes ASCII candidate text (true for
// virtually all real identifiers), so byte length doubles as display
// width here rather than using the rune-width-aware helpers the rest of
// this package is careful to use elsewhere.
func (v *View) renderCompletionPopup(w layout.Window, cols, rows, cursorCol, cursorRow int) {
	comp := v.completion
	maxRows := rows - cursorRow - 1
	if maxRows <= 0 {
		return
	}
	n := len(comp.candidates)
	if n > maxRows {
		n = maxRows
	}

	width := 0
	for _, c := range comp.candidates[:n] {
		if len(c) > width {
			width = len(c)
		}
	}
	if avail := cols - cursorCol; width > avail {
		width = avail
	}
	if width <= 0 {
		return
	}

	for i := 0; i < n; i++ {
		text := comp.candidates[i]
		if len(text) > width {
			text = text[:width]
		}
		style := layout.Style{}
		if i == comp.selected {
			style.Attr |= layout.AttrReverse
		}

		segs := []layout.Segment{
			{Text: strings.Repeat(" ", cursorCol)},
			{Text: text + strings.Repeat(" ", width-len(text)), Style: style},
		}
		if trailing := cols - cursorCol - width; trailing > 0 {
			segs = append(segs, layout.Segment{Text: strings.Repeat(" ", trailing)})
		}
		w.Println(cursorRow+1+i, segs...)
	}
}
