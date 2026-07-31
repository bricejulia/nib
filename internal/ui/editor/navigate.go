package editor

import (
	"github.com/odvcencio/gotreesitter"

	"github.com/bricejulia/kiwi/internal/debuglog"
	"github.com/bricejulia/kiwi/internal/lsp"
)

// jumpLocation is a saved cursor position on View.jumpStack — a file plus a
// position within it, since a jump can cross files. See pushJump.
type jumpLocation struct {
	path    string
	ln, col int
}

// wordUnderCursor returns the identifier-like run of runes touching t's
// cursor on its current line (reusing isIdentRune, the same rune class
// the heuristic highlighter already uses), or "" if the cursor isn't
// touching one. Checks one rune back first if the cursor sits just past
// a word's last rune — the common case right after moving to end-of-word.
func wordUnderCursor(t *tab, tabWidth int) string {
	if t.buf == nil || t.cursorLn < 0 || t.cursorLn >= len(t.buf.Lines) {
		return ""
	}
	line := t.buf.Lines[t.cursorLn]
	pos := rawIndexForExpandedCol(line, t.cursorCol, tabWidth)
	start, end, ok := wordRangeAt(line, pos)
	if !ok {
		return ""
	}
	return string([]rune(line)[start:end])
}

// wordRangeAt returns the half-open RAW rune range of the identifier-like
// word touching rune index pos in line, and ok=false if pos isn't touching
// one. Factored out of wordUnderCursor so that double-click-to-select-a-word
// (see View.HandleMouse) draws its boundaries from exactly the same rune
// class the "go to definition" word does — two different answers to "what is
// the word here" in one editor would be a bug users could see.
func wordRangeAt(line string, pos int) (start, end int, ok bool) {
	runes := []rune(line)
	// Check one rune back if pos sits just past a word's last rune — the
	// common case right after moving to end-of-word, and also what makes
	// clicking the space immediately after a word still select it.
	if pos >= len(runes) || !isIdentRune(runes[pos]) {
		pos--
	}
	if pos < 0 || pos >= len(runes) || !isIdentRune(runes[pos]) {
		return 0, 0, false
	}

	start, end = pos, pos+1
	for start > 0 && isIdentRune(runes[start-1]) {
		start--
	}
	for end < len(runes) && isIdentRune(runes[end]) {
		end++
	}
	return start, end, true
}

// byteOffsetForPosition converts a (line, raw rune column) position into a
// byte offset into buf.Source — the units gotreesitter's Tree/Node and
// DefinitionSpan/CallRef all use. Valid because Source is exactly
// strings.Join(Lines, "\n"): each line's byte length, plus one for the
// '\n' joining it to the next, sums to that line's starting offset.
func byteOffsetForPosition(buf *Buffer, ln, rawCol int) uint32 {
	var offset uint32
	for i := 0; i < ln && i < len(buf.Lines); i++ {
		offset += uint32(len(buf.Lines[i])) + 1
	}
	if ln >= 0 && ln < len(buf.Lines) {
		runes := []rune(buf.Lines[ln])
		if rawCol > len(runes) {
			rawCol = len(runes)
		}
		if rawCol < 0 {
			rawCol = 0
		}
		offset += uint32(len(string(runes[:rawCol])))
	}
	return offset
}

// positionForByteOffset is byteOffsetForPosition's inverse: a byte offset
// into buf.Source back to (line, raw rune column). An offset past the end
// of the buffer clamps to the end of the last line.
func positionForByteOffset(buf *Buffer, offset uint32) (ln, rawCol int) {
	var pos uint32
	for i, line := range buf.Lines {
		lineLen := uint32(len(line))
		if offset <= pos+lineLen {
			local := offset - pos
			return i, len([]rune(line[:local]))
		}
		pos += lineLen + 1
	}
	last := len(buf.Lines) - 1
	if last < 0 {
		return 0, 0
	}
	return last, len([]rune(buf.Lines[last]))
}

// maxJumpEntries bounds the jump stack, the same way maxUndoEntries bounds
// undo history: oldest entry dropped once full, so a long navigating
// session can't grow it without limit.
const maxJumpEntries = 100

