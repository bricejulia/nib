package filetree

// Row is one visible line of the flattened tree.
type Row struct {
	Node  *Node
	Depth int
}

// Flatten walks the already-expanded tree once and produces the render
// slice: scrolling, cursor movement, and mouse clicks all become plain
// indexing into this slice afterward. It only recurses into a directory
// that is both Loaded and Expanded — collapsed or not-yet-loaded
// directories contribute a single row with no children, so Flatten's cost
// is bounded by what's currently visible, not the whole project.
//
// Call this only on structural change (expand/collapse/lazy-load/git or fs
// refresh), never per frame — see View's dirty flag.
func Flatten(root *Node) []Row {
	var rows []Row
	var walk func(n *Node, depth int)
	walk = func(n *Node, depth int) {
		for _, c := range n.Children {
			rows = append(rows, Row{Node: c, Depth: depth})
			if c.IsDir && c.Expanded && c.Loaded {
				walk(c, depth+1)
			}
		}
	}
	walk(root, 0)
	return rows
}
