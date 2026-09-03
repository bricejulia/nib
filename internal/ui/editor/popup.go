package editor

import (
	"strings"
	"unicode"

	"github.com/bricejulia/nib/internal/layout"
	"github.com/bricejulia/nib/internal/textwidth"
)

// renderPopup draws lines as a floating box anchored at (anchorCol,
// anchorRow) — coordinates in this View's own window space, so callers pass
// what CursorPosition reports. selected, when >= 0, marks one row as
// highlighted; pass -1 for a popup with no selection.
//
// Shared by the autocomplete menu (see completion.go) and the diagnostic
// details popup (see diagnostics.go), because the fiddly parts are the same
// for both: clamp to the rows and columns actually left before the pane's
// edges, and pad every row out to the full window width. That padding is
// load-bearing — vaxis's Println only writes the cells its segments cover,
// so a short row would leave stale glyphs from the file content already
// drawn underneath showing through.
//
// Prefers drawing below the anchor (the original, and still the common,
// placement); see renderStyledPopup for what happens when there isn't room.
func renderPopup(w layout.Window, cols, rows, anchorCol, anchorRow int, lines []string, selected int) {
	styled := make([]popupLine, len(lines))
	for i, l := range lines {
		styled[i] = popupLine{Text: l}
	}
	renderStyledPopup(w, cols, rows, anchorCol, anchorRow, styled, selected)
}

// popupLine is one row of popup content with the style to draw it in — for
// popups whose rows don't all look alike, such as the git-hunk popup's
// red removals and green additions (see gitpopup.go). A zero Style renders
// exactly as the plain []string form does.
type popupLine struct {
	Text  string
	Style layout.Style
}

// renderStyledPopup is renderPopup with a per-row style. It carries all the
// actual logic; renderPopup is the unstyled convenience wrapper over it.
//
// Prefers drawing below the anchor row (today's original behavior,
// unchanged when there's room), but flips to draw ABOVE the anchor row
// instead when there isn't enough room below yet there is enough room
// above — e.g. a popup triggered near the bottom of the pane. Falls back to
// a downward clip, same as before this flip existed, only when neither
// direction has enough room for everything. Row 0 is always the tab bar
// (see View.CursorPosition), never available for popup content, so
// "above" tops out at row 1.
func renderStyledPopup(w layout.Window, cols, rows, anchorCol, anchorRow int, lines []popupLine, selected int) {
	total := len(lines)
	if total == 0 {
		return
	}

	below := rows - anchorRow - 1
	above := anchorRow - 1

	var startRow, n int
	switch {
	case below >= total:
		// Fits below already: today's normal, unclipped case.
		startRow, n = anchorRow+1, total
	case above >= total:
		// Doesn't fit below, but fits fully above: flip, ending just
		// above the anchor row instead of truncating (or, worse, hiding
		// the popup entirely).
		startRow, n = anchorRow-total, total
	default:
		// Neither direction has enough room for everything: fall back to
		// the original clipped-downward behavior.
		n = below
		if n < 0 {
			n = 0
		}
		startRow = anchorRow + 1
	}
	if n <= 0 {
		return
	}
	lines = lines[:n]

	// Width is measured in display columns, not bytes: a message can contain
	// anything, including non-ASCII.
	width := 0
	for _, l := range lines {
		if dw := textwidth.DisplayWidth(l.Text); dw > width {
			width = dw
		}
	}
	if avail := cols - anchorCol; width > avail {
		width = avail
	}
	if width <= 0 {
		return
	}

	for i := 0; i < n; i++ {
		text := clampToWidth(lines[i].Text, width)
		pad := width - textwidth.DisplayWidth(text)
		if pad < 0 {
			pad = 0
		}

		style := lines[i].Style
		if i == selected {
			style.Attr |= layout.AttrReverse
		}

		segs := []layout.Segment{
			{Text: strings.Repeat(" ", anchorCol)},
			{Text: text + strings.Repeat(" ", pad), Style: style},
		}
		if trailing := cols - anchorCol - width; trailing > 0 {
			segs = append(segs, layout.Segment{Text: strings.Repeat(" ", trailing)})
		}
		w.Println(startRow+i, segs...)
	}
}

// clampToWidth truncates s to at most width display columns, never
// splitting a double-width rune in half.
func clampToWidth(s string, width int) string {
	if textwidth.DisplayWidth(s) <= width {
		return s
	}
	var b strings.Builder
	used := 0
	for _, r := range s {
		rw := textwidth.DisplayWidth(string(r))
		if used+rw > width {
			break
		}
		b.WriteRune(r)
		used += rw
	}
	return b.String()
}

// wrapText breaks s into lines of at most width display columns, splitting
// on whitespace where possible. Used for popup content like diagnostic
// messages, which are prose and often longer than the space available.
//
// A single word longer than width is hard-split rather than allowed to
// overflow — losing the tail of a long identifier is worse than an awkward
// break.
func wrapText(s string, width int) []string {
	if width <= 0 {
		return nil
	}
	words := strings.FieldsFunc(s, unicode.IsSpace)
	if len(words) == 0 {
		return nil
	}

	var lines []string
	current := ""
	flush := func() {
		if current != "" {
			lines = append(lines, current)
			current = ""
		}
	}
	for _, word := range words {
		for textwidth.DisplayWidth(word) > width {
			// Hard-split an over-long word across rows.
			flush()
			head := clampToWidth(word, width)
			if head == "" {
				return lines // width too narrow for even one rune
			}
			lines = append(lines, head)
			word = word[len(head):]
		}
		if word == "" {
			continue
		}
		candidate := word
		if current != "" {
			candidate = current + " " + word
		}
		if textwidth.DisplayWidth(candidate) > width {
			flush()
			current = word
			continue
		}
		current = candidate
	}
	flush()
	return lines
}
