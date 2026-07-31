package editor

import (
	"os"
	"strings"

	"github.com/bricejulia/kiwi/internal/layout"
)

// defaultSaveMode is the permission Save falls back to when Load couldn't
// stat the original file (so Save still works even then, matching what
// os.WriteFile would use by default).
const defaultSaveMode = 0o644

// Buffer is the editor pane's in-memory text model: no rope, no
// piece-table, just a mutable slice of lines. Swapping in a rope later
// replaces Buffer and the View's line-fetch code only, not the View's
// scroll/cursor/tab/width logic.
type Buffer struct {
	Lines  []string // one entry per line, no trailing newline
	Path   string
	Source []byte // raw bytes Lines was split from — same text, pre-split;
	// tree-sitter's Highlight() needs byte offsets into this, not Lines.

	// Dirty is true when Lines differs from saved (see below). It is
	// always re-derived by resync/Restore, never set directly — undo/redo
	// can restore Lines to a state from before an intervening Save, and a
	// stored true/false flag carried along in that snapshot would go
	// stale the moment a Save happens in between (see resync).
	Dirty bool

	// saved is the content as of Load or the last successful Save — the
	// baseline Dirty is computed against. Comparing against this directly,
	// rather than tracking Dirty as a flag that gets carried through
	// undo/redo snapshots, is what keeps Dirty correct across a
	// save-undo-save-redo sequence: whichever of Lines/saved changes most
	// recently, Dirty always reflects the actual difference between them.
	saved []string

	// mode is the original file's permission bits, captured at Load so
	// Save doesn't silently drop them (e.g. a script's executable bit) by
	// writing back with some fixed default instead.
	mode os.FileMode

	// highlighted is real tree-sitter output (see treesitter.go), one
	// entry per Lines index, raw/not-tab-expanded — nil (as a whole, or
	// per-line) means "use the highlightLine heuristic instead", the
	// fallback for files whose language isn't recognized. Lives on Buffer
	// rather than on a per-tab view of it because it's a pure function of
	// this buffer's own content: with the same Buffer now potentially
	// shown in more than one pane (see BufferStore), a per-tab cache would
	// go stale in every OTHER tab the moment just one of them re-highlights
	// after an edit. Computed on first Open and recomputed in full after
	// every edit (see View.onBufferEdited) — not incremental, but simple and
	// correct; caching a tree-sitter *Tree and re-parsing incrementally is
	// the natural next optimization, once something needs the Tree anyway
	// (e.g. real go-to-definition/find-references built on the same parse).
	highlighted [][]layout.Segment

	// undoStack/redoStack hold one undoEntry (see view.go) per completed
	// Insert session or single-key Normal-mode edit — vim's own undo
	// granularity, and like vim, a property of the buffer, not of
	// whichever pane happens to be looking at it: undo in one pane on a
	// buffer undoes an edit committed from any pane showing that same
	// buffer. The in-progress (not yet committed) session's snapshot
	// stays on tab, not here — see tab.insertSnapshot's doc comment.
	undoStack, redoStack []undoEntry
}

// Load reads path into a Buffer.
func Load(path string) (*Buffer, error) {
	data, err := os.ReadFile(path)
	if err != nil {
		return nil, err
	}
	mode := os.FileMode(defaultSaveMode)
	if info, err := os.Stat(path); err == nil {
		mode = info.Mode()
	}
	text := string(data)
	text = strings.TrimSuffix(text, "\n")
	var lines []string
	if text == "" {
		lines = []string{""}
	} else {
		lines = strings.Split(text, "\n")
	}
	return &Buffer{Lines: lines, Path: path, Source: []byte(text), mode: mode, saved: append([]string(nil), lines...)}, nil
}

// Repath points the buffer at newPath — what Save writes to from now on —
// and recomputes whatever is derived from the path.
//
// Today that's tree-sitter highlighting, which is keyed on the file
// extension (see highlightBuffer): a rename can change the language
// outright (.txt -> .go), so the cached highlight has to be rebuilt, not
// merely kept. One full re-parse per rename is nothing next to the full
// re-parse this package already does after every edit (see
// View.onBufferEdited).
//
// Deliberately NOT re-stat'ed: mode is the file's own permission bits,
// which a move carries with the inode, and saved stays the correct Dirty
// baseline because moving a file doesn't change its bytes.
func (b *Buffer) Repath(newPath string) {
	b.Path = newPath
	b.highlighted = highlightBuffer(b)
}

