package layout

// Direction is the axis along which a SplitNode's children are stacked.
type Direction int

const (
	Horizontal Direction = iota // children stacked left-to-right
	Vertical                    // children stacked top-to-bottom
)

// SizeHintKind selects which field of a SizeHint is meaningful.
type SizeHintKind int

const (
	HintRatio SizeHintKind = iota
	HintFixed
)

// SizeHint describes how much space a child claims along its parent's
// Direction: either a fixed number of columns/rows, or a share of whatever
// space remains after fixed children are subtracted.
type SizeHint struct {
	Kind  SizeHintKind
	Ratio float64 // meaningful when Kind == HintRatio
	Fixed int     // meaningful when Kind == HintFixed
}

// Ratio returns a SizeHint that claims a proportional share of the
// remaining space.
func Ratio(r float64) SizeHint {
	return SizeHint{Kind: HintRatio, Ratio: r}
}

// Fixed returns a SizeHint that claims an exact number of columns or rows.
func Fixed(n int) SizeHint {
	return SizeHint{Kind: HintFixed, Fixed: n}
}

// LeafID is a stable identity for a leaf, assigned once at construction.
// It must never be derived from the leaf's position in the tree, so that
// focus survives re-layout.
type LeafID uint64

// Node is either a *SplitNode or a *LeafNode.
type Node interface {
	isNode()
}

// Child pairs a child Node with the SizeHint its parent should give it.
type Child struct {
	Node Node
	Hint SizeHint
}

// SplitNode divides its area among its Children along Dir.
type SplitNode struct {
	Dir      Direction
	Children []Child
}

// LeafNode holds a single View. ID is assigned by the caller and must be
// unique and stable for the lifetime of the tree.
type LeafNode struct {
	ID   LeafID
	View View
}

func (*SplitNode) isNode() {}
func (*LeafNode) isNode()  {}

// Leaves walks the tree and returns every LeafNode in left-to-right,
// top-to-bottom traversal order. This is only ever called on structural
// tree changes (construction, focus rebuild), never per frame.
func Leaves(root Node) []*LeafNode {
	var out []*LeafNode
	var walk func(n Node)
	walk = func(n Node) {
		switch v := n.(type) {
		case *LeafNode:
			out = append(out, v)
		case *SplitNode:
			for _, c := range v.Children {
				walk(c.Node)
			}
		}
	}
	walk(root)
	return out
}

// FindParentNode returns the SplitNode whose Children directly contains
// target (matched by identity, via ==, which is well-defined since Node
// always wraps a pointer — target must be the exact *SplitNode or
// *LeafNode already living in the tree, never a copy), and target's index
// within it. ok is false if target is not anywhere in root's Children,
// including if target IS root itself (which has no parent).
func FindParentNode(root Node, target Node) (parent *SplitNode, index int, ok bool) {
	sp, isSplit := root.(*SplitNode)
	if !isSplit {
		return nil, 0, false
	}
	for i, c := range sp.Children {
		if c.Node == target {
			return sp, i, true
		}
	}
	for _, c := range sp.Children {
		if p, i, found := FindParentNode(c.Node, target); found {
			return p, i, true
		}
	}
	return nil, 0, false
}

// Split replaces target's slot in its parent's Children with a new
// SplitNode containing target and newLeaf as two equally-sized (Ratio(1))
// children in direction dir. Returns false if target has no parent in
// root (target IS root).
func Split(root Node, target *LeafNode, dir Direction, newLeaf *LeafNode) bool {
	parent, idx, ok := FindParentNode(root, target)
	if !ok {
		return false
	}
	parent.Children[idx].Node = &SplitNode{
		Dir: dir,
		Children: []Child{
			{Node: target, Hint: Ratio(1)},
			{Node: newLeaf, Hint: Ratio(1)},
		},
	}
	return true
}

// Close removes target by collapsing its parent SplitNode into just the
// surviving sibling. This assumes target's parent has EXACTLY 2 children
// — true for any SplitNode Split ever creates (Close is Split's exact
// inverse, not a general n-ary remove). Returns (nil, false) if target
// has no parent, or if target's parent itself has no parent (nothing to
// collapse into). Callers can use Leaves(survivor)[0].ID to pick a leaf
// to focus afterward.
func Close(root Node, target *LeafNode) (survivor Node, ok bool) {
	parent, idx, ok := FindParentNode(root, target)
	if !ok {
		return nil, false
	}
	sibling := parent.Children[1-idx].Node
	grandparent, gpIdx, ok := FindParentNode(root, parent)
	if !ok {
		return nil, false
	}
	grandparent.Children[gpIdx].Node = sibling
	return sibling, true
}
