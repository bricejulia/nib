package editor

import (
	"path/filepath"
	"strings"
)

// pathUnder reports whether p is target itself or a file underneath it,
// target being a directory. The separator-terminated prefix test is the
// point: a bare strings.HasPrefix would report /a/foobar as living under
// /a/foo.
//
// Pure string comparison, exactly like View.Open's own already-open check —
// every path this package holds is clean and absolute (they come from
// filepath.Join'd file-tree nodes and from finder results).
func pathUnder(target, p string) bool {
	return p == target || strings.HasPrefix(p, target+string(filepath.Separator))
}

// movedPath maps p through a move of oldPath to newPath: newPath itself when
// p IS oldPath, or newPath plus the remainder when p sits underneath it (a
// directory move, which relocates every open file inside it at once). ok is
// false when p is unaffected.
//
// The single home for this arithmetic: View.Repath and CloseTabsUnder both
// go through here rather than each growing their own prefix check.
func movedPath(oldPath, newPath, p string) (string, bool) {
	if p == oldPath {
		return newPath, true
	}
	if !pathUnder(oldPath, p) {
		return "", false
	}
	return filepath.Join(newPath, p[len(oldPath)+1:]), true
}
