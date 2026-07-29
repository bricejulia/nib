package layout

import "testing"

type stubView struct{}

func (stubView) Render(Window)      {}
func (stubView) HandleKey(Key) bool { return false }
func (stubView) Title() string      { return "" }

func leaf(id LeafID) *LeafNode { return &LeafNode{ID: id, View: stubView{}} }

func TestComputeSingleLeafFillsArea(t *testing.T) {
	root := leaf(1)
	area := Rect{X: 0, Y: 0, W: 80, H: 24}
	got := Compute(root, area)
	if got[1] != area {
		t.Fatalf("got %+v, want %+v", got[1], area)
	}
}

func TestComputeFixedPlusRatioTwoPane(t *testing.T) {
	root := &SplitNode{
		Dir: Horizontal,
		Children: []Child{
			{Node: leaf(1), Hint: Fixed(30)},
			{Node: leaf(2), Hint: Ratio(1)},
		},
	}
	got := Compute(root, Rect{X: 0, Y: 0, W: 100, H: 24})

	want1 := Rect{X: 0, Y: 0, W: 30, H: 24}
	want2 := Rect{X: 30, Y: 0, W: 70, H: 24}
	if got[1] != want1 {
		t.Errorf("leaf 1: got %+v, want %+v", got[1], want1)
	}
	if got[2] != want2 {
		t.Errorf("leaf 2: got %+v, want %+v", got[2], want2)
	}
}

func TestComputeRatioSplitRounding(t *testing.T) {
	// 100 cols split 1:1:1 -> 33/33/34, no column silently dropped.
	root := &SplitNode{
		Dir: Horizontal,
		Children: []Child{
			{Node: leaf(1), Hint: Ratio(1)},
			{Node: leaf(2), Hint: Ratio(1)},
			{Node: leaf(3), Hint: Ratio(1)},
		},
	}
	got := Compute(root, Rect{X: 0, Y: 0, W: 100, H: 10})
	total := got[1].W + got[2].W + got[3].W
	if total != 100 {
		t.Fatalf("widths sum to %d, want 100 (got %+v %+v %+v)", total, got[1], got[2], got[3])
	}
	for id, r := range got {
		if r.W < 33 {
			t.Errorf("leaf %d width %d is less than the 33 floor share", id, r.W)
		}
	}
}

func TestComputeNestedSplits(t *testing.T) {
	// Horizontal[Fixed(20) | Vertical[Ratio(1), Ratio(2)]]
	inner := &SplitNode{
		Dir: Vertical,
		Children: []Child{
			{Node: leaf(2), Hint: Ratio(1)},
			{Node: leaf(3), Hint: Ratio(2)},
		},
	}
	root := &SplitNode{
		Dir: Horizontal,
		Children: []Child{
			{Node: leaf(1), Hint: Fixed(20)},
			{Node: inner, Hint: Ratio(1)},
		},
	}
	got := Compute(root, Rect{X: 0, Y: 0, W: 100, H: 30})

	if got[1] != (Rect{X: 0, Y: 0, W: 20, H: 30}) {
		t.Errorf("leaf 1: got %+v", got[1])
	}
	if got[2].X != 20 || got[3].X != 20 || got[2].W != 80 || got[3].W != 80 {
		t.Errorf("inner leaves should inherit X=20, W=80: got 2=%+v 3=%+v", got[2], got[3])
	}
	if got[2].H+got[3].H != 30 {
		t.Errorf("inner leaves heights should sum to 30: got %d+%d", got[2].H, got[3].H)
	}
	if got[3].H <= got[2].H {
		t.Errorf("leaf 3 has ratio 2 vs leaf 2's ratio 1, should be taller: got 2=%d 3=%d", got[2].H, got[3].H)
	}
}

func TestComputeDegenerateZeroWidthArea(t *testing.T) {
	root := &SplitNode{
		Dir: Horizontal,
		Children: []Child{
			{Node: leaf(1), Hint: Fixed(30)},
			{Node: leaf(2), Hint: Ratio(1)},
		},
	}
	got := Compute(root, Rect{X: 0, Y: 0, W: 10, H: 24})
	// Fixed child clamps to the available extent; ratio child gets 0, not negative.
	if got[1].W != 10 {
		t.Errorf("fixed leaf should clamp to available width 10, got %d", got[1].W)
	}
	if got[2].W != 0 {
		t.Errorf("ratio leaf should get 0 width when nothing remains, got %d", got[2].W)
	}
}
