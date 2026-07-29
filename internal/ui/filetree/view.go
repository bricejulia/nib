package filetree

import (
	"fmt"

	"github.com/bricejulia/kiwi/internal/layout"
	"github.com/bricejulia/kiwi/internal/textwidth"
	"github.com/bricejulia/kiwi/internal/ui/gitstyle"
	"github.com/bricejulia/kiwi/internal/vcs/gitstatus"
)

// hScrollStep is how many display columns Shift+Left/Shift+Right shift the
// view of a row that's wider than the pane. Plain Left/Right are already
// collapse/expand, so peeking at a long name needs a modifier here — the
// finder, where arrows are otherwise unused, just uses Left/Right directly.
const hScrollStep = 10

// View is the file-tree pane: a layout.View backed by a lazily-loaded Node
// tree, flattened to a row cache that Render and HandleKey index into
// directly. Render never walks the *Node tree — only the cached []Row
// slice — so its cost is bounded by what's visible, not by project size.
type View struct {
	root      *Node
	rows      []Row
	cursor    int
	scrollTop int
	hScroll   int // display columns; how far the selected row is peeked right
	dirty     bool

	// OnOpen is called with the absolute path when the cursor activates a
	// file row. Set by the caller (cmd/kiwi/main.go) to wire in the
	// editor pane.
	OnOpen func(path string)
}

// New creates a View rooted at absPath. The root's immediate children are
// not loaded from disk until the first Render or HandleKey call.
func New(absPath string) *View {
	return &View{root: NewRoot(absPath), dirty: true}
}

func (v *View) Title() string { return "Files" }

// Refresh invalidates every currently expanded directory so the next
// Render/HandleKey re-reads it from disk, then marks the tree dirty.
//
// fsnotify's debounced signal only says "something under the project
// changed", not which path — so rather than tracking per-path dirtiness,
// every expanded (i.e. currently visible) directory is treated as
// possibly stale and re-scanned. Collapsed directories are left alone,
// preserving the lazy-load contract: only what's visible gets re-read.
func (v *View) Refresh() {
	invalidateExpanded(v.root)
	v.dirty = true
}

// invalidateExpanded resets Loaded on every expanded directory so the next
// reloadExpanded call re-reads it from disk.
func invalidateExpanded(n *Node) {
	for _, c := range n.Children {
		if c.IsDir && c.Expanded && c.Loaded {
			c.Loaded = false
			invalidateExpanded(c)
		}
	}
}

// reloadExpanded calls EnsureLoaded on n and, recursively, on every
// expanded child directory — a no-op for anything not invalidated by
// Refresh, since EnsureLoaded itself is idempotent once Loaded.
func reloadExpanded(n *Node) {
	_ = n.EnsureLoaded()
	if !n.IsDir {
		return
	}
	for _, c := range n.Children {
		if c.IsDir && c.Expanded {
			reloadExpanded(c)
		}
	}
}

// ApplyStatus attaches rolled-up git statuses to the currently loaded
// nodes and marks the view dirty so status markers repaint.
func (v *View) ApplyStatus(rolled map[string]gitstatus.Status) {
	var walk func(n *Node)
	walk = func(n *Node) {
		for _, c := range n.Children {
			if rel, ok := relPath(v.root.Path, c.Path); ok {
				c.Status = rolled[rel]
			}
			if c.Loaded {
				walk(c)
			}
		}
	}
	walk(v.root)
	v.dirty = true
}

func (v *View) ensureFresh() {
	if !v.dirty {
		return
	}
	reloadExpanded(v.root)
	v.rows = Flatten(v.root)
	v.dirty = false
	if v.cursor >= len(v.rows) {
		v.cursor = len(v.rows) - 1
	}
	if v.cursor < 0 {
		v.cursor = 0
	}
}

func (v *View) Render(w layout.Window) {
	v.ensureFresh()

	cols, rows := w.Size()
	w.Clear()

	if v.cursor < v.scrollTop {
		v.scrollTop = v.cursor
	}
	if v.cursor >= v.scrollTop+rows {
		v.scrollTop = v.cursor - rows + 1
	}
	if v.scrollTop < 0 {
		v.scrollTop = 0
	}

	// hScroll is a manual "peek right" offset (Shift+Left/Right), clamped
	// each frame against the SELECTED row's actual width — so it never
	// scrolls past the end of the very entry you're looking at.
	if v.cursor >= 0 && v.cursor < len(v.rows) {
		selected := formatRow(v.rows[v.cursor])
		v.hScroll = textwidth.ClampScroll(v.hScroll, textwidth.DisplayWidth(selected), cols)
	}

	for i := 0; i < rows; i++ {
		idx := v.scrollTop + i
		if idx >= len(v.rows) {
			break
		}
		row := v.rows[idx]
		style := styleForRow(row, idx == v.cursor)
		text := textwidth.SliceByDisplayColumn(formatRow(row), v.hScroll, cols)
		w.Println(i, layout.Segment{Text: text, Style: style})
	}
}

