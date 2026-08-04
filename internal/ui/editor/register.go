package editor

// Register is the yank/delete clipboard behind "dd", "yy" and "p" — vim's
// unnamed register. Like vim's registers (and unlike a pane's cursor or
// scroll position) it is shared by every View given the same one via
// SetRegister, so a line cut in one split can be put in another; a View
// nobody shares one with gets its own private register from NewView and
// behaves exactly as if registers were per-pane.
//
// Contents are either linewise (whole lines, from "dd"/"yy") or charwise (a
// partial-line fragment, from a mouse selection — see View.copySelection),
// and putAfter branches on which: a linewise put inserts new lines below the
// cursor, a charwise put splices into the current line. That distinction is
// vim's own, and it is why the flag has to be stored alongside the text
// rather than inferred — a single-element []string{"foo"} is a legitimate
// value for both, meaning "a whole line reading foo" or "the three
// characters foo", and nothing in the text itself can tell them apart.
//
// This is deliberately NOT the operating system clipboard: nothing here
// shells out to pbcopy or speaks OSC 52, so nib's cut/copy/put stay
// entirely inside nib, the same way its git and language-server work is
// kept out of this package (see View.BlameFunc). Reaching the OS clipboard
// too is the caller's job, done alongside a Set here rather than underneath
// it (see View.copySelection and View.CopyFunc), so that a terminal which
// silently drops OSC 52 still leaves "p" working.
type Register struct {
	lines    []string
	charwise bool
}

// NewRegister returns an empty register — a put from it is a no-op until
// something is yanked or deleted into it.
func NewRegister() *Register {
	return &Register{}
}

// Set replaces the register's contents with a copy of lines, marked
// linewise, so later mutation of the buffer those lines came from can't
// change what's held here.
func (r *Register) Set(lines []string) {
	r.lines = append([]string(nil), lines...)
	r.charwise = false
}

// SetCharwise is Set for a partial-line fragment: lines holds the tail of
// the first line, any whole lines between, and the head of the last, exactly
// as Buffer.TextBetween returns them. A single element means the fragment
// never crossed a line break.
func (r *Register) SetCharwise(lines []string) {
	r.lines = append([]string(nil), lines...)
	r.charwise = true
}

// Charwise reports whether the contents are a partial-line fragment rather
// than whole lines — what putAfter consults to decide how to splice them
// back in.
func (r *Register) Charwise() bool {
	return r.charwise
}

// Lines returns a copy of the register's contents, nil when empty — copied
// for the mirror-image reason Set is: what a caller splices into a buffer
// must not alias the register, or a later edit to that buffer would rewrite
// the register's contents too.
func (r *Register) Lines() []string {
	if len(r.lines) == 0 {
		return nil
	}
	return append([]string(nil), r.lines...)
}

// linewiseEnd returns the last line index of a count-line linewise range
// starting at the cursor, clamped to the buffer — the inclusive end
// deleteLines/yankLines/changeLines all need. Shared so "dd"/"3dd" and
// "yy"/"3yy" and "cc"/"3cc" clamp identically at the end of the buffer.
func linewiseEnd(t *tab, count int) int {
	end := t.cursorLn + count - 1
	if last := len(t.buf.Lines) - 1; end > last {
		end = last
	}
	return end
}

// yankLines implements vim's "yy" (count == 1) and "3yy"/"y3y" (count > 1):
// copies count lines starting at the cursor into the register, leaving the
// buffer and the cursor entirely alone. No undo entry, since nothing
// changed.
func (v *View) yankLines(t *tab, count int) {
	if t.buf == nil {
		return
	}
	end := linewiseEnd(t, count)
	v.register.Set(t.buf.Lines[t.cursorLn : end+1])
}

// deleteLines implements vim's "dd" (count == 1) and "3dd"/"d3d" (count > 1):
// cuts count lines starting at the cursor — into the register, like vim (a
// delete IS a cut; "p" puts it back) — and leaves the cursor at the start of
// whatever line moved up into their place, or of the new last line if the
// deleted range reached the end. Recorded as its own undo entry, like
// "x"/"X": a single "dd" is already a complete change.
func (v *View) deleteLines(t *tab, count int) {
	if t.buf == nil {
		return
	}
	before := snapshotTab(t)
	end := linewiseEnd(t, count)
	v.register.Set(t.buf.DeleteLines(t.cursorLn, end))
	t.cursorCol = 0
	v.pushUndoIfChanged(t, before)
	v.onBufferEdited(t)
	v.clamp(t) // the deleted range may have reached the last line
}

// changeLines implements vim's "cc" (count == 1) and "3cc" (count > 1):
// clears count lines starting at the cursor, leaving one blank line in
// their place and the cursor on it at column 0 — vim's own "cc" quirk of
// clearing the line's content rather than removing the line entirely.
// Deleted text still goes to the register, like "dd". Must be called after
// enterInsertMode has already captured the pre-edit snapshot (see "o"'s
// openLineBelow, the same shape): this only mutates the buffer, and the
// eventual exitInsertMode is what commits the clear plus whatever gets
// typed next as one undo entry.
func (v *View) changeLines(t *tab, count int) {
	if t.buf == nil {
		return
	}
	end := linewiseEnd(t, count)
	// DeleteLines already leaves one blank line behind when the deleted
	// range is the WHOLE buffer, so inserting another here would leave two.
	wholeBuffer := t.cursorLn == 0 && end == len(t.buf.Lines)-1
	v.register.Set(t.buf.DeleteLines(t.cursorLn, end))
	if !wholeBuffer {
		t.buf.InsertLines(t.cursorLn, []string{""})
	}
	t.cursorCol = 0
	v.onBufferEdited(t)
	v.clamp(t)
}

