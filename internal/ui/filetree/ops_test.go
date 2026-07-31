package filetree

import (
	"errors"
	"os"
	"path/filepath"
	"testing"
)

func TestResolveInRootRejectsEmptyNames(t *testing.T) {
	for _, typed := range []string{"", "   ", "/", "//"} {
		if _, _, err := resolveInRoot("/project", typed); !errors.Is(err, errEmptyName) {
			t.Errorf("resolveInRoot(%q) error = %v, want errEmptyName", typed, err)
		}
	}
}

func TestResolveInRootRejectsPathsOutsideTheProject(t *testing.T) {
	for _, typed := range []string{"../x", "sub/../../x", "/etc/passwd", "."} {
		if _, _, err := resolveInRoot("/project", typed); !errors.Is(err, errOutsideRoot) {
			t.Errorf("resolveInRoot(%q) error = %v, want errOutsideRoot", typed, err)
		}
	}
}

func TestResolveInRootJoinsAndDetectsDirectories(t *testing.T) {
	root := filepath.FromSlash("/project")

	abs, isDir, err := resolveInRoot(root, "sub/x.go")
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}
	if want := filepath.Join(root, "sub", "x.go"); abs != want {
		t.Errorf("abs = %q, want %q", abs, want)
	}
	if isDir {
		t.Error("sub/x.go should not be reported as a directory")
	}

	abs, isDir, err = resolveInRoot(root, "sub/x/")
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}
	if !isDir {
		t.Error("a trailing slash should mean a directory")
	}
	if want := filepath.Join(root, "sub", "x"); abs != want {
		t.Errorf("abs = %q, want %q (trailing slash stripped)", abs, want)
	}

	// "." and ".." normalize the way a shell would, as long as the result
	// still lands inside the project.
	abs, _, err = resolveInRoot(root, "sub/../x.go")
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}
	if want := filepath.Join(root, "x.go"); abs != want {
		t.Errorf("abs = %q, want %q", abs, want)
	}
}

func TestCreateEntryCreatesAnEmptyFile(t *testing.T) {
	dir := t.TempDir()
	path := filepath.Join(dir, "a.txt")

	if err := createEntry(path, false); err != nil {
		t.Fatalf("createEntry: %v", err)
	}
	info, err := os.Stat(path)
	if err != nil {
		t.Fatalf("stat: %v", err)
	}
	if info.IsDir() {
		t.Error("created a directory, want a file")
	}
	if info.Size() != 0 {
		t.Errorf("size = %d, want 0", info.Size())
	}
	if got := info.Mode().Perm(); got != newFileMode {
		t.Errorf("mode = %v, want %v", got, os.FileMode(newFileMode))
	}
}

func TestCreateEntryCreatesIntermediateDirectories(t *testing.T) {
	dir := t.TempDir()
	path := filepath.Join(dir, "a", "b", "c.txt")

	if err := createEntry(path, false); err != nil {
		t.Fatalf("createEntry: %v", err)
	}
	if _, err := os.Stat(path); err != nil {
		t.Fatalf("stat: %v", err)
	}
	info, err := os.Stat(filepath.Join(dir, "a", "b"))
	if err != nil {
		t.Fatalf("stat intermediate: %v", err)
	}
	if !info.IsDir() {
		t.Error("intermediate should be a directory")
	}
}

func TestCreateEntryCreatesADirectory(t *testing.T) {
	dir := t.TempDir()
	path := filepath.Join(dir, "sub")

	if err := createEntry(path, true); err != nil {
		t.Fatalf("createEntry: %v", err)
	}
	info, err := os.Stat(path)
	if err != nil {
		t.Fatalf("stat: %v", err)
	}
	if !info.IsDir() {
		t.Error("want a directory")
	}
}

func TestCreateEntryRefusesAnExistingEntryWithoutTouchingIt(t *testing.T) {
	dir := t.TempDir()
	path := filepath.Join(dir, "a.txt")
	if err := os.WriteFile(path, []byte("keep me"), 0o644); err != nil {
		t.Fatal(err)
	}

	if err := createEntry(path, false); !errors.Is(err, errExists) {
		t.Fatalf("error = %v, want errExists", err)
	}
	// Refusing is only worth anything if the existing content survived.
	got, err := os.ReadFile(path)
	if err != nil {
		t.Fatal(err)
	}
	if string(got) != "keep me" {
		t.Errorf("content = %q, want %q", got, "keep me")
	}

	if err := createEntry(dir, true); !errors.Is(err, errExists) {
		t.Errorf("existing directory: error = %v, want errExists", err)
	}
}

func TestMovePathRenamesAndMoves(t *testing.T) {
	dir := t.TempDir()
	src := filepath.Join(dir, "a.txt")
	if err := os.WriteFile(src, []byte("hi"), 0o644); err != nil {
		t.Fatal(err)
	}
	if err := os.Mkdir(filepath.Join(dir, "sub"), 0o755); err != nil {
		t.Fatal(err)
	}

	renamed := filepath.Join(dir, "b.txt")
	if err := movePath(src, renamed); err != nil {
		t.Fatalf("rename: %v", err)
	}
	if _, err := os.Stat(src); !os.IsNotExist(err) {
		t.Error("source should be gone after a rename")
	}

	moved := filepath.Join(dir, "sub", "b.txt")
	if err := movePath(renamed, moved); err != nil {
		t.Fatalf("move: %v", err)
	}
	got, err := os.ReadFile(moved)
	if err != nil {
		t.Fatalf("read moved: %v", err)
	}
	if string(got) != "hi" {
		t.Errorf("content = %q, want %q", got, "hi")
	}
}

