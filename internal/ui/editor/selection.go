package editor

import (
	"strings"

	"github.com/bricejulia/nib/internal/layout"
	"github.com/bricejulia/nib/internal/textwidth"
	"github.com/bricejulia/nib/internal/theme"
)

// selectionStyle marks the selected range. A background colour rather than
// AttrReverse (which is what searchHighlightStyle had to settle for before
// layout.Style grew a Background field) specifically so the two compose:
// reverse-on-reverse doesn't cancel, so a search match inside a selection
// would otherwise be indistinguishable from the rest of it. Bright black is
// the palette's default "slightly lighter than the background" entry,
// which reads as a selection on both light and dark terminals without
// fighting the syntax foreground colours it sits under.
//
// A function, not a package var, because a var would be evaluated at
// package-init time — before cmd/nib's run() installs the user's theme
// (see theme.SetActive) — and would then permanently freeze on
// theme.Default regardless of what the user configured.
func selectionStyle() layout.Style {
	return layout.Style{Background: theme.Get(theme.EditorSelection)}
}

// position is a place in a buffer: a line, plus a column in cursorCol's
// units (a rune index into the TAB-EXPANDED line — see tab's doc comment).
// Deliberately the cursor's own units rather than raw rune indices, because
// every position a selection is built from arrives as a cursor position;
// conversion to raw indices happens once, at the two edges where raw is what
// is needed (highlighting, via selectionRangesOnLine, and text extraction,
// via View.selectionText).
type position struct {
	ln, col int
}

// before reports whether p comes earlier in the buffer than q.
func (p position) before(q position) bool {
	if p.ln != q.ln {
		return p.ln < q.ln
	}
	return p.col < q.col
}

// selectionSpan returns the selection as an ordered (start, end) pair, with
// ok=false when there is no selection. A drag can run backwards — up the
// screen, or right-to-left — so the anchor is not necessarily the start;
// every consumer wants document order, so normalising happens here once
// rather than in each of them.
func (t *tab) selectionSpan() (start, end position, ok bool) {
	if !t.hasSel || t.buf == nil {
		return position{}, position{}, false
	}
	cursor := position{ln: t.cursorLn, col: t.cursorCol}
	if cursor.before(t.selAnchor) {
		return cursor, t.selAnchor, true
	}
	return t.selAnchor, cursor, true
}

// clearSelection collapses the selection, leaving the cursor where it is.
func (t *tab) clearSelection() {
	t.hasSel = false
	t.selAnchor = position{}
}

// selectionRangesOnLine returns the RAW rune ranges of line ln that fall
// inside the span, ready for applyHighlightRanges — which works on raw,
// pre-tab-expansion segments because that is the only stage where a rune
// index means the same thing to the highlighter as it does to the cursor
// (see its doc comment). Always zero or one range, since a selection is
// contiguous; the slice return is purely to match what
// applyHighlightRanges takes.
//
// selectsToEOL reports that the selection continues past this line's last
// rune onto the next line, which is what renderBody needs in order to pad
// the row out to the right edge — without it a multi-line selection looks
// ragged, and a selected blank line shows nothing at all.
func selectionRangesOnLine(start, end position, line string, ln, tabWidth int) (ranges []runeRange, selectsToEOL bool) {
	if ln < start.ln || ln > end.ln {
		return nil, false
	}
	n := len([]rune(line))

	from := 0
	if ln == start.ln {
		from = rawIndexForExpandedCol(line, start.col, tabWidth)
	}
	to := n
	if ln == end.ln {
		to = rawIndexForExpandedCol(line, end.col, tabWidth)
	}
	if to > n {
		to = n
	}
	if from > to {
		from = to
	}

	// A line whose selection reaches its end AND is not the last line of the
	// span has the line break itself selected.
	selectsToEOL = ln < end.ln
	if from == to && !selectsToEOL {
		return nil, false
	}
	return []runeRange{{start: from, end: to}}, selectsToEOL
}

// selectionText returns the selected text, one string per line it touches,
// or nil when there is no selection. Converts the span's tab-expanded
// columns into the raw rune indices Buffer.TextBetween takes.
func (v *View) selectionText(t *tab) []string {
	start, end, ok := t.selectionSpan()
	if !ok {
		return nil
	}
	startRaw := rawIndexForExpandedCol(lineAt(t, start.ln), start.col, tabWidthOf(t))
	endRaw := rawIndexForExpandedCol(lineAt(t, end.ln), end.col, tabWidthOf(t))
	return t.buf.TextBetween(start.ln, startRaw, end.ln, endRaw)
}

// lineAt returns buffer line ln, or "" if it's out of range.
func lineAt(t *tab, ln int) string {
	if t.buf == nil || ln < 0 || ln >= len(t.buf.Lines) {
		return ""
	}
	return t.buf.Lines[ln]
}

