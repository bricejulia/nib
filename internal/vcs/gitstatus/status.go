// Package gitstatus shells out to git to compute per-file status and rolls
// it up to directories, for the file tree's status markers.
package gitstatus

import "os/exec"

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
// dir and returns a map from repo-relative path to Status.
func RunPorcelain(dir string) (map[string]Status, error) {
	cmd := exec.Command("git", "status", "--porcelain=v2", "-z", "--untracked-files=all")
	cmd.Dir = dir
	out, err := cmd.Output()
	if err != nil {
		return nil, err
	}
	return parsePorcelainV2(out)
}
