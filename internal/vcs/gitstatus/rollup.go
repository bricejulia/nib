package gitstatus

import "path"

// precedence ranks statuses so a directory rollup can pick the most
// "notable" status when multiple dirty children disagree.
func precedence(s Status) int {
	switch s {
	case Conflicted:
		return 5
	case Added:
		return 4
	case Deleted:
		return 3
	case Renamed:
		return 2
	case Modified:
		return 1
	case Untracked:
		return 0
	default:
		return -1
	}
}

// Rollup computes, for every ancestor directory of every dirty path, the
// highest-precedence status found beneath it — "a folder shows modified if
// anything beneath it is". It walks each dirty path's ancestor chain once
// while building the result, rather than being recomputed per node at
// render time.
func Rollup(direct map[string]Status) map[string]Status {
	rolled := make(map[string]Status, len(direct)*2)
	for p, s := range direct {
		rolled[p] = s
	}

	for relpath, st := range direct {
		for dir := path.Dir(relpath); dir != "." && dir != "/" && dir != ""; dir = path.Dir(dir) {
			if existing, ok := rolled[dir]; !ok || precedence(st) > precedence(existing) {
				rolled[dir] = st
			}
			if path.Dir(dir) == dir {
				break // guard against a non-terminating Dir() on odd input
			}
		}
	}

	return rolled
}
