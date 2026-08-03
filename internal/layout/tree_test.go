package layout

import "testing"

func TestFindParentNodeFindsDirectChild(t *testing.T) {
	l1, l2 := leaf(1), leaf(2)
	root := &SplitNode{Dir: Horizontal, Children: []Child{
		{Node: l1, Hint: Fixed(30)},
		{Node: l2, Hint: Ratio(1)},
	}}

	parent, idx, ok := FindParentNode(root, l2)
	if !ok || parent != root || idx != 1 {
		t.Fatalf("got parent=%v idx=%d ok=%v, want root, 1, true", parent, idx, ok)
	}
}

func TestFindParentNodeFindsNestedChild(t *testing.T) {
	l1, l2 := leaf(1), leaf(2)
	inner := &SplitNode{Dir: Vertical, Children: []Child{
		{Node: l1, Hint: Ratio(1)},
		{Node: l2, Hint: Ratio(1)},
	}}
	root := &SplitNode{Dir: Horizontal, Children: []Child{
		{Node: leaf(3), Hint: Fixed(30)},
		{Node: inner, Hint: Ratio(1)},
	}}

	parent, idx, ok := FindParentNode(root, l2)
	if !ok || parent != inner || idx != 1 {
		t.Fatalf("got parent=%v idx=%d ok=%v, want inner, 1, true", parent, idx, ok)
	}
}

func TestFindParentNodeNotFoundOrIsRoot(t *testing.T) {
	root := &SplitNode{Dir: Horizontal, Children: []Child{{Node: leaf(1), Hint: Ratio(1)}}}

	if _, _, ok := FindParentNode(root, leaf(99)); ok {
		t.Fatal("expected ok=false for a node not in the tree")
	}
	if _, _, ok := FindParentNode(root, root); ok {
		t.Fatal("expected ok=false when target IS root (no parent)")
	}
	if _, _, ok := FindParentNode(leaf(1), leaf(1)); ok {
		t.Fatal("expected ok=false when root itself is a bare leaf (no SplitNode to search)")
	}
}

func TestSplitWrapsTargetWithNewLeaf(t *testing.T) {
	target := leaf(2)
	sibling := leaf(1)
	root := &SplitNode{Dir: Horizontal, Children: []Child{
		{Node: sibling, Hint: Fixed(30)},
		{Node: target, Hint: Ratio(1)},
	}}

	newLeaf := leaf(3)
	if !Split(root, target, Vertical, newLeaf) {
		t.Fatal("expected Split to succeed")
	}

	split, ok := root.Children[1].Node.(*SplitNode)
	if !ok {
		t.Fatalf("expected root.Children[1] to now be a *SplitNode, got %T", root.Children[1].Node)
	}
	if split.Dir != Vertical {
		t.Fatalf("got Dir=%v, want Vertical", split.Dir)
	}
	if len(split.Children) != 2 || split.Children[0].Node != Node(target) || split.Children[1].Node != Node(newLeaf) {
		t.Fatalf("expected [target, newLeaf], got %+v", split.Children)
	}
	if split.Children[0].Hint != Ratio(1) || split.Children[1].Hint != Ratio(1) {
		t.Fatalf("expected both children to be Ratio(1), got %+v", split.Children)
	}
	// root's other slot (the sibling that was never split) must be
	// completely untouched.
	if root.Children[0].Node != Node(sibling) || root.Children[0].Hint != Fixed(30) {
		t.Fatalf("expected root.Children[0] to be unchanged, got %+v", root.Children[0])
	}
}

func TestSplitFailsWhenTargetIsRoot(t *testing.T) {
	target := leaf(1)
	if Split(target, target, Horizontal, leaf(2)) {
		t.Fatal("expected Split to fail when target has no parent (target IS root)")
	}
}

func TestCloseCollapsesToSibling(t *testing.T) {
	target := leaf(2)
	sibling := leaf(1)
	splitPair := &SplitNode{Dir: Horizontal, Children: []Child{
		{Node: sibling, Hint: Fixed(30)},
		{Node: target, Hint: Ratio(1)},
	}}
	// splitPair (the direct parent of target) needs its own parent for
	// Close to collapse into — matching nib's real shape, where the
	// editor's pane-pair is always nested under a further outer split.
	outer := &SplitNode{Dir: Vertical, Children: []Child{{Node: splitPair, Hint: Ratio(1)}}}

	survivor, ok := Close(outer, target)
	if !ok {
		t.Fatal("expected Close to succeed")
	}
	if survivor != Node(sibling) {
		t.Fatalf("expected survivor to be target's sibling leaf, got %v", survivor)
	}
	if outer.Children[0].Node != Node(sibling) {
		t.Fatalf("expected outer's slot to now hold sibling directly, got %+v", outer.Children[0])
	}
}

func TestCloseFailsWhenTargetOrParentHasNoParent(t *testing.T) {
	target := leaf(1)
	if _, ok := Close(target, target); ok {
		t.Fatal("expected Close to fail when target IS root")
	}

	root := &SplitNode{Dir: Horizontal, Children: []Child{
		{Node: target, Hint: Ratio(1)},
		{Node: leaf(2), Hint: Ratio(1)},
	}}
	if _, ok := Close(root, target); ok {
		t.Fatal("expected Close to fail when target's parent (root) itself has no parent")
	}
}

func TestSplitThenCloseSequenceStaysConsistent(t *testing.T) {
	// A realistic multi-step sequence: split right, split the new pane
	// down, close the original pane, split again — mirroring the design
	// review's stress test.
	original := leaf(1)
	fileTree := leaf(0)
	panes := &SplitNode{Dir: Horizontal, Children: []Child{
		{Node: fileTree, Hint: Fixed(30)},
		{Node: original, Hint: Ratio(1)},
	}}
	tree := &SplitNode{Dir: Vertical, Children: []Child{{Node: panes, Hint: Ratio(1)}}}

	right := leaf(2)
	if !Split(tree, original, Horizontal, right) {
		t.Fatal("split right failed")
	}

	down := leaf(3)
	if !Split(tree, right, Vertical, down) {
		t.Fatal("split down (inside the new pane) failed")
	}

	// Close the original pane: its parent (the SplitNode Split created for
	// "split right") has exactly 2 children [original, {right,down}-split],
	// so closing collapses to that {right,down}-split subtree.
	survivor, ok := Close(tree, original)
	if !ok {
		t.Fatal("close original failed")
	}
	leaves := Leaves(survivor)
	if len(leaves) != 2 || leaves[0] != right || leaves[1] != down {
		t.Fatalf("expected survivor's leaves to be [right, down], got %+v", leaves)
	}

	// Split again from "down".
	another := leaf(4)
	if !Split(tree, down, Horizontal, another) {
		t.Fatal("split again failed")
	}
	allLeaves := Leaves(tree)
	wantIDs := map[LeafID]bool{0: true, 2: true, 3: true, 4: true}
	if len(allLeaves) != len(wantIDs) {
		t.Fatalf("expected %d leaves, got %d: %+v", len(wantIDs), len(allLeaves), allLeaves)
	}
	for _, l := range allLeaves {
		if !wantIDs[l.ID] {
			t.Errorf("unexpected leaf ID %d in final tree", l.ID)
		}
	}
}
