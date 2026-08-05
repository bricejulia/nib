package editor

import (
	"fmt"
	"os"
	"strings"

	"github.com/bricejulia/nib/internal/layout"
	"github.com/bricejulia/nib/internal/textfile"
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

	// Charset and EOL are the file's on-disk encoding and line-ending
	// style, detected once at Load and reproduced by Save — see
	// internal/textfile. Their zero values (textfile.UTF8, textfile.LF)
	// are today's defaults, so a Buffer{} literal that never sets these
	// (as most tests still do) behaves exactly as before. Source/Lines
	// stay plain UTF-8 split on "\n" regardless of either field — only
	// Load and Save ever convert to/from the on-disk form.
	Charset textfile.Charset
	EOL     textfile.EOL

	// IndentUseSpaces and IndentWidth are this file's Tab-key behavior:
	// whether Tab inserts spaces (IndentWidth of them) or a literal tab
	// character, and the width used for cursor/display math either way.
	// Unlike Charset/EOL (derived once at Load from the file's own bytes),
	// these come from the language-keyed config a View holds (see
	// View.tabModes) — Load has no access to that, so View.Open derives
	// them the first time it opens this Buffer (IndentWidth == 0 is the
	// "not derived yet" sentinel; a real width is always > 0). A Buffer
	// shared by two split panes on the same file keeps one indent setting
	// for both, same as Charset/EOL.
	IndentUseSpaces bool
	IndentWidth     int

	// highlighted is real tree-sitter output (see treesitter.go), one
	// entry per Lines index, raw/not-tab-expanded — nil (as a whole, or
	// per-line) means "use the highlightLine heuristic instead", the
	// fallback for files whose language isn't recognized. Lives on Buffer
	// rather than on a per-tab view of it because it's a pure function of
	// this buffer's own content: with the same Buffer now potentially
	// shown in more than one pane (see BufferStore), a per-tab cache would
	// go stale in every OTHER tab the moment just one of them re-highlights
	// after an edit.
	//
	// A full re-parse is far too slow to run on a keystroke — measured at
	// 236ms per key on an 1800-line Go file, and 8-12x worse than a clean
	// parse whenever the file is momentarily unparseable, which while
	// typing it nearly always is. So edits do NOT recompute this: they
	// keep it index-aligned with Lines via spliceHighlight, nil-ing only
	// the lines they touched (which renderBody then draws with the cheap
	// highlightLine heuristic), and hand the new text to the background
	// highlighter, which replaces this wholesale when it finishes. See
	// Highlighter and View.submitHighlight.
	highlighted [][]layout.Segment

	// rev counts content changes, so a highlight computed in the
	// background can tell whether it is still describing this buffer's
	// current text (see ApplyHighlightResult). Bumped by resync — i.e. by
	// every mutation, from any pane — and by Repath, since a rename can
	// change the language a pending result was computed for.
	rev uint64

	// undoStack/redoStack hold one undoEntry (see view.go) per completed
	// Insert session or single-key Normal-mode edit — vim's own undo
	// granularity, and like vim, a property of the buffer, not of
	// whichever pane happens to be looking at it: undo in one pane on a
	// buffer undoes an edit committed from any pane showing that same
	// buffer. The in-progress (not yet committed) session's snapshot
	// stays on tab, not here — see tab.insertSnapshot's doc comment.
	undoStack, redoStack []undoEntry
}

// maxLoadableFileSize bounds how large a file Load will read into memory
// at all. Load has no other size check anywhere below this — the whole
// file becomes one []byte, then one string, then a slice of line strings —
// so without a cap here, a file large enough (even a well-formed one) can
// exhaust memory just being opened. Comfortably above any source file
// anyone hand-edits, and rejected up front via Stat rather than after an
// os.ReadFile that's already paid the cost this exists to avoid.
// A var, like highlightTimeoutMicros/maxTreeSitterBytes, only so a test can
// shrink it without writing an actual 100MB fixture to disk.
var maxLoadableFileSize int64 = 100 << 20 // 100MB

// humanBytes formats n as whichever of B/KB/MB/GB reads most naturally,
// for the maxLoadableFileSize error message above — just enough precision
// to be legible, not a general-purpose formatter.
func humanBytes(n int64) string {
	const unit = 1024
	if n < unit {
		return fmt.Sprintf("%dB", n)
	}
	div, exp := int64(unit), 0
	for m := n / unit; m >= unit; m /= unit {
		div *= unit
		exp++
	}
	return fmt.Sprintf("%.1f%cB", float64(n)/float64(div), "KMGTPE"[exp])
}