// InsertText inserts s (which must not contain '\n') into line ln at rune
// index col and resyncs (Source, Dirty). It returns the rune index just
// past the inserted text, for repositioning the cursor.
func (b *Buffer) InsertText(ln, col int, s string) int {
	runes := []rune(b.Lines[ln])
	ins := []rune(s)

	merged := make([]rune, 0, len(runes)+len(ins))
	merged = append(merged, runes[:col]...)
	merged = append(merged, ins...)
	merged = append(merged, runes[col:]...)
	b.Lines[ln] = string(merged)

	b.resync()
	return col + len(ins)
}

// SplitLine splits line ln at rune index col into two lines: ln keeps the
// runes before col, a new line at ln+1 holds the rest — used for the
// Enter key. Rebuilt as a fresh slice (rather than shifting elements in
// place) to keep the insert-in-the-middle bookkeeping obviously correct.
func (b *Buffer) SplitLine(ln, col int) {
	runes := []rune(b.Lines[ln])
	before := string(runes[:col])
	after := string(runes[col:])

	lines := make([]string, 0, len(b.Lines)+1)
	lines = append(lines, b.Lines[:ln]...)
	lines = append(lines, before, after)
	lines = append(lines, b.Lines[ln+1:]...)
	b.Lines = lines

	b.resync()
}

// DeleteBackward deletes the rune immediately before (ln, col) — used for
// Backspace. If col == 0 and ln > 0, it instead joins line ln onto the end
// of line ln-1, removing the line break. A no-op at the very start of the
// buffer (ln == 0, col == 0) returns (ln, col) unchanged. It returns the
// resulting cursor position, since a join changes both ln and col.
func (b *Buffer) DeleteBackward(ln, col int) (newLn, newCol int) {
	if col > 0 {
		runes := []rune(b.Lines[ln])
		b.Lines[ln] = string(append(runes[:col-1], runes[col:]...))
		b.resync()
		return ln, col - 1
	}
	if ln == 0 {
		return ln, col
	}

	joinCol := len([]rune(b.Lines[ln-1]))
	b.Lines[ln-1] += b.Lines[ln]
	b.Lines = append(b.Lines[:ln], b.Lines[ln+1:]...)
	b.resync()
	return ln - 1, joinCol
}

// DeleteLine removes line ln entirely — the linewise counterpart to
// DeleteBackward's single rune, used by vim's "dd". It returns the removed
// line's text, so the caller can put it in a register. Deleting the only
// line leaves the buffer holding one empty line rather than none, since
// every other method here (and Load itself) relies on Lines never being
// empty. Out-of-range ln is a no-op returning "".
func (b *Buffer) DeleteLine(ln int) string {
	if ln < 0 || ln >= len(b.Lines) {
		return ""
	}
	removed := b.Lines[ln]
	if len(b.Lines) == 1 {
		b.Lines[0] = ""
	} else {
		b.Lines = append(b.Lines[:ln], b.Lines[ln+1:]...)
	}
	b.resync()
	return removed
}

// InsertLines splices lines into the buffer so that the first of them lands
// at index at, shifting whatever was there down — used by vim's linewise
// "p". at is clamped into [0, len(Lines)], so at == len(Lines) appends.
// Rebuilt as a fresh slice for the same reason SplitLine is: the
// insert-in-the-middle bookkeeping stays obviously correct, and the
// caller's slice can't end up aliased by the buffer.
func (b *Buffer) InsertLines(at int, lines []string) {
	if len(lines) == 0 {
		return
	}
	if at < 0 {
		at = 0
	}
	if at > len(b.Lines) {
		at = len(b.Lines)
	}

	merged := make([]string, 0, len(b.Lines)+len(lines))
	merged = append(merged, b.Lines[:at]...)
	merged = append(merged, lines...)
	merged = append(merged, b.Lines[at:]...)
	b.Lines = merged

	b.resync()
}

