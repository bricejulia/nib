package editor

import (
	"fmt"
	"slices"

	"github.com/bricejulia/nib/internal/layout"
	"github.com/bricejulia/nib/internal/merge"
	"github.com/bricejulia/nib/internal/theme"
)

// conflictActions are the merge-conflict actions (see Scope), resolved by
// runConflictAction rather than runAction's main switch.
var conflictActions = []string{
	"conflict_take_left", "conflict_take_right", "conflict_take_both",
	"conflict_take_neither", "conflict_auto_resolve",
	"conflict_next", "conflict_prev", "conflict_unresolve",
}

// regionAt is the conflict region containing line ln of t, if any.
func regionAt(t *tab, ln int) (merge.Region, bool) {
	for _, r := range merge.Parse(t.buf.Lines) {
		if r.Contains(ln) {
			return r, true
		}
	}
	return merge.Region{}, false
}

// runConflictAction runs one of conflictActions on t. Reports false for
// any other action.
func (v *View) runConflictAction(t *tab, action string) bool {
	if !slices.Contains(conflictActions, action) {
		return false
	}
	if t.buf == nil {
		return true
	}
	switch action {
	case "conflict_next":
		v.jumpConflict(t, true)
		return true
	case "conflict_prev":
		v.jumpConflict(t, false)
		return true
	case "conflict_auto_resolve":
		v.autoResolve(t)
		return true
	case "conflict_unresolve":
		if v.UnresolveFunc == nil || t.path == "" {
			return true
		}
		if err := v.UnresolveFunc(t.path); err != nil {
			v.notice = "Unresolve failed: " + err.Error()
		} else {
			v.notice = "Marked as conflicted again"
		}
		return true
	}

	r, ok := regionAt(t, t.cursorLn)
	if !ok {
		v.notice = "Not in a merge conflict"
		return true
	}
	var repl []string
	switch action {
	case "conflict_take_left":
		repl = r.TakeLeft()
	case "conflict_take_right":
		repl = r.TakeRight()
	case "conflict_take_both":
		repl = r.TakeBoth()
	case "conflict_take_neither":
		repl = r.TakeNeither()
	}
	before := snapshotTab(t)
	t.buf.Restore(replaceRegions(t.buf.Lines, []merge.Region{r}, [][]string{repl}))
	t.cursorLn, t.cursorCol = r.Start, 0
	v.clamp(t)
	v.pushUndoIfChanged(t, before)
	v.onBufferEdited(t)
	v.noteConflictsLeft(t)
	return true
}

// replaceRegions is lines with each of regions (in order, not
// overlapping) swapped for the matching entry of repls.
func replaceRegions(lines []string, regions []merge.Region, repls [][]string) []string {
	out := make([]string, 0, len(lines))
	prev := 0
	for i, r := range regions {
		out = append(out, lines[prev:r.Start]...)
		out = append(out, repls[i]...)
		prev = r.End + 1
	}
	out = append(out, lines[prev:]...)
	if len(out) == 0 {
		out = []string{""} // a Buffer is never zero lines — see DeleteLines
	}
	return out
}

// autoResolve is the magic wand: every region of t's file that
// merge.Region.AutoResolve can settle is settled, as one undoable edit.
// It needs each region's base text; a file conflicted in git's default
// style has none, so for a file with no unsaved edits it first asks git to
// rewrite the conflicts with their base (see RegenerateConflictsFunc).
func (v *View) autoResolve(t *tab) {
	regions := merge.Parse(t.buf.Lines)
	if len(regions) == 0 {
		v.notice = "No merge conflicts in this file"
		return
	}
	if !slices.ContainsFunc(regions, merge.Region.HasBase) {
		if t.buf.Dirty || v.RegenerateConflictsFunc == nil || t.path == "" {
			v.notice = "Conflicts have no base version: save first, or set merge.conflictStyle = zdiff3"
			return
		}
		if err := v.RegenerateConflictsFunc(t.path); err != nil {
			v.notice = "Could not get the base version: " + err.Error()
			return
		}
		if err := t.buf.Reload(); err != nil {
			v.notice = "Reload failed: " + err.Error()
			return
		}
		v.RefreshBuffer(t.buf)
		regions = merge.Parse(t.buf.Lines)
	}

	var solved []merge.Region
	var repls [][]string
	for _, r := range regions {
		if repl, ok := r.AutoResolve(); ok {
			solved = append(solved, r)
			repls = append(repls, repl)
		}
	}
	if len(solved) == 0 {
		v.notice = fmt.Sprintf("Nothing to resolve automatically: %s left", plainCount(len(regions), "conflict"))
		return
	}
	before := snapshotTab(t)
	t.buf.Restore(replaceRegions(t.buf.Lines, solved, repls))
	v.clamp(t)
	v.pushUndoIfChanged(t, before)
	v.onBufferEdited(t)
	left := len(regions) - len(solved)
	if left == 0 {
		v.notice = fmt.Sprintf("Resolved all %s — save to mark the file resolved", plainCount(len(solved), "conflict"))
		return
	}
	v.notice = fmt.Sprintf("Resolved %d of %d conflicts: %d left", len(solved), len(regions), left)
	if r, ok := firstRegionFrom(t, 0, true); ok {
		t.cursorLn, t.cursorCol = r.Start, 0
	}
}

