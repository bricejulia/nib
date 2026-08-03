package editor

import (
	"sort"
	"strings"

	"github.com/bricejulia/nib/internal/layout"
	"github.com/bricejulia/nib/internal/lsp"
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
// current cursor position from buffer words, or nil if there's no buffer or
// nothing matches.
//
// An empty prefix is allowed and means "offer everything in the buffer"
// (capped like any other result set) — the same thing vim's own Ctrl+n does
// with nothing typed. Rejecting it would make Ctrl+Space silently do
// nothing right after a "." or an opening parent, which is exactly where
// people reach for it.
func computeCompletionCandidates(t *tab, tabWidth int) *completionState {
	if t == nil || t.buf == nil {
		return nil
	}
	prefix, prefixLen := wordBeforeCursor(t, tabWidth)

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

// triggerAutocomplete implements Ctrl+Space, preferring the language
// server's suggestions and falling back to buffer words when no server is
// running for this file.
//
// The distinction matters most for member access: after "myObject." only
// the server knows what type myObject is and therefore what may follow.
// Scanning the buffer for identifiers cannot answer that question, no
// matter how it's filtered.
func (v *View) triggerAutocomplete() {
	t := v.activeTab()
	if t == nil || t.buf == nil {
		return
	}
	if v.lsp != nil {
		if lang := languageFor(t.path); lang != "" && v.lsp.Ready(lang) {
			if v.requestLSPCompletion(t, lang) {
				return
			}
		}
	}
	v.completion = computeCompletionCandidates(t, v.tabWidth)
}

// requestLSPCompletion asks the server for candidates at the cursor,
// returning true if the request was dispatched (false means "fall back").
// The popup appears when the answer arrives, a moment later — normal for
// server-backed completion.
//
// If the server returns nothing usable, this falls back to buffer words
// rather than leaving the user with no popup at all: a server declining to
// answer shouldn't be worse than having no server.
func (v *View) requestLSPCompletion(t *tab, lang string) bool {
	raw := rawIndexForExpandedCol(t.buf.Lines[t.cursorLn], t.cursorCol, v.tabWidth)
	_, prefixLen := wordBeforeCursor(t, v.tabWidth)

	return v.lsp.Completion(t.path, lang, t.cursorLn, raw, func(items []lsp.CompletionItem, ok bool) {
		if v.activeTab() != t {
			return // the user moved on while the server was thinking
		}
		if candidates := completionLabels(items, prefixLen, t, v.tabWidth); ok && len(candidates) > 0 {
			v.completion = &completionState{candidates: candidates, prefixLen: prefixLen}
			return
		}
		v.completion = computeCompletionCandidates(t, v.tabWidth)
	})
}

// completionLabels turns a server's items into the popup's flat candidate
// strings: filtered by whatever partial word is already typed, ordered by
// the server's own ranking (see lsp.CompletionItem.Order), and capped.
//
// Servers routinely return hundreds of items and expect the client to do
// this filtering — asking at "myObj.fo" may still yield every member of
// myObj, not just the ones starting "fo".
func completionLabels(items []lsp.CompletionItem, prefixLen int, t *tab, tabWidth int) []string {
	prefix, _ := wordBeforeCursor(t, tabWidth)

	matching := make([]lsp.CompletionItem, 0, len(items))
	for _, it := range items {
		text := it.Text()
		if text == "" || text == prefix {
			continue
		}
		if prefix != "" && !strings.HasPrefix(text, prefix) {
			continue
		}
		matching = append(matching, it)
	}
	sort.SliceStable(matching, func(i, j int) bool { return matching[i].Order() < matching[j].Order() })

	candidates := make([]string, 0, maxCompletionCandidates)
	for _, it := range matching {
		if len(candidates) >= maxCompletionCandidates {
			break
		}
		candidates = append(candidates, it.Text())
	}
	return candidates
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

// renderCompletionPopup draws the candidate menu below the cursor, with the
// selected entry highlighted — see renderPopup for the layout mechanics.
func (v *View) renderCompletionPopup(w layout.Window, cols, rows, cursorCol, cursorRow int) {
	renderPopup(w, cols, rows, cursorCol, cursorRow, v.completion.candidates, v.completion.selected)
}
