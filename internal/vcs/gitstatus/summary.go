package gitstatus

import (
	"fmt"
	"strings"
)

// Summary condenses a direct status map into a short "+A ~M -D ?U" style
// count string for a status bar (omitting any counter that is zero), e.g.
// "+2 ~1 ?3". It returns "" when direct is clean.
func Summary(direct map[string]Status) string {
	var added, modified, deleted, untracked, conflicted int
	for _, s := range direct {
		switch s {
		case Added:
			added++
		case Modified, Renamed:
			modified++
		case Deleted:
			deleted++
		case Untracked:
			untracked++
		case Conflicted:
			conflicted++
		case Unmodified:
			// Not counted: Summary only reports what changed.
		}
	}

	var parts []string
	if conflicted > 0 {
		parts = append(parts, fmt.Sprintf("!%d", conflicted))
	}
	if added > 0 {
		parts = append(parts, fmt.Sprintf("+%d", added))
	}
	if modified > 0 {
		parts = append(parts, fmt.Sprintf("~%d", modified))
	}
	if deleted > 0 {
		parts = append(parts, fmt.Sprintf("-%d", deleted))
	}
	if untracked > 0 {
		parts = append(parts, fmt.Sprintf("?%d", untracked))
	}
	return strings.Join(parts, " ")
}