// Load reads path into a Buffer.
func Load(path string) (*Buffer, error) {
	mode := os.FileMode(defaultSaveMode)
	if info, err := os.Stat(path); err == nil {
		mode = info.Mode()
		if info.Size() > maxLoadableFileSize {
			return nil, fmt.Errorf("file too large to open (%s, limit %s)",
				humanBytes(info.Size()), humanBytes(maxLoadableFileSize))
		}
	}
	data, err := os.ReadFile(path)
	if err != nil {
		return nil, err
	}
	text, charset, err := textfile.Decode(data)
	if err != nil {
		return nil, err
	}
	lines, eol := textfile.SplitLines(text)
	source := strings.Join(lines, "\n")
	return &Buffer{
		Lines: lines, Path: path, Source: []byte(source), mode: mode,
		saved:   append([]string(nil), lines...),
		Charset: charset, EOL: eol,
	}, nil
}

// Repath points the buffer at newPath — what Save writes to from now on —
// and drops whatever is derived from the path.
//
// Today that's tree-sitter highlighting, which is keyed on the file
// extension (see highlightBuffer): a rename can change the language
// outright (.txt -> .go), so the cached highlight is discarded rather than
// kept, and rev is bumped so a result already in flight for the OLD
// language can't land on top of the new one. Rebuilding it is the caller's
// job — see View.Repath, which submits the buffer for re-highlighting once
// the store has accepted the move.
//
// Deliberately NOT re-stat'ed: mode is the file's own permission bits,
// which a move carries with the inode, and saved stays the correct Dirty
// baseline because moving a file doesn't change its bytes.
func (b *Buffer) Repath(newPath string) {
	b.Path = newPath
	b.highlighted = nil
	b.rev++
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

	b.spliceHighlight(ln, 1, 1)
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

	b.spliceHighlight(ln, 1, 2)
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
		b.spliceHighlight(ln, 1, 1)
		b.resync()
		return ln, col - 1
	}
	if ln == 0 {
		return ln, col
	}

	joinCol := len([]rune(b.Lines[ln-1]))
	b.Lines[ln-1] += b.Lines[ln]
	b.Lines = append(b.Lines[:ln], b.Lines[ln+1:]...)
	b.spliceHighlight(ln-1, 2, 1)
	b.resync()
	return ln - 1, joinCol
}

// DeleteLine removes line ln entirely — the linewise counterpart to
// DeleteBackward's single rune, used by vim's "dd". It returns the removed
// line's text, so the caller can put it in a register. Out-of-range ln is a
// no-op returning "". A thin wrapper over DeleteLines(ln, ln) — see there
// for the "buffer never ends up with zero lines" invariant.
func (b *Buffer) DeleteLine(ln int) string {
	removed := b.DeleteLines(ln, ln)
	if len(removed) == 0 {
		return ""
	}
	return removed[0]
}

// DeleteLines removes the inclusive line range [startLn, endLn] in one
// resync/spliceHighlight pass — the multi-line generalization of DeleteLine,
// needed for vim's "3dd"/"d3d" and linewise operator+motion combinations.
// Deleting every line leaves the buffer holding one empty line rather than
// none, exactly like DeleteLine, since every other method here (and Load
// itself) relies on Lines never being empty; a caller that needs to tell
// "this delete emptied the whole buffer" apart from "there's one line left
// over" (e.g. "cc"'s guard against double-inserting a blank line) must check
// that before calling, not after. Arguments in the wrong order are swapped,
// matching TextBetween. Returns the removed lines, or nil for an
// out-of-range startLn (matching DeleteLine's "" for an out-of-range ln).
func (b *Buffer) DeleteLines(startLn, endLn int) []string {
	if endLn < startLn {
		startLn, endLn = endLn, startLn
	}
	if startLn < 0 || startLn >= len(b.Lines) {
		return nil
	}
	if endLn >= len(b.Lines) {
		endLn = len(b.Lines) - 1
	}

	removed := append([]string(nil), b.Lines[startLn:endLn+1]...)
	if startLn == 0 && endLn == len(b.Lines)-1 {
		b.Lines = []string{""}
		b.spliceHighlight(0, len(removed), 1) // the buffer survives as one empty line, so one entry does too
	} else {
		b.Lines = append(b.Lines[:startLn], b.Lines[endLn+1:]...)
		b.spliceHighlight(startLn, len(removed), 0)
	}
	b.resync()
	return removed
}

