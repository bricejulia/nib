package editor

import (
	"errors"
	"fmt"
	"time"

	"github.com/bricejulia/nib/internal/debuglog"
	"github.com/bricejulia/nib/internal/layout"
	"github.com/bricejulia/nib/internal/textwidth"
	"github.com/bricejulia/nib/internal/theme"
	"github.com/bricejulia/nib/internal/vcs/gitblame"
	"github.com/bricejulia/nib/internal/vcs/gitstatus"
)

// This file is the editor pane's two git tooltips: "who last changed this
// line" (blame) and "what did this line replace" (the hunk under the
// cursor). Both are shaped like the diagnostic tooltip already here — a
// transient popup dismissed by the very next keypress, not another mode —
// and both get their data from a callback rather than shelling out to git
// themselves, the same discipline ApplyLineStatus follows: this View has no
// repository knowledge of its own.

// showBlame fills gitPopup with blame for the cursor's line, or leaves it
// empty if blame isn't available (no BlameFunc wired, or git declined —
// outside a repository, or a path git has never heard of).
//
// The git query runs inline on the UI goroutine, unlike the finder's
// content search and every LSP request, which marshal their results back
// through Post. That's a deliberate difference in kind: those are triggered
// incidentally (typing a character, opening a file) and can take
// arbitrarily long, whereas this one is a single explicit keypress asking
// for one line's history — and gitblame.Line stays cheap precisely so it
// can be answered this way, without a pending-request state machine for a
// tooltip the next keypress dismisses.
func (v *View) showBlame(t *tab) {
	if v.BlameFunc == nil || t.path == "" {
		return
	}
	info, err := v.BlameFunc(t.path, t.cursorLn+1) // gitblame counts lines from 1
	if err != nil {
		debuglog.Warn("blame %s:%d: %v", t.path, t.cursorLn+1, err)
		return
	}
	v.gitPopup = blamePopupLines(info, t.buf != nil && t.buf.Dirty, v.popupWidth())
}

// showLineDiff fills gitPopup with the diff hunk covering the cursor's
// line. An unchanged line still gets a popup saying so: the keypress was
// deliberate, and silence would be indistinguishable from the feature being
// broken.
func (v *View) showLineDiff(t *tab) {
	if v.HunkFunc == nil || t.path == "" {
		return
	}
	dim := layout.Style{Attr: layout.AttrDim}
	h, ok, err := v.HunkFunc(t.path, t.cursorLn)
	switch {
	case errors.Is(err, gitstatus.ErrUntracked):
		// Not an error worth logging — a brand new file is an ordinary
		// thing to have open, and "all of it is new" is the whole answer.
		v.gitPopup = []popupLine{{Text: "untracked file — every line is new", Style: dim}}
		return
	case err != nil:
		debuglog.Warn("line diff %s:%d: %v", t.path, t.cursorLn+1, err)
		return
	case !ok:
		v.gitPopup = []popupLine{{Text: "no change on this line", Style: dim}}
		return
	}

	v.gitPopup = hunkPopupLines(h, tabWidthOf(t), v.popupWidth())
	// Same caveat as blame: hunks are computed from the file on disk (see
	// gitstatus.FileHunkList), so unsaved edits can shift which hunk the
	// cursor's line really belongs to.
	if t.buf != nil && t.buf.Dirty {
		v.gitPopup = append(v.gitPopup, popupLine{
			Text:  "(unsaved changes — hunk may be offset)",
			Style: dim,
		})
	}
}

// popupWidth is how many display columns a tooltip anchored at the cursor
// has to work with: whatever is left between the cursor and the pane's
// right edge, as of the last Render (the only thing that knows the pane's
// width). Zero or less means there's no room, which the popup builders
// treat as "draw nothing".
func (v *View) popupWidth() int {
	col, _, ok := v.CursorPosition()
	if !ok {
		return 0
	}
	return v.lastWidth - col
}

// maxHunkPopupRows bounds the hunk popup so a large rewritten block can't
// cover the whole pane, matching maxDiagnosticPopupRows' reasoning. Split
// across the two sides so a hunk that both removes and adds a lot still
// shows some of each, rather than filling the budget with removals alone.
const (
	maxHunkPopupRows = 10
	maxHunkPopupSide = maxHunkPopupRows / 2
)

