package gitblame

import (
	"os"
	"os/exec"
	"path/filepath"
	"strings"
	"testing"
	"time"
)

func newTestRepo(t *testing.T) string {
	t.Helper()
	dir := t.TempDir()
	run := func(args ...string) {
		t.Helper()
		cmd := exec.Command("git", args...)
		cmd.Dir = dir
		if out, err := cmd.CombinedOutput(); err != nil {
			t.Fatalf("git %v: %v\n%s", args, err, out)
		}
	}
	run("init", "-q", "-b", "main")
	run("config", "user.email", "blame@example.com")
	run("config", "user.name", "Blame Tester")
	return dir
}

func commit(t *testing.T, dir, name, content, message string) {
	t.Helper()
	if err := os.WriteFile(filepath.Join(dir, name), []byte(content), 0o644); err != nil {
		t.Fatal(err)
	}
	for _, args := range [][]string{{"add", name}, {"commit", "-q", "-m", message}} {
		cmd := exec.Command("git", args...)
		cmd.Dir = dir
		if out, err := cmd.CombinedOutput(); err != nil {
			t.Fatalf("git %v: %v\n%s", args, err, out)
		}
	}
}

func TestLineReportsCommitAuthorAndSummary(t *testing.T) {
	dir := newTestRepo(t)
	commit(t, dir, "f.txt", "first\nsecond\n", "add f.txt")
	commit(t, dir, "f.txt", "first\nCHANGED\n", "change the second line")

	path := filepath.Join(dir, "f.txt")

	got, err := Line(dir, path, 2)
	if err != nil {
		t.Fatalf("Line: %v", err)
	}
	if got.Uncommitted {
		t.Error("expected a committed line")
	}
	if got.Author != "Blame Tester" {
		t.Errorf("author = %q, want %q", got.Author, "Blame Tester")
	}
	if got.Summary != "change the second line" {
		t.Errorf("summary = %q", got.Summary)
	}
	if len(got.Commit) != shortHashLen {
		t.Errorf("commit = %q, want %d abbreviated characters", got.Commit, shortHashLen)
	}
	if got.Time.IsZero() {
		t.Error("expected a commit time")
	}
	if d := time.Since(got.Time); d > time.Hour || d < -time.Hour {
		t.Errorf("commit time %v is not close to now", got.Time)
	}

	// Line 1 predates that change, so it must blame the earlier commit.
	first, err := Line(dir, path, 1)
	if err != nil {
		t.Fatalf("Line: %v", err)
	}
	if first.Summary != "add f.txt" {
		t.Errorf("line 1 summary = %q, want %q", first.Summary, "add f.txt")
	}
	if first.Commit == got.Commit {
		t.Error("expected the two lines to blame different commits")
	}
}

func TestLineReportsUncommittedWorkingTreeChange(t *testing.T) {
	dir := newTestRepo(t)
	commit(t, dir, "f.txt", "first\n", "add f.txt")

	path := filepath.Join(dir, "f.txt")
	if err := os.WriteFile(path, []byte("first\nbrand new\n"), 0o644); err != nil {
		t.Fatal(err)
	}

	got, err := Line(dir, path, 2)
	if err != nil {
		t.Fatalf("Line: %v", err)
	}
	if !got.Uncommitted {
		t.Errorf("expected Uncommitted for a working-tree-only line, got %+v", got)
	}
	if got.Commit != "" {
		t.Errorf("expected no commit hash for an uncommitted line, got %q", got.Commit)
	}
}

func TestLineRejectsOutOfRangeLine(t *testing.T) {
	dir := newTestRepo(t)
	commit(t, dir, "f.txt", "only\n", "add f.txt")

	if _, err := Line(dir, filepath.Join(dir, "f.txt"), 0); err == nil {
		t.Error("expected an error for line 0")
	}
	// Past the end of the file, git itself refuses.
	if _, err := Line(dir, filepath.Join(dir, "f.txt"), 99); err == nil {
		t.Error("expected an error for a line past the end of the file")
	}
}

func TestLineOutsideARepositoryErrors(t *testing.T) {
	dir := t.TempDir()
	path := filepath.Join(dir, "f.txt")
	if err := os.WriteFile(path, []byte("x\n"), 0o644); err != nil {
		t.Fatal(err)
	}
	if _, err := Line(dir, path, 1); err == nil {
		t.Error("expected an error outside a git repository")
	}
}

func TestParsePorcelainSkipsUnknownHeaders(t *testing.T) {
	out := []byte(strings.Join([]string{
		"4b825dc642cb6eb9a060e54bf8d69288fbee4904 12 12 1",
		"author Ada Lovelace",
		"author-mail <ada@example.com>",
		"author-time 1700000000",
		"author-tz +0200",
		"committer Someone Else",
		"committer-time 1700000001",
		"summary the subject line",
		"boundary",
		"previous 0000000000000000000000000000000000000001 f.txt",
		"filename f.txt",
		"\tthe blamed source line",
	}, "\n"))

	got, err := parsePorcelain(out)
	if err != nil {
		t.Fatalf("parsePorcelain: %v", err)
	}
	if got.Author != "Ada Lovelace" {
		t.Errorf("author = %q", got.Author)
	}
	if got.Summary != "the subject line" {
		t.Errorf("summary = %q", got.Summary)
	}
	if got.Commit != "4b825dc" {
		t.Errorf("commit = %q, want %q", got.Commit, "4b825dc")
	}
	if got.Time.Unix() != 1700000000 {
		t.Errorf("time = %v, want unix 1700000000", got.Time)
	}
	if _, offset := got.Time.Zone(); offset != 2*3600 {
		t.Errorf("zone offset = %d seconds, want %d", offset, 2*3600)
	}
	// "committer Someone Else" must not overwrite the author.
	if got.Author == "Someone Else" {
		t.Error("committer leaked into Author")
	}
}

// A source line beginning with a keyword-looking word must not be parsed as
// a header — the TAB prefix is what ends the header block.
func TestParsePorcelainStopsAtTheSourceLine(t *testing.T) {
	out := []byte(strings.Join([]string{
		"4b825dc642cb6eb9a060e54bf8d69288fbee4904 1 1 1",
		"author Real Author",
		"summary real summary",
		"filename f.txt",
		"\tsummary not-a-header",
		"author Bogus",
	}, "\n"))

	got, err := parsePorcelain(out)
	if err != nil {
		t.Fatalf("parsePorcelain: %v", err)
	}
	if got.Author != "Real Author" {
		t.Errorf("author = %q, want %q", got.Author, "Real Author")
	}
	if got.Summary != "real summary" {
		t.Errorf("summary = %q, want %q", got.Summary, "real summary")
	}
}

func TestParsePorcelainRejectsGarbage(t *testing.T) {
	for _, in := range []string{"", "not a blame header\n", "short 1 1 1\n"} {
		if _, err := parsePorcelain([]byte(in)); err == nil {
			t.Errorf("expected an error for %q", in)
		}
	}
}

func TestParseTZ(t *testing.T) {
	cases := map[string]int{
		"+0200": 2 * 3600,
		"-0530": -(5*3600 + 30*60),
		"+0000": 0,
		"":      0,
		"weird": 0,
		"+02":   0,
	}
	for in, want := range cases {
		if _, offset := time.Now().In(parseTZ(in)).Zone(); offset != want {
			t.Errorf("parseTZ(%q) offset = %d, want %d", in, offset, want)
		}
	}
}
