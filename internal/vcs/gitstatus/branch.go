package gitstatus

import (
	"os/exec"
	"strings"
)

// CurrentBranch returns the repo's current branch name via `git branch
// --show-current`, for display in a status bar. It returns "" (with a nil
// error) on a detached HEAD, matching that command's own behavior.
func CurrentBranch(dir string) (string, error) {
	cmd := exec.Command("git", "branch", "--show-current")
	cmd.Dir = dir
	out, err := cmd.Output()
	if err != nil {
		return "", err
	}
	return strings.TrimSpace(string(out)), nil
}
