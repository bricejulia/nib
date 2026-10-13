package workspace

import (
	"strings"

	"github.com/bricejulia/nib/internal/debuglog"
	"github.com/bricejulia/nib/internal/layout"
	"github.com/bricejulia/nib/internal/ui/editor"
)

// Split opens a new pane beside the Target pane in direction dir, showing
// the same file (the SAME Buffer) as it. Returns the new pane's leaf, to
// focus once the caller has rebuilt its focus order.
func (w *Workspace) Split(dir layout.Direction) (layout.LeafID, bool) {
	target := w.target()
	np, ok := w.splitPane(target, dir)
	if !ok {
		return 0, false
	}
	if path := target.view.ActivePath(); path != "" {
		np.view.Open(path)
		w.refreshLineStatus(path)
	}
	return np.leaf.ID, true
}

// ClosePane closes the Target pane, returning a surviving leaf to focus.
// Refuses (ok=false) when it's the last pane, or when it holds unsaved
// changes: closing it releases its tabs' Buffers, and one not held open by
// another pane would lose its edits for good — the same refusal ":qa"
// gives.
func (w *Workspace) ClosePane() (layout.LeafID, bool) {
	target := w.target()
	if len(w.panes) == 1 {
		return 0, false
	}
	if dirty := target.view.DirtyPaths(); len(dirty) > 0 {
		debuglog.Warn("close pane: %d unsaved file(s) (save first): %s", len(dirty), strings.Join(dirty, ", "))
		return 0, false
	}
	// Release every tab's Buffer reference first, or the store would keep
	// them alive — and hand back their stale content instead of a fresh
	// load — forever.
	target.view.CloseAllTabs()
	survivor, ok := layout.Close(w.root, target.leaf)
	if !ok {
		return 0, false
	}
	delete(w.panes, target.leaf.ID)
	next := layout.Leaves(survivor)[0].ID
	if w.active == target {
		w.active = w.panes[next]
	}
	return next, true
}

// MoveActiveTab moves the Target pane's active tab into the next (dir=+1)
// or previous (dir=-1) pane in focus-cycling order, wrapping around.
// Returns the pane it landed in; ok is false if there's only one pane.
func (w *Workspace) MoveActiveTab(dir int) (layout.LeafID, bool) {
	p := w.target()
	return w.moveTabToPane(p, p.view.ActiveIndex(), dir)
}

// MaxPanes caps how many editor panes a session can hold. Past a handful
// they're too narrow to use anyway, and without a cap a held-down (or
// misdecoded — tmux can deliver Ctrl+Down as Ctrl+w) split key splits
// until the layout breaks, a layout the saved session then brings back on
// every launch. Restore applies the same cap, which heals such a session.
const MaxPanes = 8

// splitPane creates a new pane and splices it into the layout beside
// target in direction dir. Refuses once there are MaxPanes panes.
func (w *Workspace) splitPane(target *pane, dir layout.Direction) (*pane, bool) {
	if len(w.panes) >= MaxPanes {
		debuglog.Warn("split: already %d panes (max %d)", len(w.panes), MaxPanes)
		return nil, false
	}
	np := w.newPane()
	if !layout.Split(w.root, target.leaf, dir, np.leaf) {
		return nil, false
	}
	w.add(np)
	return np, true
}

// orderedPaneIDs lists every pane's leaf in the left-to-right,
// top-to-bottom order Tab/Shift+Tab cycle through, so moving a tab to the
// "next pane" agrees with what focus-cycling calls next.
func (w *Workspace) orderedPaneIDs() []layout.LeafID {
	var ids []layout.LeafID
	for _, leaf := range layout.Leaves(w.root) {
		if _, ok := w.panes[leaf.ID]; ok {
			ids = append(ids, leaf.ID)
		}
	}
	return ids
}

// moveTabToPane moves source's tab at index into the next/previous pane in
// orderedPaneIDs' order, wrapping around.
func (w *Workspace) moveTabToPane(source *pane, index, dir int) (layout.LeafID, bool) {
	ids := w.orderedPaneIDs()
	if len(ids) < 2 {
		return 0, false
	}
	pos := -1
	for i, id := range ids {
		if id == source.leaf.ID {
			pos = i
			break
		}
	}
	if pos < 0 {
		return 0, false
	}
	dest := w.panes[ids[(pos+dir+len(ids))%len(ids)]]
	source.view.TransferTabTo(index, dest.view)
	return dest.leaf.ID, true
}

// paneInDirection is the pane geometrically adjacent to source on screen
// (Horizontal = right, Vertical = down when forward), if that neighbor is
// an editor pane at all.
func (w *Workspace) paneInDirection(source *pane, dir layout.Direction, forward bool) (*pane, bool) {
	if w.opts.Rects == nil {
		return nil, false
	}
	id, ok := layout.Neighbor(w.opts.Rects(), source.leaf.ID, dir, forward)
	if !ok {
		return nil, false
	}
	p, ok := w.panes[id]
	return p, ok
}

// wirePaneMoves connects v's tab menu ("Move to next pane", "Split
// Right/Down", "Move Right/Down") to the Workspace: v only knows its own
// tabs, so it hands the index over and the move happens here.
func (w *Workspace) wirePaneMoves(v *editor.View) {
	v.OnMoveTabToNextPane = func(index int) {
		if p, ok := w.paneForView(v); ok {
			if dest, ok := w.moveTabToPane(p, index, 1); ok {
				w.layoutChanged(dest)
			}
		}
	}
	// Unlike Split, which shows the active file in the new pane too, these
	// move the one tab the menu was opened on.
	splitAndMove := func(index int, dir layout.Direction) {
		p, ok := w.paneForView(v)
		if !ok {
			return
		}
		np, ok := w.splitPane(p, dir)
		if !ok {
			return
		}
		p.view.TransferTabTo(index, np.view)
		w.layoutChanged(np.leaf.ID)
	}
	v.OnSplitAndMoveRight = func(index int) { splitAndMove(index, layout.Horizontal) }
	v.OnSplitAndMoveDown = func(index int) { splitAndMove(index, layout.Vertical) }

	moveDirection := func(index int, dir layout.Direction) {
		p, ok := w.paneForView(v)
		if !ok {
			return
		}
		dest, ok := w.paneInDirection(p, dir, true)
		if !ok {
			return
		}
		p.view.TransferTabTo(index, dest.view)
		w.layoutChanged(dest.leaf.ID)
	}
	v.OnMoveRight = func(index int) { moveDirection(index, layout.Horizontal) }
	v.OnMoveDown = func(index int) { moveDirection(index, layout.Vertical) }

	// The menu hides Move Right/Down rather than showing an item that
	// would do nothing.
	canMove := func(dir layout.Direction) bool {
		p, ok := w.paneForView(v)
		if !ok {
			return false
		}
		_, ok = w.paneInDirection(p, dir, true)
		return ok
	}
	v.CanMoveRight = func() bool { return canMove(layout.Horizontal) }
	v.CanMoveDown = func() bool { return canMove(layout.Vertical) }
}
