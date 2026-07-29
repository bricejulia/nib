package finder

import (
	"os"
	"os/exec"
	"path/filepath"
	"testing"
)

// newContentSearchRepo builds a small git repo with a known "needle" match
// on line 2 of haystack.go, for content-search tests.
func newContentSearchRepo(t *testing.T) string {
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

	content := "package main\n// contains needle for testing\nfunc main() {}\n"
	if err := os.WriteFile(filepath.Join(dir, "haystack.go"), []byte(content), 0o644); err != nil {
		t.Fatal(err)
	}
	if err := os.WriteFile(filepath.Join(dir, "other.go"), []byte("package main\n"), 0o644); err != nil {
		t.Fatal(err)
	}
	run("add", "haystack.go", "other.go")
	run("commit", "-q", "-m", "init")

	return dir
}

func TestSearchContentFindsMatchWithLineNumber(t *testing.T) {
	dir := newContentSearchRepo(t)
	matches, err := searchContent(dir, "needle")
	if err != nil {
		t.Fatalf("searchContent: %v", err)
	}
	if len(matches) != 1 {
		t.Fatalf("got %d matches, want 1: %+v", len(matches), matches)
	}
	if matches[0].path != "haystack.go" {
		t.Errorf("path = %q, want %q", matches[0].path, "haystack.go")
	}
	if matches[0].line != 2 {
		t.Errorf("line = %d, want 2", matches[0].line)
	}
	if matches[0].text == "" {
		t.Error("expected non-empty matched line text")
	}
}

func TestSearchContentIsCaseInsensitive(t *testing.T) {
	dir := newContentSearchRepo(t)
	matches, err := searchContent(dir, "NEEDLE")
	if err != nil {
		t.Fatalf("searchContent: %v", err)
	}
	if len(matches) != 1 {
		t.Fatalf("expected a case-insensitive match, got %d matches", len(matches))
	}
}

func TestSearchContentNoMatchesReturnsEmptyNotError(t *testing.T) {
	dir := newContentSearchRepo(t)
	matches, err := searchContent(dir, "definitely-not-present-anywhere")
	if err != nil {
		t.Fatalf("expected no error for zero matches (git grep's exit code 1), got %v", err)
	}
	if len(matches) != 0 {
		t.Errorf("expected no matches, got %+v", matches)
	}
}

func TestSearchContentTreatsQueryAsLiteralNotRegex(t *testing.T) {
	dir := newContentSearchRepo(t)
	// "main(" contains a regex metacharacter; as a fixed string it should
	// still match "main()" in the fixture.
	matches, err := searchContent(dir, "main(")
	if err != nil {
		t.Fatalf("searchContent: %v", err)
	}
	if len(matches) == 0 {
		t.Error(`expected "main(" to match literally against "func main() {}"`)
	}
}

func TestSearchContentNonRepoErrors(t *testing.T) {
	dir := t.TempDir()
	if _, err := searchContent(dir, "anything"); err == nil {
		t.Fatal("expected an error searching outside a git repo")
	}
}
