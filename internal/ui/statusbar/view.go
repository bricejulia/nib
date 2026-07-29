// Package statusbar is a single-line, full-width pane meant to sit at the
// bottom of the window tree (a Fixed(1) leaf) and display information
// pulled from other panes — e.g. the editor's cursor position.
package statusbar

import (
	"strings"

	"github.com/bricejulia/kiwi/internal/layout"
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
	if len(right) > cols {
		right = right[len(right)-cols:]
	}

	// The right side (cursor position, git status, ...) is the more
	// load-bearing of the two, so on a narrow terminal it wins: the
	// hint gets truncated first, then dropped entirely once there's no
	// room left for it.
	avail := cols - len(right)
	left := v.Hint
	if avail < 0 {
		avail = 0
	}
	if len(left) > avail {
		left = left[:avail]
	}

	pad := cols - len(left) - len(right)
	line := left + strings.Repeat(" ", pad) + right
	w.Println(0, layout.Segment{Text: line, Style: layout.Style{Attr: layout.AttrDim}})
}
