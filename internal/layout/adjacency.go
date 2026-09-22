package layout

// Neighbor finds the leaf geometrically adjacent to from in direction dir,
// on the far side (forward=true: right for Horizontal, down for Vertical)
// or near side (forward=false: left/up), using rects — the same
// map[LeafID]Rect Compute produces every frame (see App.Rects). Because
// Compute always produces an exact tiling (no gaps or overlaps between
// siblings), "touches from's edge with positive overlap on the cross
// axis" is both necessary and sufficient for true visual adjacency. This
// is deliberately geometric rather than structural (walking
// FindParentNode ancestors): tree order and visual order can diverge —
// e.g. a horizontal split whose right child is itself split vertically
// puts "next leaf in tree order" directly below the left child, not to
// its right — and Compute's rects sidestep that entirely.
//
// When more than one leaf touches the matching edge (the far side was
// itself split further along the cross axis), the one with the greatest
// overlap wins; ties resolve to whichever candidate starts first along
// the cross axis (topmost for Horizontal, leftmost for Vertical) so the
// result never depends on Go's randomized map iteration order.
//
// ok is false if from is not in rects (including when rects is nil), or
// nothing touches the matching edge with positive overlap.
func Neighbor(rects map[LeafID]Rect, from LeafID, dir Direction, forward bool) (LeafID, bool) {
	src, ok := rects[from]
	if !ok {
		return 0, false
	}
	var best LeafID
	var bestOverlap, bestCross int
	found := false
	for id, r := range rects {
		if id == from {
			continue
		}
		var touches bool
		var overlap, cross int
		if dir == Horizontal {
			if forward {
				touches = r.X == src.X+src.W
			} else {
				touches = r.X+r.W == src.X
			}
			overlap, cross = overlapLen(src.Y, src.Y+src.H, r.Y, r.Y+r.H), r.Y
		} else {
			if forward {
				touches = r.Y == src.Y+src.H
			} else {
				touches = r.Y+r.H == src.Y
			}
			overlap, cross = overlapLen(src.X, src.X+src.W, r.X, r.X+r.W), r.X
		}
		if !touches || overlap <= 0 {
			continue
		}
		if !found || overlap > bestOverlap || (overlap == bestOverlap && cross < bestCross) {
			best, bestOverlap, bestCross, found = id, overlap, cross, true
		}
	}
	return best, found
}

// overlapLen returns the length of the intersection of [a0,a1) and
// [b0,b1); <= 0 means no overlap.
func overlapLen(a0, a1, b0, b1 int) int {
	lo, hi := a0, a1
	if b0 > lo {
		lo = b0
	}
	if b1 < hi {
		hi = b1
	}
	return hi - lo
}
