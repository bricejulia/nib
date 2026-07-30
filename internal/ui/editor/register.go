package editor

// Register is the yank/delete clipboard behind "dd", "yy" and "p" — vim's
// unnamed register. Like vim's registers (and unlike a pane's cursor or
// scroll position) it is shared by every View given the same one via
// SetRegister, so a line cut in one split can be put in another; a View
// nobody shares one with gets its own private register from NewView and
// behaves exactly as if registers were per-pane.
//
// Contents are linewise — whole lines, no partial-line fragment — because
// every gesture that writes here operates on whole lines. That is why there
// is no charwise flag to consult: adding "dw"/"yw" later is what would need
// one, along with a put that splices into the current line instead of
// between lines.
//
// This is deliberately NOT the operating system clipboard: nothing here
// shells out to pbcopy or speaks OSC 52, so kiwi's cut/copy/put stay
// entirely inside kiwi, the same way its git and language-server work is
// kept out of this package (see View.BlameFunc).
type Register struct {
	lines []string
}

// NewRegister returns an empty register — a put from it is a no-op until
// something is yanked or deleted into it.
func NewRegister() *Register {
	return &Register{}
}

// Set replaces the register's contents with a copy of lines, so later
// mutation of the buffer those lines came from can't change what's held
// here.
func (r *Register) Set(lines []string) {
	r.lines = append([]string(nil), lines...)
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

// yankLine implements vim's "yy": copies the cursor's line into the
// register, leaving the buffer and the cursor entirely alone. No undo entry,
// since nothing changed.
func (v *View) yankLine(t *tab) {
	if t.buf == nil {
		return
	}
	v.register.Set([]string{t.buf.Lines[t.cursorLn]})
}

// deleteLine implements vim's "dd": cuts the cursor's line — into the
// register, like vim (a delete IS a cut; "p" puts it back) — and leaves the
// cursor at the start of whatever line moved up into its place, or of the
// new last line if the deleted one was last. Recorded as its own undo entry,
// like "x"/"X": a single "dd" is already a complete change.
func (v *View) deleteLine(t *tab) {
	if t.buf == nil {
		return
	}
	before := snapshotTab(t)
	v.register.Set([]string{t.buf.DeleteLine(t.cursorLn)})
	t.cursorCol = 0
	v.pushUndoIfChanged(t, before)
	v.onBufferEdited(t)
	v.clamp(t) // the deleted line may have been the last one
}

// putAfter implements vim's linewise "p": inserts the register's lines below
// the cursor's line and moves the cursor to the start of the first of them.
// A no-op on an empty register. Its own undo entry, same as deleteLine.
func (v *View) putAfter(t *tab) {
	if t.buf == nil {
		return
	}
	lines := v.register.Lines()
	if len(lines) == 0 {
		return
	}
	before := snapshotTab(t)
	t.buf.InsertLines(t.cursorLn+1, lines)
	t.cursorLn++
	t.cursorCol = 0
	v.pushUndoIfChanged(t, before)
	v.onBufferEdited(t)
	v.clamp(t)
}
