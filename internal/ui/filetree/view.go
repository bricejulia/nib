package filetree

import (
	"fmt"
	"path/filepath"
	"strings"

	"github.com/bricejulia/kiwi/internal/config"
	"github.com/bricejulia/kiwi/internal/layout"
	"github.com/bricejulia/kiwi/internal/textwidth"
	"github.com/bricejulia/kiwi/internal/ui/gitstyle"
	"github.com/bricejulia/kiwi/internal/vcs/gitstatus"
)

// DefaultKeybinds are the file tree pane's built-in keybindings,
// overridable via the user config's "filetree" scope (see
// internal/config).
//
// These are only consulted in the pane's normal browsing state: while a
// create/rename/delete prompt is open, keys go to handlePromptKey, which
// never looks at this map. That's what keeps "a"/"r"/"d" — or any other
// plain letter bound here — typeable in a filename.
var DefaultKeybinds = config.Defaults{
	{Trigger: "Down", Action: "move_down"},
	{Trigger: "j", Action: "move_down"},
	{Trigger: "Up", Action: "move_up"},
	{Trigger: "k", Action: "move_up"},
	{Trigger: "Enter", Action: "open_or_expand"},
	{Trigger: "Right", Action: "open_or_expand"},
	{Trigger: "l", Action: "open_or_expand"},
	{Trigger: "Left", Action: "collapse"},
	{Trigger: "h", Action: "collapse"},
	{Trigger: "Shift+Right", Action: "peek_right"},
	{Trigger: "Shift+Left", Action: "peek_left"},
	{Trigger: "a", Action: "create"},
	{Trigger: "r", Action: "rename"},
	{Trigger: "d", Action: "delete"},
}

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

	// Prompt state for the file operations — see prompt.go. The pending
	// operation's target is kept as an absolute PATH, never as a *Node:
	// the watcher's debounced Refresh can land between two keystrokes, and
	// EnsureLoaded rebuilds child Nodes from scratch on a reload, so a
	// *Node captured when the prompt opened could be an orphan by the time
	// Enter is pressed.
	prompt       promptMode
	promptBuf    []rune
	promptCaret  int    // rune index into promptBuf
	promptErr    string // refusal shown inline, cleared by the next edit
	promptTarget string // absolute path the pending rename/delete acts on
	promptCount  int    // entries inside promptTarget, for a recursive delete
	promptScroll int    // display columns the prompt row is scrolled by

	// lastHeight is the pane height at the last Render, so CursorPosition
	// knows which row the prompt was drawn on. Safe to rely on: ui.App
	// renders a pane immediately before asking it for its cursor.
	lastHeight int

	// OnOpen is called with the absolute path when the cursor activates a
	// file row. Set by the caller (cmd/kiwi/main.go) to wire in the
	// editor pane.
	OnOpen func(path string)

	// OnPathMoved is called after a successful rename or move, before the
	// tree itself is updated, so the caller can retarget anything holding
	// the old path (an open editor buffer's tab, its language-server
	// registration). Called ONCE with the renamed entry's own old and new
	// paths — for a directory, the caller handles what's underneath it by
	// prefix.
	OnPathMoved func(oldPath, newPath string)

	// OnPathDeleted is called after a successful delete, with the absolute
	// path that was removed — once, and again by prefix on the caller's
	// side for anything that was inside a deleted directory.
	OnPathDeleted func(path string)

	// OnMutated is called after any successful create/rename/delete, and
	// never after a cancel or a refusal, so the caller can re-run the
	// refresh (git status, per-line diffs) it would otherwise only do when
	// the debounced fsnotify signal arrives a fifth of a second later.
	OnMutated func()

	keymap map[string]string
}

// New creates a View rooted at absPath. The root's immediate children are
// not loaded from disk until the first Render or HandleKey call.
func New(absPath string) *View {
	return &View{root: NewRoot(absPath), dirty: true, keymap: DefaultKeybinds.Resolve(nil)}
}

