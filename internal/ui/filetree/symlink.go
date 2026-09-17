package filetree

import (
	"os"
	"path/filepath"
)

// LinkState classifies a symlink entry once its chain has been resolved.
// The zero value, LinkNone, means "not a symlink at all" — every Node
// starts there, and only EnsureLoaded's symlink branch ever sets one of
// the other three.
type LinkState int

const (
	LinkNone LinkState = iota
	// LinkOK: the chain resolves to a real file or directory inside the
	// project root. Followed normally.
	LinkOK
	// LinkBroken: the chain ends at a target that doesn't exist.
	LinkBroken
	// LinkBlocked: the chain resolves fine, but not to somewhere nib will
	// follow it — either the real target sits outside the project root,
	// or following it would loop (a cycle in the readlink chain itself,
	// or the target is an ancestor directory already open in the tree).
	LinkBlocked
)

// maxSymlinkHops bounds how many links resolveSymlinkChain will follow
// before giving up and reporting a cycle. 40 matches Linux's own ELOOP
// threshold — comfortably more than any real chain, so hitting it only
// ever means an actual loop.
const maxSymlinkHops = 40

// resolveSymlinkChain follows path — which must itself be a symlink —
// through as many hops as it takes to reach a non-symlink, and classifies
// what it finds. It never returns an error: every outcome is expressed as
// a LinkState instead, because a broken or looping link is routine content
// for the tree to display, not a failure of the walk it, itself.
//
// real is the last path visited, valid for every state: the missing
// target for LinkBroken, the path that would repeat for LinkBlocked's
// cycle case, and the actual resolved target for LinkOK and for
// LinkBlocked's outside-root case (so the caller can still show the user
// where it points).
func resolveSymlinkChain(root, path string) (real string, isDir bool, state LinkState) {
	visited := map[string]bool{}
	cur := path
	for range maxSymlinkHops {
		if visited[cur] {
			return cur, false, LinkBlocked
		}
		visited[cur] = true

		fi, err := os.Lstat(cur)
		if err != nil {
			return cur, false, LinkBroken
		}
		if fi.Mode()&os.ModeSymlink == 0 {
			if _, inside := relPath(root, cur); !inside {
				return cur, fi.IsDir(), LinkBlocked
			}
			return cur, fi.IsDir(), LinkOK
		}

		target, err := os.Readlink(cur)
		if err != nil {
			return cur, false, LinkBroken
		}
		if !filepath.IsAbs(target) {
			target = filepath.Join(filepath.Dir(cur), target)
		}
		cur = filepath.Clean(target)
	}
	return cur, false, LinkBlocked
}

// realPath is where n's content actually lives on disk: n's own Path,
// except for a followed directory symlink, which reports what it points
// at instead. Used by isAncestorCycle to catch a symlink that resolves to
// a directory already open above it in the tree — a loop resolveSymlinkChain
// itself can't see, since nothing about the readlink chain repeats; it's
// the TREE that would recurse forever, not the filesystem lookup.
func (n *Node) realPath() string {
	if n.IsSymlink && n.LinkState == LinkOK {
		return n.LinkReal
	}
	return n.Path
}

// isAncestorCycle reports whether real — a followed symlink's resolved
// directory — is also the real location of parent or any directory above
// it. Expanding such a symlink would show the same directory's contents
// again, forever.
func isAncestorCycle(parent *Node, real string) bool {
	for a := parent; a != nil; a = a.Parent {
		if a.realPath() == real {
			return true
		}
	}
	return false
}
