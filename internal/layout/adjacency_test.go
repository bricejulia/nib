package layout

import "testing"

// 2x2 grid: Horizontal[ Vertical[1,2] | Vertical[3,4] ]
//
//	+---+---+
//	| 1 | 3 |
//	+---+---+
//	| 2 | 4 |
//	+---+---+
func twoByTwoTree() Node {
	return &SplitNode{
		Dir: Horizontal,
		Children: []Child{
			{Node: &SplitNode{Dir: Vertical, Children: []Child{
				{Node: leaf(1), Hint: Ratio(1)},
				{Node: leaf(2), Hint: Ratio(1)},
			}}, Hint: Ratio(1)},
			{Node: &SplitNode{Dir: Vertical, Children: []Child{
				{Node: leaf(3), Hint: Ratio(1)},
				{Node: leaf(4), Hint: Ratio(1)},
			}}, Hint: Ratio(1)},
		},
	}
}

func TestNeighborTwoByTwoGrid(t *testing.T) {
	rects := Compute(twoByTwoTree(), Rect{X: 0, Y: 0, W: 100, H: 30})

	cases := []struct {
		from    LeafID
		dir     Direction
		forward bool
		wantID  LeafID
		wantOK  bool
	}{
		{1, Horizontal, true, 3, true},  // right of 1 -> 3
		{1, Vertical, true, 2, true},    // down from 1 -> 2
		{3, Horizontal, false, 1, true}, // left of 3 -> 1
		{4, Vertical, false, 3, true},   // up from 4 -> 3
		{2, Horizontal, true, 4, true},  // right of 2 -> 4
		{1, Vertical, false, 0, false},  // nothing above 1
		{3, Horizontal, true, 0, false}, // nothing right of 3
	}
	for _, c := range cases {
		got, ok := Neighbor(rects, c.from, c.dir, c.forward)
		if ok != c.wantOK || (ok && got != c.wantID) {
			t.Errorf("Neighbor(from=%d, dir=%v, forward=%v) = (%d, %v), want (%d, %v)",
				c.from, c.dir, c.forward, got, ok, c.wantID, c.wantOK)
		}
	}
}

// Asymmetric 3-pane split: Horizontal[ 1 | Vertical[2, 3] ]
//
//	+-----+-----+
//	|     |  2  |
//	|  1  +-----+
//	|     |  3  |
//	+-----+-----+
//
// Both 2 and 3 touch 1's right edge, so this exercises the
// largest-overlap tie-break (both overlap it fully — same amount here
// since 2/3 split the height evenly — landing on the deterministic
// topmost-wins fallback).
func asymmetricTree() Node {
	return &SplitNode{
		Dir: Horizontal,
		Children: []Child{
			{Node: leaf(1), Hint: Ratio(1)},
			{Node: &SplitNode{Dir: Vertical, Children: []Child{
				{Node: leaf(2), Hint: Ratio(1)},
				{Node: leaf(3), Hint: Ratio(1)},
			}}, Hint: Ratio(1)},
		},
	}
}

func TestNeighborAsymmetricSplitTieBreak(t *testing.T) {
	rects := Compute(asymmetricTree(), Rect{X: 0, Y: 0, W: 100, H: 30})

	got, ok := Neighbor(rects, 1, Horizontal, true)
	if !ok || got != 2 {
		t.Fatalf("Neighbor(1, right) = (%d, %v), want (2, true) — topmost candidate should win the tie", got, ok)
	}

	// From 2 or 3, left goes back to 1 regardless of which one asks.
	if got, ok := Neighbor(rects, 2, Horizontal, false); !ok || got != 1 {
		t.Errorf("Neighbor(2, left) = (%d, %v), want (1, true)", got, ok)
	}
	if got, ok := Neighbor(rects, 3, Horizontal, false); !ok || got != 1 {
		t.Errorf("Neighbor(3, left) = (%d, %v), want (1, true)", got, ok)
	}

	// 2 and 3 are adjacent to each other vertically, not horizontally.
	if _, ok := Neighbor(rects, 2, Horizontal, true); ok {
		t.Errorf("expected nothing to the right of 2 (it's the rightmost column)")
	}
	if got, ok := Neighbor(rects, 2, Vertical, true); !ok || got != 3 {
		t.Errorf("Neighbor(2, down) = (%d, %v), want (3, true)", got, ok)
	}
}

func TestNeighborUnknownLeaf(t *testing.T) {
	rects := Compute(twoByTwoTree(), Rect{X: 0, Y: 0, W: 100, H: 30})
	if _, ok := Neighbor(rects, 999, Horizontal, true); ok {
		t.Fatalf("expected ok=false for a leaf not present in rects")
	}
	if _, ok := Neighbor(nil, 1, Horizontal, true); ok {
		t.Fatalf("expected ok=false for a nil rects map")
	}
}
