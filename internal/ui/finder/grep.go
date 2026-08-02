package finder

import (
	"bufio"
	"errors"
	"os/exec"
	"strconv"
	"strings"
)

// maxContentMatches caps how many content-search results are kept, so a
// query that matches a very common word doesn't flood the list or slow
// rendering.
const maxContentMatches = 200

// contentMatch is one line hit from searchContent.
type contentMatch struct {
	path string
	line int // 1-based
	text string
}

// searchContent runs `git grep` for query as a literal (not regex),
// case-insensitive substring across the project's tracked and
// untracked-but-not-ignored files — the same file set listFiles indexes,
// via the same "shell out to git" approach the rest of the project uses
// rather than reimplementing gitignore matching. Returns (nil, nil) for
// "no matches", which git grep reports as a non-error exit code 1.
func searchContent(root, query string) ([]contentMatch, error) {
	cmd := exec.Command("git", "grep",
		"--fixed-strings", "--ignore-case", "-n", "-I", "--untracked",
		"-e", query, "--")
	cmd.Dir = root

	out, err := cmd.Output()
	if err != nil {
		var exitErr *exec.ExitError
		if errors.As(err, &exitErr) && exitErr.ExitCode() == 1 {
			return nil, nil
		}
		return nil, err
	}

	var matches []contentMatch
	scanner := bufio.NewScanner(strings.NewReader(string(out)))
	for scanner.Scan() && len(matches) < maxContentMatches {
		// Format: path:lineno:content — SplitN(..., 3) keeps any colons
		// inside the matched content itself intact.
		parts := strings.SplitN(scanner.Text(), ":", 3)
		if len(parts) != 3 {
			continue
		}
		lineno, err := strconv.Atoi(parts[1])
		if err != nil {
			continue
		}
		matches = append(matches, contentMatch{
			path: parts[0],
			line: lineno,
			text: parts[2],
		})
	}
	return matches, nil
}