// pushJump records where the cursor is now, for a later jumpBack (Ctrl+b)
// to return to — called by goToParent/goToDefinition just before they move
// the cursor.
//
// The stack lives on the View (the pane), not on a tab, and stores the path
// alongside the position: a real go-to-definition frequently lands in a
// DIFFERENT file, and a per-tab stack could never bring you back across
// that boundary (the destination tab's own stack would be empty). This
// matches how editors model a jump list generally — navigation history
// belongs to the window you're navigating in, not to any one document.
func (v *View) pushJump(t *tab) {
	if len(v.jumpStack) >= maxJumpEntries {
		v.jumpStack = v.jumpStack[1:]
	}
	v.jumpStack = append(v.jumpStack, jumpLocation{path: t.path, ln: t.cursorLn, col: t.cursorCol})
}

// jumpBack returns to the position saved by the most recent goToParent/
// goToDefinition, reopening (or switching to) its file first if the jump
// crossed files. A no-op on an empty jump stack.
func (v *View) jumpBack() {
	if len(v.jumpStack) == 0 {
		return
	}
	loc := v.jumpStack[len(v.jumpStack)-1]
	v.jumpStack = v.jumpStack[:len(v.jumpStack)-1]

	t := v.activeTab()
	if t == nil || t.path != loc.path {
		v.Open(loc.path) // a cross-file jump: go back to where it started
		t = v.activeTab()
		if t == nil {
			return
		}
	}
	t.cursorLn, t.cursorCol = loc.ln, loc.col
	v.clamp(t)
}

// goToParent implements "go to parent": moves the cursor to the start of
// the syntax-tree parent of the node at the cursor, pushing the current
// position onto the jump stack first (see jumpBack). Repeated presses
// climb further up: naively re-querying "the node at the cursor" after
// moving to a parent's start byte would often just re-find the very same
// innermost node again (a call expression's start byte is frequently
// identical to its callee identifier's, an expression statement's to its
// expression's, and so on) and get permanently stuck — so this walks past
// every ancestor sharing the current node's exact start byte first, and
// only stops at the first one that actually begins somewhere new. A
// no-op if the language isn't recognized, there's no buffer, or every
// remaining ancestor up to the root shares the same start byte (already
// at the outermost node starting there).
func (v *View) goToParent(t *tab) {
	if t.buf == nil {
		return
	}
	tree, ok := parseTree(t.buf)
	if !ok {
		return
	}
	raw := rawIndexForExpandedCol(t.buf.Lines[t.cursorLn], t.cursorCol, v.tabWidth)
	offset := byteOffsetForPosition(t.buf, t.cursorLn, raw)
	node := tree.RootNode().NamedDescendantForByteRange(offset, offset)
	if node == nil {
		return
	}

	start := node.StartByte()
	parent := node.Parent()
	for parent != nil && parent.StartByte() == start {
		parent = parent.Parent()
	}
	if parent == nil {
		return
	}

	v.pushJump(t)
	ln, col := positionForByteOffset(t.buf, parent.StartByte())
	t.cursorLn = ln
	t.cursorCol = expandedColForRawIndex(t.buf.Lines[ln], col, v.tabWidth)
}

// goToDefinition implements "go to definition", preferring a language
// server's real semantic answer and falling back to the tree-sitter
// approximation (see goToDefinitionTreeSitter) when no server is running
// for this file's language. Same key either way — the feature just gets
// better when a server is available: LSP resolves scope and shadowing
// correctly and can land in a different file, neither of which the
// syntax-only fallback can do.
func (v *View) goToDefinition(t *tab) {
	if t.buf == nil {
		return
	}
	if v.lsp != nil {
		if lang := languageFor(t.path); lang != "" && v.lsp.Ready(lang) {
			if v.goToDefinitionLSP(t, lang) {
				return
			}
		}
	}
	v.goToDefinitionTreeSitter(t)
}

