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
	got, err := ListFiles(dir, "", false)
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
	if _, err := ListFiles(dir, "", false); err == nil {
		t.Fatal("expected an error listing files outside a git repo")
	}
}

func TestListFilesScopeNarrowsToSubtree(t *testing.T) {
	dir := newFilesTestRepo(t)
	got, err := ListFiles(dir, "src", false)
	if err != nil {
		t.Fatalf("ListFiles: %v", err)
	}
	want := []string{"src/main.go"}
	if len(got) != len(want) || got[0] != want[0] {
		t.Fatalf("got %v, want %v", got, want)
	}
}

func TestListFilesIncludeIgnoredListsIgnoredFiles(t *testing.T) {
	dir := newFilesTestRepo(t)
	got, err := ListFiles(dir, "", true)
	if err != nil {
		t.Fatalf("ListFiles: %v", err)
	}
	sort.Strings(got)

	want := []string{".gitignore", "ignored.txt", "src/main.go", "untracked.txt", "vendor/dep.go"}
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

func TestIsIgnored(t *testing.T) {
	dir := newFilesTestRepo(t)
	for path, want := range map[string]bool{
		"vendor":        true,
		"vendor/dep.go": true,
		"ignored.txt":   true,
		"src":           false,
		"untracked.txt": false,
	} {
		if got := IsIgnored(dir, path); got != want {
			t.Errorf("IsIgnored(%q) = %v, want %v", path, got, want)
		}
	}
	if IsIgnored(t.TempDir(), "anything") {
		t.Error("IsIgnored outside a git repo should be false")
	}
}
