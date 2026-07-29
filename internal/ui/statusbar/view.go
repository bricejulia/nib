// Package statusbar is a single-line, full-width pane meant to sit at the
// bottom of the window tree (a Fixed(1) leaf) and display information
// pulled from other panes — e.g. the editor's cursor position.
package statusbar

import (
	"github.com/bricejulia/kiwi/internal/layout"
)

// View displays whatever TextFunc returns, right-aligned, re-evaluated on
// every render. It never claims focus (see Unfocusable) and never
// consumes a key, so Tab-cycling and all keyboard input skip straight past
// it — it's display-only.
type View struct {
	// TextFunc is called on every Render to get the current text to
	// display. A nil TextFunc renders an empty bar.
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
	if v.TextFunc == nil {
		return
	}
	text := v.TextFunc()
	if text == "" {
		return
	}

	cols, _ := w.Size()
	pad := cols - len(text)
	if pad < 0 {
		pad = 0
		if cols > 0 && len(text) > cols {
			text = text[len(text)-cols:]
		}
	}

	line := ""
	for i := 0; i < pad; i++ {
		line += " "
	}
	line += text
	w.Println(0, layout.Segment{Text: line, Style: layout.Style{Attr: layout.AttrDim}})
}
