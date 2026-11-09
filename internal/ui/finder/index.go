package finder

import (
	"os"
	"path/filepath"
	"strings"

	"github.com/bricejulia/nib/internal/vcs/gitstatus"
)

// commonIgnoredDirNames is the fallback ignore list used only when root
// isn't a git repository (so there's no .gitignore for `git ls-files` to
// consult) — a small, fixed set of directories almost never useful to
// fuzzy-find into.
var commonIgnoredDirNames = map[string]bool{
	".git":         true,
	".nib":         true, // nib's own per-project state — see internal/session
	"node_modules": true,
	"vendor":       true,
}

// alwaysSkippedDirNames are skipped by the fallback walk even when
// ignored files are included: neither is ever a source file anyone wants
// to open from the finder.
var alwaysSkippedDirNames = map[string]bool{
	".git": true,
	".nib": true,
}

// nibStatePrefix is nib's own per-project state directory (see
// internal/session), which ignores itself via its own `*` .gitignore —
// so it only ever shows up once ignored files are included, and is
// filtered back out then.
const nibStatePrefix = ".nib/"

// effectiveIncludeIgnored decides whether a search should include
// git-ignored files: when the user toggled it on, or when the scope itself
// points into an ignored path (e.g. "node_modules/react") — scoping there
// can only mean the user wants to search it. Shells out to git, so call it
// off the UI thread wherever the caller already does that for the search.
func effectiveIncludeIgnored(root, scope string, toggle bool) bool {
	return toggle || (scope != "" && gitstatus.IsIgnored(root, scope))
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
//
// includeIgnored also lists git-ignored files (node_modules, an ignored
// vendor/, …) — or, outside git, stops skipping those directory names.
func listFiles(root, scope string, includeIgnored bool) []string {
	if files, err := gitstatus.ListFiles(root, scope, includeIgnored); err == nil {
		if includeIgnored {
			files = dropNibState(files)
		}
		return files
	}
	return walkFiles(root, scope, includeIgnored)
}

// dropNibState filters nib's own state directory out of paths, in place.
func dropNibState(paths []string) []string {
	out := paths[:0]
	for _, p := range paths {
		if !strings.HasPrefix(p, nibStatePrefix) {
			out = append(out, p)
		}
	}
	return out
}

func walkFiles(root, scope string, includeIgnored bool) []string {
	skip := commonIgnoredDirNames
	if includeIgnored {
		skip = alwaysSkippedDirNames
	}
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
			if path != start && skip[d.Name()] {
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