// copySelection copies the selection to both the yank register (so "p" puts
// it back inside nib) and, if wired, the system clipboard. Reports whether
// there was anything to copy. This is the explicit "y" gesture.
//
// Both, not either: a clipboard write can fail invisibly (see
// internal/clipboard), so relying on it alone would make "p" mysteriously
// fail for some users; and the register alone would make the copy useless
// outside nib, which is usually the point of selecting with the mouse.
// Marked charwise, since a selection is a fragment — unlike "yy", which is
// whole lines.
func (v *View) copySelection(t *tab) bool {
	lines := v.selectionText(t)
	if len(lines) == 0 {
		return false
	}
	v.register.SetCharwise(lines)
	if v.CopyFunc != nil {
		v.CopyFunc(strings.Join(lines, "\n"))
	}
	return true
}

// copySelectionToClipboard copies the selection to the system clipboard and
// DELIBERATELY LEAVES THE YANK REGISTER ALONE. It is what the mouse gestures
// use, on the reasoning that selecting with the mouse is a clipboard gesture
// while the register is vim state: if every drag overwrote the register, a
// stray selection would destroy a line just put there by "yy" and about to be
// put back with "p". Pressing "y" is how you ask for both (see
// copySelection).
//
// A no-op with no CopyFunc wired, which is the case in tests and would be the
// case for any caller that hasn't opted into OS integration.
func (v *View) copySelectionToClipboard(t *tab) bool {
	if v.CopyFunc == nil {
		return false
	}
	text := v.selectionString(t)
	if text == "" {
		return false
	}
	v.CopyFunc(text)
	return true
}

// selectionString is the selection as a single string, "" when empty — the
// form the clipboard takes, as opposed to the per-line slice the register
// holds.
func (v *View) selectionString(t *tab) string {
	lines := v.selectionText(t)
	if len(lines) == 0 {
		return ""
	}
	return strings.Join(lines, "\n")
}

// HandleMouse implements layout.MouseHandler: click to place the cursor,
// drag to select, double-click for a word, triple-click for a line,
// Shift+click to extend. Returns false for anything it doesn't claim (the
// wheel, the tab bar, a click with no file open) so App's generic handling
// still applies — see App.handleMouse.
//
// m.Col/m.Row are relative to the window this View was last given in Render,
// matching CursorPosition's convention. They are NOT clamped to it: during a
// drag the pointer can leave the pane, and a row outside it is exactly the
// signal to auto-scroll.
func (v *View) HandleMouse(m layout.Mouse) bool {
	if isWheelButton(m.Button) {
		return false
	}
	t := v.activeTab()
	if t == nil || t.buf == nil {
		return false
	}
	// Only the left button selects. Right/middle are unclaimed so a future
	// context menu or middle-click-paste can have them.
	if m.Button != layout.MouseLeft && m.Button != layout.MouseNone {
		return false
	}

	switch m.EventType {
	case layout.EventPress:
		// Row 0 is the tab bar, not text. Left unclaimed rather than
		// swallowed, so click-to-switch-tabs can be added later without
		// having to undo anything here.
		if m.Row == 0 {
			return false
		}
		return v.mousePress(t, m)
	case layout.EventMotion:
		// All-motion tracking means bare hover arrives here continuously
		// with nothing held down; only an actual drag extends a selection.
		if m.Button != layout.MouseLeft || !v.dragging {
			return false
		}
		v.extendSelectionTo(t, m)
		v.dragMoved = true
		return true
	case layout.EventRelease:
		if !v.dragging {
			return false
		}
		// The selection survives the release — that's what leaves it on
		// screen, and available to "y", after the button comes back up.
		v.dragging = false
		// Finishing a drag copies, the way selecting in a terminal does.
		// Guarded on the pointer having actually MOVED: a double- or
		// triple-click is a press (which already copied, in mousePress)
		// immediately followed by a release, so without this every one of
		// them would copy twice and spawn two clipboard helpers for one
		// gesture.
		if v.dragMoved {
			v.copySelectionToClipboard(t)
			v.dragMoved = false
		}
		return true
	default:
		// EventRepeat never applies to a mouse event.
		return false
	}
}

