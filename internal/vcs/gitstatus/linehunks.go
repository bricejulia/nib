package gitstatus

import (
	"os"
	"os/exec"
	"regexp"
	"strconv"
	"strings"
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
		return wholeFileAdded(path)
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

// wholeFileAdded reads path and marks every line LineAdded, splitting the
// same way editor.Buffer.Load does (trim exactly one trailing newline,
// an all-empty file is a single empty line) so the returned indices line
// up with Buffer.Lines.
func wholeFileAdded(path string) (map[int]LineStatus, error) {
	data, err := os.ReadFile(path)
	if err != nil {
		return nil, err
	}
	text := strings.TrimSuffix(string(data), "\n")
	n := 1
	if text != "" {
		n = strings.Count(text, "\n") + 1
	}
	out := make(map[int]LineStatus, n)
	for i := range n {
		out[i] = LineAdded
	}
	return out, nil
}

// parseHunks scans a `-U0` unified diff for hunk headers and marks the
// affected new-file lines, per hunk shape:
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
	for _, line := range strings.Split(string(diff), "\n") {
		m := hunkHeaderRe.FindStringSubmatch(line)
		if m == nil {
			continue
		}
		oldLines := hunkCount(m[2])
		newStart, _ := strconv.Atoi(m[3])
		newLines := hunkCount(m[4])

		switch {
		case oldLines == 0 && newLines > 0:
			for i := range newLines {
				out[newStart-1+i] = LineAdded
			}
		case newLines == 0:
			out[newStart] = LineDeletedBefore
		default:
			for i := range newLines {
				out[newStart-1+i] = LineModified
			}
		}
	}
	return out
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
