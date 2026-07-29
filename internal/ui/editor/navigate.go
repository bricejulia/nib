package editor

import (
	"github.com/odvcencio/gotreesitter"

	"github.com/bricejulia/kiwi/internal/debuglog"
)

// jumpLocation is a saved cursor position on tab.jumpStack — see pushJump.
type jumpLocation struct {
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
	runes := []rune(line)
	pos := rawIndexForExpandedCol(line, t.cursorCol, tabWidth)
	if pos >= len(runes) || !isIdentRune(runes[pos]) {
		pos--
	}
	if pos < 0 || pos >= len(runes) || !isIdentRune(runes[pos]) {
		return ""
	}

	start, end := pos, pos+1
	for start > 0 && isIdentRune(runes[start-1]) {
		start--
	}
	for end < len(runes) && isIdentRune(runes[end]) {
		end++
	}
	return string(runes[start:end])
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

// pushJump saves t's current cursor position onto its jump stack, for a
// later jumpBack (Ctrl+b) to return to — called by goToParent/
// goToDefinition just before they move the cursor.
func pushJump(t *tab) {
	t.jumpStack = append(t.jumpStack, jumpLocation{ln: t.cursorLn, col: t.cursorCol})
}

// jumpBack returns t's cursor to the position saved by the most recent
// goToParent/goToDefinition. A no-op on an empty jump stack.
func (v *View) jumpBack(t *tab) {
	if len(t.jumpStack) == 0 {
		return
	}
	loc := t.jumpStack[len(t.jumpStack)-1]
	t.jumpStack = t.jumpStack[:len(t.jumpStack)-1]
	t.cursorLn, t.cursorCol = loc.ln, loc.col
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

	pushJump(t)
	ln, col := positionForByteOffset(t.buf, parent.StartByte())
	t.cursorLn = ln
	t.cursorCol = expandedColForRawIndex(t.buf.Lines[ln], col, v.tabWidth)
}

// goToDefinition implements "go to definition": jumps to the nearest
// same-named declaration in the current file (via gotreesitter's
// ExtractDefinitionSpans), pushing the current position onto the jump
// stack first. This is a same-file, first-name-match stand-in, not real
// go-to-implementation — it has no scope resolution, so a shadowed name
// (e.g. a parameter reusing a global's name) can jump to the wrong one;
// real disambiguation needs an LSP's type information. Logs via
// debuglog.Warn and does nothing if the cursor isn't on an identifier, the
// language isn't recognized, or no matching declaration is found.
func (v *View) goToDefinition(t *tab) {
	if t.buf == nil {
		return
	}
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
			pushJump(t)
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