// TextBetween returns the text of the range starting at (startLn, startCol)
// and ending just before (endLn, endCol), as one string per line the range
// touches: the tail of the first line, whole lines in between, and the head
// of the last. A single-line range therefore returns exactly one string, and
// the caller joins with "\n" to get the flat text.
//
// Columns are RAW rune indices into Lines — the same units InsertText,
// SplitLine and DeleteBackward take — NOT the tab-expanded indices the
// cursor uses. Callers holding a cursor position must convert first (see
// rawIndexForExpandedCol); this method deliberately knows nothing about tab
// expansion, exactly as every other Buffer method doesn't.
//
// Arguments in the wrong order are swapped rather than rejected, so callers
// derived from a drag (where the end can be above the start) don't each have
// to normalise. Out-of-range lines are clamped, and cols are clamped to
// their own line's length, so a position past the end of a short line
// (legal for a rectangular drag) yields that line's text rather than a
// panic. Returns nil for an empty range.
func (b *Buffer) TextBetween(startLn, startCol, endLn, endCol int) []string {
	if len(b.Lines) == 0 {
		return nil
	}
	if endLn < startLn || (endLn == startLn && endCol < startCol) {
		startLn, startCol, endLn, endCol = endLn, endCol, startLn, startCol
	}
	startLn = clampIndex(startLn, len(b.Lines)-1)
	endLn = clampIndex(endLn, len(b.Lines)-1)

	runesAt := func(ln int) []rune { return []rune(b.Lines[ln]) }
	startCol = clampIndex(startCol, len(runesAt(startLn)))
	endCol = clampIndex(endCol, len(runesAt(endLn)))

	if startLn == endLn {
		if startCol == endCol {
			return nil
		}
		return []string{string(runesAt(startLn)[startCol:endCol])}
	}

	out := make([]string, 0, endLn-startLn+1)
	out = append(out, string(runesAt(startLn)[startCol:]))
	for ln := startLn + 1; ln < endLn; ln++ {
		out = append(out, b.Lines[ln])
	}
	out = append(out, string(runesAt(endLn)[:endCol]))
	return out
}

// clampIndex bounds i to [0, max].
func clampIndex(i, max int) int {
	if i < 0 {
		return 0
	}
	if i > max {
		return max
	}
	return i
}

// resync re-derives Source from Lines — the same join Load's initial split
// is the inverse of — and recomputes Dirty by comparing Lines against
// saved, so tree-sitter re-highlighting and Save both see the current
// text and the dirty marker stays correct regardless of how Lines got to
// its current state (a direct edit, or Restore rewinding/replaying one).
func (b *Buffer) resync() {
	b.Source = []byte(strings.Join(b.Lines, "\n"))
	b.Dirty = !linesEqual(b.Lines, b.saved)
}

// Restore replaces the buffer's contents with lines and resyncs — the
// snapshot-restore counterpart to InsertText/SplitLine/DeleteBackward,
// used by View's undo/redo to snap a buffer back to a recorded state.
// Deliberately does not take a Dirty flag to restore verbatim: Dirty is
// always recomputed against saved (see resync), since a Save that
// happened after the snapshot was taken would make a carried-along flag
// stale — restoring content from before that Save must still compare
// against what's actually on disk now, not against a snapshot of Dirty
// taken before the Save ever happened.
func (b *Buffer) Restore(lines []string) {
	b.Lines = lines
	b.resync()
}

// Save writes the buffer's current contents back to Path, preserving the
// original file's permission bits (see Load), records that content as the
// new saved baseline, and clears Dirty.
func (b *Buffer) Save() error {
	if err := os.WriteFile(b.Path, b.Source, b.mode); err != nil {
		return err
	}
	b.saved = append([]string(nil), b.Lines...)
	b.Dirty = false
	return nil
}

// linesEqual reports whether a and b hold the same lines in the same
// order.
func linesEqual(a, b []string) bool {
	if len(a) != len(b) {
		return false
	}
	for i := range a {
		if a[i] != b[i] {
			return false
		}
	}
	return true
}
