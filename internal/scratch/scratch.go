// Package scratch manages a project's scratch files: throwaway notes and
// snippets kept in .nib/scratch, so they stay with the project without
// ever showing up in its git status (.nib is git-ignored as a whole — see
// session.Trust).
//
// Scratch files only exist for a trusted project: the package itself
// doesn't check, but callers must, since creating the scratch folder would
// otherwise create .nib and silently trust the project.
package scratch

import (
	"errors"
	"fmt"
	"io/fs"
	"os"
	"path/filepath"
	"sort"
	"strings"
	"time"

	"github.com/bricejulia/nib/internal/session"
)

// dirName is the scratch folder inside a project's .nib folder.
const dirName = "scratch"

// Entry is one scratch file.
type Entry struct {
	Path    string
	Name    string
	ModTime time.Time
}

// Dir returns root's scratch folder.
func Dir(root string) string { return filepath.Join(session.Dir(root), dirName) }

// List returns root's scratch files, most recently modified first. A
// missing scratch folder is not an error: it returns (nil, nil).
func List(root string) ([]Entry, error) {
	dir := Dir(root)
	items, err := os.ReadDir(dir)
	if errors.Is(err, fs.ErrNotExist) {
		return nil, nil
	}
	if err != nil {
		return nil, err
	}
	var out []Entry
	for _, it := range items {
		if !it.Type().IsRegular() {
			continue
		}
		info, err := it.Info()
		if err != nil {
			continue
		}
		out = append(out, Entry{Path: filepath.Join(dir, it.Name()), Name: it.Name(), ModTime: info.ModTime()})
	}
	sort.SliceStable(out, func(i, j int) bool {
		if !out[i].ModTime.Equal(out[j].ModTime) {
			return out[i].ModTime.After(out[j].ModTime)
		}
		return out[i].Name < out[j].Name
	})
	return out, nil
}

// Create returns the path of root's scratch file called name, creating it
// (and the scratch folder) if it doesn't exist yet — an existing one is
// simply reused, so asking for "notes.md" twice reopens the same file. A
// blank name picks the first free "scratch-<n>.txt". The name must be a
// plain file name: no path separators, and not "." or "..".
func Create(root, name string) (string, error) {
	name = strings.TrimSpace(name)
	if name == "." || name == ".." || strings.ContainsAny(name, `/\`) {
		return "", fmt.Errorf("invalid scratch file name %q", name)
	}
	dir := Dir(root)
	if err := os.MkdirAll(dir, 0o755); err != nil {
		return "", err
	}
	if name != "" {
		path := filepath.Join(dir, name)
		f, err := os.OpenFile(path, os.O_RDWR|os.O_CREATE, 0o644)
		if err != nil {
			return "", err
		}
		return path, f.Close()
	}
	for n := 1; ; n++ {
		path := filepath.Join(dir, fmt.Sprintf("scratch-%d.txt", n))
		f, err := os.OpenFile(path, os.O_RDWR|os.O_CREATE|os.O_EXCL, 0o644)
		if errors.Is(err, fs.ErrExist) {
			continue
		}
		if err != nil {
			return "", err
		}
		return path, f.Close()
	}
}

// Delete removes the scratch file at path.
func Delete(path string) error { return os.Remove(path) }
