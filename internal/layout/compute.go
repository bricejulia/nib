package layout

// Compute walks the tree and returns each leaf's Rect within area. It is a
// pure function with no rendering side effects.
//
// Algorithm, per SplitNode: subtract every HintFixed child's size from the
// extent along Dir first (clamped so it never goes negative), then
// distribute the remainder across HintRatio children in proportion to
// Ratio/sum(Ratios), using the largest-remainder method so rounding never
// costs a child more than one column/row. The cross-axis dimension passes
// through unchanged to every child.
func Compute(root Node, area Rect) map[LeafID]Rect {
	out := map[LeafID]Rect{}
	computeInto(root, area, out)
	return out
}

func computeInto(n Node, area Rect, out map[LeafID]Rect) {
	switch v := n.(type) {
	case *LeafNode:
		out[v.ID] = area
	case *SplitNode:
		sizes := distribute(v.Dir, v.Children, area)
		offset := 0
		for i, c := range v.Children {
			var sub Rect
			if v.Dir == Horizontal {
				sub = Rect{X: area.X + offset, Y: area.Y, W: sizes[i], H: area.H}
			} else {
				sub = Rect{X: area.X, Y: area.Y + offset, W: area.W, H: sizes[i]}
			}
			computeInto(c.Node, sub, out)
			offset += sizes[i]
		}
	}
}

// distribute returns, for each child, its size along dir.
func distribute(dir Direction, children []Child, area Rect) []int {
	extent := area.W
	if dir == Vertical {
		extent = area.H
	}

	sizes := make([]int, len(children))
	remaining := extent
	var ratioSum float64
	ratioIdx := []int{}

	for i, c := range children {
		if c.Hint.Kind == HintFixed {
			n := c.Hint.Fixed
			if n > remaining {
				n = remaining
			}
			if n < 0 {
				n = 0
			}
			sizes[i] = n
			remaining -= n
		} else {
			ratioSum += c.Hint.Ratio
			ratioIdx = append(ratioIdx, i)
		}
	}

	if len(ratioIdx) == 0 || remaining <= 0 || ratioSum <= 0 {
		for _, i := range ratioIdx {
			sizes[i] = 0
		}
		return sizes
	}

	// Largest-remainder method: assign the floor share to each ratio
	// child, then hand out leftover units (remaining - sum(floors)) one
	// at a time to the children with the largest fractional remainder.
	type frac struct {
		idx  int
		rem  float64
		size int
	}
	fracs := make([]frac, len(ratioIdx))
	assigned := 0
	for k, i := range ratioIdx {
		share := float64(remaining) * children[i].Hint.Ratio / ratioSum
		floor := int(share)
		fracs[k] = frac{idx: i, rem: share - float64(floor), size: floor}
		sizes[i] = floor
		assigned += floor
	}
	leftover := remaining - assigned
	for leftover > 0 {
		best := -1
		for k := range fracs {
			if fracs[k].rem <= 0 {
				continue
			}
			if best == -1 || fracs[k].rem > fracs[best].rem {
				best = k
			}
		}
		if best == -1 {
			// All fractional remainders exhausted (e.g. exact
			// integer shares); hand the rest to the last ratio
			// child so nothing is silently dropped.
			sizes[ratioIdx[len(ratioIdx)-1]] += leftover
			break
		}
		sizes[fracs[best].idx]++
		fracs[best].rem = -1 // consumed
		leftover--
	}

	return sizes
}
