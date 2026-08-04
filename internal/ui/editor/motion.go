package editor

import "github.com/bricejulia/nib/internal/layout"

// pendingOperator is the operator armed and waiting for its second press
// (the doubled "dd"/"yy"/"cc" form), a motion to combine with ("dw", "d$"),
// or a text-object prefix ("diw") — the zero value means no operator is
// armed. See View.pendingOp and tryCompleteOperator.
type pendingOperator struct {
	action string // "delete_line" | "yank_line" | "change_line"; "" = none armed
	count  int    // count typed before/with the operator key; 0 = none
	prefix rune   // 0, 'i', or 'a' — set once a text-object prefix key is seen
}

// isOperatorAction reports whether action is one of the three keys that arm
// an operator ("d", "y", "c" in the default keymap) rather than running
// immediately.
func isOperatorAction(action string) bool {
	switch action {
	case "delete_line", "yank_line", "change_line":
		return true
	}
	return false
}

// resolvedCount applies vim's "no count typed means 1" rule: n is whatever
// was accumulated in View.count or pendingOperator.count, where 0 means
// nothing was typed.
func resolvedCount(n int) int {
	if n == 0 {
		return 1
	}
	return n
}

// normalModeDigit reports whether k is a plain digit keypress with no
// modifiers, and if so, which digit. Named keys (arrows, Enter, ...) never
// carry a meaningful Text digit, but are excluded explicitly rather than
// relying on that, so a future Named key that happened to set Text
// wouldn't silently start being read as a count.
func normalModeDigit(k layout.Key) (n int, ok bool) {
	if k.Named != "" || k.Mods != 0 || len(k.Text) != 1 {
		return 0, false
	}
	r := k.Text[0]
	if r < '0' || r > '9' {
		return 0, false
	}
	return int(r - '0'), true
}

// tryCompleteOperator handles a key arriving while v.pendingOp is armed. It
// reports whether the key was consumed by the pending operator (whether
// that completed it, as the doubled form does, or merely continued arming
// it); false means the key ABORTS the pending operator instead — v.pendingOp
// and v.count are already cleared in that case, and the caller must go on to
// dispatch action fresh, exactly as if no operator had been pending. This
// mirrors nib's existing "any other key clears a pending operator" rule
// (see TestKeyBetweenTheTwoDsAbortsTheDelete), generalized beyond the
// doubled form alone.
func (v *View) tryCompleteOperator(action string, ok bool) bool {
	op := v.pendingOp

	// A text-object prefix was already seen ("di"/"da", waiting on the
	// object letter): the only key that means anything now is "w" —
	// word_forward's action, i.e. a literal "w" press — completing
	// "diw"/"daw"/"ciw"/"caw"/"yiw"/"yaw". Anything else, including a
	// second "i"/"a" (no nested text objects), aborts.
	if op.prefix != 0 {
		v.pendingOp = pendingOperator{}
		v.count = 0
		if ok && action == "word_forward" {
			if t := v.activeTab(); t != nil {
				v.applyTextObject(t, op)
			}
			return true
		}
		return false
	}

	if ok && action == op.action {
		// The doubled linewise form: "dd", "yy", "cc" — each optionally
		// counted on either or both presses (vim's own "2d3d" = 6 lines).
		count := resolvedCount(op.count) * resolvedCount(v.count)
		v.pendingOp = pendingOperator{}
		v.count = 0
		if t := v.activeTab(); t != nil {
			v.applyLinewiseOperator(t, op.action, count)
		}
		return true
	}

	if ok {
		if _, isMotion := operatorMotions[action]; isMotion {
			// A motion to combine with: "dw", "d$", "cw", ... — vim's own
			// count-multiplication rule applies here too ("2d3w" = 6 words).
			count := resolvedCount(op.count) * resolvedCount(v.count)
			v.pendingOp = pendingOperator{}
			v.count = 0
			if t := v.activeTab(); t != nil {
				v.applyCharwiseOperatorMotion(t, op.action, action, count)
			}
			return true
		}

		// The "i"/"a" prefix: bare "i"/"a" (whose ordinary actions are
		// insert_mode/append_mode) are reinterpreted as a text-object
		// prefix while an operator is armed, instead of entering Insert
		// mode — vim's own overload of the same two keys. Count doesn't
		// apply to text objects ("3diw" is out of scope), so v.count is
		// simply dropped here rather than carried into op.prefix.
		if action == "insert_mode" || action == "append_mode" {
			if action == "insert_mode" {
				v.pendingOp.prefix = 'i'
			} else {
				v.pendingOp.prefix = 'a'
			}
			v.count = 0
			return true
		}
	}

	v.pendingOp = pendingOperator{}
	v.count = 0
	return false
}

