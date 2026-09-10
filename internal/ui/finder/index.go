package finder

import (
	"os"
	"path/filepath"

	"github.com/bricejulia/nib/internal/vcs/gitstatus"
)

// commonIgnoredDirNames is the fallback ignore list used only when root
// isn't a git repository (so there's no .gitignore for `git ls-files` to
// consult) — a small, fixed set of directories almost never useful to
// fuzzy-find into.
var commonIgnoredDirNames = map[string]bool{
	".git":         true,
	"node_modules": true,
	"vendor":       true,
}

// listFiles returns repo-relative, forward-slash paths for every file
// worth offering in the finder. It prefers `git ls-files` (fast, and
// respects .gitignore for free); non-git projects fall back to a plain
// directory walk skipping commonIgnoredDirNames.
//
// scope, if non-empty, is a root-relative subdirectory (forward-slash
// separated, e.g. "src/handlers") narrowing the result to that subtree —
// see finder.View's scope field. Returned paths stay root-relative
// regardless of scope, since callers (status-map lookups, filepath.Join
// on selection) all key off the project root, not the scope.
func listFiles(root, scope string) []string {
	if files, err := gitstatus.ListFiles(root, scope); err == nil {
		return files
	}
	return walkFiles(root, scope)
}

func walkFiles(root, scope string) []string {
	start := root
	if scope != "" {
		start = filepath.Join(root, scope)
	}
	var files []string
	_ = filepath.WalkDir(start, func(path string, d os.DirEntry, err error) error {
		if err != nil {
			return nil // a permission error on one subtree shouldn't abort the whole walk
		}
		if d.IsDir() {
			if path != start && commonIgnoredDirNames[d.Name()] {
				return filepath.SkipDir
			}
			return nil
		}
		rel, err := filepath.Rel(root, path)
		if err != nil {
			return nil
		}
		files = append(files, filepath.ToSlash(rel))
		return nil
	})
	return files
}
