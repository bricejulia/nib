package layout

import "testing"

func twoPaneTree() Node {
	return &SplitNode{
		Dir: Horizontal,
		Children: []Child{
			{Node: leaf(1), Hint: Fixed(30)},
			{Node: leaf(2), Hint: Ratio(1)},
		},
	}
}

func TestFocusManagerTraversalOrderAndDefault(t *testing.T) {
	var fm FocusManager
	fm.Rebuild(twoPaneTree())

	id, ok := fm.Focused()
	if !ok || id != 1 {
		t.Fatalf("expected leaf 1 focused by default, got %v ok=%v", id, ok)
	}
}

func TestFocusManagerNextPrevWrap(t *testing.T) {
	var fm FocusManager
	fm.Rebuild(twoPaneTree())

	fm.Next()
	id, _ := fm.Focused()
	if id != 2 {
		t.Fatalf("after Next, expected leaf 2, got %v", id)
	}
	fm.Next()
	id, _ = fm.Focused()
	if id != 1 {
		t.Fatalf("Next should wrap around to leaf 1, got %v", id)
	}
	fm.Prev()
	id, _ = fm.Focused()
	if id != 2 {
		t.Fatalf("Prev should wrap around to leaf 2, got %v", id)
	}
}

func TestFocusManagerFocusAt(t *testing.T) {
	var fm FocusManager
	fm.Rebuild(twoPaneTree())

	fm.FocusAt(2)
	id, _ := fm.Focused()
	if id != 2 {
		t.Fatalf("FocusAt(2) failed, got %v", id)
	}

	fm.FocusAt(999) // unknown leaf: no-op
	id, _ = fm.Focused()
	if id != 2 {
		t.Fatalf("FocusAt with unknown id should be a no-op, got %v", id)
	}
}

func TestFocusManagerRebuildPreservesFocusedLeaf(t *testing.T) {
	var fm FocusManager
	fm.Rebuild(twoPaneTree())
	fm.FocusAt(2)

	// Rebuild with the same leaf IDs present (e.g. after a resize).
	fm.Rebuild(twoPaneTree())
	id, _ := fm.Focused()
	if id != 2 {
		t.Fatalf("Rebuild should preserve focus on leaf 2, got %v", id)
	}
}

func TestFocusManagerRebuildFallsBackWhenFocusedLeafGone(t *testing.T) {
	var fm FocusManager
	fm.Rebuild(twoPaneTree())
	fm.FocusAt(2)

	fm.Rebuild(leaf(1)) // leaf 2 no longer exists
	id, ok := fm.Focused()
	if !ok || id != 1 {
		t.Fatalf("Rebuild should fall back to first leaf, got %v ok=%v", id, ok)
	}
}

type recordingView struct {
	handled bool
	consume bool
}

func (v *recordingView) Render(Window) {}
func (v *recordingView) HandleKey(k Key) bool {
	v.handled = true
	return v.consume
}
func (v *recordingView) Title() string { return "" }

func TestDispatchPrefersFocusedViewOverGlobal(t *testing.T) {
	view := &recordingView{consume: true}
	calledGlobal := false
	global := map[string]func(){"a": func() { calledGlobal = true }}

	consumed := Dispatch(Key{Text: "a"}, view, global)
	if !consumed || !view.handled || calledGlobal {
		t.Fatalf("expected focused view to consume key without falling through: consumed=%v handled=%v global=%v",
			consumed, view.handled, calledGlobal)
	}
}

func TestDispatchFallsBackToGlobalWhenUnconsumed(t *testing.T) {
	view := &recordingView{consume: false}
	calledGlobal := false
	global := map[string]func(){"Tab": func() { calledGlobal = true }}

	consumed := Dispatch(Key{Text: "", Mods: 0, Codepoint: 0, EventType: EventPress}, view, global)
	_ = consumed
	// Text is empty and no matching global key "Tab" via this key -> not consumed.
	if calledGlobal {
		t.Fatalf("should not have matched an unrelated global key")
	}

	consumed = Dispatch(keyNamed("Tab"), view, global)
	if !consumed || !calledGlobal {
		t.Fatalf("expected bubble-through to global keymap: consumed=%v global=%v", consumed, calledGlobal)
	}
}

func keyNamed(text string) Key { return Key{Text: text} }

func TestDispatchIgnoresReleaseEvents(t *testing.T) {
	view := &recordingView{consume: false}
	calledGlobal := false
	global := map[string]func(){"Tab": func() { calledGlobal = true }}

	consumed := Dispatch(Key{Named: KeyTab, EventType: EventRelease}, view, global)
	if consumed {
		t.Fatalf("a release event should never be reported as consumed")
	}
	if view.handled {
		t.Fatalf("a release event should not even reach the focused view")
	}
	if calledGlobal {
		t.Fatalf("a release event should not fall through to the global keymap")
	}
}

func TestDispatchPressThenReleaseFiresGlobalHandlerOnlyOnce(t *testing.T) {
	// Regression test: terminals using the kitty keyboard protocol's
	// event-reporting mode send a press AND a release per physical
	// keypress. For a key like Tab, unclaimed by any View and bound
	// globally to cycle focus, both events used to fall through to the
	// global keymap — firing CycleFocus twice per keypress, which for a
	// 2-pane layout exactly cancels itself out (looks like focus never
	// moved).
	view := &recordingView{consume: false}
	calls := 0
	global := map[string]func(){"Tab": func() { calls++ }}

	Dispatch(Key{Named: KeyTab, EventType: EventPress}, view, global)
	Dispatch(Key{Named: KeyTab, EventType: EventRelease}, view, global)

	if calls != 1 {
		t.Fatalf("expected exactly 1 global handler call for one press+release pair, got %d", calls)
	}
}

func TestDispatchNoFocusedViewStillChecksGlobal(t *testing.T) {
	calledGlobal := false
	global := map[string]func(){"Ctrl+c": func() { calledGlobal = true }}

	consumed := Dispatch(Key{Text: "c", Mods: ModCtrl}, nil, global)
	if !consumed || !calledGlobal {
		t.Fatalf("expected nil focused view to fall through to global: consumed=%v global=%v", consumed, calledGlobal)
	}
}
