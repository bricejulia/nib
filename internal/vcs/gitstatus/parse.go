package gitstatus

import "strings"

// parsePorcelainV2 parses the NUL-delimited output of
// `git status --porcelain=v2 -z --untracked-files=all`.
//
// Record shapes (fields are space-separated; path is always the final
// field of its record and may itself contain spaces):
//
//	1 XY sub mH mI mW hH hI path        ordinary changed entry
//	2 XY sub mH mI mW hH hI Xscore path   renamed/copied entry, followed
//	                                       by a second NUL-delimited chunk
//	                                       holding origPath
//	u XY sub m1 m2 m3 mW h1 h2 h3 path   unmerged (conflicted) entry
//	? path                                untracked
//	! path                                ignored (not requested here, but
//	                                       tolerated if present)
func parsePorcelainV2(raw []byte) (map[string]Status, error) {
	out := map[string]Status{}

	records := strings.Split(string(raw), "\x00")
	for i := 0; i < len(records); i++ {
		rec := records[i]
		if rec == "" {
			continue
		}

		switch rec[0] {
		case '1':
			fields := strings.SplitN(rec[2:], " ", 8)
			if len(fields) < 8 {
				continue
			}
			xy, path := fields[0], fields[7]
			out[path] = statusFromXY(xy)

		case '2':
			fields := strings.SplitN(rec[2:], " ", 9)
			if len(fields) < 9 {
				continue
			}
			path := fields[8]
			out[path] = Renamed
			// The next NUL-delimited chunk is origPath; consume it
			// so it isn't misinterpreted as its own record.
			i++

		case 'u':
			fields := strings.SplitN(rec[2:], " ", 10)
			if len(fields) < 10 {
				continue
			}
			out[fields[9]] = Conflicted

		case '?':
			path := rec[2:]
			out[path] = Untracked

		case '!':
			// Ignored entries are handled separately (see
			// internal/vcs/watch.ignoredDirs); not requested via
			// --ignored here, but tolerate if present.
			continue
		}
	}

	return out, nil
}

// statusFromXY maps a two-character XY status code to a single Status,
// preferring the more "notable" change when both index and worktree are
// dirty (e.g. staged-and-then-modified again).
func statusFromXY(xy string) Status {
	if len(xy) != 2 {
		return Modified
	}
	x, y := xy[0], xy[1]
	switch {
	case x == 'A' || y == 'A':
		return Added
	case x == 'D' || y == 'D':
		return Deleted
	default:
		return Modified
	}
}
