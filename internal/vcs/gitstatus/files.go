package gitstatus

import (
	"os/exec"
	"strings"
)

// ListFiles returns every file git considers relevant under dir — tracked
// files plus untracked-but-not-ignored ones — via a single `git ls-files`
// call, which already respects .gitignore without reimplementing it.
// Paths are repo-relative, forward-slash separated.
//
// scope, if non-empty, is a dir-relative pathspec (e.g. "src/handlers")
// narrowing the listing to that subtree — git resolves it natively, so
// results stay identical to the unscoped case when scope is "".
//
// includeIgnored also lists untracked files .gitignore excludes (e.g.
// node_modules/, an ignored vendor/) by dropping --exclude-standard.
func ListFiles(dir, scope string, includeIgnored bool) ([]string, error) {
	args := []string{"ls-files", "--cached", "--others"}
	if !includeIgnored {
		args = append(args, "--exclude-standard")
	}
	if scope != "" {
		args = append(args, "--", scope)
	}
	cmd := exec.Command("git", args...)
	cmd.Dir = dir
	out, err := cmd.Output()
	if err != nil {
		return nil, err
	}

	trimmed := strings.TrimRight(string(out), "\n")
	if trimmed == "" {
		return nil, nil
	}
	return strings.Split(trimmed, "\n"), nil
}

// IsIgnored reports whether path (relative to dir) is excluded by
// .gitignore, via `git check-ignore`. Any error — not a git repository,
// git missing — reports false.
func IsIgnored(dir, path string) bool {
	cmd := exec.Command("git", "check-ignore", "-q", "--", path)
	cmd.Dir = dir
	return cmd.Run() == nil
}
