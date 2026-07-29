// Package help is a read-only pane listing every keybinding in the app
// plus the current version — meant to be shown as a modal overlay (see
// ui.App.ShowOverlay/CloseOverlay), the same pattern internal/ui/debug
// uses for the debug log.
package help

import (
	"fmt"

	"github.com/bricejulia/kiwi/internal/config"
	"github.com/bricejulia/kiwi/internal/layout"
)

// DefaultKeybinds are the help overlay's built-in keybindings,
// overridable via the user config's "help" scope (see internal/config).
var DefaultKeybinds = config.Defaults{
	{Trigger: "Esc", Action: "close"},
	{Trigger: "Down", Action: "scroll_down"},
	{Trigger: "Up", Action: "scroll_up"},
	{Trigger: "PageDown", Action: "page_down"},
	{Trigger: "PageUp", Action: "page_up"},
	{Trigger: "Home", Action: "home"},
	{Trigger: "End", Action: "end"},
}

// View renders the static keybinding reference in sections, with a
// scrollable viewport for terminals too small to show it all at once.
type View struct {
	lines    [][]layout.Segment
	topLine  int
	lastRows int // last Render's visible row count, for PageUp/PageDown sizing

	// OnClose is called when Esc is pressed, dismissing the overlay.
	OnClose func()

	keymap map[string]string
}

// New creates a help view showing version alongside every keybinding.
func New(version string) *View {
	return &View{lines: buildLines(version), keymap: DefaultKeybinds.Resolve(nil)}
}

// SetKeymap merges the user config's "help" scope overrides on top of
// DefaultKeybinds, replacing the pane's active keymap.
func (v *View) SetKeymap(overrides map[string]string) {
	v.keymap = DefaultKeybinds.Resolve(overrides)
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

	switch v.keymap[k.String()] {
	case "close":
		if v.OnClose != nil {
			v.OnClose()
		}
	case "scroll_down":
		v.topLine++
	case "scroll_up":
		v.topLine--
	case "page_down":
		v.topLine += page
	case "page_up":
		v.topLine -= page
	case "home":
		v.topLine = 0
	case "end":
		v.topLine = len(v.lines)
	}
	if v.topLine < 0 {
		v.topLine = 0
	}
	return true
}