// DeleteRange removes the charwise range TextBetween reads — start
// inclusive, end exclusive — and returns the removed text in TextBetween's
// per-line shape (tail of the first line, whole lines between, head of the
// last). Same conventions as TextBetween: columns are raw rune indices,
// arguments in the wrong order are swapped rather than rejected, and
// out-of-range lines/columns clamp rather than panic. A no-op (returns nil)
// for an empty range.
func (b *Buffer) DeleteRange(startLn, startCol, endLn, endCol int) []string {
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
		runes := runesAt(startLn)
		removed := string(runes[startCol:endCol])
		b.Lines[startLn] = string(runes[:startCol]) + string(runes[endCol:])
		b.spliceHighlight(startLn, 1, 1)
		b.resync()
		return []string{removed}
	}

	startRunes := runesAt(startLn)
	endRunes := runesAt(endLn)
	removed := make([]string, 0, endLn-startLn+1)
	removed = append(removed, string(startRunes[startCol:]))
	for ln := startLn + 1; ln < endLn; ln++ {
		removed = append(removed, b.Lines[ln])
	}
	removed = append(removed, string(endRunes[:endCol]))

	b.Lines[startLn] = string(startRunes[:startCol]) + string(endRunes[endCol:])
	b.Lines = append(b.Lines[:startLn+1], b.Lines[endLn+1:]...)
	b.spliceHighlight(startLn, endLn-startLn+1, 1)
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

	b.spliceHighlight(at, 0, len(lines))
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
	b.rev++
}

// spliceHighlight keeps highlighted index-aligned with Lines across a
// structural edit: the removed entries at ln are replaced by inserted nil
// ones, matching the splice the caller just performed on Lines itself. The
// nils are what make an edit feel instant — renderBody draws a nil entry
// with the highlightLine heuristic (see its loop), so the edited lines
// stay readable at keystroke speed while the real tree-sitter result is
// computed in the background and swapped in whole.
//
// A no-op while highlighted is nil, which is the "nothing cached yet"
// state every buffer starts in and returns to on Restore/Repath: there is
// no alignment to maintain, and allocating an all-nil array here would
// only pretend otherwise.
//
// Out-of-range ln, or a removal running past the end, means the caller's
// splice and this one disagree — the alignment invariant is already lost,
// so the whole cache is dropped rather than a mangled one kept.
func (b *Buffer) spliceHighlight(ln, removed, inserted int) {
	if b.highlighted == nil {
		return
	}
	if ln < 0 || removed < 0 || inserted < 0 || ln+removed > len(b.highlighted) {
		b.highlighted = nil
		return
	}

	// The hot path — typing, which replaces one line with one line — needs
	// no splice at all: clearing in place keeps the common case free of an
	// allocation and a copy of one entry per line of the buffer, on every
	// single keystroke. Only edits that change the line COUNT (Enter, dd,
	// p, a backspace that joins) rebuild the array below.
	if removed == inserted {
		for i := ln; i < ln+removed; i++ {
			b.highlighted[i] = nil
		}
		return
	}

	spliced := make([][]layout.Segment, 0, len(b.highlighted)-removed+inserted)
	spliced = append(spliced, b.highlighted[:ln]...)
	spliced = append(spliced, make([][]layout.Segment, inserted)...)
	spliced = append(spliced, b.highlighted[ln+removed:]...)
	b.highlighted = spliced
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
// The highlight cache is dropped rather than spliced: a snapshot replaces
// the content wholesale, so there is no line-to-line mapping from the old
// array to the new one to preserve. Every line falls back to the
// highlightLine heuristic until the background result lands.
func (b *Buffer) Restore(lines []string) {
	b.Lines = lines
	b.highlighted = nil
	b.resync()
}

// Save writes the buffer's current contents back to Path, preserving the
// original file's permission bits (see Load) and its detected charset/EOL
// (see textfile), records that content as the new saved baseline, and
// clears Dirty.
func (b *Buffer) Save() error {
	data, err := textfile.Encode(textfile.JoinLines(b.Lines, b.EOL), b.Charset)
	if err != nil {
		return err
	}
	if err := os.WriteFile(b.Path, data, b.mode); err != nil {
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
