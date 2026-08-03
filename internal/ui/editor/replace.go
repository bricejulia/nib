package editor

import "strings"

// This file is Find & Replace in Path's apply step: given a confirmed set
// of occurrences to replace (see Occurrence), rewrite each affected file —
// through its shared Buffer if some pane has it open, straight to disk
// otherwise — and report what happened. The search itself (finding which
// files/lines/occurrences exist) lives in internal/ui/finder; this package
// only ever receives an already-decided, already-bounded list to act on.
//
// Runs synchronously on the caller's (UI) goroutine, deliberately: unlike
// the open-ended `git grep` search that already ran before any of this is
// called, the occurrence set here is fixed and bounded (finder caps a
// search at maxContentMatches lines), so this is the same order of
// blocking work Buffer.Save or filetree's create/rename/delete operations
// already do with no async/Post plumbing of their own.

// Occurrence identifies one confirmed, checked match to replace. Line is
// 1-based, matching finder's contentMatch. Ordinal is 0-based, and refers
// to FindOccurrences' left-to-right, case-insensitive scan of that line's
// CURRENT text at apply time — never a byte column, and never trusted from
// whatever the original search snapshot looked like: an open buffer's
// unsaved edits, or any change made between search and replace, can
// already disagree with what `git grep` saw on disk. An ordinal that no
// longer resolves on the current line is reported in Result.Skipped
// rather than misapplied to the wrong text.
type Occurrence struct {
	AbsPath string
	Line    int
	Ordinal int
}

// Result summarizes an Apply call. A failure on one file never aborts the
// rest — Failed collects per-path errors so 49 successful replacements
// aren't lost because the 50th file was unwritable, matching how
// View.saveActive logs and moves on rather than treating a single save
// failure as fatal to the session.
type Result struct {
	Replaced int
	Skipped  []Occurrence
	Failed   map[string]error // absolute path -> the error writing/rewriting it
}

// FindOccurrences returns the byte offset each case-insensitive,
// non-overlapping, left-to-right occurrence of search starts at within
// line. This is the single source of truth for "how many occurrences, and
// which is which" — both the results list (one row per occurrence) and
// rewriteLine (which occurrence a given ordinal refers to) are built from
// it, so the two can never disagree about a line's occurrence count.
func FindOccurrences(line, search string) []int {
	if search == "" {
		return nil
	}
	lower, lowerSearch := strings.ToLower(line), strings.ToLower(search)
	var offsets []int
	pos := 0
	for {
		idx := strings.Index(lower[pos:], lowerSearch)
		if idx < 0 {
			break
		}
		offsets = append(offsets, pos+idx)
		pos += idx + len(search)
	}
	return offsets
}

// rewriteLine splices replacement in place of the occurrences of search
// named in ordinals (0-based, per FindOccurrences), exactly as typed —
// no attempt to mirror the matched text's original casing, consistent
// with search being case-insensitive but replacement being literal.
// Ordinals past how many occurrences the line currently has are returned
// in missed rather than applied to the wrong position.
func rewriteLine(line, search, replacement string, ordinals []int) (rewritten string, applied, missed []int) {
	offsets := FindOccurrences(line, search)
	want := make(map[int]bool, len(ordinals))
	for _, o := range ordinals {
		if o < 0 || o >= len(offsets) {
			missed = append(missed, o)
			continue
		}
		want[o] = true
	}
	if len(want) == 0 {
		return line, nil, missed
	}

	var b strings.Builder
	last := 0
	for i, off := range offsets {
		if !want[i] {
			continue
		}
		b.WriteString(line[last:off])
		b.WriteString(replacement)
		last = off + len(search)
		applied = append(applied, i)
	}
	b.WriteString(line[last:])
	return b.String(), applied, missed
}

// occurrencesToSkip turns a rewriteLine result's missed ordinals into
// Occurrence values for a Result, sparing every caller the same
// three-line loop.
func occurrencesToSkip(path string, line1Based int, missed []int) []Occurrence {
	if len(missed) == 0 {
		return nil
	}
	out := make([]Occurrence, len(missed))
	for i, o := range missed {
		out[i] = Occurrence{AbsPath: path, Line: line1Based, Ordinal: o}
	}
	return out
}

