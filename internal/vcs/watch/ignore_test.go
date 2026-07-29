package watch

import (
	"os"
	"os/exec"
	"path/filepath"
	"testing"
)

func newTestRepo(t *testing.T) string {
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

	if err := os.WriteFile(filepath.Join(dir, ".gitignore"), []byte("vendor/\nnode_modules/\n"), 0o644); err != nil {
		t.Fatal(err)
	}
	if err := os.MkdirAll(filepath.Join(dir, "vendor", "pkg"), 0o755); err != nil {
		t.Fatal(err)
	}
	if err := os.MkdirAll(filepath.Join(dir, "node_modules", "leftpad"), 0o755); err != nil {
		t.Fatal(err)
	}
	if err := os.MkdirAll(filepath.Join(dir, "src"), 0o755); err != nil {
		t.Fatal(err)
	}
	if err := os.WriteFile(filepath.Join(dir, "vendor", "pkg", "a.go"), []byte("x"), 0o644); err != nil {
		t.Fatal(err)
	}
	if err := os.WriteFile(filepath.Join(dir, "src", "main.go"), []byte("package main"), 0o644); err != nil {
		t.Fatal(err)
	}
	run("add", ".gitignore", "src/main.go")
	run("commit", "-q", "-m", "init")

	return dir
}

func TestIgnoredDirsFindsVendorAndNodeModules(t *testing.T) {
	dir := newTestRepo(t)
	got, err := ignoredDirs(dir)
	if err != nil {
		t.Fatalf("ignoredDirs: %v", err)
	}
	if !got["vendor"] {
		t.Errorf("expected vendor to be reported ignored, got %v", got)
	}
	if !got["node_modules"] {
		t.Errorf("expected node_modules to be reported ignored, got %v", got)
	}
	if got["src"] {
		t.Errorf("src should not be reported ignored")
	}
}

func TestIgnoredDirsNonRepoErrors(t *testing.T) {
	dir := t.TempDir() // not a git repo
	if _, err := ignoredDirs(dir); err == nil {
		t.Fatal("expected an error running git ls-files outside a repo")
	}
}
