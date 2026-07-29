package finder

import "strings"

// fuzzyMatch reports whether every rune of query appears in candidate, in
// order (a case-insensitive subsequence match), and scores it in two
// tiers:
//
//  1. A base subsequence score: higher for runs starting right at a
//     path/word boundary, higher still for consecutive matched runes.
//     This alone would rank "m_a_i_n.go" (a boundary at every character,
//     thanks to the underscores) above "main.go" for query "main", which
//     is backwards from what anyone would expect.
//  2. A large flat bonus — dominating the base score — if query also
//     appears as one contiguous run in candidate, so a clean substring
//     match always outranks a scattered one; an extra bonus if that run
//     starts at a boundary.
//
// A small length penalty breaks remaining ties toward shorter/more
// specific paths. This is a simple heuristic in the spirit of
// fzf/VSCode's quick-open matching, not a port of either.
func fuzzyMatch(query, candidate string) (score int, ok bool) {
	if query == "" {
		return 0, true
	}

	q := []rune(strings.ToLower(query))
	c := []rune(strings.ToLower(candidate))

	qi := 0
	consecutive := 0
	for ci := 0; ci < len(c) && qi < len(q); ci++ {
		if c[ci] != q[qi] {
			consecutive = 0
			continue
		}
		bonus := 1
		if ci == 0 || isBoundaryRune(c[ci-1]) {
			bonus += 3
		}
		if consecutive > 0 {
			bonus += 3
		}
		score += bonus
		consecutive++
		qi++
	}
	if qi < len(q) {
		return 0, false
	}

	if idx := runeIndex(c, q); idx >= 0 {
		score += 100
		if idx == 0 || isBoundaryRune(c[idx-1]) {
			score += 20
		}
	}

	score -= len(c) / 10
	return score, true
}

// runeIndex returns the index of the first contiguous occurrence of q
// within c, or -1 if q never appears as one unbroken run.
func runeIndex(c, q []rune) int {
	if len(q) == 0 || len(q) > len(c) {
		return -1
	}
	for i := 0; i+len(q) <= len(c); i++ {
		match := true
		for j := range q {
			if c[i+j] != q[j] {
				match = false
				break
			}
		}
		if match {
			return i
		}
	}
	return -1
}

func isBoundaryRune(r rune) bool {
	return r == '/' || r == '_' || r == '-' || r == '.'
}
