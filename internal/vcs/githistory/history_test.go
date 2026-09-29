package githistory

import (
	"os"
	"os/exec"
	"path/filepath"
	"strings"
	"testing"
)

func newTestRepo(t *testing.T) string {
	t.Helper()
	dir := t.TempDir()
	git(t, dir, "init", "-q", "-b", "main")
	git(t, dir, "config", "user.email", "test@example.com")
	git(t, dir, "config", "user.name", "tester")
	return dir
}

func git(t *testing.T, dir string, args ...string) {
	t.Helper()
	cmd := exec.Command("git", args...)
	cmd.Dir = dir
	if out, err := cmd.CombinedOutput(); err != nil {
		t.Fatalf("git %v: %v\n%s", args, err, out)
	}
}

func writeAndCommit(t *testing.T, dir, name, content, msg string) {
	t.Helper()
	if err := os.MkdirAll(filepath.Dir(filepath.Join(dir, name)), 0o755); err != nil {
		t.Fatal(err)
	}
	if err := os.WriteFile(filepath.Join(dir, name), []byte(content), 0o644); err != nil {
		t.Fatal(err)
	}
	git(t, dir, "add", name)
	git(t, dir, "commit", "-q", "-m", msg)
}

func TestLogListsCommitsNewestFirst(t *testing.T) {
	dir := newTestRepo(t)
	writeAndCommit(t, dir, "f.txt", "a\n", "first")
	writeAndCommit(t, dir, "other.txt", "x\n", "unrelated")
	writeAndCommit(t, dir, "f.txt", "a\nb\n", "second")

	got, err := Log(dir, filepath.Join(dir, "f.txt"), 0)
	if err != nil {
		t.Fatalf("Log: %v", err)
	}
	if len(got) != 2 {
		t.Fatalf("got %d commits, want 2: %+v", len(got), got)
	}
	if got[0].Summary != "second" || got[1].Summary != "first" {
		t.Errorf("summaries = %q, %q; want second, first", got[0].Summary, got[1].Summary)
	}
	for _, c := range got {
		if len(c.Hash) != 40 || !strings.HasPrefix(c.Hash, c.ShortHash) || c.ShortHash == "" {
			t.Errorf("bad hashes: %q / %q", c.Hash, c.ShortHash)
		}
		if c.Author != "tester" {
			t.Errorf("author = %q, want tester", c.Author)
		}
		if c.Time.IsZero() {
			t.Error("time is zero")
		}
		if c.Path != "f.txt" {
			t.Errorf("path = %q, want f.txt", c.Path)
		}
	}
}

func TestLogRespectsLimit(t *testing.T) {
	dir := newTestRepo(t)
	writeAndCommit(t, dir, "f.txt", "a\n", "one")
	writeAndCommit(t, dir, "f.txt", "b\n", "two")
	writeAndCommit(t, dir, "f.txt", "c\n", "three")

	got, err := Log(dir, "f.txt", 2)
	if err != nil {
		t.Fatalf("Log: %v", err)
	}
	if len(got) != 2 || got[0].Summary != "three" {
		t.Fatalf("got %+v, want the 2 newest", got)
	}
}

func TestLogFollowsRenames(t *testing.T) {
	dir := newTestRepo(t)
	writeAndCommit(t, dir, "old.txt", "a\nb\nc\n", "create")
	git(t, dir, "mv", "old.txt", "new.txt")
	git(t, dir, "commit", "-q", "-m", "rename")
	writeAndCommit(t, dir, "new.txt", "a\nb\nc\nd\n", "edit")

	got, err := Log(dir, "new.txt", 0)
	if err != nil {
		t.Fatalf("Log: %v", err)
	}
	var paths, sums []string
	for _, c := range got {
		paths = append(paths, c.Path)
		sums = append(sums, c.Summary)
	}
	if strings.Join(sums, ",") != "edit,rename,create" {
		t.Fatalf("summaries = %v", sums)
	}
	if strings.Join(paths, ",") != "new.txt,new.txt,old.txt" {
		t.Fatalf("paths = %v", paths)
	}
}

