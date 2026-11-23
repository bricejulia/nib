package main

import (
	"path/filepath"

	"github.com/bricejulia/nib/internal/vcs/gitmerge"
)

// nextConflictedFile is the conflicted file after (or before) path among
// those git has unmerged under root, as an absolute path, wrapping round.
// ok is false when there is no other one to go to.
func nextConflictedFile(root, path string, forward bool) (string, bool) {
	rel, err := gitmerge.Conflicted(root)
	if err != nil {
		return "", false
	}
	abs := make([]string, len(rel))
	for i, p := range rel {
		abs[i] = filepath.Join(root, p)
	}
	return pickNext(abs, path, forward)
}

// pickNext is the entry of paths after (or before) current, wrapping
// round; when current isn't one of them, the first (or last). ok is false
// when paths holds nothing but current.
func pickNext(paths []string, current string, forward bool) (string, bool) {
	n := len(paths)
	at := -1
	for i, p := range paths {
		if p == current {
			at = i
		}
	}
	switch {
	case n == 0, n == 1 && at == 0:
		return "", false
	case at < 0 && forward:
		return paths[0], true
	case at < 0:
		return paths[n-1], true
	case forward:
		return paths[(at+1)%n], true
	default:
		return paths[(at-1+n)%n], true
	}
}