// mousePress handles a button-down in the text area: it moves the cursor
// there and then, depending on the click count, leaves an empty selection
// (single), selects the word (double) or the whole line (triple).
func (v *View) mousePress(t *tab, m layout.Mouse) bool {
	ln, col := v.positionAt(t, m)

	// Shift+click extends the existing selection instead of starting a new
	// one, keeping whichever end was already anchored.
	if m.Mods&layout.ModShift != 0 && t.hasSel {
		t.cursorLn, t.cursorCol = ln, col
		v.dragging = true
		v.clamp(t)
		return true
	}

	t.cursorLn, t.cursorCol = ln, col
	v.clamp(t)

	switch m.Clicks {
	case 2:
		v.selectWordAt(t)
		// A double- or triple-click is a COMPLETE gesture: there is nothing
		// further to wait for, so it copies here rather than on the release
		// that follows. (The release still arrives; dragMoved is what stops it
		// copying a second time.)
		v.copySelectionToClipboard(t)
	case 3:
		v.selectLineAt(t)
		v.copySelectionToClipboard(t)
	default:
		// A bare click just repositions the cursor and drops any previous
		// selection; the anchor is planted so that a drag out of this press
		// has something to extend from. Nothing is copied — there is no
		// selection, and clearing the clipboard because someone clicked would
		// be actively unhelpful.
		t.selAnchor = position{ln: t.cursorLn, col: t.cursorCol}
		t.hasSel = false
	}
	v.dragging = true
	v.dragMoved = false
	return true
}

// selectWordAt selects the identifier-like word the cursor is touching,
// leaving the cursor at its end. A click not touching a word selects
// nothing, rather than guessing at a neighbouring one.
func (v *View) selectWordAt(t *tab) {
	line := lineAt(t, t.cursorLn)
	pos := rawIndexForExpandedCol(line, t.cursorCol, tabWidthOf(t))
	start, end, ok := wordRangeAt(line, pos)
	if !ok {
		t.selAnchor = position{ln: t.cursorLn, col: t.cursorCol}
		t.hasSel = false
		return
	}
	t.selAnchor = position{ln: t.cursorLn, col: expandedColForRawIndex(line, start, tabWidthOf(t))}
	t.cursorCol = expandedColForRawIndex(line, end, tabWidthOf(t))
	t.hasSel = true
}

// selectLineAt selects the cursor's whole line, ending at the start of the
// next one so the line break is included — which is what makes a
// triple-click copy paste back as a whole line.
func (v *View) selectLineAt(t *tab) {
	t.selAnchor = position{ln: t.cursorLn, col: 0}
	if t.cursorLn+1 < len(t.buf.Lines) {
		t.cursorLn++
		t.cursorCol = 0
	} else {
		t.cursorCol = len(currentLineRunes(t, t.cursorLn, tabWidthOf(t)))
	}
	t.hasSel = true
}

// extendSelectionTo moves the selection's cursor end to the pointer,
// scrolling the viewport when the pointer has left the pane.
func (v *View) extendSelectionTo(t *tab, m layout.Mouse) {
	ln, col := v.positionAt(t, m)
	t.cursorLn, t.cursorCol = ln, col
	t.hasSel = true
	v.clamp(t)
}

// positionAt converts a pane-relative mouse cell into a buffer position, in
// cursorLn/cursorCol units. This is the inverse of CursorPosition, and the
// two must stay in step: row 0 is the tab bar, the gutter occupies the first
// gutterWidthFor columns, and leftCol is the horizontal scroll offset.
//
// A row outside the pane maps to the line just past the corresponding edge
// of the viewport, which is what produces auto-scroll: cursorLn drives
// topLine in renderBody, so moving the cursor one line past the edge scrolls
// by one line, with no separate scrolling code. Note this advances one line
// per motion EVENT, so holding the pointer still outside the pane does not
// keep scrolling — continuous scroll would need a ticker posted through
// App.Post.
func (v *View) positionAt(t *tab, m layout.Mouse) (ln, col int) {
	bodyRows := v.lastHeight - 1
	if bodyRows < 1 {
		bodyRows = 1
	}

	switch {
	case m.Row < 1:
		ln = t.topLine - 1
	case m.Row > bodyRows:
		ln = t.topLine + bodyRows
	default:
		ln = t.topLine + m.Row - 1
	}
	if ln < 0 {
		ln = 0
	}
	if ln >= len(t.buf.Lines) {
		ln = len(t.buf.Lines) - 1
	}

	gutter := gutterWidthFor(t)
	// A click in the gutter (line number, diff or diagnostic marker) reads as
	// the start of the line — the same thing clicking the far left of a line
	// does in a GUI editor, and what makes dragging down the gutter select
	// whole lines.
	if m.Col < gutter {
		return ln, 0
	}
	displayCol := m.Col - gutter + t.leftCol
	expanded := string(currentLineRunes(t, ln, tabWidthOf(t)))
	return ln, textwidth.RuneIndexForDisplayColumn(expanded, displayCol)
}

// isWheelButton reports whether b is a wheel direction. The editor leaves
// the wheel to App, which translates it into the same Up/Down key presses a
// keyboard scroll uses.
func isWheelButton(b layout.MouseButton) bool {
	switch b {
	case layout.MouseWheelUp, layout.MouseWheelDown, layout.MouseWheelLeft, layout.MouseWheelRight:
		return true
	default:
		return false
	}
}
