package filetree

import (
	"path"
	"path/filepath"
	"sort"

	"github.com/bricejulia/nib/internal/vcs/gitstatus"
)

// buildChangesTree builds the synthetic tree ModeChanges renders: the same
// shape EnsureLoaded would produce for rootAbsPath, but pruned to only the
// paths in direct (git's per-file status, keyed by repo-relative,
// forward-slash path — see gitstatus.RunPorcelain) and their ancestor
// directories.
//
// Every directory comes up expanded except those whose absolute path is in
// collapsed, which is how ApplyChanges carries the user's collapses across
// a rebuild (nil collapses nothing).
//
// Unlike EnsureLoaded, this never touches disk. It's built purely from the
// path strings git reports, which is what lets a deleted-but-still-tracked
// file show up here even though it has nothing left on disk to os.Stat —
// something ModeFiles, which lists real directory entries, can't do.
func buildChangesTree(rootAbsPath string, direct map[string]gitstatus.Status, collapsed map[string]bool) *Node {
	root := &Node{Name: filepath.Base(rootAbsPath), Path: rootAbsPath, IsDir: true, Expanded: true, Loaded: true}
	if len(direct) == 0 {
		return root
	}
	rolled := gitstatus.Rollup(direct)

	// dirs is keyed by repo-relative path ("" for the root itself) so a
	// directory shared by more than one dirty file is only ever created
	// once, however many of its descendants are dirty.
	dirs := map[string]*Node{"": root}
	var dirNode func(rel string) *Node
	dirNode = func(rel string) *Node {
		if n, ok := dirs[rel]; ok {
			return n
		}
		parentRel := path.Dir(rel)
		if parentRel == "." {
			parentRel = ""
		}
		parent := dirNode(parentRel)
		abs := filepath.Join(rootAbsPath, filepath.FromSlash(rel))
		n := &Node{
			Name:     path.Base(rel),
			Path:     abs,
			IsDir:    true,
			Expanded: !collapsed[abs],
			Loaded:   true,
			Parent:   parent,
			Status:   rolled[rel],
		}
		parent.Children = append(parent.Children, n)
		dirs[rel] = n
		return n
	}

	rels := make([]string, 0, len(direct))
	for rel := range direct {
		rels = append(rels, rel)
	}
	sort.Strings(rels)

	for _, rel := range rels {
		parentRel := path.Dir(rel)
		if parentRel == "." {
			parentRel = ""
		}
		parent := dirNode(parentRel)
		leaf := &Node{
			Name:   path.Base(rel),
			Path:   filepath.Join(rootAbsPath, filepath.FromSlash(rel)),
			Loaded: true,
			Parent: parent,
			Status: direct[rel],
		}
		parent.Children = append(parent.Children, leaf)
	}

	sortTree(root)
	return root
}

// sortTree applies sortChildren (dirs first, then alphabetical — see
// node.go) recursively, so ordering matches the ModeFiles tree's
// convention.
func sortTree(n *Node) {
	sortChildren(n.Children)
	for _, c := range n.Children {
		sortTree(c)
	}
}