// goToDefinitionLSP asks the language server where the symbol at the
// cursor is defined, returning true if the request was dispatched at all
// (false means "no server took it — use the fallback"). The answer arrives
// later, on the UI goroutine, via the Manager's Post plumbing.
//
// The callback captures t directly rather than re-resolving "the active
// tab" when it runs: by then the user may have switched tabs or panes, and
// silently moving the cursor in whatever they're now looking at would be
// worse than applying to the tab they asked from (which, if it's since
// been closed, is simply inert).
func (v *View) goToDefinitionLSP(t *tab, lang string) bool {
	raw := rawIndexForExpandedCol(t.buf.Lines[t.cursorLn], t.cursorCol, v.tabWidth)
	fromLn, fromCol := t.cursorLn, t.cursorCol

	return v.lsp.Definition(t.path, lang, t.cursorLn, raw, func(loc lsp.Location, ok bool) {
		if !ok {
			debuglog.Warn("go to definition: no definition found")
			return
		}
		target := loc.Path()
		if target == "" {
			return
		}

		// Record where the jump started before moving, so Ctrl+b returns
		// there. Uses the position captured when the key was pressed, not
		// t's current one: the user may have moved the cursor while the
		// server was thinking.
		if len(v.jumpStack) >= maxJumpEntries {
			v.jumpStack = v.jumpStack[1:]
		}
		v.jumpStack = append(v.jumpStack, jumpLocation{path: t.path, ln: fromLn, col: fromCol})

		if target == t.path {
			t.cursorLn = loc.Range.Start.Line
			t.cursorCol = expandedColForRawIndexIn(t.buf, loc.Range.Start.Line, loc.Range.Start.Character, v.tabWidth)
			v.clamp(t)
			return
		}
		// Cross-file definition: open (or switch to) the target file and
		// land on the right line — OpenAtLine takes a 1-based line, while
		// LSP is 0-based.
		v.OpenAtLine(target, loc.Range.Start.Line+1)
		if nt := v.activeTab(); nt != nil && nt.buf != nil {
			nt.cursorCol = expandedColForRawIndexIn(nt.buf, loc.Range.Start.Line, loc.Range.Start.Character, v.tabWidth)
			v.clamp(nt)
		}
	})
}

// expandedColForRawIndexIn converts a raw rune index on buf's line ln into
// a tab-expanded cursorCol, tolerating an out-of-range line (returning 0)
// since a server's answer is computed against its own copy of the file and
// could in principle disagree with ours.
func expandedColForRawIndexIn(buf *Buffer, ln, rawCol, tabWidth int) int {
	if ln < 0 || ln >= len(buf.Lines) {
		return 0
	}
	return expandedColForRawIndex(buf.Lines[ln], rawCol, tabWidth)
}

// goToDefinitionTreeSitter jumps to the nearest same-named declaration in
// the current file (via gotreesitter's ExtractDefinitionSpans), pushing the
// current position onto the jump stack first. A same-file,
// first-name-match approximation used when no language server is available
// for this file: it has no scope resolution, so a shadowed name (e.g. a
// parameter reusing a global's name) can jump to the wrong one. Logs via
// debuglog.Warn and does nothing if the cursor isn't on an identifier, the
// language isn't recognized, or no matching declaration is found.
func (v *View) goToDefinitionTreeSitter(t *tab) {
	word := wordUnderCursor(t, v.tabWidth)
	if word == "" {
		return
	}
	tree, ok := parseTree(t.buf)
	if !ok {
		debuglog.Warn("go to definition: %s isn't a recognized language", t.path)
		return
	}

	for _, d := range gotreesitter.ExtractDefinitionSpans(tree) {
		if d.Name == word {
			v.pushJump(t)
			ln, col := positionForByteOffset(t.buf, d.NameStartByte)
			t.cursorLn = ln
			t.cursorCol = expandedColForRawIndex(t.buf.Lines[ln], col, v.tabWidth)
			return
		}
	}
	debuglog.Warn("go to definition: no declaration found for %q", word)
}

// findReferences implements "find references": calls OnFindReferences
// with the identifier under the cursor, if any and if set (see
// cmd/kiwi/main.go, which wires this to the finder overlay's
// content-search — see finder.View.OpenWithQuery).
func (v *View) findReferences(t *tab) {
	if t.buf == nil || v.OnFindReferences == nil {
		return
	}
	if word := wordUnderCursor(t, v.tabWidth); word != "" {
		v.OnFindReferences(word)
	}
}