// deleteRange implements a charwise delete operator (e.g. "dw", "d$",
// "diw") over the raw-rune range [startLn,startCol, endLn,endCol) —
// end-exclusive, matching Buffer.DeleteRange/TextBetween — leaving the
// cursor at the deletion point (startLn,startCol), the same way "x" leaves
// it where the deleted rune was. Deleted text goes to the register
// charwise, like a mouse-selection "y" (see copySelection). Recorded as its
// own undo entry.
func (v *View) deleteRange(t *tab, startLn, startCol, endLn, endCol int) {
	if t.buf == nil {
		return
	}
	before := snapshotTab(t)
	v.register.SetCharwise(t.buf.DeleteRange(startLn, startCol, endLn, endCol))
	t.cursorLn = startLn
	t.cursorCol = expandedColForRawIndex(t.buf.Lines[startLn], startCol, v.tabWidth)
	v.pushUndoIfChanged(t, before)
	v.onBufferEdited(t)
	v.clamp(t)
}

// yankRange implements a charwise yank operator (e.g. "yw", "y$", "yiw")
// over the raw-rune range [startLn,startCol, endLn,endCol) — leaves the
// buffer and cursor alone, only fills the register. No undo entry, since
// nothing changed.
func (v *View) yankRange(t *tab, startLn, startCol, endLn, endCol int) {
	if t.buf == nil {
		return
	}
	v.register.SetCharwise(t.buf.TextBetween(startLn, startCol, endLn, endCol))
}

// changeRange implements a charwise change operator (e.g. "cw", "ciw") over
// the raw-rune range [startLn,startCol, endLn,endCol): deletes it, moving
// the cursor to the deletion point. Deleted text still goes to the
// register, like deleteRange. Must be called after enterInsertMode has
// already captured the pre-edit snapshot, the same shape as changeLines —
// this only mutates the buffer, leaving the eventual exitInsertMode to
// commit the delete plus whatever gets typed next as one undo entry.
func (v *View) changeRange(t *tab, startLn, startCol, endLn, endCol int) {
	if t.buf == nil {
		return
	}
	v.register.SetCharwise(t.buf.DeleteRange(startLn, startCol, endLn, endCol))
	t.cursorLn = startLn
	t.cursorCol = expandedColForRawIndex(t.buf.Lines[startLn], startCol, v.tabWidth)
	v.onBufferEdited(t)
	v.clamp(t)
}

// putAfter implements vim's "p", in whichever of its two forms the register
// holds (see Register): linewise, inserting whole lines below the cursor's
// line and moving the cursor to the start of the first of them; or charwise,
// splicing a fragment in just after the cursor's character. A no-op on an
// empty register. One undo entry either way, same as deleteLines.
func (v *View) putAfter(t *tab) {
	if t.buf == nil {
		return
	}
	lines := v.register.Lines()
	if len(lines) == 0 {
		return
	}
	before := snapshotTab(t)
	if v.register.Charwise() {
		v.putCharwise(t, lines)
	} else {
		t.buf.InsertLines(t.cursorLn+1, lines)
		t.cursorLn++
		t.cursorCol = 0
	}
	v.pushUndoIfChanged(t, before)
	v.onBufferEdited(t)
	v.clamp(t)
}

// putCharwise splices a partial-line fragment in after the cursor — the
// inverse of how Buffer.TextBetween took it apart, so a copy followed by a
// put reproduces the text exactly.
//
// The cursor lands on the LAST character of what was put, which is where vim
// leaves a charwise put (and unlike the linewise branch above, which lands on
// the first line of it). That's what makes a repeated "p" append rather than
// build up in reverse.
//
// Every step goes through Buffer's own methods rather than touching Lines
// directly, so Source and Dirty stay in step; that costs a few redundant
// resyncs per put, which is nothing next to the full re-parse
// onBufferEdited already does.
func (v *View) putCharwise(t *tab, lines []string) {
	line := t.buf.Lines[t.cursorLn]
	runes := []rune(line)
	at := rawIndexForExpandedCol(line, t.cursorCol, v.tabWidth)
	// "After the cursor" — except at end of line (or on an empty line),
	// where there is no character to go after and the cursor position is
	// already the insertion point.
	if at < len(runes) {
		at++
	}

	if len(lines) == 1 {
		end := t.buf.InsertText(t.cursorLn, at, lines[0])
		t.cursorCol = lastPutColumn(v, t.buf.Lines[t.cursorLn], at, end)
		return
	}

	// Multi-line: break the current line in two, hang the fragment's first
	// piece off the head and its last piece off the tail, and drop any whole
	// lines from the middle in between.
	t.buf.SplitLine(t.cursorLn, at)
	headLen := len([]rune(t.buf.Lines[t.cursorLn]))
	t.buf.InsertText(t.cursorLn, headLen, lines[0])

	if mid := lines[1 : len(lines)-1]; len(mid) > 0 {
		t.buf.InsertLines(t.cursorLn+1, mid)
	}

	lastLn := t.cursorLn + len(lines) - 1
	last := lines[len(lines)-1]
	end := t.buf.InsertText(lastLn, 0, last)
	t.cursorLn = lastLn
	t.cursorCol = lastPutColumn(v, t.buf.Lines[lastLn], 0, end)
}

// lastPutColumn converts the raw rune index just past a freshly inserted
// fragment into the cursorCol (tab-expanded) index of the fragment's last
// character. An empty fragment has no last character, so the cursor stays at
// the insertion point.
func lastPutColumn(v *View, line string, at, end int) int {
	raw := end - 1
	if raw < at {
		raw = at
	}
	return expandedColForRawIndex(line, raw, v.tabWidth)
}
