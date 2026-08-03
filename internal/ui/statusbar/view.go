// Package statusbar is a single-line, full-width pane meant to sit at the
// bottom of the window tree (a Fixed(1) leaf) and display information
// pulled from other panes — e.g. the editor's cursor position.
package statusbar

import (
	"strings"

	"github.com/bricejulia/nib/internal/layout"
	"github.com/bricejulia/nib/internal/textwidth"
)

// View displays a static left-aligned Hint (e.g. a shortcuts reminder)
// and whatever TextFunc returns right-aligned, re-evaluated on every
// render. It never claims focus (see Unfocusable) and never consumes a
// key, so Tab-cycling and all keyboard input skip straight past it — it's
// display-only.
type View struct {
	// Hint is shown left-aligned. It is static (set once, unlike
	// TextFunc) since it's meant as a fixed shortcuts reminder rather
	// than dynamic state.
	Hint string

	// TextFunc is called on every Render to get the current text to
	// display right-aligned. A nil TextFunc renders no right-hand text.
	TextFunc func() string
}

func New() *View { return &View{} }

func (v *View) Title() string { return "" }

// Unfocusable opts this View out of the Tab-cycle order (see
// layout.Unfocusable).
func (v *View) Unfocusable() bool { return true }

func (v *View) HandleKey(layout.Key) bool { return false }

func (v *View) Render(w layout.Window) {
	w.Clear()
	cols, _ := w.Size()
	if cols <= 0 {
		return
	}

	right := ""
	if v.TextFunc != nil {
		right = v.TextFunc()
	}
	if v.Hint == "" && right == "" {
		return // nothing to show: leave the bar blank rather than a padded empty line
	}
	// All the arithmetic below is in DISPLAY COLUMNS, not bytes. Both
	// strings routinely contain multi-byte glyphs — the hint's "·"
	// separators, and markers like the editor's language-server "●" — and
	// byte-slicing those cuts a UTF-8 sequence in half, which the terminal
	// renders as a replacement character. See internal/textwidth.
	rightWidth := textwidth.DisplayWidth(right)
	if rightWidth > cols {
		right = textwidth.SliceByDisplayColumn(right, rightWidth-cols, cols)
		rightWidth = textwidth.DisplayWidth(right)
	}

	// The right side (cursor position, git status, ...) is the more
	// load-bearing of the two, so on a narrow terminal it wins: the
	// hint gets truncated first, then dropped entirely once there's no
	// room left for it.
	avail := cols - rightWidth
	if avail < 0 {
		avail = 0
	}
	left := v.Hint
	if textwidth.DisplayWidth(left) > avail {
		left = textwidth.SliceByDisplayColumn(left, 0, avail)
	}

	pad := cols - textwidth.DisplayWidth(left) - rightWidth
	if pad < 0 {
		pad = 0
	}
	line := left + strings.Repeat(" ", pad) + right
	w.Println(0, layout.Segment{Text: line, Style: layout.Style{Attr: layout.AttrDim}})
}
