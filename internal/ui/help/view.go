// Package help is a read-only pane listing every keybinding in the app
// plus the current version — meant to be shown as a modal overlay (see
// ui.App.ShowOverlay/CloseOverlay), the same pattern internal/ui/debug
// uses for the debug log.
package help

import (
	"fmt"

	"github.com/bricejulia/kiwi/internal/layout"
)

// View renders the static keybinding reference in sections, with a
// scrollable viewport for terminals too small to show it all at once.
type View struct {
	lines    [][]layout.Segment
	topLine  int
	lastRows int // last Render's visible row count, for PageUp/PageDown sizing

	// OnClose is called when Esc is pressed, dismissing the overlay.
	OnClose func()
}

// New creates a help view showing version alongside every keybinding.
func New(version string) *View {
	return &View{lines: buildLines(version)}
}

func (v *View) Title() string { return "Help" }

// buildLines flattens version and sections into one styled segment slice
// per displayed row — computed once at New, since this content never
// changes at runtime.
func buildLines(version string) [][]layout.Segment {
	lines := [][]layout.Segment{
		{{Text: "kiwi " + version}},
	}
	for _, sec := range sections {
		lines = append(lines, nil) // blank separator
		lines = append(lines, []layout.Segment{{Text: sec.Title, Style: layout.Style{Attr: layout.AttrBold}}})
		for _, b := range sec.Bindings {
			text := fmt.Sprintf("  %-24s %s", b.Key, b.Desc)
			lines = append(lines, []layout.Segment{{Text: text}})
		}
	}
	return lines
}

func (v *View) Render(w layout.Window) {
	_, rows := w.Size()
	w.Clear()
	v.lastRows = rows

	maxTop := len(v.lines) - rows
	if maxTop < 0 {
		maxTop = 0
	}
	if v.topLine > maxTop {
		v.topLine = maxTop
	}
	if v.topLine < 0 {
		v.topLine = 0
	}

	for i := 0; i < rows; i++ {
		idx := v.topLine + i
		if idx >= len(v.lines) {
			break
		}
		w.Println(i, v.lines[idx]...)
	}
}

// HandleKey always reports the key consumed: a modal should never leak
// input through to whatever is behind it.
func (v *View) HandleKey(k layout.Key) bool {
	if k.EventType == layout.EventRelease {
		return true
	}

	page := v.lastRows
	if page <= 0 {
		page = 1
	}

	switch {
	case k.Named == layout.KeyEsc:
		if v.OnClose != nil {
			v.OnClose()
		}
	case k.Named == layout.KeyDown:
		v.topLine++
	case k.Named == layout.KeyUp:
		v.topLine--
	case k.Named == layout.KeyPageDown:
		v.topLine += page
	case k.Named == layout.KeyPageUp:
		v.topLine -= page
	case k.Named == layout.KeyHome:
		v.topLine = 0
	case k.Named == layout.KeyEnd:
		v.topLine = len(v.lines)
	}
	if v.topLine < 0 {
		v.topLine = 0
	}
	return true
}
