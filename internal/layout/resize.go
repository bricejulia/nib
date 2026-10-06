package layout

// MinFixed and MinRest bound a dragged HintFixed child (see ClampFixed): the
// fixed pane never shrinks below MinFixed columns, and always leaves at
// least MinRest columns for its siblings.
const (
	MinFixed = 12
	MinRest  = 20
)

// FixedEdgeAt reports whether (col, row) lies on the draggable edge of a
// HintFixed child inside a Horizontal split: that child's last column (its
// right border) or the next sibling's first column (that sibling's left
// border) — the two cells that together read as the line between them.
// It returns the split, the fixed child's index in it, and the split's own
// area, which ClampFixed needs as its extent.
//
// Sizes come from the same distribute Compute uses, so the edge found here
// is always the one that was drawn. Only Fixed children are resizable;
// ratio splits (side-by-side editors) are deliberately left alone.
func FixedEdgeAt(root Node, area Rect, col, row int) (split *SplitNode, idx int, splitArea Rect, ok bool) {
	if row < area.Y || row >= area.Y+area.H || col < area.X || col >= area.X+area.W {
		return nil, 0, Rect{}, false
	}
	sp, isSplit := root.(*SplitNode)
	if !isSplit {
		return nil, 0, Rect{}, false
	}
	sizes := distribute(sp.Dir, sp.Children, area)
	offset := 0
	for i, c := range sp.Children {
		var sub Rect
		if sp.Dir == Horizontal {
			sub = Rect{X: area.X + offset, Y: area.Y, W: sizes[i], H: area.H}
			edge := sub.X + sub.W - 1
			if c.Hint.Kind == HintFixed && i+1 < len(sp.Children) && sub.W > 0 &&
				(col == edge || col == edge+1) {
				return sp, i, area, true
			}
		} else {
			sub = Rect{X: area.X, Y: area.Y + offset, W: area.W, H: sizes[i]}
		}
		if s, j, a, found := FixedEdgeAt(c.Node, sub, col, row); found {
			return s, j, a, true
		}
		offset += sizes[i]
	}
	return nil, 0, Rect{}, false
}

// ClampFixed bounds a dragged fixed size n to [MinFixed, extent-MinRest].
// When extent is too small for both minimums, MinFixed wins only as far as
// extent itself allows.
func ClampFixed(n, extent int) int {
	if hi := extent - MinRest; n > hi {
		n = hi
	}
	if n < MinFixed {
		n = MinFixed
	}
	if n > extent {
		n = extent
	}
	return n
}
