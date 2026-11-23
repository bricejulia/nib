package merge

import (
	"strings"
	"unicode"
)

// AutoResolve is the magic wand for one region: it resolves the region
// when the two sides' changes don't actually collide, and reports false
// when they do (or when the region has no base to judge that against).
//
// In order:
//   - one side is unchanged from the base: take the other;
//   - both sides made the same change: take it;
//   - the sides changed different lines (a line-level three-way merge);
//   - the sides changed different words, even on the same line (a
//     token-level three-way merge) — the case git itself always leaves as
//     a conflict, because it merges whole lines.
func (r Region) AutoResolve() ([]string, bool) {
	if !r.HasBase() {
		return nil, false
	}
	switch {
	case equal(r.Left, r.BaseLines):
		return r.TakeRight(), true
	case equal(r.Right, r.BaseLines), equal(r.Left, r.Right):
		return r.TakeLeft(), true
	}
	if merged, ok := Merge3(r.BaseLines, r.Left, r.Right); ok {
		return merged, true
	}
	return mergeTokens(r.BaseLines, r.Left, r.Right)
}

// mergeTokens three-way merges the regions' text word by word, keeping
// line breaks as tokens of their own so the result splits back into lines.
func mergeTokens(base, left, right []string) ([]string, bool) {
	merged, ok := Merge3(tokenize(base), tokenize(left), tokenize(right))
	if !ok {
		return nil, false
	}
	text := strings.Join(merged, "")
	if text == "" {
		return nil, true
	}
	return strings.Split(strings.TrimSuffix(text, "\n"), "\n"), true
}

// tokenize splits lines into runs of word characters, runs of horizontal
// whitespace, single punctuation characters and line breaks. Each line is
// followed by a "\n" token, including the last, so an edit to the end of
// the last line is still anchored on something both sides share.
func tokenize(lines []string) []string {
	var toks []string
	for _, line := range lines {
		rs := []rune(line)
		for i := 0; i < len(rs); {
			j := i + 1
			switch {
			case isWord(rs[i]):
				for j < len(rs) && isWord(rs[j]) {
					j++
				}
			case rs[i] == ' ' || rs[i] == '\t':
				for j < len(rs) && (rs[j] == ' ' || rs[j] == '\t') {
					j++
				}
			}
			toks = append(toks, string(rs[i:j]))
			i = j
		}
		toks = append(toks, "\n")
	}
	return toks
}

func isWord(r rune) bool {
	return r == '_' || unicode.IsLetter(r) || unicode.IsDigit(r)
}

// Merge3 is a three-way merge of left and right, both derived from base:
// a change made on one side only is applied, the same change made on both
// sides is applied once, and different changes to the same stretch of
// base are a conflict (ok is false, merged is nil). Changes to adjacent
// stretches with nothing kept in between also count as touching the same
// stretch, as in git.
func Merge3(base, left, right []string) (merged []string, ok bool) {
	lm := matchBase(base, left)
	rm := matchBase(base, right)
	merged = []string{}
	i, a, b := 0, 0, 0
	for {
		// The stable run: base lines kept, in place, on both sides.
		j := 0
		for i+j < len(base) && lm[i+j] == a+j && rm[i+j] == b+j {
			j++
		}
		if j > 0 {
			merged = append(merged, base[i:i+j]...)
			i, a, b = i+j, a+j, b+j
			continue
		}
		if i == len(base) && a == len(left) && b == len(right) {
			return merged, true
		}
		// The unstable chunk: everything up to the next base line both
		// sides kept (or to the end).
		k := i
		for k < len(base) && (lm[k] < 0 || rm[k] < 0) {
			k++
		}
		ae, be := len(left), len(right)
		if k < len(base) {
			ae, be = lm[k], rm[k]
		}
		o, l, r := base[i:k], left[a:ae], right[b:be]
		switch {
		case equal(l, o):
			merged = append(merged, r...)
		case equal(r, o), equal(l, r):
			merged = append(merged, l...)
		default:
			return nil, false
		}
		i, a, b = k, ae, be
	}
}

// matchBase maps each base index to the index of the line it is matched
// with in other by a longest common subsequence, or -1. The mapping only
// ever increases, which is what lets Merge3 walk the three in lockstep.
func matchBase(base, other []string) []int {
	match := make([]int, len(base))
	for i := range match {
		match[i] = -1
	}
	// The common prefix and suffix match trivially; only what's between
	// needs the quadratic table.
	pre := 0
	for pre < len(base) && pre < len(other) && base[pre] == other[pre] {
		match[pre] = pre
		pre++
	}
	suf := 0
	for suf < len(base)-pre && suf < len(other)-pre &&
		base[len(base)-1-suf] == other[len(other)-1-suf] {
		match[len(base)-1-suf] = len(other) - 1 - suf
		suf++
	}
	b, o := base[pre:len(base)-suf], other[pre:len(other)-suf]
	n, m := len(b), len(o)
	if n == 0 || m == 0 || n*m > maxTableCells {
		// Too big to compare: leaving the middle unmatched makes it one
		// changed chunk, so a real difference surfaces as a conflict
		// rather than a guess.
		return match
	}
	// lcs[i][j] is the LCS length of b[i:] and o[j:].
	lcs := make([][]int32, n+1)
	for i := range lcs {
		lcs[i] = make([]int32, m+1)
	}
	for i := n - 1; i >= 0; i-- {
		for j := m - 1; j >= 0; j-- {
			if b[i] == o[j] {
				lcs[i][j] = lcs[i+1][j+1] + 1
			} else {
				lcs[i][j] = max(lcs[i+1][j], lcs[i][j+1])
			}
		}
	}
	for i, j := 0, 0; i < n && j < m; {
		switch {
		case b[i] == o[j]:
			match[pre+i] = pre + j
			i++
			j++
		case lcs[i+1][j] >= lcs[i][j+1]:
			i++
		default:
			j++
		}
	}
	return match
}

// maxTableCells bounds matchBase's table (4 bytes a cell) so a huge
// conflict can't make the wand allocate hundreds of megabytes.
const maxTableCells = 4_000_000

func equal(a, b []string) bool {
	if len(a) != len(b) {
		return false
	}
	for i := range a {
		if a[i] != b[i] {
			return false
		}
	}
	return true
}
