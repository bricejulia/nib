// Package debug is a read-only pane that renders debuglog's ring buffer —
// meant to be shown as a modal overlay (see ui.App.ShowOverlay/CloseOverlay)
// so a message logged anywhere in the codebase via debuglog.Debug/Info/
// Warn/Error can be inspected without a separate terminal or log file.
package debug

import (
	"fmt"

	"github.com/bricejulia/kiwi/internal/config"
	"github.com/bricejulia/kiwi/internal/debuglog"
	"github.com/bricejulia/kiwi/internal/layout"
)

// minLevels is the cycle order Tab steps through: everything, then
// progressively noisier messages filtered out.
var minLevels = []debuglog.Level{debuglog.LevelDebug, debuglog.LevelInfo, debuglog.LevelWarn, debuglog.LevelError}

// DefaultKeybinds are the debug log pane's built-in keybindings,
// overridable via the user config's "debug" scope (see internal/config).
var DefaultKeybinds = config.Defaults{
	{Trigger: "Esc", Action: "close"},
	{Trigger: "Tab", Action: "cycle_filter"},
	{Trigger: "Up", Action: "scroll_up"},
	{Trigger: "Down", Action: "scroll_down"},
	{Trigger: "PageUp", Action: "page_up"},
	{Trigger: "PageDown", Action: "page_down"},
	{Trigger: "Home", Action: "oldest"},
	{Trigger: "End", Action: "newest"},
}

// View renders EntriesFunc's result, newest at the bottom, auto-following
// as new messages arrive until the user scrolls up to look at history. Tab
// cycles a minimum-severity filter so a noisy Debug stream can be narrowed
// down to Info/Warn/Error without losing anything from the underlying log.
type View struct {
	offsetFromBottom int
	lastRows         int // last Render's visible row count, for PageUp/PageDown sizing
	minLevel         debuglog.Level

	// EntriesFunc is called on every Render to get the current messages to
	// display, oldest first. Set by New to debuglog.Entries; overridable
	// (e.g. in tests) so View doesn't depend on debuglog's package-level
	// state.
	EntriesFunc func() []debuglog.Entry

	// OnClose is called when Esc is pressed, dismissing the overlay.
	OnClose func()

	keymap map[string]string
}

// New creates a debug log view backed by debuglog's global ring buffer,
// initially showing every level.
func New() *View {
	return &View{EntriesFunc: debuglog.Entries, minLevel: debuglog.LevelDebug, keymap: DefaultKeybinds.Resolve(nil)}
}

// SetKeymap merges the user config's "debug" scope overrides on top of
// DefaultKeybinds, replacing the pane's active keymap.
func (v *View) SetKeymap(overrides map[string]string) {
	v.keymap = DefaultKeybinds.Resolve(overrides)
}

func (v *View) Title() string {
	if v.minLevel == debuglog.LevelDebug {
		return "Debug Log"
	}
	return fmt.Sprintf("Debug Log (>= %s, Tab to change)", v.minLevel)
}

// visibleEntries returns EntriesFunc's result filtered down to v.minLevel
// and above, oldest first.
func (v *View) visibleEntries() []debuglog.Entry {
	all := v.EntriesFunc()
	if v.minLevel == debuglog.LevelDebug {
		return all // nothing filtered out; avoid the allocation below
	}
	out := make([]debuglog.Entry, 0, len(all))
	for _, e := range all {
		if e.Level >= v.minLevel {
			out = append(out, e)
		}
	}
	return out
}

func (v *View) Render(w layout.Window) {
	_, rows := w.Size()
	w.Clear()
	v.lastRows = rows

	entries := v.visibleEntries()
	if len(entries) == 0 {
		w.Println(0, layout.Segment{Text: "(no log messages yet)", Style: layout.Style{Attr: layout.AttrDim}})
		return
	}

	// offsetFromBottom counts entries hidden below the visible window; 0
	// means pinned to the newest message.
	maxOffset := len(entries) - rows
	if maxOffset < 0 {
		maxOffset = 0
	}
	if v.offsetFromBottom > maxOffset {
		v.offsetFromBottom = maxOffset
	}

	start := len(entries) - rows - v.offsetFromBottom
	if start < 0 {
		start = 0
	}
	for i := 0; i < rows; i++ {
		idx := start + i
		if idx >= len(entries) {
			break
		}
		e := entries[idx]
		text := fmt.Sprintf("%s %-5s %s", e.Time.Format("15:04:05.000"), e.Level, e.Text)
		w.Println(i, layout.Segment{Text: text, Style: levelStyle(e.Level)})
	}
}

// levelStyle colors a line by severity: dim for Debug, default for Info,
// yellow for Warn, bold bright-red for Error — the same weighting
// gitstyle.Style uses for git statuses (louder color for louder severity).
func levelStyle(l debuglog.Level) layout.Style {
	switch l {
	case debuglog.LevelDebug:
		return layout.Style{Attr: layout.AttrDim}
	case debuglog.LevelWarn:
		return layout.Style{Foreground: layout.ColorYellow}
	case debuglog.LevelError:
		return layout.Style{Foreground: layout.ColorBrightRed, Attr: layout.AttrBold}
	default:
		return layout.Style{}
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
	case "cycle_filter":
		v.cycleMinLevel()
	case "scroll_up":
		v.offsetFromBottom++
	case "scroll_down":
		v.scrollDown(1)
	case "page_up":
		v.offsetFromBottom += page
	case "page_down":
		v.scrollDown(page)
	case "oldest":
		v.offsetFromBottom = len(v.visibleEntries())
	case "newest":
		v.offsetFromBottom = 0
	}
	return true
}

// cycleMinLevel steps to the next filter level, wrapping back to showing
// everything. Changing the filter resets the scroll position to the
// bottom — the old offset was counted against a different set of rows and
// no longer means anything meaningful.
func (v *View) cycleMinLevel() {
	for i, l := range minLevels {
		if l == v.minLevel {
			v.minLevel = minLevels[(i+1)%len(minLevels)]
			break
		}
	}
	v.offsetFromBottom = 0
}

func (v *View) scrollDown(n int) {
	v.offsetFromBottom -= n
	if v.offsetFromBottom < 0 {
		v.offsetFromBottom = 0
	}
}
