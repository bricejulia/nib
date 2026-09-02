// Package gitstatus shells out to git to compute per-file status and rolls
// it up to directories, for the file tree's status markers.
package gitstatus

import (
	"os/exec"
	"strings"
)

// Status is the simplified per-file status shown in the file tree. Where a
// file has more than one applicable status (e.g. staged-and-modified),
// precedence (see rollup.go) picks the one worth surfacing.
type Status byte

const (
	Unmodified Status = iota
	Modified
	Added
	Deleted
	Renamed
	Untracked
	Conflicted
)

// RunPorcelain runs `git status --porcelain=v2 -z --untracked-files=all` in
// dir and returns a map from dir-relative path to Status.
//
// git always reports porcelain paths relative to the repository's top-level
// directory, never relative to dir -- so when dir is a subdirectory of the
// repo (nib opened from a child folder of it), those raw paths would be
// wrong relative to dir, which is what every caller (the file tree,
// singleFileStatus, ...) assumes it's getting. `git rev-parse --show-prefix`
// reports dir's own path from the repo root, so stripping it off each entry
// converts back to dir-relative; entries outside dir's own subtree are
// dropped, since dir's tree has nothing to attach them to.
func RunPorcelain(dir string) (map[string]Status, error) {
	cmd := exec.Command("git", "status", "--porcelain=v2", "-z", "--untracked-files=all")
	cmd.Dir = dir
	out, err := cmd.Output()
	if err != nil {
		return nil, err
	}
	direct, err := parsePorcelainV2(out)
	if err != nil {
		return nil, err
	}

	prefix, err := showPrefix(dir)
	if err != nil {
		return nil, err
	}
	if prefix == "" {
		return direct, nil
	}
	scoped := make(map[string]Status, len(direct))
	for path, status := range direct {
		if rel, ok := strings.CutPrefix(path, prefix); ok {
			scoped[rel] = status
		}
	}
	return scoped, nil
}

// showPrefix returns dir's own path relative to its repository's top-level
// directory (via `git rev-parse --show-prefix`), forward-slash separated
// with a trailing slash -- or "" when dir is itself the repo root.
func showPrefix(dir string) (string, error) {
	cmd := exec.Command("git", "rev-parse", "--show-prefix")
	cmd.Dir = dir
	out, err := cmd.Output()
	if err != nil {
		return "", err
	}
	return strings.TrimSpace(string(out)), nil
}
