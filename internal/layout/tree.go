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
