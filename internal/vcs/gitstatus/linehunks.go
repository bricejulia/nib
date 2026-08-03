package gitstatus

import (
	"os"
	"os/exec"
	"regexp"
	"strconv"
	"strings"

	"github.com/bricejulia/nib/internal/textfile"
)

// LineStatus is the per-line git diff marker shown in the editor gutter —
// coarser than a full diff (no distinction between "changed" and
// "changed+partially deleted"), which is all a single gutter glyph per
// line can usefully show.
type LineStatus byte

const (
	LineUnchanged LineStatus = iota
	LineAdded
	LineModified
	// LineDeletedBefore marks the line immediately after a run of deleted
	// lines — there's no line of its own to attach the marker to, so it
	// rides on its neighbor, the same convention vim-gitgutter and
	// VSCode's gutter use.
	LineDeletedBefore
)

// hunkHeaderRe matches a unified diff hunk header, e.g. "@@ -2,3 +4 @@".
// The trailing context (function name, with -p) is ignored.
var hunkHeaderRe = regexp.MustCompile(`^@@ -(\d+)(?:,(\d+))? \+(\d+)(?:,(\d+))? @@`)

// FileHunks returns path's current working-tree line-level diff against
// HEAD (covering both staged and unstaged changes, since most editing
// sessions mix the two), as a map from 0-based line index — into path's
// current content, i.e. the same indexing as editor.Buffer.Lines — to
// LineStatus. A clean or fully-committed file returns an empty, nil-error
// map; lines absent from the map are unchanged.
//
// An untracked file (never `git add`ed) has no HEAD blob to diff against
// — `git diff` simply omits it rather than reporting it as "all added" —
// so that case is special-cased to read path directly and mark every
// line LineAdded.
func FileHunks(dir, path string) (map[int]LineStatus, error) {
	st, err := singleFileStatus(dir, path)
	if err != nil {
		return nil, err
	}
	switch st {
	case Unmodified:
		return nil, nil
	case Untracked:
		return wholeFileAdded(dir, path)
	default:
		// Modified/Added/Deleted/Renamed/Conflicted all fall through to
		// the general `git diff` path below.
	}

	cmd := exec.Command("git", "diff", "HEAD", "--no-color", "-U0", "--", path)
	cmd.Dir = dir
	out, err := cmd.Output()
	if err != nil {
		return nil, err
	}
	return parseHunks(out), nil
}

// singleFileStatus is RunPorcelain narrowed to one path, for FileHunks'
// untracked/clean/dirty branch above — cheaper than computing (and
// discarding) the whole repo's status map per open tab.
func singleFileStatus(dir, path string) (Status, error) {
	cmd := exec.Command("git", "status", "--porcelain=v2", "-z", "--untracked-files=all", "--", path)
	cmd.Dir = dir
	out, err := cmd.Output()
	if err != nil {
		return Unmodified, err
	}
	m, err := parsePorcelainV2(out)
	if err != nil {
		return Unmodified, err
	}
	for _, s := range m {
		return s, nil // exactly one entry expected for a single-path query
	}
	return Unmodified, nil
}

// wholeFileAdded reads path and marks every line LineAdded, splitting it
// exactly the way editor.Buffer.Load does (see internal/textfile) so the
// returned indices line up with Buffer.Lines regardless of the file's
// charset or line-ending style.
func wholeFileAdded(dir, path string) (map[int]LineStatus, error) {
	data, err := os.ReadFile(resolve(dir, path))
	if err != nil {
		return nil, err
	}
	text, _, err := textfile.Decode(data)
	if err != nil {
		return nil, err
	}
	lines, _ := textfile.SplitLines(text)
	out := make(map[int]LineStatus, len(lines))
	for i := range lines {
		out[i] = LineAdded
	}
	return out, nil
}

// Hunk is one contiguous change from a `-U0` unified diff, with the text on
// both sides kept — which is what separates it from the LineStatus map
// above: a gutter glyph only needs to know a line changed, while "show me
// the diff of this line" needs the lines it replaced.
//
// NewStart is 1-based into the new file, matching the diff header. For a
// pure deletion (NewLines == 0) it is the line the removal happened
// *before*, per the unified-diff convention.
type Hunk struct {
	NewStart int
	NewLines int
	OldStart int
	OldLines int
	Removed  []string // the "-" side, without the leading marker
	Added    []string // the "+" side, without the leading marker
}

