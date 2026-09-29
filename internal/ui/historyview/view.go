// Package historyview is a file's git history, meant to be shown as a modal
// overlay (see ui.App.ShowOverlay/CloseOverlay): the commits that touched
// the file listed at the top, and the diff the selected one made to it
// previewed underneath. Moving through the list moves the preview with it,
// so "when did this change, and how?" is a matter of pressing Down.
//
// Like diffview, it runs no git itself. The caller hands over the list via
// Open and each entry's diff on demand via DiffFunc (see
// cmd/nib/main.go's openFileHistory and internal/vcs/githistory) — asked
// for lazily, one entry at a time, because a long history's diffs are far
// more expensive to produce all at once than its list.
//
// The preview is a diffview.View rendered into the lower part of this
// view's window, so a commit's diff looks, and scrolls, exactly like the
// working-tree diff behind "D".
package historyview

import (
	"fmt"
	"strings"

	"github.com/bricejulia/nib/internal/config"
	"github.com/bricejulia/nib/internal/layout"
	"github.com/bricejulia/nib/internal/textwidth"
	"github.com/bricejulia/nib/internal/theme"
	"github.com/bricejulia/nib/internal/ui/diffview"
)

// DefaultKeybinds are the history overlay's built-in keybindings,
// overridable via the user config's "history" scope (see internal/config).
//
// The plain movement keys belong to the commit list, since stepping
// through commits is what this overlay is for; the preview gets the paging
// keys, plus J/K for a line at a time.
var DefaultKeybinds = config.Defaults{
	{Trigger: "Esc", Action: "close"},
	{Trigger: "Down", Action: "next_commit"},
	{Trigger: "j", Action: "next_commit"},
	{Trigger: "Up", Action: "prev_commit"},
	{Trigger: "k", Action: "prev_commit"},
	{Trigger: "Home", Action: "first_commit"},
	{Trigger: "End", Action: "last_commit"},
	{Trigger: "J", Action: "scroll_down"},
	{Trigger: "K", Action: "scroll_up"},
	{Trigger: "PageDown", Action: "page_down"},
	{Trigger: "Ctrl+d", Action: "page_down"},
	{Trigger: "PageUp", Action: "page_up"},
	{Trigger: "Ctrl+u", Action: "page_up"},
	{Trigger: "Right", Action: "peek_right"},
	{Trigger: "Left", Action: "peek_left"},
}

// Entry is one row of the list. ID is what identifies it in the row's
// first column — a short commit hash, or a placeholder like "working" for
// the uncommitted changes — and When is already formatted by the caller,
// which is the one that knows what kind of entry this is.
type Entry struct {
	ID, When, Author, Summary string
}

// minListRows is the fewest commit rows the list is given when there's room,
// and listFraction the share of the window it gets otherwise: the list is
// for choosing, the preview for reading, so the preview gets the most room.
const (
	minListRows  = 5
	listFraction = 3
)

// maxAuthorWidth caps the author column, so one long name can't push every
// row's summary off the edge.
const maxAuthorWidth = 20

// View is the history overlay's content.
type View struct {
	title   string
	entries []Entry

	cursor              int
	scrollTop, listRows int

	preview *diffview.View
	// diffs caches DiffFunc's answers by entry index, so moving back to an
	// entry already seen doesn't run git again.
	diffs map[int][]string

	// DiffFunc returns entry i's diff as display lines. It's called the
	// first time each entry is selected, including the first one on Open.
	DiffFunc func(i int) []string
	// OnClose is called when Esc is pressed, dismissing the overlay.
	OnClose func()

	keymap map[string]string
}

// New creates an empty history view; call Open before showing it.
func New() *View {
	p := diffview.New()
	p.EmptyText = "(no changes to this file in this commit)"
	return &View{preview: p, keymap: DefaultKeybinds.Resolve(nil)}
}

// SetKeymap merges the user config's "history" scope overrides on top of
// DefaultKeybinds, replacing the overlay's active keymap.
func (v *View) SetKeymap(overrides map[string]string) {
	v.keymap = DefaultKeybinds.Resolve(overrides)
}

// Open replaces the list with entries, newest first, selects the first
// one, and loads its diff. title names the file (shown in the overlay's
// border). Set DiffFunc before calling this.
func (v *View) Open(title string, entries []Entry) {
	v.title = title
	v.entries = entries
	v.cursor = 0
	v.scrollTop = 0
	v.diffs = map[int][]string{}
	v.preview.Show("", nil)
	if len(entries) > 0 {
		v.selectEntry(0)
	}
}

func (v *View) Title() string {
	if v.title == "" {
		return "History"
	}
	return "History: " + v.title
}

// selectEntry moves the cursor to i and shows its diff, fetching it
// through DiffFunc unless it's already cached.
func (v *View) selectEntry(i int) {
	if i < 0 || i >= len(v.entries) {
		return
	}
	v.cursor = i
	lines, ok := v.diffs[i]
	if !ok {
		if v.DiffFunc != nil {
			lines = v.DiffFunc(i)
		}
		v.diffs[i] = lines
	}
	v.preview.Show("", lines)
}

func (v *View) headerText() string {
	if len(v.entries) == 1 {
		return "1 commit"
	}
	return fmt.Sprintf("%d commits", len(v.entries))
}

