package watch

import (
	"os/exec"
	"strings"
)

// ignoredDirs returns the set of top-level-and-deeper ignored directories
// (repo-relative, no trailing slash) under root, using git itself rather
// than reimplementing .gitignore matching.
func ignoredDirs(root string) (map[string]bool, error) {
	cmd := exec.Command("git", "-C", root, "ls-files",
		"--others", "--ignored", "--exclude-standard", "--directory")
	out, err := cmd.Output()
	if err != nil {
		return nil, err
	}

	dirs := map[string]bool{}
	for _, line := range strings.Split(string(out), "\n") {
		line = strings.TrimSuffix(line, "/")
		if line == "" {
			continue
		}
		dirs[line] = true
	}
	return dirs, nil
}
