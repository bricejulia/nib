// Package merge understands the conflict regions git leaves in a file it
// could not merge on its own: it finds them (Parse), resolves one by
// picking a side (Region.TakeLeft and friends), and resolves the easy ones
// automatically (Region.AutoResolve, the "magic wand").
//
// Sides are called left and right — the upper and lower blocks of a
// region — never ours and theirs: during a rebase git's "ours" is the
// upstream and "theirs" is your own commit, which is exactly backwards
// from what those words suggest. The labels git writes after the markers
// (HEAD, a commit and its subject) are kept so the UI can show which is
// which.
//
// Pure: no git, no I/O. Everything here works on a file's lines.
package merge

import "strings"

// minMarkerLen is git's default conflict-marker-size. A repository can ask
// for longer markers (the conflict-marker-size attribute), so any run of
// at least this many is accepted, as long as every marker of one region
// has the same length.
const minMarkerLen = 7

// Region is one conflict region: the lines from its "<<<<<<<" marker to
// its ">>>>>>>" marker, inclusive. Line numbers are 0-based indexes into
// the lines Parse was given.
type Region struct {
	Start int // the "<<<<<<<" line
	Base  int // the "|||||||" line, or -1 without a base section
	Mid   int // the "=======" line
	End   int // the ">>>>>>>" line

	LeftLabel, BaseLabel, RightLabel string

	Left, BaseLines, Right []string
}

// HasBase reports whether the region carries the common ancestor's text
// (git's diff3 or zdiff3 conflict style), which AutoResolve needs.
func (r Region) HasBase() bool { return r.Base >= 0 }

// Contains reports whether line ln is inside the region, markers included.
func (r Region) Contains(ln int) bool { return ln >= r.Start && ln <= r.End }

// TakeLeft is the region resolved to its upper side.
func (r Region) TakeLeft() []string { return clone(r.Left) }

// TakeRight is the region resolved to its lower side.
func (r Region) TakeRight() []string { return clone(r.Right) }

// TakeBoth is the region resolved to its upper side followed by its lower.
func (r Region) TakeBoth() []string {
	return append(clone(r.Left), r.Right...)
}

// TakeNeither is the region resolved to nothing.
func (r Region) TakeNeither() []string { return nil }

// Parse finds every well-formed conflict region in lines, in order. A
// "<<<<<<<" with no matching "=======" and ">>>>>>>" after it is not a
// region (it could be a line of a document about git); a second
// "<<<<<<<" before the first one closes restarts the search there.
func Parse(lines []string) []Region {
	var regions []Region
	for i := 0; i < len(lines); i++ {
		n, ok := marker(lines[i], '<')
		if !ok {
			continue
		}
		r, ok := parseRegion(lines, i, n)
		if !ok {
			continue
		}
		regions = append(regions, r)
		i = r.End
	}
	return regions
}

// parseRegion reads the region whose "<<<<<<<" (n characters long) is at
// lines[start].
func parseRegion(lines []string, start, n int) (Region, bool) {
	r := Region{Start: start, Base: -1, Mid: -1, End: -1, LeftLabel: label(lines[start], n)}
	for i := start + 1; i < len(lines); i++ {
		line := lines[i]
		if m, ok := marker(line, '<'); ok && m == n {
			return Region{}, false // a new region opens before this one closed
		}
		switch {
		case r.Mid < 0 && r.Base < 0 && isMarker(line, '|', n):
			r.Base = i
			r.BaseLabel = label(line, n)
		case r.Mid < 0 && isMarker(line, '=', n) && len(strings.TrimRight(line, " \t")) == n:
			r.Mid = i
		case r.Mid >= 0 && isMarker(line, '>', n):
			r.End = i
			r.RightLabel = label(line, n)
			leftEnd := r.Mid
			if r.Base >= 0 {
				leftEnd = r.Base
				r.BaseLines = clone(lines[r.Base+1 : r.Mid])
			}
			r.Left = clone(lines[start+1 : leftEnd])
			r.Right = clone(lines[r.Mid+1 : r.End])
			return r, true
		}
	}
	return Region{}, false
}

// marker reports whether line opens with a run of at least minMarkerLen
// c characters followed by nothing or a space, and how long the run is.
func marker(line string, c byte) (int, bool) {
	n := 0
	for n < len(line) && line[n] == c {
		n++
	}
	if n < minMarkerLen {
		return 0, false
	}
	if n < len(line) && line[n] != ' ' {
		return 0, false
	}
	return n, true
}

// isMarker reports whether line is a c marker exactly n characters long.
func isMarker(line string, c byte, n int) bool {
	m, ok := marker(line, c)
	return ok && m == n
}

// label is whatever git wrote after a marker: "HEAD", "abc1234 (subject)".
func label(line string, n int) string {
	return strings.TrimSpace(line[n:])
}

// IsMarkerLine reports whether line is one of a region's four marker
// lines, for styling.
func (r Region) IsMarkerLine(ln int) bool {
	return ln == r.Start || ln == r.Base || ln == r.Mid || ln == r.End
}

// Side names which part of a region a line belongs to.
type Side int

const (
	SideNone Side = iota
	SideLeft
	SideBase
	SideRight
)

// SideOf reports which part of the region line ln belongs to; marker lines
// belong to the part they open (the "<<<<<<<" to the left, the "|||||||"
// to the base, the "=======" and ">>>>>>>" to the right).
func (r Region) SideOf(ln int) Side {
	switch {
	case !r.Contains(ln):
		return SideNone
	case r.Base >= 0 && ln >= r.Base && ln < r.Mid:
		return SideBase
	case ln >= r.Mid:
		return SideRight
	default:
		return SideLeft
	}
}

func clone(lines []string) []string {
	if len(lines) == 0 {
		return nil
	}
	return append([]string(nil), lines...)
}
