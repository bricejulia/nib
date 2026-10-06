package layout

import "testing"

// appTree mirrors nib's own shape: a Fixed(20) tree beside a ratio editor,
// above a Fixed(1) status bar.
func appTree() (*SplitNode, *SplitNode) {
	panes := &SplitNode{Dir: Horizontal, Children: []Child{
		{Node: leaf(1), Hint: Fixed(20)},
		{Node: leaf(2), Hint: Ratio(1)},
	}}
	root := &SplitNode{Dir: Vertical, Children: []Child{
		{Node: panes, Hint: Ratio(1)},
		{Node: leaf(3), Hint: Fixed(1)},
	}}
	return root, panes
}

func TestFixedEdgeAtHitsBothBorderColumns(t *testing.T) {
	root, panes := appTree()
	area := Rect{W: 100, H: 30}
	// Column 19 is the tree's right border, 20 the editor's left border.
	for _, col := range []int{19, 20} {
		split, idx, a, ok := FixedEdgeAt(root, area, col, 5)
		if !ok || split != panes || idx != 0 {
			t.Fatalf("col %d: got (%p, %d, %v), want (%p, 0, true)", col, split, idx, ok, panes)
		}
		if a != (Rect{W: 100, H: 29}) {
			t.Errorf("col %d: split area = %+v, want the 100x29 panes row", col, a)
		}
	}
}

func TestFixedEdgeAtMisses(t *testing.T) {
	root, _ := appTree()
	area := Rect{W: 100, H: 30}
	for _, c := range []struct{ col, row int }{
		{18, 5},  // inside the tree
		{21, 5},  // inside the editor
		{19, 29}, // on the status bar row
		{19, 30}, // off the screen
	} {
		if _, _, _, ok := FixedEdgeAt(root, area, c.col, c.row); ok {
			t.Errorf("(%d,%d) hit, want a miss", c.col, c.row)
		}
	}
}

func TestFixedEdgeAtIgnoresRatioSplits(t *testing.T) {
	root := &SplitNode{Dir: Horizontal, Children: []Child{
		{Node: leaf(1), Hint: Ratio(1)},
		{Node: leaf(2), Hint: Ratio(1)},
	}}
	if _, _, _, ok := FixedEdgeAt(root, Rect{W: 100, H: 30}, 49, 5); ok {
		t.Error("ratio boundary hit, want a miss")
	}
}

func TestClampFixed(t *testing.T) {
	for _, c := range []struct{ n, extent, want int }{
		{40, 100, 40},
		{2, 100, MinFixed},
		{95, 100, 100 - MinRest},
		{30, 10, 10}, // tiny terminal: never wider than the split itself
	} {
		if got := ClampFixed(c.n, c.extent); got != c.want {
			t.Errorf("ClampFixed(%d, %d) = %d, want %d", c.n, c.extent, got, c.want)
		}
	}
}
