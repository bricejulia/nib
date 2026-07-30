package editor

import (
	"strings"
	"unicode"

	"github.com/bricejulia/kiwi/internal/layout"
	"github.com/bricejulia/kiwi/internal/textwidth"
)

// renderPopup draws lines as a floating box on the rows directly below
// (anchorCol, anchorRow) — coordinates in this View's own window space, so
// callers pass what CursorPosition reports. selected, when >= 0, marks one
// row as highlighted; pass -1 for a popup with no selection.
//
// Shared by the autocomplete menu (see completion.go) and the diagnostic
// details popup (see diagnostics.go), because the fiddly parts are the same
// for both: clamp to the rows and columns actually left before the pane's
// edges, and pad every row out to the full window width. That padding is
// load-bearing — vaxis's Println only writes the cells its segments cover,
// so a short row would leave stale glyphs from the file content already
// drawn underneath showing through.
//
// If there isn't room below the anchor, fewer rows are drawn (no
// flip-above-anchor, a deferred nicety).
func renderPopup(w layout.Window, cols, rows, anchorCol, anchorRow int, lines []string, selected int) {
	maxRows := rows - anchorRow - 1
	if maxRows <= 0 || len(lines) == 0 {
		return
	}
	n := len(lines)
	if n > maxRows {
		n = maxRows
	}

	// Width is measured in display columns, not bytes: a message can contain
	// anything, including non-ASCII.
	width := 0
	for _, l := range lines[:n] {
		if dw := textwidth.DisplayWidth(l); dw > width {
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
		text := clampToWidth(lines[i], width)
		pad := width - textwidth.DisplayWidth(text)
		if pad < 0 {
			pad = 0
		}

		style := layout.Style{}
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
		w.Println(anchorRow+1+i, segs...)
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