// blamePopupLines renders one line's blame as popup rows: the commit,
// author and date on the first row, its subject on the second.
//
// dirty reports whether the buffer has unsaved edits, which is worth saying
// out loud: git blames the file on DISK, so in a modified buffer the line
// numbers it answers about may no longer be the line the cursor is on (see
// gitblame.Line).
func blamePopupLines(info gitblame.Info, dirty bool, width int) []popupLine {
	if width <= 0 {
		return nil
	}

	var out []popupLine
	add := func(text string, style layout.Style) {
		for _, l := range wrapText(text, width) {
			out = append(out, popupLine{Text: l, Style: style})
		}
	}

	if info.Uncommitted {
		// Nothing else in Info is meaningful for a working-tree-only line,
		// so the placeholder author git supplies is never shown.
		add("Not committed yet (local change)", layout.Style{Attr: layout.AttrDim})
		return out
	}

	header := info.Commit
	if info.Author != "" {
		header += "  " + info.Author
	}
	if !info.Time.IsZero() {
		header += "  " + relativeTime(info.Time, time.Now()) + " (" + info.Time.Format("2006-01-02") + ")"
	}
	add(header, layout.Style{Foreground: theme.Get(theme.GitHeader)})
	if info.Summary != "" {
		add(info.Summary, layout.Style{})
	}
	if dirty {
		add("(unsaved changes — blame may be offset)", layout.Style{Attr: layout.AttrDim})
	}
	return out
}

// relativeTime renders how long before now t was, in the coarse "3 days
// ago" form git log --relative-date and every code-review UI use — the
// useful reading for blame is nearly always "recent or ancient", with the
// exact date available alongside it for when it isn't.
func relativeTime(t, now time.Time) string {
	d := now.Sub(t)
	if d < 0 {
		// A commit dated in the future (clock skew, or a rewritten date):
		// claiming "in -3 days" would be worse than declining to say.
		return "just now"
	}
	switch {
	case d < time.Minute:
		return "just now"
	case d < time.Hour:
		return plural(int(d.Minutes()), "minute")
	case d < 24*time.Hour:
		return plural(int(d.Hours()), "hour")
	case d < 30*24*time.Hour:
		return plural(int(d.Hours()/24), "day")
	case d < 365*24*time.Hour:
		return plural(int(d.Hours()/(24*30)), "month")
	default:
		return plural(int(d.Hours()/(24*365)), "year")
	}
}

func plural(n int, unit string) string {
	if n == 1 {
		return "1 " + unit + " ago"
	}
	return fmt.Sprintf("%d %ss ago", n, unit)
}

// hunkPopupLines renders the git hunk covering the cursor's line as popup
// rows: removed lines in red, added lines in green, under a dim header
// summarizing the change — a diff read at the scale of one edit, where
// show_file_diff's overlay reads the whole file's.
//
// Leading whitespace is kept as-is so an indentation-only change is
// visible, but tabs are expanded to tabWidth: a raw tab inside a popup row
// would be measured as one column here and drawn as several by the
// terminal, tearing the popup's padding (see renderStyledPopup).
func hunkPopupLines(h gitstatus.Hunk, tabWidth, width int) []popupLine {
	if width <= 0 {
		return nil
	}

	out := []popupLine{{
		Text:  hunkSummary(h),
		Style: layout.Style{Foreground: theme.Get(theme.GitHeader)},
	}}

	side := func(texts []string, marker string, style layout.Style) {
		shown := texts
		if len(shown) > maxHunkPopupSide {
			shown = shown[:maxHunkPopupSide]
		}
		for _, t := range shown {
			text := marker + textwidth.ExpandTabs(t, tabWidth)
			out = append(out, popupLine{Text: clampToWidth(text, width), Style: style})
		}
		if omitted := len(texts) - len(shown); omitted > 0 {
			out = append(out, popupLine{
				Text:  fmt.Sprintf("  … %d more", omitted),
				Style: layout.Style{Attr: layout.AttrDim},
			})
		}
	}
	side(h.Removed, "-", layout.Style{Foreground: theme.Get(theme.GitDeleted)})
	side(h.Added, "+", layout.Style{Foreground: theme.Get(theme.GitAdded)})
	return out
}

// hunkSummary describes a hunk in words, so a hunk with only one populated
// side still says what happened to the other one.
func hunkSummary(h gitstatus.Hunk) string {
	switch {
	case len(h.Removed) == 0:
		return fmt.Sprintf("@@ %s added", plainCount(len(h.Added), "line"))
	case len(h.Added) == 0:
		return fmt.Sprintf("@@ %s removed", plainCount(len(h.Removed), "line"))
	default:
		return fmt.Sprintf("@@ %s → %s",
			plainCount(len(h.Removed), "line"), plainCount(len(h.Added), "line"))
	}
}

func plainCount(n int, unit string) string {
	if n == 1 {
		return "1 " + unit
	}
	return fmt.Sprintf("%d %ss", n, unit)
}