// applyTextObject completes a text-object operator combination ("diw",
// "daw", "ciw", "caw", "yiw", "yaw") using the word/punctuation/blank run
// touching the cursor — the "inner" form for op.prefix == 'i', that run
// plus adjacent whitespace for op.prefix == 'a' (see wordObjectRange/
// aWordObjectRange in navigate.go). Ignores op.count: "3diw" is out of
// scope for this feature.
func (v *View) applyTextObject(t *tab, op pendingOperator) {
	if t.buf == nil {
		return
	}
	line := t.buf.Lines[t.cursorLn]
	raw := rawIndexForExpandedCol(line, t.cursorCol, tabWidthOf(t))

	var start, end int
	var ok bool
	if op.prefix == 'a' {
		start, end, ok = aWordObjectRange(line, raw)
	} else {
		start, end, ok = wordObjectRange(line, raw)
	}
	if !ok {
		return
	}

	switch op.action {
	case "delete_line":
		v.deleteRange(t, t.cursorLn, start, t.cursorLn, end)
	case "yank_line":
		v.yankRange(t, t.cursorLn, start, t.cursorLn, end)
	case "change_line":
		if v.enterInsertMode() {
			v.changeRange(t, t.cursorLn, start, t.cursorLn, end)
		}
	}
}

// applyCharwiseOperatorMotion runs a charwise operator (delete/yank/change)
// over the span between the cursor and where action lands, repeated count
// times — "dw", "d$", "cw", etc. Two vim quirks are special-cased here
// rather than folded into the general exclusive/inclusive math, because
// both change what "the destination" even means for THIS combination
// specifically:
//
//   - "cw" behaves like "ce": vim's own rule that changing a word must not
//     eat the whitespace after it, since the user is about to retype the
//     word. Implemented by substituting word_end's (inclusive) destination
//     for word_forward's whenever the operator is "change_line".
//   - "dw"/"yw" clip to end-of-line when the motion's destination lands on
//     a LATER line: vim's own gotcha where "dw" on a line's last word
//     would otherwise eat the newline and the next line's leading
//     whitespace too. Does not apply to "cw" (already redirected above) or
//     to "d$"/"de" (line_end/word_end).
func (v *View) applyCharwiseOperatorMotion(t *tab, op, action string, count int) {
	if t.buf == nil {
		return
	}
	cursorLn, cursorCol := t.cursorLn, t.cursorCol
	cursorRaw := rawIndexForExpandedCol(t.buf.Lines[cursorLn], cursorCol, tabWidthOf(t))

	motionAction := action
	inclusive := operatorMotions[action] == motionInclusive
	if op == "change_line" && action == "word_forward" {
		motionAction = "word_end" // "cw" behaves like "ce"
		inclusive = true
	}

	var destLn, destRaw int
	if motionAction == "line_end" {
		destLn = cursorLn
		destRaw = len([]rune(t.buf.Lines[cursorLn])) - 1
		if destRaw < cursorRaw {
			destRaw = cursorRaw // an empty line: "$" doesn't move past the cursor
		}
	} else {
		destLn, destRaw = motionDestination(t.buf, cursorLn, cursorRaw, motionAction, count)
	}

	if motionAction == "word_forward" && destLn > cursorLn {
		destLn = cursorLn
		destRaw = len([]rune(t.buf.Lines[cursorLn]))
	}

	if inclusive {
		destRaw++ // one past the included rune, for the exclusive-range calls below
	}

	// Order the two ends: a backward motion's destination precedes the
	// cursor, a forward one follows it — deleteRange/yankRange/changeRange
	// all expect (and DeleteRange/TextBetween themselves would otherwise
	// re-swap) a start-before-end pair.
	startLn, startRaw, endLn, endRaw := cursorLn, cursorRaw, destLn, destRaw
	if endLn < startLn || (endLn == startLn && endRaw < startRaw) {
		startLn, startRaw, endLn, endRaw = endLn, endRaw, startLn, startRaw
	}

	switch op {
	case "delete_line":
		v.deleteRange(t, startLn, startRaw, endLn, endRaw)
	case "yank_line":
		v.yankRange(t, startLn, startRaw, endLn, endRaw)
	case "change_line":
		if v.enterInsertMode() {
			v.changeRange(t, startLn, startRaw, endLn, endRaw)
		}
	}
}

