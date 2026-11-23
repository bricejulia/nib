// Package gitmerge is the git side of resolving merge conflicts: which
// files git left unmerged, rewriting one with its base version shown,
// and marking one resolved (or unresolved again). Like the rest of
// internal/vcs it shells out to git; the conflict regions themselves are
// internal/merge's business.
package gitmerge

import (
	"bytes"
	"fmt"
	"os/exec"
	"strings"
)

// Conflicted lists the files under dir git has left unmerged, relative to
// dir and in git's (sorted) order. Empty, not an error, when nothing is
// mid-merge.
func Conflicted(dir string) ([]string, error) {
	out, err := run(dir, "ls-files", "--unmerged", "-z")
	if err != nil {
		return nil, err
	}
	var paths []string
	seen := map[string]bool{}
	for _, rec := range bytes.Split(out, []byte{0}) {
		// "<mode> <object> <stage>\t<path>", one record per stage.
		_, path, ok := strings.Cut(string(rec), "\t")
		if !ok || seen[path] {
			continue
		}
		seen[path] = true
		paths = append(paths, path)
	}
	return paths, nil
}

// IsConflicted reports whether git still has path unmerged.
func IsConflicted(dir, path string) (bool, error) {
	out, err := run(dir, "ls-files", "--unmerged", "--", path)
	if err != nil {
		return false, err
	}
	return len(bytes.TrimSpace(out)) > 0, nil
}

// RegenerateWithBase rewrites path's conflict regions in the zdiff3 style,
// which adds the common ancestor's text (the "|||||||" section) the magic
// wand needs. It recreates the conflicted file from the index, so any
// edit made to it since the merge stopped is lost: only call it on a file
// with no such edits.
func RegenerateWithBase(dir, path string) error {
	_, err := run(dir, "checkout", "--conflict=zdiff3", "--", path)
	return err
}

// Stage marks path resolved: `git add`.
func Stage(dir, path string) error {
	_, err := run(dir, "add", "--", path)
	return err
}

// Unresolve undoes Stage on a file that was conflicted: it puts the
// unmerged index entries back, leaving the working-tree file as it is.
func Unresolve(dir, path string) error {
	_, err := run(dir, "update-index", "--unresolve", "--", path)
	return err
}

func run(dir string, args ...string) ([]byte, error) {
	cmd := exec.Command("git", args...)
	cmd.Dir = dir
	var stderr bytes.Buffer
	cmd.Stderr = &stderr
	out, err := cmd.Output()
	if err != nil {
		if msg := strings.TrimSpace(stderr.String()); msg != "" {
			return nil, fmt.Errorf("git %s: %s", args[0], msg)
		}
		return nil, fmt.Errorf("git %s: %w", args[0], err)
	}
	return out, nil
}
