package finder

import "testing"

func TestFuzzyMatchEmptyQueryMatchesEverything(t *testing.T) {
	score, ok := fuzzyMatch("", "anything/at/all.go")
	if !ok {
		t.Fatal("empty query should match")
	}
	if score != 0 {
		t.Errorf("empty query should score 0, got %d", score)
	}
}

func TestFuzzyMatchSubsequenceInOrder(t *testing.T) {
	if _, ok := fuzzyMatch("mvc", "main.go"); ok {
		t.Error(`"mvc" should not match "main.go" (runes not in order/present)`)
	}
	if _, ok := fuzzyMatch("man", "main.go"); !ok {
		t.Error(`"man" should match "main.go" as a subsequence (m-a-...-n)`)
	}
}

func TestFuzzyMatchCaseInsensitive(t *testing.T) {
	if _, ok := fuzzyMatch("MAIN", "src/main.go"); !ok {
		t.Error("match should be case-insensitive")
	}
}

func TestFuzzyMatchRanksBoundaryMatchHigher(t *testing.T) {
	// "main" starts right at a path-segment boundary in both, but
	// "src/main.go" has an earlier/cleaner boundary right before 'm'
	// than "domain.go" (where 'm' is mid-word, no boundary bonus).
	boundaryScore, ok1 := fuzzyMatch("main", "src/main.go")
	midWordScore, ok2 := fuzzyMatch("main", "domain.go")
	if !ok1 || !ok2 {
		t.Fatal("both candidates should match")
	}
	if boundaryScore <= midWordScore {
		t.Errorf("expected boundary match to score higher: boundary=%d midword=%d", boundaryScore, midWordScore)
	}
}

func TestFuzzyMatchRanksConsecutiveHigher(t *testing.T) {
	consecutive, ok1 := fuzzyMatch("main", "main.go")
	scattered, ok2 := fuzzyMatch("main", "m_a_i_n.go")
	if !ok1 || !ok2 {
		t.Fatal("both candidates should match")
	}
	if consecutive <= scattered {
		t.Errorf("expected consecutive match to score higher: consecutive=%d scattered=%d", consecutive, scattered)
	}
}

func TestFuzzyMatchContiguousSubstringBeatsScatteredSubsequence(t *testing.T) {
	// Regression test: a naive per-character boundary bonus alone would
	// rank "m_a_i_n.go" above "main.go" for query "main", since every
	// character in the scattered version sits right after an underscore
	// (a boundary). A contiguous match must win regardless.
	contiguous, ok1 := fuzzyMatch("main", "main.go")
	scattered, ok2 := fuzzyMatch("main", "m_a_i_n.go")
	if !ok1 || !ok2 {
		t.Fatal("both candidates should match")
	}
	if contiguous <= scattered {
		t.Errorf("expected the contiguous match to score higher: contiguous=%d scattered=%d", contiguous, scattered)
	}
}

func TestFuzzyMatchNoMatchWhenRuneMissing(t *testing.T) {
	if _, ok := fuzzyMatch("xyz", "main.go"); ok {
		t.Error(`"xyz" should not match "main.go"`)
	}
}

func TestFuzzyMatchShorterCandidatePreferredAsTiebreaker(t *testing.T) {
	short, ok1 := fuzzyMatch("main", "main.go")
	long, ok2 := fuzzyMatch("main", "main.go.backup.old")
	if !ok1 || !ok2 {
		t.Fatal("both candidates should match")
	}
	if short <= long {
		t.Errorf("expected the shorter candidate to score higher as a tiebreaker: short=%d long=%d", short, long)
	}
}
