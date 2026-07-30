package gitstatus

import (
	"errors"
	"fmt"
	"os"
	"os/exec"
	"path/filepath"
	"strings"
)

// ErrUntracked reports that a path has no committed version to diff
// against, because git has never been told about it. Callers get to decide
// how to say that — see FileDiff, which synthesizes an all-added diff, and
// FileHunkAt, which declines and lets the UI put it in words.
var ErrUntracked = errors.New("gitstatus: file is untracked")

// FileDiff returns path's working-tree diff against HEAD as display lines
// (no trailing blank), covering staged and unstaged changes alike — the
// whole-file view behind the editor's show_file_diff overlay, where
// FileHunks/FileHunkList are the line-level views of the same change.
//
// Unlike those two, this keeps git's default three lines of surrounding
// context: a diff being read as a document wants the context, whereas a
// diff being mapped onto specific buffer lines does not.
//
// A clean file returns no lines and no error. An untracked file has no HEAD
// blob for `git diff` to compare with — git simply omits it rather than
// reporting every line as added — so its content is turned into an
// equivalent all-added diff here, the same accommodation FileHunks makes
// via wholeFileAdded.
func FileDiff(dir, path string) ([]string, error) {
	st, err := singleFileStatus(dir, path)
	if err != nil {
		return nil, err
	}
	switch st {
	case Unmodified:
		return nil, nil
	case Untracked:
		return untrackedDiff(dir, path)
	}

	cmd := exec.Command("git", "diff", "HEAD", "--no-color", "--", path)
	cmd.Dir = dir
	out, err := cmd.Output()
	if err != nil {
		return nil, err
	}
	return splitDiffLines(out), nil
}

// untrackedDiff renders path as a diff adding the whole file, matching what
// git itself would print once the file is staged (`git add -N`). Written
// out here rather than shelling out to `git diff --no-index /dev/null path`
// so it needs no special-casing of that command's deliberate exit status 1,
// and so it stays testable without a repository.
func untrackedDiff(dir, path string) ([]string, error) {
	data, err := os.ReadFile(resolve(dir, path))
	if err != nil {
		return nil, err
	}
	// Split exactly the way wholeFileAdded (and editor.Buffer.Load) does, so
	// the line count in the hunk header matches the line count the gutter and
	// the editor agree on.
	text := strings.TrimSuffix(string(data), "\n")
	var content []string
	if text != "" {
		content = strings.Split(text, "\n")
	} else {
		content = []string{""}
	}

	out := make([]string, 0, len(content)+4)
	out = append(out,
		"--- /dev/null",
		"+++ b/"+path,
		fmt.Sprintf("@@ -0,0 +1,%d @@", len(content)),
	)
	for _, l := range content {
		out = append(out, "+"+l)
	}
	return out, nil
}

// resolve makes path absolute against dir unless it already is. Everything
// else in this package reaches a file by handing path to git with
// cmd.Dir = dir, so a relative path is read as relative to THE REPOSITORY;
// os.ReadFile would instead read it as relative to the process's own working
// directory, which is a different place entirely (and, for a program with no
// reason to chdir, an arbitrary one). Callers that read a file directly have
// to close that gap themselves.
func resolve(dir, path string) string {
	if filepath.IsAbs(path) {
		return path
	}
	return filepath.Join(dir, path)
}

// splitDiffLines turns git's raw output into display lines, dropping the
// single trailing newline every diff ends with (which would otherwise show
// as a spurious blank final row).
func splitDiffLines(out []byte) []string {
	text := strings.TrimSuffix(string(out), "\n")
	if text == "" {
		return nil
	}
	return strings.Split(text, "\n")
}
