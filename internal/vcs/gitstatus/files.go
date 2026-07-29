package gitstatus

import (
	"os/exec"
	"strings"
)

// ListFiles returns every file git considers relevant under dir — tracked
// files plus untracked-but-not-ignored ones — via a single `git ls-files`
// call, which already respects .gitignore without reimplementing it.
// Paths are repo-relative, forward-slash separated.
func ListFiles(dir string) ([]string, error) {
	cmd := exec.Command("git", "ls-files", "--cached", "--others", "--exclude-standard")
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