// noteConflictsLeft reports how many regions remain after a resolution.
func (v *View) noteConflictsLeft(t *tab) {
	switch n := len(merge.Parse(t.buf.Lines)); n {
	case 0:
		v.notice = "No conflicts left — save to mark the file resolved"
	default:
		v.notice = plainCount(n, "conflict") + " left"
	}
}

// firstRegionFrom is the first region starting after line ln going
// forward (or before it going backward).
func firstRegionFrom(t *tab, ln int, forward bool) (merge.Region, bool) {
	regions := merge.Parse(t.buf.Lines)
	if forward {
		for _, r := range regions {
			if r.Start >= ln {
				return r, true
			}
		}
		return merge.Region{}, false
	}
	for i := len(regions) - 1; i >= 0; i-- {
		if regions[i].Start <= ln {
			return regions[i], true
		}
	}
	return merge.Region{}, false
}

// jumpConflict moves to the next (or previous) conflict region in this
// file; past the last one, OnConflictFileDone may move on to the next
// conflicted file.
func (v *View) jumpConflict(t *tab, forward bool) {
	from := t.cursorLn + 1
	if !forward {
		from = t.cursorLn - 1
		if r, ok := regionAt(t, t.cursorLn); ok {
			from = r.Start - 1
		}
	}
	if r, ok := firstRegionFrom(t, from, forward); ok {
		v.pushJump(t)
		t.cursorLn, t.cursorCol = r.Start, 0
		return
	}
	if v.OnConflictFileDone != nil && v.OnConflictFileDone(t.path, forward) {
		return
	}
	// No other conflicted file to move to: wrap within this one.
	var r merge.Region
	var ok bool
	if forward {
		r, ok = firstRegionFrom(t, 0, true)
	} else {
		r, ok = firstRegionFrom(t, len(t.buf.Lines), false)
	}
	if !ok {
		v.notice = "No merge conflicts"
		return
	}
	v.pushJump(t)
	t.cursorLn, t.cursorCol = r.Start, 0
}

// JumpToConflict puts the active tab's cursor on its first conflict region
// (or its last, with last set), for a caller that just opened a conflicted
// file. Reports whether there was one.
func (v *View) JumpToConflict(last bool) bool {
	t := v.activeTab()
	if t == nil || t.buf == nil {
		return false
	}
	var r merge.Region
	var ok bool
	if last {
		r, ok = firstRegionFrom(t, len(t.buf.Lines), false)
	} else {
		r, ok = firstRegionFrom(t, 0, true)
	}
	if ok {
		t.cursorLn, t.cursorCol = r.Start, 0
	}
	return ok
}

// SetNotice shows msg in the status line until the next key, like the
// notices this pane's own actions leave.
func (v *View) SetNotice(msg string) { v.notice = msg }

// conflictRender is what renderBody needs to draw conflict regions.
type conflictRender struct {
	regions []merge.Region
	hint    string // appended to each region's "<<<<<<<" line
}

// conflictHint names the keys bound to the take-a-side actions, from the
// live keymap so a rebind shows up here.
func (v *View) conflictHint() string {
	keys := Scope.Actions.KeysFor(v.keymap)
	hint := ""
	for _, a := range []struct{ id, label string }{
		{"conflict_take_left", "left"},
		{"conflict_take_right", "right"},
		{"conflict_take_both", "both"},
		{"conflict_take_neither", "neither"},
		{"conflict_auto_resolve", "auto"},
	} {
		if k := keys[a.id]; len(k) > 0 {
			if hint != "" {
				hint += " · "
			}
			hint += k[0] + " " + a.label
		}
	}
	return hint
}

func conflictRole(s merge.Side) theme.Role {
	switch s {
	case merge.SideBase:
		return theme.ConflictBase
	case merge.SideRight:
		return theme.ConflictRight
	default:
		return theme.ConflictLeft
	}
}

// conflictLook is how one line of a conflict region renders.
type conflictLook struct {
	in     bool             // the line is inside a region at all
	marker bool             // it is one of the region's marker lines
	segs   []layout.Segment // replaces the line's highlighting; nil keeps it
	hint   *layout.Segment  // shown after the opening marker's text
	gutter layout.Segment   // the side's bar, for the git-marker column
}

// conflictLine is how line ln renders if it is inside one of cr's
// regions: a marker line becomes a full-width colored bar, a base line is
// dimmed, and every line gets its side's bar in the gutter. The key hint
// is kept apart from segs so whitespace rendering never dots its spaces.
func conflictLine(cr conflictRender, ln int, line string) conflictLook {
	for _, r := range cr.regions {
		if !r.Contains(ln) {
			continue
		}
		side := r.SideOf(ln)
		color := theme.Get(conflictRole(side))
		look := conflictLook{in: true, gutter: layout.Segment{Text: "▌", Style: layout.Style{Foreground: color}}}
		switch {
		case r.IsMarkerLine(ln):
			style := layout.Style{Background: color, Attr: layout.AttrBold}
			look.marker = true
			look.segs = []layout.Segment{{Text: line, Style: style}}
			if ln == r.Start && cr.hint != "" {
				look.hint = &layout.Segment{Text: "   " + cr.hint, Style: style}
			}
		case side == merge.SideBase:
			look.segs = []layout.Segment{{Text: line, Style: layout.Style{Attr: layout.AttrDim}}}
		}
		return look
	}
	return conflictLook{}
}
