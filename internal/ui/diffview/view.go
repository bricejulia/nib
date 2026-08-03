// Package diffview is a read-only pane that renders a unified diff —
// meant to be shown as a modal overlay (see ui.App.ShowOverlay/CloseOverlay)
// so "what have I changed in this file?" can be answered without leaving
// nib for a shell.
//
// It only displays lines handed to it via Show; producing them is the
// caller's job (see gitstatus.FileDiff), the same division of labor the
// file tree and finder use for git status.
package diffview

import (
	"strings"

	"github.com/bricejulia/nib/internal/config"
	"github.com/bricejulia/nib/internal/layout"
	"github.com/bricejulia/nib/internal/textwidth"
)

// DefaultKeybinds are the diff pane's built-in keybindings, overridable via
// the user config's "diff" scope (see internal/config).
var DefaultKeybinds = config.Defaults{
	{Trigger: "Esc", Action: "close"},
	{Trigger: "Up", Action: "scroll_up"},
	{Trigger: "k", Action: "scroll_up"},
	{Trigger: "Down", Action: "scroll_down"},
	{Trigger: "j", Action: "scroll_down"},
	{Trigger: "PageUp", Action: "page_up"},
	{Trigger: "PageDown", Action: "page_down"},
	{Trigger: "Home", Action: "top"},
	{Trigger: "End", Action: "bottom"},
	{Trigger: "Right", Action: "peek_right"},
	{Trigger: "Left", Action: "peek_left"},
}

// hScrollStep is how many display columns Left/Right shift the view, for
// reading past the right edge of a long line — the same "peek" gesture, and
// the same step, the finder uses.
const hScrollStep = 10

// View renders the diff lines given to it, scrolled from the top. Unlike
// debug.View (which tails a live log and so pins itself to the newest line),
// a diff is a fixed document read downwards from its first line.
type View struct {
	title   string
	lines   []string
	top     int
	hScroll int

	lastRows int // last Render's visible row count, for PageUp/PageDown sizing

	// OnClose is called when Esc is pressed, dismissing the overlay.
	OnClose func()

	keymap map[string]string
}

// New creates an empty diff view; call Show before displaying it.
func New() *View {
	return &View{keymap: DefaultKeybinds.Resolve(nil)}
}

// SetKeymap merges the user config's "diff" scope overrides on top of
// DefaultKeybinds, replacing the pane's active keymap.
func (v *View) SetKeymap(overrides map[string]string) {
	v.keymap = DefaultKeybinds.Resolve(overrides)
}

// Show replaces the displayed diff and scrolls back to the top. title names
// what is being diffed (shown in the overlay's border); lines is the diff
// body, which may be empty for an unchanged file.
func (v *View) Show(title string, lines []string) {
	v.title = title
	v.lines = lines
	v.top = 0
	v.hScroll = 0
}

func (v *View) Title() string {
	if v.title == "" {
		return "Diff"
	}
	return "Diff: " + v.title
}

func (v *View) Render(w layout.Window) {
	cols, rows := w.Size()
	w.Clear()
	v.lastRows = rows

	if len(v.lines) == 0 {
		w.Println(0, layout.Segment{
			Text:  "(no changes against HEAD)",
			Style: layout.Style{Attr: layout.AttrDim},
		})
		return
	}

	// Clamp here rather than in the key handler: only Render knows how many
	// rows there are to fill, and it's the same reason topLine is derived
	// during render in the editor pane.
	if maxTop := len(v.lines) - rows; v.top > maxTop {
		v.top = maxTop
	}
	if v.top < 0 {
		v.top = 0
	}

	// hScroll is clamped against the widest line actually on screen, so
	// peeking right can never run off past the content into blank space —
	// the finder applies the same policy to its selected row.
	widest := 0
	for i := 0; i < rows && v.top+i < len(v.lines); i++ {
		if dw := textwidth.DisplayWidth(v.lines[v.top+i]); dw > widest {
			widest = dw
		}
	}
	v.hScroll = textwidth.ClampScroll(v.hScroll, widest, cols)

	for i := 0; i < rows; i++ {
		idx := v.top + i
		if idx >= len(v.lines) {
			break
		}
		line := v.lines[idx]
		segs := []layout.Segment{{Text: line, Style: lineStyle(line)}}
		w.Println(i, textwidth.SliceSegmentsByDisplayColumn(segs, v.hScroll, cols)...)
	}
}

// lineStyle colors a diff line by its leading marker: green for additions,
// red for removals, cyan for hunk headers, dim for the file header — the
// conventional diff palette, and the same color-per-meaning mapping
// gitstyle uses for git status (green=added, red=deleted, cyan=renamed).
//
// The "+++"/"---" file-header lines are checked before the single-character
// "+"/"-" cases, since they start with those same characters but are
// metadata rather than content.
func lineStyle(line string) layout.Style {
	switch {
	case strings.HasPrefix(line, "+++"), strings.HasPrefix(line, "---"),
		strings.HasPrefix(line, "diff "), strings.HasPrefix(line, "index "),
		strings.HasPrefix(line, "new file"), strings.HasPrefix(line, "deleted file"),
		strings.HasPrefix(line, "similarity"), strings.HasPrefix(line, "rename "):
		return layout.Style{Attr: layout.AttrDim}
	case strings.HasPrefix(line, "@@"):
		return layout.Style{Foreground: layout.ColorCyan}
	case strings.HasPrefix(line, "+"):
		return layout.Style{Foreground: layout.ColorGreen}
	case strings.HasPrefix(line, "-"):
		return layout.Style{Foreground: layout.ColorRed}
	case strings.HasPrefix(line, `\`):
		// git's "\ No newline at end of file" note.
		return layout.Style{Attr: layout.AttrDim}
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
	case "scroll_up":
		v.scroll(-1)
	case "scroll_down":
		v.scroll(1)
	case "page_up":
		v.scroll(-page)
	case "page_down":
		v.scroll(page)
	case "top":
		v.top = 0
	case "bottom":
		// Clamped against the row count in Render, which is the only place
		// that knows it.
		v.top = len(v.lines)
	case "peek_right":
		v.hScroll += hScrollStep
	case "peek_left":
		v.hScroll -= hScrollStep
		if v.hScroll < 0 {
			v.hScroll = 0
		}
	}
	return true
}

// ScrollState implements layout.Scrollable.
func (v *View) ScrollState() layout.ScrollState {
	return layout.ScrollState{Top: v.top, Viewport: v.lastRows, Total: len(v.lines)}
}

// ScrollTo implements layout.ScrollTarget. Unlike the editor/file tree,
// v.top is never re-derived from anything else — Render only clamps it
// (see the comment there) — so this is a direct, no-followup assignment.
func (v *View) ScrollTo(top int) {
	maxTop := len(v.lines) - v.lastRows
	if maxTop < 0 {
		maxTop = 0
	}
	if top < 0 {
		top = 0
	}
	if top > maxTop {
		top = maxTop
	}
	v.top = top
}

func (v *View) scroll(delta int) {
	v.top += delta
	if v.top < 0 {
		v.top = 0
	}
	if v.top >= len(v.lines) {
		v.top = len(v.lines) - 1
	}
}
