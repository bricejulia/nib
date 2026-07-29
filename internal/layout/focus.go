package layout

// FocusManager tracks which single leaf has focus. Keys go to the focused
// View first; unhandled keys bubble to a global keymap (Dispatch). That is
// the entire input contract — new panels never require touching input
// handling.
type FocusManager struct {
	order   []LeafID
	byID    map[LeafID]*LeafNode
	current int
}

// Rebuild recomputes the focus traversal order from the current tree. Call
// it whenever the tree structure changes (not on every frame). If the
// previously focused leaf still exists, focus is preserved; otherwise focus
// moves to the first leaf.
func (fm *FocusManager) Rebuild(root Node) {
	var prev LeafID
	hadPrev := len(fm.order) > 0
	if hadPrev {
		prev = fm.order[fm.current]
	}

	leaves := Leaves(root)
	fm.order = make([]LeafID, 0, len(leaves))
	fm.byID = make(map[LeafID]*LeafNode, len(leaves))
	for _, l := range leaves {
		fm.byID[l.ID] = l
		if uf, ok := l.View.(Unfocusable); ok && uf.Unfocusable() {
			continue // e.g. a status bar: still renderable, never a Tab stop
		}
		fm.order = append(fm.order, l.ID)
	}

	fm.current = 0
	if hadPrev {
		for i, id := range fm.order {
			if id == prev {
				fm.current = i
				break
			}
		}
	}
}

// Focused returns the currently focused leaf's ID, or false if there are no
// leaves.
func (fm *FocusManager) Focused() (LeafID, bool) {
	if len(fm.order) == 0 {
		return 0, false
	}
	return fm.order[fm.current], true
}

// FocusedView returns the View held by the focused leaf, or nil.
func (fm *FocusManager) FocusedView() View {
	id, ok := fm.Focused()
	if !ok {
		return nil
	}
	return fm.ViewAt(id)
}

// ViewAt returns the View held by leaf id, or nil if id is not a known
// leaf. Unlike FocusedView, this does not require id to be focused — it is
// used e.g. to route a mouse-wheel scroll to whichever pane the cursor is
// hovering, independent of keyboard focus.
func (fm *FocusManager) ViewAt(id LeafID) View {
	if l, ok := fm.byID[id]; ok {
		return l.View
	}
	return nil
}

// Next moves focus to the next leaf, wrapping around.
func (fm *FocusManager) Next() {
	if len(fm.order) == 0 {
		return
	}
	fm.current = (fm.current + 1) % len(fm.order)
}

// Prev moves focus to the previous leaf, wrapping around.
func (fm *FocusManager) Prev() {
	if len(fm.order) == 0 {
		return
	}
	fm.current = (fm.current - 1 + len(fm.order)) % len(fm.order)
}

// FocusAt moves focus directly to the given leaf, e.g. resolved from a
// mouse click via a Rect hit-test. It is a no-op if id is not a known leaf.
func (fm *FocusManager) FocusAt(id LeafID) {
	for i, o := range fm.order {
		if o == id {
			fm.current = i
			return
		}
	}
}

// Dispatch is the entire input contract: try the focused View first, then
// fall back to the global keymap.
//
// Release events are dropped here, centrally, before either path sees them.
// Terminals using the kitty keyboard protocol's event-reporting mode send a
// press AND a release per physical keypress. Individual Views already
// ignore releases themselves, but that only stops a release from being
// handled twice by the SAME View — it does nothing to stop a release that a
// View declines from then falling through to the global keymap. A key that
// isn't claimed by any View (e.g. Tab, bound globally to cycle focus) would
// otherwise fire its global handler twice per keypress: once for the press,
// once for the release that follows it — for a 2-pane cycle, the second
// call exactly cancels the first, making focus look like it never moved.
func Dispatch(k Key, focused View, global map[string]func()) bool {
	if k.EventType == EventRelease {
		return false
	}
	if focused != nil && focused.HandleKey(k) {
		return true
	}
	if fn, ok := global[k.String()]; ok {
		fn()
		return true
	}
	return false
}