// SetKeymap merges the user config's "filetree" scope overrides on top
// of DefaultKeybinds, replacing the pane's active keymap.
func (v *View) SetKeymap(overrides map[string]string) {
	v.keymap = DefaultKeybinds.Resolve(overrides)
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
	// The root itself first: it has no row of its own, so invalidateExpanded
	// — which only looks at a node's children — would never reach it, and a
	// file created at the top level of the project would never appear.
	v.root.Loaded = false
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
	v.lastHeight = rows

	// A prompt takes the pane's bottom row, so the tree gets one row less
	// to work with — and the scroll clamp below has to agree, or the cursor
	// could sit on the row the prompt is drawn over.
	treeRows := rows
	if v.prompt != promptNone {
		treeRows--
	}
	if treeRows < 0 {
		treeRows = 0
	}

	if v.cursor < v.scrollTop {
		v.scrollTop = v.cursor
	}
	if v.cursor >= v.scrollTop+treeRows {
		v.scrollTop = v.cursor - treeRows + 1
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

	for i := 0; i < treeRows; i++ {
		idx := v.scrollTop + i
		if idx >= len(v.rows) {
			break
		}
		row := v.rows[idx]
		style := styleForRow(row, idx == v.cursor)
		text := textwidth.SliceByDisplayColumn(formatRow(row), v.hScroll, cols)
		w.Println(i, layout.Segment{Text: text, Style: style})
	}

	if v.prompt != promptNone && rows > 0 {
		v.renderPrompt(w, rows-1, cols)
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
			icon = " ▼ "
		} else {
			icon = " ▶ "
		}
	}
	return fmt.Sprintf("%s %s%s%s", gitstyle.Marker(r.Node.Status), indent, icon, r.Node.Name)
}

// treeRows is how many rows the tree itself gets to render into — the
// pane's full height, minus one if a create/rename/delete prompt is
// currently occupying the bottom row. Shared by Render's own clamp and by
// ScrollState/ScrollTo, so a scrollbar interaction while a prompt is open
// can't disagree with what's actually on screen.
func (v *View) treeRows() int {
	rows := v.lastHeight
	if v.prompt != promptNone {
		rows--
	}
	if rows < 0 {
		rows = 0
	}
	return rows
}

// ScrollState implements layout.Scrollable.
func (v *View) ScrollState() layout.ScrollState {
	return layout.ScrollState{Top: v.scrollTop, Viewport: v.treeRows(), Total: len(v.rows)}
}

// ScrollTo implements layout.ScrollTarget. Like the editor's ScrollTo, this
// also has to move the cursor: Render re-derives scrollTop from cursor on
// every frame ("if v.cursor < v.scrollTop ...", "if v.cursor >=
// v.scrollTop+treeRows ..."), so a bare assignment to scrollTop would be
// undone on the very next frame if the cursor were left outside it.
func (v *View) ScrollTo(top int) {
	viewport := v.treeRows()
	maxTop := len(v.rows) - viewport
	if maxTop < 0 {
		maxTop = 0
	}
	if top < 0 {
		top = 0
	}
	if top > maxTop {
		top = maxTop
	}
	v.scrollTop = top

	if viewport > 0 {
		if v.cursor < top {
			v.cursor = top
		} else if v.cursor >= top+viewport {
			v.cursor = top + viewport - 1
		}
	}
	if v.cursor < 0 {
		v.cursor = 0
	}
	if v.cursor >= len(v.rows) {
		v.cursor = len(v.rows) - 1
	}
	v.hScroll = 0 // peeking is per-row: start from the left on the new selection, same as moveCursor
}

func (v *View) HandleKey(k layout.Key) bool {
	if k.EventType == layout.EventRelease {
		return false
	}
	// Before ensureFresh, and before any keymap lookup: a prompt keystroke
	// must not re-read every expanded directory from disk, and must never
	// be matched against a binding — see handlePromptKey.
	if v.prompt != promptNone {
		return v.handlePromptKey(k)
	}
	v.ensureFresh()
	if v.keymap == nil {
		// A View built via a bare struct literal (as some tests do, to
		// set up a fixture root directly) skips New's initialization —
		// fall back to the defaults rather than consuming no keys.
		v.keymap = DefaultKeybinds.Resolve(nil)
	}

	switch v.keymap[k.String()] {
	case "peek_right":
		v.scrollRight()
	case "peek_left":
		v.scrollLeft()
	case "move_down":
		v.moveCursor(1)
	case "move_up":
		v.moveCursor(-1)
	case "open_or_expand":
		v.activate()
	case "collapse":
		v.collapse()
	case "create":
		v.beginCreate()
	case "rename":
		v.beginRename()
	case "delete":
		v.beginDelete()
	default:
		return false
	}
	return true
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
	v.selectNode(parent)
}

// selectNode puts the cursor on n's row, if n has one.
func (v *View) selectNode(n *Node) bool {
	for i, row := range v.rows {
		if row.Node == n {
			v.cursor = i
			v.hScroll = 0 // peeking is per-row: start from the left on the new selection
			return true
		}
	}
	return false
}

