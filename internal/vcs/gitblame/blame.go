// Package gitblame shells out to `git blame` to answer "who last touched
// this line, and why" for a single line at a time — the granularity the
// editor's blame popup needs (see editor.View's show_blame action).
//
// Deliberately one line per call, via `git blame -L n,n`, rather than
// blaming the whole file and caching it: blame walks history and is by far
// the most expensive git query kiwi makes, whole-file results go stale on
// every edit, and the popup only ever displays one line's worth. A
// single-line blame stays cheap enough to run on the keypress that asks
// for it.
package gitblame

import (
	"fmt"
	"os/exec"
	"strconv"
	"strings"
	"time"
)

// Info is one line's blame: which commit last changed it, by whom, when,
// and that commit's subject line.
//
// Uncommitted is true for a line that exists only in the working tree (an
// unstaged edit, or any line of an untracked file). git reports those
// against an all-zero commit hash with the placeholder author "Not
// Committed Yet", which is a fact about git's output rather than something
// worth showing a user verbatim — callers should present Uncommitted in
// their own words and ignore the other fields.
type Info struct {
	Commit      string // abbreviated to shortHashLen; "" when Uncommitted
	Author      string
	Time        time.Time
	Summary     string
	Uncommitted bool
}

// shortHashLen is how much of the 40-character hash to keep — git's own
// default abbreviation length, and enough to stay unambiguous in any
// repository a person edits by hand.
const shortHashLen = 7

// zeroHash is the all-zero commit hash git attributes a working-tree-only
// line to.
const zeroHash = "0000000000000000000000000000000000000000"

// Line returns blame for path's line (1-based). path may be absolute or
// relative to dir.
//
// Blame is computed against the file as it exists ON DISK: unsaved editor
// changes are invisible to git, so in a dirty buffer the caller's line
// number can refer to different text than git blames. That's not
// detectable here (this package sees no buffer), so callers holding a dirty
// buffer should say so when presenting the result — see
// editor.blamePopupLines.
func Line(dir, path string, line int) (Info, error) {
	if line < 1 {
		return Info{}, fmt.Errorf("gitblame: line %d out of range", line)
	}
	// --porcelain, not --line-porcelain: the extra repetition
	// --line-porcelain adds only matters when blaming several lines at
	// once, and a single-line blame always carries the full commit header.
	loc := strconv.Itoa(line)
	cmd := exec.Command("git", "blame", "--porcelain", "-L", loc+","+loc, "--", path)
	cmd.Dir = dir
	out, err := cmd.Output()
	if err != nil {
		return Info{}, err
	}
	return parsePorcelain(out)
}

// parsePorcelain reads `git blame --porcelain` output for a single line.
//
// The format is a header line ("<40-hex sha> <origLine> <finalLine>
// [numLines]"), then any number of "key value" lines, then the blamed
// source line itself prefixed with a TAB. Only the fields the popup shows
// are picked out; everything else (committer, author-mail, previous,
// boundary, ...) is skipped rather than enumerated, so a future git
// version adding more headers changes nothing here.
func parsePorcelain(out []byte) (Info, error) {
	lines := strings.Split(string(out), "\n")
	if len(lines) == 0 || lines[0] == "" {
		return Info{}, fmt.Errorf("gitblame: empty blame output")
	}

	var info Info
	var unixTime int64
	var tz string

	// The header's sha is everything up to the first space.
	sha, _, _ := strings.Cut(lines[0], " ")
	if len(sha) != len(zeroHash) {
		return Info{}, fmt.Errorf("gitblame: unexpected header %q", lines[0])
	}
	if sha == zeroHash {
		info.Uncommitted = true
	} else {
		info.Commit = sha[:shortHashLen]
	}

	for _, l := range lines[1:] {
		if strings.HasPrefix(l, "\t") {
			break // the blamed source line: every header has been seen
		}
		key, value, found := strings.Cut(l, " ")
		if !found {
			continue // a valueless header such as "boundary"
		}
		switch key {
		case "author":
			info.Author = value
		case "author-time":
			unixTime, _ = strconv.ParseInt(value, 10, 64)
		case "author-tz":
			tz = value
		case "summary":
			info.Summary = value
		}
	}

	if unixTime != 0 {
		info.Time = time.Unix(unixTime, 0).In(parseTZ(tz))
	}
	return info, nil
}

// parseTZ turns git's "+0200"-style offset into a fixed-offset location, so
// a commit's timestamp renders in the timezone it was authored in (what git
// log shows by default) rather than silently reinterpreted in the reader's.
// An unparseable or absent offset falls back to UTC.
func parseTZ(tz string) *time.Location {
	if len(tz) != 5 {
		return time.UTC
	}
	sign := 1
	switch tz[0] {
	case '+':
	case '-':
		sign = -1
	default:
		return time.UTC
	}
	hours, err := strconv.Atoi(tz[1:3])
	if err != nil {
		return time.UTC
	}
	mins, err := strconv.Atoi(tz[3:5])
	if err != nil {
		return time.UTC
	}
	return time.FixedZone(tz, sign*(hours*3600+mins*60))
}
