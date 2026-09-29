// Package githistory shells out to `git log`/`git show` to answer "how did
// this file get to be the way it is": the list of commits that touched a
// single file, and what each of those commits changed in it — the data
// behind the file-history overlay (see internal/ui/historyview).
//
// Like gitblame, it is split into two calls rather than one `git log -p`:
// the list is cheap and shown immediately, while a commit's diff is only
// fetched when the user actually lands on it, so opening the history of a
// file with hundreds of commits never pays for hundreds of diffs.
package githistory

import (
	"fmt"
	"os/exec"
	"strconv"
	"strings"
	"time"

	"github.com/bricejulia/nib/internal/vcs/gitstatus"
)

// Commit is one entry in a file's history.
//
// Path is the file's repository-relative path AS OF THIS COMMIT, which is
// not necessarily the path it has today: `git log --follow` walks through
// renames, and CommitDiff needs the historical name to find the file in an
// older commit's tree.
type Commit struct {
	Hash      string
	ShortHash string
	Author    string
	Time      time.Time
	Summary   string
	Path      string
}

// fieldSep separates the --format fields. A unit separator can't appear in
// a hash, a name, a timestamp, or (in practice) a commit subject, and — the
// property parseLog actually relies on — never in a name-status token, so
// its presence alone tells a commit header apart from a file entry.
const fieldSep = "\x1f"

// logFormat is the --format for Log: hash, short hash, author, author time
// (Unix seconds, so no timezone parsing is needed), and subject.
const logFormat = "%H%x1f%h%x1f%an%x1f%at%x1f%s"

// Log returns up to limit commits that touched path, newest first,
// following it across renames. path may be absolute or relative to dir.
// limit <= 0 means no limit.
//
// A file git has never committed (untracked, or a repository with no
// commits yet) has no history: that returns no commits and no error, the
// same "nothing to show isn't a failure" contract gitstatus.FileDiff has
// for a clean file. An error means git itself couldn't answer — no
// repository, or no git.
func Log(dir, path string, limit int) ([]Commit, error) {
	args := []string{"log", "--follow", "--no-color", "-z", "--format=" + logFormat, "--name-status"}
	if limit > 0 {
		args = append(args, "--max-count="+strconv.Itoa(limit))
	}
	args = append(args, "--", path)
	cmd := exec.Command("git", args...)
	cmd.Dir = dir
	out, err := cmd.Output()
	if err != nil {
		if hasNoCommits(dir) {
			return nil, nil
		}
		return nil, err
	}
	return parseLog(string(out))
}

// hasNoCommits reports whether dir is a repository whose HEAD doesn't
// resolve yet — the one case where `git log` exits non-zero ("does not
// have any commits yet") for what is really just an empty history.
func hasNoCommits(dir string) bool {
	inRepo := exec.Command("git", "rev-parse", "--git-dir")
	inRepo.Dir = dir
	if inRepo.Run() != nil {
		return false
	}
	head := exec.Command("git", "rev-parse", "--verify", "-q", "HEAD")
	head.Dir = dir
	return head.Run() != nil
}

// parseLog reads `git log -z --format=<logFormat> --name-status` output.
//
// With -z, every header and every name-status field is NUL-terminated,
// and git puts a newline between a commit's header and its first status
// token — so after trimming that newline, the stream is: a header (the
// only token containing fieldSep), a status letter, then one path, or two
// for a rename/copy ("R100", "C75") where the second is the new name.
// A commit that lists no file at all (possible for a merge) just keeps
// the Path of the commit after it, since the file can't have been renamed
// by a commit that didn't touch it.
func parseLog(out string) ([]Commit, error) {
	var commits []Commit
	tokens := strings.Split(out, "\x00")
	for i := 0; i < len(tokens); i++ {
		tok := strings.TrimPrefix(tokens[i], "\n")
		if tok == "" {
			continue
		}
		if !strings.Contains(tok, fieldSep) {
			return nil, fmt.Errorf("githistory: unexpected token %q in git log output", tok)
		}
		c, err := parseHeader(tok)
		if err != nil {
			return nil, err
		}

		// The file entry, if this commit has one.
		if i+2 < len(tokens) {
			status := strings.TrimPrefix(tokens[i+1], "\n")
			if status != "" && !strings.Contains(status, fieldSep) {
				switch status[0] {
				case 'R', 'C':
					if i+3 < len(tokens) {
						c.Path = tokens[i+3]
					}
					i += 3
				default:
					c.Path = tokens[i+2]
					i += 2
				}
			}
		}
		if c.Path == "" && len(commits) > 0 {
			c.Path = commits[len(commits)-1].Path
		}
		commits = append(commits, c)
	}
	return commits, nil
}

func parseHeader(tok string) (Commit, error) {
	f := strings.SplitN(tok, fieldSep, 5)
	if len(f) != 5 {
		return Commit{}, fmt.Errorf("githistory: malformed commit header %q", tok)
	}
	secs, err := strconv.ParseInt(f[3], 10, 64)
	if err != nil {
		return Commit{}, fmt.Errorf("githistory: bad author time %q: %w", f[3], err)
	}
	return Commit{
		Hash:      f[0],
		ShortHash: f[1],
		Author:    f[2],
		Time:      time.Unix(secs, 0),
		Summary:   f[4],
	}, nil
}

// CommitDiff returns the change c made to its file, as display lines.
//
// prevPath is the file's path in c's parent — for commits[i], that is
// commits[i+1].Path from the same Log call (or c.Path for the oldest).
// When it differs from c.Path, c is the commit that renamed the file, and
// passing both names (with -M) is what lets git show it as a rename rather
// than one file deleted and an unrelated one added.
//
// Both paths are handed to git with the :(top) pathspec magic, since Log's
// paths are relative to the repository's top level while dir may be any
// directory inside it.
func CommitDiff(dir string, c Commit, prevPath string) ([]string, error) {
	args := []string{"show", "--no-color", "--format=", "-M", c.Hash, "--", ":(top)" + c.Path}
	if prevPath != "" && prevPath != c.Path {
		args = append(args, ":(top)"+prevPath)
	}
	cmd := exec.Command("git", args...)
	cmd.Dir = dir
	out, err := cmd.Output()
	if err != nil {
		return nil, err
	}
	return gitstatus.SplitDiffLines(out), nil
}