// ReplaceLines rewrites the occurrences named in edits (0-based line ->
// ordinals) on path's buffer as ONE undo entry, if some tab in THIS pane
// has path open — a no-op (false, 0, nil) otherwise, the same shape as
// ApplyLineStatus.
//
// Unlike ApplyLineStatus/ApplyDiagnostics, which only ever set per-tab
// display state and so are harmless called on every pane sharing a
// buffer, this mutates the buffer's own shared Lines — callers must stop
// at the first pane that reports ok rather than fan out to every pane
// showing path (see Apply, which does exactly that via findPane).
//
// Deliberately leaves the buffer Dirty rather than saving it: every other
// edit in this pane (typing, x, undo/redo) leaves Dirty for an explicit
// :w, and auto-saving here would also break "one Ctrl+Z undoes the whole
// replace" — the file on disk would keep showing the replacement until a
// second explicit save even after undo reverted the in-memory buffer.
func (v *View) ReplaceLines(path, search, replacement string, edits map[int][]int) (ok bool, replaced int, skipped []Occurrence) {
	for _, t := range v.tabs {
		if t.path != path || t.buf == nil {
			continue
		}
		before := snapshotTab(t)
		newLines := append([]string(nil), t.buf.Lines...)
		for line, ordinals := range edits {
			if line < 0 || line >= len(newLines) {
				skipped = append(skipped, occurrencesToSkip(path, line+1, ordinals)...)
				continue
			}
			rewritten, applied, missed := rewriteLine(newLines[line], search, replacement, ordinals)
			newLines[line] = rewritten
			replaced += len(applied)
			skipped = append(skipped, occurrencesToSkip(path, line+1, missed)...)
		}
		t.buf.Restore(newLines)
		v.pushUndoIfChanged(t, before)
		v.onBufferEdited(t)
		return true, replaced, skipped
	}
	return false, 0, nil
}

// RewriteFile applies the same replacement straight to disk, for a path
// with no pane open anywhere. buf is a throwaway Buffer, never registered
// with any BufferStore and discarded once Save returns — it can't collide
// with a later legitimate Open of the same path, and reusing Load/Save
// gets mode-preservation and the newline-join logic for free instead of a
// second writer.
func RewriteFile(path, search, replacement string, edits map[int][]int) (replaced int, skipped []Occurrence, err error) {
	buf, err := Load(path)
	if err != nil {
		return 0, nil, err
	}
	newLines := append([]string(nil), buf.Lines...)
	for line, ordinals := range edits {
		if line < 0 || line >= len(newLines) {
			skipped = append(skipped, occurrencesToSkip(path, line+1, ordinals)...)
			continue
		}
		rewritten, applied, missed := rewriteLine(newLines[line], search, replacement, ordinals)
		newLines[line] = rewritten
		replaced += len(applied)
		skipped = append(skipped, occurrencesToSkip(path, line+1, missed)...)
	}
	buf.Restore(newLines)
	if !buf.Dirty {
		return replaced, skipped, nil // every requested occurrence here was stale; nothing to write
	}
	return replaced, skipped, buf.Save()
}

// Apply groups occurrences by file and, for each, replaces through the
// shared open Buffer if findPane reports one, else writes straight to
// disk — findPane is how the caller (cmd/nib/main.go, which owns the
// registry of live editor panes) answers "is this path open anywhere,
// and in which pane" without this package needing to know about panes,
// leaves, or the window tree at all.
func Apply(search, replacement string, occurrences []Occurrence, findPane func(absPath string) (*View, bool)) Result {
	byPath := map[string]map[int][]int{}
	for _, o := range occurrences {
		if byPath[o.AbsPath] == nil {
			byPath[o.AbsPath] = map[int][]int{}
		}
		byPath[o.AbsPath][o.Line-1] = append(byPath[o.AbsPath][o.Line-1], o.Ordinal)
	}

	res := Result{Failed: map[string]error{}}
	for path, edits := range byPath {
		if v, ok := findPane(path); ok {
			_, replaced, skipped := v.ReplaceLines(path, search, replacement, edits)
			res.Replaced += replaced
			res.Skipped = append(res.Skipped, skipped...)
			continue
		}
		replaced, skipped, err := RewriteFile(path, search, replacement, edits)
		if err != nil {
			res.Failed[path] = err
			continue
		}
		res.Replaced += replaced
		res.Skipped = append(res.Skipped, skipped...)
	}
	return res
}