func (v *View) Render(w layout.Window) {
	cols, rows := w.Size()
	w.Clear()

	if len(v.entries) == 0 {
		w.Println(0, layout.Segment{
			Text:  "(no history — the file is untracked, or this isn't a git repository)",
			Style: layout.Style{Attr: layout.AttrDim},
		})
		return
	}

	dim := layout.Style{Attr: layout.AttrDim}
	w.Println(0, layout.Segment{
		Text:  v.headerText() + " · j/k select · J/K PageUp/PageDown scroll the diff",
		Style: dim,
	})

	// Layout, top to bottom: header, list, divider, preview. The list takes
	// a third of the window (at least minListRows, at most every entry),
	// but always leaves the preview at least one row.
	listRows := rows / listFraction
	if listRows < minListRows {
		listRows = minListRows
	}
	if listRows > len(v.entries) {
		listRows = len(v.entries)
	}
	if maxList := rows - 3; listRows > maxList {
		listRows = maxList
	}
	if listRows < 1 {
		listRows = 1
	}
	v.listRows = listRows

	if v.cursor < v.scrollTop {
		v.scrollTop = v.cursor
	}
	if v.cursor >= v.scrollTop+listRows {
		v.scrollTop = v.cursor - listRows + 1
	}
	if v.scrollTop < 0 {
		v.scrollTop = 0
	}

	idWidth, authorWidth := 0, 0
	for _, e := range v.entries {
		idWidth = max(idWidth, textwidth.DisplayWidth(e.ID))
		authorWidth = max(authorWidth, textwidth.DisplayWidth(e.Author))
	}
	authorWidth = min(authorWidth, maxAuthorWidth)

	for i := 0; i < listRows; i++ {
		idx := v.scrollTop + i
		if idx >= len(v.entries) {
			break
		}
		w.Println(1+i, v.rowSegments(v.entries[idx], idx == v.cursor, idWidth, authorWidth, cols)...)
	}

	divider := 1 + listRows
	w.Println(divider, layout.Segment{Text: strings.Repeat("─", cols), Style: dim})

	v.preview.Render(&offsetWindow{w: w, top: divider + 1, rows: rows - divider - 1})
}

// rowSegments renders one entry as aligned columns — id, date, author,
// summary — padded to cols when selected so the highlight spans the row.
func (v *View) rowSegments(e Entry, selected bool, idWidth, authorWidth, cols int) []layout.Segment {
	segs := []layout.Segment{
		{Text: pad(e.ID, idWidth) + "  ", Style: layout.Style{Foreground: theme.Get(theme.GitHeader)}},
		{Text: e.When + "  ", Style: layout.Style{Attr: layout.AttrDim}},
		{Text: pad(truncate(e.Author, authorWidth), authorWidth) + "  "},
		{Text: e.Summary},
	}
	if !selected {
		return segs
	}
	used := 0
	for _, s := range segs {
		used += textwidth.DisplayWidth(s.Text)
	}
	if used < cols {
		segs = append(segs, layout.Segment{Text: strings.Repeat(" ", cols-used)})
	}
	for i := range segs {
		segs[i].Style.Attr |= layout.AttrReverse
	}
	return segs
}

func pad(s string, width int) string {
	if n := textwidth.DisplayWidth(s); n < width {
		return s + strings.Repeat(" ", width-n)
	}
	return s
}

// truncate shortens s to at most width display columns, marking the cut
// with an ellipsis.
func truncate(s string, width int) string {
	if textwidth.DisplayWidth(s) <= width || width <= 0 {
		return s
	}
	var b strings.Builder
	used := 0
	for _, r := range s {
		rw := textwidth.DisplayWidth(string(r))
		if used+rw > width-1 {
			break
		}
		b.WriteRune(r)
		used += rw
	}
	return b.String() + "…"
}

// HandleKey always reports the key consumed: a modal should never leak
// input through to whatever is behind it.
func (v *View) HandleKey(k layout.Key) bool {
	if k.EventType == layout.EventRelease {
		return true
	}

	switch v.keymap[k.String()] {
	case "close":
		if v.OnClose != nil {
			v.OnClose()
		}
	case "next_commit":
		v.selectEntry(v.cursor + 1)
	case "prev_commit":
		v.selectEntry(v.cursor - 1)
	case "first_commit":
		v.selectEntry(0)
	case "last_commit":
		v.selectEntry(len(v.entries) - 1)
	case "scroll_down":
		v.preview.ScrollBy(1)
	case "scroll_up":
		v.preview.ScrollBy(-1)
	case "page_down":
		v.preview.PageBy(1)
	case "page_up":
		v.preview.PageBy(-1)
	case "peek_right":
		v.preview.PeekBy(1)
	case "peek_left":
		v.preview.PeekBy(-1)
	}
	return true
}

// offsetWindow is the part of a layout.Window from row top down, rows tall:
// how the preview diffview draws below the list as if it had the whole
// window to itself.
type offsetWindow struct {
	w         layout.Window
	top, rows int
}

func (o *offsetWindow) Size() (int, int) {
	cols, _ := o.w.Size()
	return cols, max(o.rows, 0)
}

func (o *offsetWindow) Println(row int, segs ...layout.Segment) {
	if row < 0 || row >= o.rows {
		return
	}
	o.w.Println(o.top+row, segs...)
}

// Clear is a no-op: the parent window was already cleared at the top of
// Render, and clearing it again here would erase the list.
func (o *offsetWindow) Clear() {}