func TestLogUntrackedFileHasNoHistory(t *testing.T) {
	dir := newTestRepo(t)
	writeAndCommit(t, dir, "tracked.txt", "x\n", "init")
	if err := os.WriteFile(filepath.Join(dir, "new.txt"), []byte("y\n"), 0o644); err != nil {
		t.Fatal(err)
	}

	got, err := Log(dir, "new.txt", 0)
	if err != nil || len(got) != 0 {
		t.Fatalf("got %+v, %v; want no commits, no error", got, err)
	}
}

func TestLogEmptyRepositoryHasNoHistory(t *testing.T) {
	dir := newTestRepo(t)
	got, err := Log(dir, "f.txt", 0)
	if err != nil || len(got) != 0 {
		t.Fatalf("got %+v, %v; want no commits, no error", got, err)
	}
}

func TestLogOutsideRepositoryErrors(t *testing.T) {
	if _, err := Log(t.TempDir(), "f.txt", 0); err == nil {
		t.Fatal("want an error outside a git repository")
	}
}

func TestCommitDiffShowsThatCommitsChange(t *testing.T) {
	dir := newTestRepo(t)
	writeAndCommit(t, dir, "f.txt", "a\n", "first")
	writeAndCommit(t, dir, "f.txt", "a\nb\n", "second")

	commits, err := Log(dir, "f.txt", 0)
	if err != nil {
		t.Fatal(err)
	}
	lines, err := CommitDiff(dir, commits[0], commits[1].Path)
	if err != nil {
		t.Fatalf("CommitDiff: %v", err)
	}
	joined := strings.Join(lines, "\n")
	if !strings.Contains(joined, "+b") || strings.Contains(joined, "+a") {
		t.Errorf("diff should add only b:\n%s", joined)
	}

	// The root commit diffs against nothing: the whole file is added.
	lines, err = CommitDiff(dir, commits[1], commits[1].Path)
	if err != nil {
		t.Fatalf("CommitDiff root: %v", err)
	}
	if !strings.Contains(strings.Join(lines, "\n"), "+a") {
		t.Errorf("root diff should add a:\n%s", strings.Join(lines, "\n"))
	}
}

func TestCommitDiffShowsRenameAsRename(t *testing.T) {
	dir := newTestRepo(t)
	writeAndCommit(t, dir, "old.txt", "a\nb\nc\n", "create")
	git(t, dir, "mv", "old.txt", "new.txt")
	git(t, dir, "commit", "-q", "-m", "rename")

	commits, err := Log(dir, "new.txt", 0)
	if err != nil {
		t.Fatal(err)
	}
	lines, err := CommitDiff(dir, commits[0], commits[1].Path)
	if err != nil {
		t.Fatalf("CommitDiff: %v", err)
	}
	joined := strings.Join(lines, "\n")
	if !strings.Contains(joined, "rename from old.txt") || !strings.Contains(joined, "rename to new.txt") {
		t.Errorf("want a rename diff, got:\n%s", joined)
	}
}

func TestCommitDiffFromSubdirectory(t *testing.T) {
	dir := newTestRepo(t)
	writeAndCommit(t, dir, "sub/f.txt", "a\n", "first")
	writeAndCommit(t, dir, "sub/f.txt", "a\nb\n", "second")
	sub := filepath.Join(dir, "sub")

	commits, err := Log(sub, "f.txt", 0)
	if err != nil {
		t.Fatal(err)
	}
	if len(commits) != 2 || commits[0].Path != "sub/f.txt" {
		t.Fatalf("got %+v", commits)
	}
	lines, err := CommitDiff(sub, commits[0], commits[1].Path)
	if err != nil {
		t.Fatalf("CommitDiff: %v", err)
	}
	if !strings.Contains(strings.Join(lines, "\n"), "+b") {
		t.Errorf("diff missing +b:\n%s", strings.Join(lines, "\n"))
	}
}