// styleForRow colors a row by its git status (see gitstyle), bolds
// directories, and reverses the currently selected row on top of that.
func styleForRow(r Row, isCursor bool) layout.Style {
	style := gitstyle.Style(r.Node.Status)
	if r.Node.IsDir {
		style.Attr |= layout.AttrBold
	}
	if isCursor {
		style.Attr |= layout.AttrReverse
	}
	return style
}

func formatRow(r Row) string {
	indent := ""
	for i := 0; i < r.Depth; i++ {
		indent += "  "
	}
	icon := " "
	if r.Node.IsDir {
		if r.Node.Expanded {
			icon = " 📂 "
		} else {
			icon = " ▼ "
		}
	}
	return fmt.Sprintf("%s %s%s%s", gitstyle.Marker(r.Node.Status), indent, icon, r.Node.Name)
}

func (v *View) HandleKey(k layout.Key) bool {
	if k.EventType == layout.EventRelease {
		return false
	}
	v.ensureFresh()

	switch {
	// Checked before the plain Left/Right cases below, since a switch
	// with no tag takes the first matching case: Shift+Right must not
	// also satisfy the bare k.Named == layout.KeyRight case.
	case k.Named == layout.KeyRight && k.Mods&layout.ModShift != 0:
		v.scrollRight()
		return true
	case k.Named == layout.KeyLeft && k.Mods&layout.ModShift != 0:
		v.scrollLeft()
		return true
	case k.Named == layout.KeyDown || k.Text == "j":
		v.moveCursor(1)
		return true
	case k.Named == layout.KeyUp || k.Text == "k":
		v.moveCursor(-1)
		return true
	case k.Named == layout.KeyEnter || k.Named == layout.KeyRight || k.Text == "l":
		v.activate()
		return true
	case k.Named == layout.KeyLeft || k.Text == "h":
		v.collapse()
		return true
	}
	return false
}

func (v *View) scrollRight() {
	v.hScroll += hScrollStep // clamped against the selected row's width in Render
}

func (v *View) scrollLeft() {
	v.hScroll -= hScrollStep
	if v.hScroll < 0 {
		v.hScroll = 0
	}
}

func (v *View) moveCursor(delta int) {
	v.cursor += delta
	if v.cursor < 0 {
		v.cursor = 0
	}
	if v.cursor >= len(v.rows) {
		v.cursor = len(v.rows) - 1
	}
	v.hScroll = 0 // peeking is per-row: start from the left on the new selection
}

func (v *View) activate() {
	if v.cursor < 0 || v.cursor >= len(v.rows) {
		return
	}
	n := v.rows[v.cursor].Node
	if n.IsDir {
		_ = n.EnsureLoaded()
		n.Expanded = !n.Expanded
		v.dirty = true
		return
	}
	if v.OnOpen != nil {
		v.OnOpen(n.Path)
	}
}

// collapse handles Left/h. On an expanded directory it just closes that
// directory. Otherwise — a file, or an already-collapsed directory — it
// walks up to the parent directory, closes that instead, and moves the
// cursor onto the parent's row, so collapsing works from anywhere inside a
// folder, not just from the folder's own row. There's nothing to do if the
// node is a top-level entry: its parent is the root, which has no row of
// its own to collapse onto.
func (v *View) collapse() {
	if v.cursor < 0 || v.cursor >= len(v.rows) {
		return
	}
	n := v.rows[v.cursor].Node
	if n.IsDir && n.Expanded {
		n.Expanded = false
		v.dirty = true
		return
	}

	parent := n.Parent
	if parent == nil || parent == v.root {
		return
	}
	parent.Expanded = false
	v.dirty = true
	v.ensureFresh()
	for i, row := range v.rows {
		if row.Node == parent {
			v.cursor = i
			break
		}
	}
}