// applyLinewiseOperator runs a linewise operator (delete/yank/change) over
// count lines starting at the cursor — today only reached by the doubled
// "dd"/"yy"/"cc" form (see tryCompleteOperator).
func (v *View) applyLinewiseOperator(t *tab, op string, count int) {
	switch op {
	case "delete_line":
		v.deleteLines(t, count)
	case "yank_line":
		v.yankLines(t, count)
	case "change_line":
		if v.enterInsertMode() {
			v.changeLines(t, count)
		}
	}
}

// motionKind classifies how an operator combines with a motion — whether
// the destination's own rune is included in the range the operator acts
// on. See applyCharwiseOperatorMotion (added alongside the operators
// themselves).
type motionKind int

const (
	motionExclusive motionKind = iota // up to, but not including, the destination
	motionInclusive                   // up to AND including the destination's rune
)

// operatorMotions is the ONLY set of actions that combine with an armed
// operator (see tryCompleteOperator) — vertical motions (move_up/down,
// first_line/last_line, paging) deliberately do NOT combine, matching
// nib's existing "an unrelated key aborts the pending operator" rule (see
// TestKeyBetweenTheTwoDsAbortsTheDelete) and this feature's confirmed
// scope.
var operatorMotions = map[string]motionKind{
	"word_forward":  motionExclusive,
	"word_backward": motionExclusive,
	"word_end":      motionInclusive,
	"line_end":      motionInclusive,
}

// runeClass is vim's own three-way classification for word-motion
// scanning: an identifier run, a punctuation run, or a run of blanks — each
// distinct from the other two, unlike isIdentRune's plain identifier/
// non-identifier split (which selectWordAt's double-click uses instead;
// see navigate.go). A punctuation run is its own "word" for "w"/"b"/"e",
// e.g. "foo.bar" is three words: "foo", ".", "bar".
type runeClass int

const (
	classBlank runeClass = iota
	classPunct
	classWord
)

// classifyRune buckets r into one of the three word-motion classes. Tabs
// count as blank alongside spaces — Buffer.Lines holds raw, un-expanded
// text (see currentLineRunes's doc comment), so a literal '\t' is what a
// tab-indented line's leading whitespace actually looks like here.
func classifyRune(r rune) runeClass {
	switch {
	case r == ' ' || r == '\t':
		return classBlank
	case isIdentRune(r):
		return classWord
	default:
		return classPunct
	}
}

// motionDestination computes where action lands, repeated count times,
// starting from the RAW rune position (ln, rawCol) — the single source of
// truth both a bare cursor motion (applyMovement) and an operator
// combination (applyCharwiseOperatorMotion) go through, so "3w" and "d3w"
// can never disagree about where they end up. Only the word motions need
// iterative scanning; line_end (a direct jump) is computed inline by its
// callers instead.
func motionDestination(buf *Buffer, ln, rawCol int, action string, count int) (newLn, newRawCol int) {
	for i := 0; i < count; i++ {
		switch action {
		case "word_forward":
			ln, rawCol = wordForwardOnce(buf, ln, rawCol)
		case "word_backward":
			ln, rawCol = wordBackwardOnce(buf, ln, rawCol)
		case "word_end":
			ln, rawCol = wordEndOnce(buf, ln, rawCol)
		}
	}
	return ln, rawCol
}

