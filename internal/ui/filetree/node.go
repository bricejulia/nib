// Package filetree implements the file-tree pane: a lazily-loaded directory
// tree, flattened to a row slice for O(1) scroll/click indexing, with
// git-status markers.
package filetree

import (
	"os"
	"path/filepath"
	"sort"
	"strings"

	"github.com/bricejulia/nib/internal/vcs/gitstatus"
)

// Node is one entry in the file tree. Children is nil until Loaded, so
// startup never walks the whole project (vendor/, node_modules would hang
// it).
type Node struct {
	Name     string
	Path     string // absolute path
	IsDir    bool
	Expanded bool
	Loaded   bool
	Children []*Node
	Parent   *Node
	Status   gitstatus.Status
}

// NewRoot builds the (unloaded) root node for absPath. Call EnsureLoaded to
// populate its immediate children.
func NewRoot(absPath string) *Node {
	return &Node{
		Name:     filepath.Base(absPath),
		Path:     absPath,
		IsDir:    true,
		Expanded: true,
	}
}

// relPath returns path relative to root, in the forward-slash form git
// uses in its porcelain output.
func relPath(root, path string) (string, bool) {
	rel, err := filepath.Rel(root, path)
	if err != nil || rel == "." || strings.HasPrefix(rel, "..") {
		return "", false
	}
	return filepath.ToSlash(rel), true
}

// EnsureLoaded populates n.Children from disk on first call. Subsequent
// calls are no-ops UNLESS Loaded has been reset to false (e.g. by
// invalidateExpanded after an fsnotify refresh), in which case it re-reads
// the directory but carries over each surviving child's Expanded, Loaded,
// Children, and Status from before the reload — otherwise every re-scan
// would rebuild fresh Node structs with Expanded: false, silently
// collapsing whatever subdirectories were open.
func (n *Node) EnsureLoaded() error {
	if n.Loaded || !n.IsDir {
		return nil
	}
	entries, err := os.ReadDir(n.Path)
	if err != nil {
		n.Loaded = true // don't retry every render on a permission error
		return err
	}

	existing := make(map[string]*Node, len(n.Children))
	for _, c := range n.Children {
		existing[c.Name] = c
	}

	children := make([]*Node, 0, len(entries))
	for _, e := range entries {
		child := &Node{
			Name:   e.Name(),
			Path:   filepath.Join(n.Path, e.Name()),
			IsDir:  e.IsDir(),
			Parent: n,
		}
		if old, ok := existing[e.Name()]; ok && old.IsDir == child.IsDir {
			child.Expanded = old.Expanded
			child.Loaded = old.Loaded
			child.Children = old.Children
			child.Status = old.Status
		}
		children = append(children, child)
	}
	sortChildren(children)

	n.Children = children
	n.Loaded = true
	return nil
}

// sortChildren puts a directory's children in display order: directories
// first, then alphabetically. Shared by EnsureLoaded and by the in-place
// tree updates that follow a rename (see View.moveNodeInTree), so a moved
// node lands where a fresh re-scan would have put it.
func sortChildren(children []*Node) {
	sort.Slice(children, func(i, j int) bool {
		if children[i].IsDir != children[j].IsDir {
			return children[i].IsDir // dirs first
		}
		return children[i].Name < children[j].Name
	})
}

// child returns n's already-loaded child named name, or nil. It never reads
// the disk — a nil result means "not loaded, or not there", and the caller
// is expected to treat both the same way.
func (n *Node) child(name string) *Node {
	for _, c := range n.Children {
		if c.Name == name {
			return c
		}
	}
	return nil
}

// Retarget rewrites n's Name and Path — and, recursively, every loaded
// descendant's Path — after n itself was renamed or its containing
// directory moved on disk.
//
// This is what lets a rename preserve node identity. Without it the only
// way to reflect a rename would be to invalidate the parent and let
// EnsureLoaded rebuild, and EnsureLoaded carries Expanded/Loaded/Children
// over by NAME — so a rename reads as a delete plus a create, and a
// renamed open directory silently collapses and drops its loaded subtree.
func (n *Node) Retarget(newParentPath, newName string) {
	n.Name = newName
	n.Path = filepath.Join(newParentPath, newName)
	for _, c := range n.Children {
		c.Retarget(n.Path, c.Name)
	}
}