func TestMovePathCreatesMissingParentDirectories(t *testing.T) {
	dir := t.TempDir()
	src := filepath.Join(dir, "a.txt")
	if err := os.WriteFile(src, nil, 0o644); err != nil {
		t.Fatal(err)
	}

	dst := filepath.Join(dir, "new", "nested", "a.txt")
	if err := movePath(src, dst); err != nil {
		t.Fatalf("movePath: %v", err)
	}
	if _, err := os.Stat(dst); err != nil {
		t.Fatalf("stat: %v", err)
	}
}

func TestMovePathRefusesAnExistingDestination(t *testing.T) {
	dir := t.TempDir()
	src := filepath.Join(dir, "a.txt")
	dst := filepath.Join(dir, "b.txt")
	if err := os.WriteFile(src, []byte("src"), 0o644); err != nil {
		t.Fatal(err)
	}
	if err := os.WriteFile(dst, []byte("dst"), 0o644); err != nil {
		t.Fatal(err)
	}

	if err := movePath(src, dst); !errors.Is(err, errExists) {
		t.Fatalf("error = %v, want errExists", err)
	}
	if _, err := os.Stat(src); err != nil {
		t.Error("source should still be there after a refusal")
	}
	got, err := os.ReadFile(dst)
	if err != nil {
		t.Fatal(err)
	}
	if string(got) != "dst" {
		t.Errorf("destination content = %q, want it untouched", got)
	}
}

// A case-only rename must work even on a case-insensitive filesystem
// (macOS), where Lstat("A.txt") happily answers for "a.txt" and a naive
// existence check would refuse a perfectly ordinary rename.
func TestMovePathAllowsACaseOnlyRename(t *testing.T) {
	dir := t.TempDir()
	src := filepath.Join(dir, "a.txt")
	dst := filepath.Join(dir, "A.txt")
	if err := os.WriteFile(src, []byte("hi"), 0o644); err != nil {
		t.Fatal(err)
	}

	if err := movePath(src, dst); err != nil {
		t.Fatalf("movePath: %v", err)
	}
	got, err := os.ReadFile(dst)
	if err != nil {
		t.Fatalf("read: %v", err)
	}
	if string(got) != "hi" {
		t.Errorf("content = %q, want %q", got, "hi")
	}
}

func TestMovePathRefusesMovingADirectoryIntoItself(t *testing.T) {
	dir := t.TempDir()
	src := filepath.Join(dir, "sub")
	if err := os.Mkdir(src, 0o755); err != nil {
		t.Fatal(err)
	}

	if err := movePath(src, filepath.Join(src, "inner")); !errors.Is(err, errIntoSelf) {
		t.Errorf("error = %v, want errIntoSelf", err)
	}
}

func TestDeletePathRemovesFilesAndEmptyDirectories(t *testing.T) {
	dir := t.TempDir()
	file := filepath.Join(dir, "a.txt")
	empty := filepath.Join(dir, "empty")
	if err := os.WriteFile(file, nil, 0o644); err != nil {
		t.Fatal(err)
	}
	if err := os.Mkdir(empty, 0o755); err != nil {
		t.Fatal(err)
	}

	if err := deletePath(file, false); err != nil {
		t.Fatalf("delete file: %v", err)
	}
	if err := deletePath(empty, false); err != nil {
		t.Fatalf("delete empty dir: %v", err)
	}
	for _, p := range []string{file, empty} {
		if _, err := os.Stat(p); !os.IsNotExist(err) {
			t.Errorf("%s should be gone", p)
		}
	}
}

func TestDeletePathRefusesANonEmptyDirectoryUnlessRecursive(t *testing.T) {
	dir := t.TempDir()
	sub := filepath.Join(dir, "sub")
	child := filepath.Join(sub, "a.txt")
	if err := os.Mkdir(sub, 0o755); err != nil {
		t.Fatal(err)
	}
	if err := os.WriteFile(child, nil, 0o644); err != nil {
		t.Fatal(err)
	}

	if err := deletePath(sub, false); !errors.Is(err, errNotEmpty) {
		t.Fatalf("error = %v, want errNotEmpty", err)
	}
	if _, err := os.Stat(child); err != nil {
		t.Error("child should survive a refused delete")
	}

	if err := deletePath(sub, true); err != nil {
		t.Fatalf("recursive delete: %v", err)
	}
	if _, err := os.Stat(sub); !os.IsNotExist(err) {
		t.Error("directory should be gone after a recursive delete")
	}
}

func TestDirEntryCount(t *testing.T) {
	dir := t.TempDir()
	if got := dirEntryCount(dir); got != 0 {
		t.Errorf("empty dir count = %d, want 0", got)
	}

	for _, name := range []string{"a.txt", ".hidden"} {
		if err := os.WriteFile(filepath.Join(dir, name), nil, 0o644); err != nil {
			t.Fatal(err)
		}
	}
	if got := dirEntryCount(dir); got != 2 {
		t.Errorf("count = %d, want 2 (dotfiles included)", got)
	}
	if got := dirEntryCount(filepath.Join(dir, "a.txt")); got != 0 {
		t.Errorf("count for a file = %d, want 0", got)
	}
}