// wordForwardOnce returns the position of the next word-motion stop after
// (ln, col) — vim's "w": the start of the next word-or-punctuation run.
// Always leaves whatever run the cursor currently sits in, even from its
// very first rune. A blank (non-empty, whitespace-only) line is skipped
// through like any other run of blanks, but a truly EMPTY line is a stop
// in its own right — vim's own quirk, and the only reason this can't be
// one uniform "skip blanks" loop across the whole buffer. Clamps (does not
// wrap) at the end of the buffer.
func wordForwardOnce(buf *Buffer, ln, col int) (int, int) {
	line := []rune(buf.Lines[ln])

	if col < len(line) {
		startClass := classifyRune(line[col])
		if startClass != classBlank {
			for col < len(line) && classifyRune(line[col]) == startClass {
				col++
			}
		}
	}

	for {
		if col >= len(line) {
			if ln+1 >= len(buf.Lines) {
				return ln, len(line) // clamp at EOF
			}
			ln++
			col = 0
			line = []rune(buf.Lines[ln])
			if len(line) == 0 {
				return ln, 0 // an empty line is a stop in its own right
			}
			continue
		}
		if classifyRune(line[col]) != classBlank {
			return ln, col
		}
		col++
	}
}

// wordBackwardOnce returns the position of the previous word-motion stop
// before (ln, col) — vim's "b": the start of the previous word-or-
// punctuation run, mirroring wordForwardOnce (including the empty-line
// stop). Clamps (does not wrap) at the start of the buffer.
func wordBackwardOnce(buf *Buffer, ln, col int) (int, int) {
	line := []rune(buf.Lines[ln])

	// Step back at least one position before scanning, mirroring
	// wordForwardOnce's "always leaves the current run" rule.
	switch {
	case col > 0:
		col--
	case ln == 0:
		return 0, 0 // clamp at BOF
	default:
		ln--
		line = []rune(buf.Lines[ln])
		if len(line) == 0 {
			return ln, 0
		}
		col = len(line) - 1
	}

	for classifyRune(line[col]) == classBlank {
		if col == 0 {
			if ln == 0 {
				return 0, 0
			}
			ln--
			line = []rune(buf.Lines[ln])
			if len(line) == 0 {
				return ln, 0
			}
			col = len(line) - 1
			continue
		}
		col--
	}

	class := classifyRune(line[col])
	for col > 0 && classifyRune(line[col-1]) == class {
		col--
	}
	return ln, col
}

// wordEndOnce returns the position of the next word-motion end — vim's
// "e": the last rune of the next word-or-punctuation run. Always advances
// at least one position first, so pressing "e" while already sitting on a
// run's last rune advances to the NEXT run's end rather than staying put.
// Unlike "w"/"b", an empty (or blank) line has no "end" of its own — vim's
// own "e" quirk — so it's passed through exactly like a run of blanks
// rather than stopping there. Clamps at EOF.
func wordEndOnce(buf *Buffer, ln, col int) (int, int) {
	line := []rune(buf.Lines[ln])
	col++

	for {
		for col >= len(line) {
			if ln+1 >= len(buf.Lines) {
				if len(line) == 0 {
					return ln, 0
				}
				return ln, len(line) - 1 // clamp at EOF
			}
			ln++
			col = 0
			line = []rune(buf.Lines[ln])
		}
		if classifyRune(line[col]) != classBlank {
			break
		}
		col++
	}

	class := classifyRune(line[col])
	for col+1 < len(line) && classifyRune(line[col+1]) == class {
		col++
	}
	return ln, col
}
