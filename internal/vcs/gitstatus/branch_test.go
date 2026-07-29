package gitstatus

import (
	"os/exec"
	"testing"
)

func newBranchTestRepo(t *testing.T) string {
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
	run("commit", "-q", "--allow-empty", "-m", "init")
	return dir
}

func TestCurrentBranchReturnsBranchName(t *testing.T) {
	dir := newBranchTestRepo(t)
	got, err := CurrentBranch(dir)
	if err != nil {
		t.Fatalf("CurrentBranch: %v", err)
	}
	if got != "main" {
		t.Fatalf("got %q, want %q", got, "main")
	}
}

func TestCurrentBranchDetachedHeadReturnsEmpty(t *testing.T) {
	dir := newBranchTestRepo(t)
	cmd := exec.Command("git", "checkout", "-q", "--detach", "HEAD")
	cmd.Dir = dir
	if out, err := cmd.CombinedOutput(); err != nil {
		t.Fatalf("git checkout: %v\n%s", err, out)
	}

	got, err := CurrentBranch(dir)
	if err != nil {
		t.Fatalf("CurrentBranch: %v", err)
	}
	if got != "" {
		t.Fatalf("expected empty branch name on detached HEAD, got %q", got)
	}
}

func TestCurrentBranchNonRepoErrors(t *testing.T) {
	dir := t.TempDir()
	if _, err := CurrentBranch(dir); err == nil {
		t.Fatal("expected an error outside a git repo")
	}
}