// parseHunks marks the new-file lines affected by a `-U0` unified diff,
// derived from parseHunkList so there is exactly one hunk-header parser to
// keep correct. Per hunk shape:
//
//   - old=0, new>0: pure addition -> those new lines are LineAdded.
//   - old>0, new=0: pure deletion, no new lines to mark -> the line right
//     after the deletion point (newStart, which git already reports as a
//     0-based "line before which this happened" index when new=0) gets
//     LineDeletedBefore. A deletion at the very end of the file has no
//     "line after" to attach to and is simply not shown — an accepted
//     gap, not worth a sentinel for.
//   - old>0, new>0: a changed range -> those new lines are LineModified,
//     even if old != new (no separate marker for "changed AND shrank").
func parseHunks(diff []byte) map[int]LineStatus {
	out := map[int]LineStatus{}
	for _, h := range parseHunkList(diff) {
		switch {
		case h.OldLines == 0 && h.NewLines > 0:
			for i := range h.NewLines {
				out[h.NewStart-1+i] = LineAdded
			}
		case h.NewLines == 0:
			out[h.NewStart] = LineDeletedBefore
		default:
			for i := range h.NewLines {
				out[h.NewStart-1+i] = LineModified
			}
		}
	}
	return out
}

// parseHunkList scans a `-U0` unified diff into Hunks, in file order.
//
// With -U0 there is no context, so every body line between two headers is
// either a removal or an addition — except git's "\ No newline at end of
// file" note, which describes the preceding line rather than being one, and
// the leading "diff --git"/"---"/"+++" file header, whose "---"/"+++" lines
// would otherwise be misread as a removal and an addition. Both are skipped
// by only collecting body lines once a hunk header has been seen, and by
// matching the exact "---"/"+++" file-header spellings.
func parseHunkList(diff []byte) []Hunk {
	var hunks []Hunk
	for _, line := range strings.Split(string(diff), "\n") {
		if m := hunkHeaderRe.FindStringSubmatch(line); m != nil {
			oldStart, _ := strconv.Atoi(m[1])
			newStart, _ := strconv.Atoi(m[3])
			hunks = append(hunks, Hunk{
				NewStart: newStart,
				NewLines: hunkCount(m[4]),
				OldStart: oldStart,
				OldLines: hunkCount(m[2]),
			})
			continue
		}
		if len(hunks) == 0 {
			continue // still in the file header, before the first hunk
		}
		h := &hunks[len(hunks)-1]
		switch {
		case line == "---" || line == "+++" || strings.HasPrefix(line, "--- ") || strings.HasPrefix(line, "+++ "):
			// A file header, not content — only reachable in a multi-file
			// diff, where the second file's header follows the first file's
			// last hunk.
		case strings.HasPrefix(line, "-"):
			h.Removed = append(h.Removed, line[1:])
		case strings.HasPrefix(line, "+"):
			h.Added = append(h.Added, line[1:])
		}
	}
	return hunks
}

// HunkAtLine returns the hunk affecting the 0-based line index ln — the
// same indexing parseHunks uses, so a line showing a gutter marker resolves
// to the hunk that put it there. ok is false for an unchanged line.
func HunkAtLine(hunks []Hunk, ln int) (Hunk, bool) {
	for _, h := range hunks {
		if h.NewLines == 0 {
			// A pure deletion's marker rides on the following line (see
			// parseHunks), so that's the line it must be found from.
			if ln == h.NewStart {
				return h, true
			}
			continue
		}
		if ln >= h.NewStart-1 && ln < h.NewStart-1+h.NewLines {
			return h, true
		}
	}
	return Hunk{}, false
}

// FileHunkAt returns the hunk covering path's 0-based line index ln, for
// the editor's "diff of the current line" popup. ok is false when that line
// is unchanged. An untracked file reports ErrUntracked: there is no
// committed version to diff a single line against, and every line being new
// is better said in words than as a whole-file diff popup.
func FileHunkAt(dir, path string, ln int) (Hunk, bool, error) {
	hunks, err := FileHunkList(dir, path)
	if err != nil {
		return Hunk{}, false, err
	}
	h, ok := HunkAtLine(hunks, ln)
	return h, ok, nil
}

// FileHunkList returns every hunk in path's working-tree diff against HEAD,
// with both sides' text — the Hunk-level counterpart to FileHunks, sharing
// its `git diff HEAD -U0` output and its ErrUntracked handling.
func FileHunkList(dir, path string) ([]Hunk, error) {
	st, err := singleFileStatus(dir, path)
	if err != nil {
		return nil, err
	}
	switch st {
	case Unmodified:
		return nil, nil
	case Untracked:
		return nil, ErrUntracked
	default:
		// Modified/Added/Deleted/Renamed/Conflicted all fall through to
		// the general `git diff` path below.
	}

	cmd := exec.Command("git", "diff", "HEAD", "--no-color", "-U0", "--", path)
	cmd.Dir = dir
	out, err := cmd.Output()
	if err != nil {
		return nil, err
	}
	return parseHunkList(out), nil
}

// hunkCount parses a hunk header's optional ",N" count, defaulting to 1
// per the unified diff format (an omitted count always means 1; a count
// of 0 is always written out explicitly, never omitted).
func hunkCount(s string) int {
	if s == "" {
		return 1
	}
	n, _ := strconv.Atoi(s)
	return n
}
