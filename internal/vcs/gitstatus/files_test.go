package gitstatus

import (
	"os"
	"os/exec"
	"path/filepath"
	"sort"
	"testing"
)

func newFilesTestRepo(t *testing.T) string {
	t.Helper()
	dir := t.TempDir()
	run := func(args ...string) {
		cmd := exec.Command("git", args...)
		cmd.Dir = dir
		if out, err := cmd.CombinedOutput(); err != nil {
			t.Fatalf("git %v: %v\n%s", args, err, out)
		}
	}
	run("init", "-q")
	run("config", "user.email", "test@example.com")
	run("config", "user.name", "test")

	if err := os.WriteFile(filepath.Join(dir, ".gitignore"), []byte("ignored.txt\nvendor/\n"), 0o644); err != nil {
		t.Fatal(err)
	}
	if err := os.MkdirAll(filepath.Join(dir, "src"), 0o755); err != nil {
		t.Fatal(err)
	}
	if err := os.MkdirAll(filepath.Join(dir, "vendor"), 0o755); err != nil {
		t.Fatal(err)
	}
	if err := os.WriteFile(filepath.Join(dir, "src", "main.go"), []byte("package main"), 0o644); err != nil {
		t.Fatal(err)
	}
	run("add", ".gitignore", "src/main.go")
	run("commit", "-q", "-m", "init")

	// untracked-but-not-ignored file
	if err := os.WriteFile(filepath.Join(dir, "untracked.txt"), []byte("x"), 0o644); err != nil {
		t.Fatal(err)
	}
	// ignored file and ignored directory
	if err := os.WriteFile(filepath.Join(dir, "ignored.txt"), []byte("x"), 0o644); err != nil {
		t.Fatal(err)
	}
	if err := os.WriteFile(filepath.Join(dir, "vendor", "dep.go"), []byte("x"), 0o644); err != nil {
		t.Fatal(err)
	}

	return dir
}

func TestListFilesIncludesTrackedAndUntrackedExcludesIgnored(t *testing.T) {
	dir := newFilesTestRepo(t)
	got, err := ListFiles(dir)
	if err != nil {
		t.Fatalf("ListFiles: %v", err)
	}
	sort.Strings(got)

	want := []string{".gitignore", "src/main.go", "untracked.txt"}
	if len(got) != len(want) {
		t.Fatalf("got %v, want %v", got, want)
	}
	for i := range want {
		if got[i] != want[i] {
			t.Errorf("got %v, want %v", got, want)
			break
		}
	}
}

func TestListFilesNonRepoErrors(t *testing.T) {
	dir := t.TempDir()
	if _, err := ListFiles(dir); err == nil {
		t.Fatal("expected an error listing files outside a git repo")
	}
}
