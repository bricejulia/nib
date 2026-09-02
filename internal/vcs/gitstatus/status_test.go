package gitstatus

import (
	"os"
	"os/exec"
	"path/filepath"
	"testing"
)

func newStatusTestRepo(t *testing.T) string {
	t.Helper()
	dir := t.TempDir()
	run := func(args ...string) {
		cmd := exec.Command("git", args...)
		cmd.Dir = dir
		if out, err := cmd.CombinedOutput(); err != nil {
			t.Fatalf("git %v: %v\n%s", args, err, out)
		}
	}
	run("init", "-q", "-b", "main")
	run("config", "user.email", "test@example.com")
	run("config", "user.name", "test")

	if err := os.MkdirAll(filepath.Join(dir, "sub"), 0o755); err != nil {
		t.Fatal(err)
	}
	if err := os.WriteFile(filepath.Join(dir, "sub", "foo.txt"), []byte("hi"), 0o644); err != nil {
		t.Fatal(err)
	}
	if err := os.WriteFile(filepath.Join(dir, "top.txt"), []byte("hi"), 0o644); err != nil {
		t.Fatal(err)
	}
	run("add", ".")
	run("commit", "-q", "-m", "init")

	return dir
}

func TestRunPorcelainAtRepoRootReturnsRepoRelativePaths(t *testing.T) {
	dir := newStatusTestRepo(t)
	if err := os.WriteFile(filepath.Join(dir, "sub", "foo.txt"), []byte("changed"), 0o644); err != nil {
		t.Fatal(err)
	}

	got, err := RunPorcelain(dir)
	if err != nil {
		t.Fatalf("RunPorcelain: %v", err)
	}
	if len(got) != 1 || got["sub/foo.txt"] != Modified {
		t.Fatalf("got %+v, want {\"sub/foo.txt\": Modified}", got)
	}
}

// This is the case a nib session run from a child folder of the repo hits
// on every git-status refresh: dir is "sub", not the repo root, so git
// itself still reports "sub/foo.txt" (porcelain paths are always
// repo-root-relative) -- but every caller of RunPorcelain expects a path
// relative to dir, i.e. "foo.txt" here. Getting this wrong is what made
// opening a file from the file tree's git-changes mode build an invalid
// path (see internal/ui/filetree/changes.go's buildChangesTree, which joins
// these paths directly onto its own root).
func TestRunPorcelainFromSubdirectoryReturnsSubdirRelativePaths(t *testing.T) {
	dir := newStatusTestRepo(t)
	if err := os.WriteFile(filepath.Join(dir, "sub", "foo.txt"), []byte("changed"), 0o644); err != nil {
		t.Fatal(err)
	}
	if err := os.WriteFile(filepath.Join(dir, "top.txt"), []byte("changed too"), 0o644); err != nil {
		t.Fatal(err)
	}

	got, err := RunPorcelain(filepath.Join(dir, "sub"))
	if err != nil {
		t.Fatalf("RunPorcelain: %v", err)
	}
	// "top.txt" sits outside the "sub" subtree, so it has nothing to attach
	// to under dir and must be dropped rather than surface as (say) a
	// literal "../top.txt" or a bogus "top.txt".
	if len(got) != 1 || got["foo.txt"] != Modified {
		t.Fatalf("got %+v, want {\"foo.txt\": Modified}", got)
	}
}

func TestRunPorcelainNonRepoErrors(t *testing.T) {
	dir := t.TempDir()
	if _, err := RunPorcelain(dir); err == nil {
		t.Fatal("expected an error outside a git repo")
	}
}