// loadedDirNode returns the in-memory node for absDir if the tree has
// already read that far, or nil. It never touches the disk: a directory the
// tree hasn't loaded has nothing to invalidate, since it will be read fresh
// whenever it's first expanded.
func (v *View) loadedDirNode(absDir string) *Node {
	if absDir == v.root.Path {
		return v.root
	}
	rel, ok := relPath(v.root.Path, absDir)
	if !ok {
		return nil
	}
	n := v.root
	for _, name := range strings.Split(rel, "/") {
		if !n.Loaded {
			return nil
		}
		c := n.child(name)
		if c == nil || !c.IsDir {
			return nil
		}
		n = c
	}
	return n
}

// childOrReload returns n's child named name, re-reading n from disk once if
// it isn't there yet. That second read is what makes revealing a
// just-created path work at any depth: a create can bring whole
// intermediate directories into existence (os.MkdirAll), and their parents
// were last read before any of it was there. The re-read carries existing
// children's state over by name, so nothing already open collapses.
func childOrReload(n *Node, name string) *Node {
	if err := n.EnsureLoaded(); err != nil {
		return nil
	}
	if c := n.child(name); c != nil {
		return c
	}
	n.Loaded = false
	if err := n.EnsureLoaded(); err != nil {
		return nil
	}
	return n.child(name)
}

// revealPath expands (loading from disk as it goes) every directory between
// the root and abs, then leaves the cursor on abs's own row. Reports false
// if abs is the root, sits outside it, or isn't in the tree — e.g. because
// it was created inside a directory the tree can't read.
func (v *View) revealPath(abs string) bool {
	rel, ok := relPath(v.root.Path, abs)
	if !ok {
		return false
	}
	names := strings.Split(rel, "/")

	n := v.root
	for _, name := range names[:len(names)-1] {
		c := childOrReload(n, name)
		if c == nil || !c.IsDir {
			return false
		}
		c.Expanded = true
		n = c
	}
	target := childOrReload(n, names[len(names)-1])
	if target == nil {
		return false
	}

	// Expanding an ancestor changes which rows exist, so re-flatten before
	// looking the target's row up. ensureFresh also re-runs EnsureLoaded on
	// everything expanded, which is a no-op for what was just loaded above.
	v.dirty = true
	v.ensureFresh()
	return v.selectNode(target)
}

// syncAfter brings the tree back in step with the disk after a mutation:
// it invalidates the specific directories that changed, re-flattens, and
// leaves the cursor on selectPath (pass "" when there's nothing to select,
// e.g. after a delete).
//
// Refresh would not do: it only invalidates directories that are currently
// EXPANDED, which misses a create or move destination inside a
// collapsed-but-already-loaded folder — the tree would keep showing its
// stale children until something else happened to re-read it.
func (v *View) syncAfter(selectPath string, dirs ...string) {
	for _, d := range dirs {
		if n := v.loadedDirNode(d); n != nil {
			n.Loaded = false
		}
	}
	v.dirty = true
	if selectPath != "" && v.revealPath(selectPath) {
		return
	}
	v.ensureFresh()
}

// moveNodeInTree relocates the node at src to dst within the tree,
// preserving its identity so a renamed or moved directory keeps its
// Expanded/Loaded/Children state and its subtree stays where it was.
//
// Reports false when either end isn't loaded in memory, in which case the
// caller falls back to invalidating both directories and letting
// EnsureLoaded rebuild them — correct, but a renamed directory collapses,
// because EnsureLoaded carries state over by NAME and so reads a rename as
// a delete plus a create.
func (v *View) moveNodeInTree(src, dst string) bool {
	srcParent := v.loadedDirNode(filepath.Dir(src))
	dstParent := v.loadedDirNode(filepath.Dir(dst))
	if srcParent == nil || dstParent == nil {
		return false
	}
	n := srcParent.child(filepath.Base(src))
	if n == nil {
		return false
	}
	if dstParent.child(filepath.Base(dst)) != nil {
		return false // something is already there in the tree; re-read instead
	}

	for i, c := range srcParent.Children {
		if c == n {
			srcParent.Children = append(srcParent.Children[:i], srcParent.Children[i+1:]...)
			break
		}
	}
	n.Parent = dstParent
	n.Retarget(dstParent.Path, filepath.Base(dst))
	dstParent.Children = append(dstParent.Children, n)
	sortChildren(dstParent.Children)
	return true
}
