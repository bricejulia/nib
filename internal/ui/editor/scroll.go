package editor

import (
	"github.com/bricejulia/nib/internal/layout"
	"github.com/bricejulia/nib/internal/ui/gitstyle"
	"github.com/bricejulia/nib/internal/vcs/gitstatus"
)

// scrollTabBarRows is how many rows sit above the scrollable text — row 0
// is always the tab bar (see CursorPosition's own "+1"), so the scrollbar's
// track has to start one row down too, or it would visually cover the tab
// bar instead of lining up with the text it represents.
const scrollTabBarRows = 1

// ScrollState implements layout.Scrollable. Viewport/Total mirror exactly
// what renderBody's own clamp (view.go, "if t.cursorLn >= t.topLine+rows")
// is computed against, so the scrollbar can never show a position the pane
// itself disagrees with.
func (v *View) ScrollState() layout.ScrollState {
	t := v.activeTab()
	if t == nil || t.buf == nil {
		return layout.ScrollState{}
	}
	viewport := v.lastHeight - scrollTabBarRows
	if viewport < 0 {
		viewport = 0
	}
	return layout.ScrollState{
		Top:       t.topLine,
		Viewport:  viewport,
		Total:     len(t.buf.Lines),
		RowOffset: scrollTabBarRows,
	}
}

// ScrollTo implements layout.ScrollTarget. Unlike every other pane's
// ScrollTo, this one also has to move the cursor: renderBody re-derives
// topLine from cursorLn on every frame ("if t.cursorLn < t.topLine ...",
// "if t.cursorLn >= t.topLine+rows ..."), so a bare assignment to topLine
// would be silently overwritten on the very next render the moment the
// cursor and the new topLine disagree. Moving the cursor into the new
// viewport (and clearing the selection, matching every other cursor-moving
// action's convention) is what makes the scroll actually stick.
func (v *View) ScrollTo(top int) {
	t := v.activeTab()
	if t == nil || t.buf == nil {
		return
	}
	viewport := v.lastHeight - scrollTabBarRows
	if viewport < 0 {
		viewport = 0
	}
	maxTop := len(t.buf.Lines) - viewport
	if maxTop < 0 {
		maxTop = 0
	}
	if top < 0 {
		top = 0
	}
	if top > maxTop {
		top = maxTop
	}
	t.topLine = top

	if viewport > 0 {
		moved := false
		if t.cursorLn < top {
			t.cursorLn = top
			moved = true
		} else if t.cursorLn >= top+viewport {
			t.cursorLn = top + viewport - 1
			moved = true
		}
		if moved {
			t.clearSelection()
		}
	}
	v.clamp(t)
}

// scrollMarkPriority ranks a line's git status for BucketMarks, used only
// when two changed lines land in the same track row and one has to win.
// Kept local to this package rather than added to gitstatus, matching how
// diagnosticMarker/diagnosticStyle already stay local — this ranking only
// ever matters for the scrollbar's ruler.
//
// Deletions rank highest: a silently-removed block is the easiest kind of
// change to miss when skimming a long file, since — unlike an addition or
// a modification — it leaves no lines of its own to draw your eye.
func scrollMarkPriority(s gitstatus.LineStatus) int {
	switch s {
	case gitstatus.LineDeletedBefore:
		return 2
	case gitstatus.LineAdded, gitstatus.LineModified:
		return 1
	default:
		return 0
	}
}

// ScrollMarks implements layout.ScrollMarker: one mark per changed line in
// the active tab, styled exactly as the gutter itself would (see
// gitstyle.LineStyle) so the ruler and the gutter never disagree about
// what a line's status means.
func (v *View) ScrollMarks() []layout.ScrollMark {
	t := v.activeTab()
	if t == nil || len(t.lineStatus) == 0 {
		return nil
	}
	marks := make([]layout.ScrollMark, 0, len(t.lineStatus))
	for line, status := range t.lineStatus {
		marks = append(marks, layout.ScrollMark{
			Line:     line,
			Priority: scrollMarkPriority(status),
			Style:    gitstyle.LineStyle(status),
		})
	}
	return marks
}
