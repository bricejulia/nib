package filetree

import "testing"

// buildFakeTree constructs an in-memory tree without touching disk, to
// test Flatten's traversal/depth logic in isolation.
func buildFakeTree() *Node {
	fileC := &Node{Name: "c.txt", IsDir: false}
	dirSub := &Node{Name: "sub", IsDir: true, Expanded: true, Loaded: true, Children: []*Node{fileC}}
	fileA := &Node{Name: "a.txt", IsDir: false}
	dirCollapsed := &Node{Name: "closed", IsDir: true, Expanded: false, Loaded: true,
		Children: []*Node{{Name: "hidden.txt"}}}
	root := &Node{IsDir: true, Loaded: true, Children: []*Node{dirSub, fileA, dirCollapsed}}
	return root
}

func TestFlattenExpandedDirRecursesWithIncrementedDepth(t *testing.T) {
	rows := Flatten(buildFakeTree())

	if len(rows) != 4 {
		t.Fatalf("got %d rows, want 4 (sub, c.txt, a.txt, closed): %+v", len(rows), rowNames(rows))
	}
	if rows[0].Node.Name != "sub" || rows[0].Depth != 0 {
		t.Errorf("row 0: got %+v", rows[0])
	}
	if rows[1].Node.Name != "c.txt" || rows[1].Depth != 1 {
		t.Errorf("row 1 (child of expanded sub) should be at depth 1: got %+v", rows[1])
	}
	if rows[2].Node.Name != "a.txt" || rows[2].Depth != 0 {
		t.Errorf("row 2: got %+v", rows[2])
	}
	if rows[3].Node.Name != "closed" || rows[3].Depth != 0 {
		t.Errorf("row 3: got %+v", rows[3])
	}
}

func TestFlattenCollapsedDirContributesNoChildRows(t *testing.T) {
	rows := Flatten(buildFakeTree())
	for _, r := range rows {
		if r.Node.Name == "hidden.txt" {
			t.Fatalf("collapsed directory's child must not appear in flattened rows")
		}
	}
}

func TestFlattenNotYetLoadedDirIsNotWalkedEvenIfMarkedExpanded(t *testing.T) {
	// Expanded=true but Loaded=false must not cause Flatten to walk nil
	// Children (which would happen to be a no-op range, but the point is
	// it must never trigger a disk read either — Flatten is pure).
	notLoaded := &Node{Name: "notloaded", IsDir: true, Expanded: true, Loaded: false}
	root := &Node{IsDir: true, Loaded: true, Children: []*Node{notLoaded}}

	rows := Flatten(root)
	if len(rows) != 1 || rows[0].Node.Name != "notloaded" {
		t.Fatalf("got %+v, want a single row for the not-yet-loaded dir", rows)
	}
}

func TestFlattenEmptyRoot(t *testing.T) {
	root := &Node{IsDir: true, Loaded: true}
	rows := Flatten(root)
	if len(rows) != 0 {
		t.Fatalf("expected no rows for an empty root, got %+v", rows)
	}
}

func rowNames(rows []Row) []string {
	names := make([]string, len(rows))
	for i, r := range rows {
		names[i] = r.Node.Name
	}
	return names
}
