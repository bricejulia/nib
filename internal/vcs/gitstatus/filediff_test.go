package gitstatus

import (
	"errors"
	"os"
	"path/filepath"
	"strings"
	"testing"
)

func TestParseHunkListKeepsBothSides(t *testing.T) {
	diff := []byte(strings.Join([]string{
		"diff --git a/f.txt b/f.txt",
		"index 1234567..89abcde 100644",
		"--- a/f.txt",
		"+++ b/f.txt",
		"@@ -2,2 +2,1 @@",
		"-old one",
		"-old two",
		"+new one",
		"@@ -10,0 +9,1 @@",
		"+appended",
	}, "\n"))

	hunks := parseHunkList(diff)
	if len(hunks) != 2 {
		t.Fatalf("expected 2 hunks, got %d: %+v", len(hunks), hunks)
	}

	first := hunks[0]
	if first.NewStart != 2 || first.NewLines != 1 || first.OldStart != 2 || first.OldLines != 2 {
		t.Errorf("first hunk header: %+v", first)
	}
	if got, want := strings.Join(first.Removed, "|"), "old one|old two"; got != want {
		t.Errorf("first hunk removed = %q, want %q", got, want)
	}
	if got, want := strings.Join(first.Added, "|"), "new one"; got != want {
		t.Errorf("first hunk added = %q, want %q", got, want)
	}

	// The "---"/"+++" file header must not be mistaken for a removal and an
	// addition belonging to the first hunk.
	if len(hunks[1].Added) != 1 || hunks[1].Added[0] != "appended" {
		t.Errorf("second hunk added = %+v", hunks[1].Added)
	}
	if len(hunks[1].Removed) != 0 {
		t.Errorf("second hunk should remove nothing, got %+v", hunks[1].Removed)
	}
}

func TestParseHunkListSkipsNoNewlineNote(t *testing.T) {
	diff := []byte(strings.Join([]string{
		"@@ -1 +1 @@",
		"-a",
		"\\ No newline at end of file",
		"+b",
	}, "\n"))

	hunks := parseHunkList(diff)
	if len(hunks) != 1 {
		t.Fatalf("expected 1 hunk, got %+v", hunks)
	}
	if len(hunks[0].Removed) != 1 || len(hunks[0].Added) != 1 {
		t.Errorf("expected one line each side, got %+v", hunks[0])
	}
}

func TestHunkAtLine(t *testing.T) {
	hunks := []Hunk{
		{NewStart: 3, NewLines: 2, OldLines: 2},  // modifies new lines 3-4 (indices 2-3)
		{NewStart: 9, NewLines: 0, OldLines: 3},  // pure deletion before new line 9 (index 9)
		{NewStart: 20, NewLines: 1, OldLines: 0}, // adds new line 20 (index 19)
	}

	cases := []struct {
		ln       int
		wantOK   bool
		wantHunk int
	}{
		{ln: 0, wantOK: false},
		{ln: 2, wantOK: true, wantHunk: 0},
		{ln: 3, wantOK: true, wantHunk: 0},
		{ln: 4, wantOK: false},
		{ln: 9, wantOK: true, wantHunk: 1},
		{ln: 19, wantOK: true, wantHunk: 2},
	}
	for _, c := range cases {
		got, ok := HunkAtLine(hunks, c.ln)
		if ok != c.wantOK {
			t.Errorf("line %d: ok = %v, want %v", c.ln, ok, c.wantOK)
			continue
		}
		if ok && got.NewStart != hunks[c.wantHunk].NewStart {
			t.Errorf("line %d: got hunk %+v, want %+v", c.ln, got, hunks[c.wantHunk])
		}
	}
}

// The line-status map and the hunk list come from one parser now, so they
// must keep agreeing about which lines a diff touches.
func TestParseHunksAgreesWithParseHunkList(t *testing.T) {
	diff := []byte(strings.Join([]string{
		"@@ -1,0 +1,2 @@",
		"+added one",
		"+added two",
		"@@ -8,3 +10,0 @@",
		"-gone one",
		"-gone two",
		"-gone three",
		"@@ -20,2 +22,2 @@",
		"-before",
		"-before two",
		"+after",
		"+after two",
	}, "\n"))

	status := parseHunks(diff)
	want := map[int]LineStatus{
		0:  LineAdded,
		1:  LineAdded,
		10: LineDeletedBefore,
		21: LineModified,
		22: LineModified,
	}
	if len(status) != len(want) {
		t.Fatalf("got %+v, want %+v", status, want)
	}
	for ln, s := range want {
		if status[ln] != s {
			t.Errorf("line %d: got %v, want %v", ln, status[ln], s)
		}
	}

	// Every line the map marks must resolve back to a hunk.
	hunks := parseHunkList(diff)
	for ln := range want {
		if _, ok := HunkAtLine(hunks, ln); !ok {
			t.Errorf("line %d is marked in the status map but resolves to no hunk", ln)
		}
	}
}

func TestFileHunkAtReturnsTheChangeForALine(t *testing.T) {
	dir := newLineHunksTestRepo(t)
	writeAndCommit(t, dir, "f.txt", "a\nb\nc\n")

	path := filepath.Join(dir, "f.txt")
	if err := os.WriteFile(path, []byte("a\nCHANGED\nc\n"), 0o644); err != nil {
		t.Fatal(err)
	}

	h, ok, err := FileHunkAt(dir, path, 1) // 0-based: the second line
	if err != nil {
		t.Fatalf("FileHunkAt: %v", err)
	}
	if !ok {
		t.Fatal("expected the modified line to resolve to a hunk")
	}
	if len(h.Removed) != 1 || h.Removed[0] != "b" {
		t.Errorf("removed = %+v, want [b]", h.Removed)
	}
	if len(h.Added) != 1 || h.Added[0] != "CHANGED" {
		t.Errorf("added = %+v, want [CHANGED]", h.Added)
	}

	if _, ok, err := FileHunkAt(dir, path, 0); err != nil || ok {
		t.Errorf("unchanged line: ok = %v, err = %v, want false/nil", ok, err)
	}
}

func TestFileHunkAtUntrackedReportsErrUntracked(t *testing.T) {
	dir := newLineHunksTestRepo(t)
	writeAndCommit(t, dir, "committed.txt", "x\n")

	path := filepath.Join(dir, "new.txt")
	if err := os.WriteFile(path, []byte("fresh\n"), 0o644); err != nil {
		t.Fatal(err)
	}

	if _, _, err := FileHunkAt(dir, path, 0); !errors.Is(err, ErrUntracked) {
		t.Errorf("err = %v, want ErrUntracked", err)
	}
}

func TestFileDiffCleanFileReturnsNothing(t *testing.T) {
	dir := newLineHunksTestRepo(t)
	writeAndCommit(t, dir, "f.txt", "a\nb\n")

	lines, err := FileDiff(dir, filepath.Join(dir, "f.txt"))
	if err != nil {
		t.Fatalf("FileDiff: %v", err)
	}
	if len(lines) != 0 {
		t.Errorf("expected no diff lines for a clean file, got %+v", lines)
	}
}

func TestFileDiffShowsBothSidesWithContext(t *testing.T) {
	dir := newLineHunksTestRepo(t)
	writeAndCommit(t, dir, "f.txt", "one\ntwo\nthree\n")

	path := filepath.Join(dir, "f.txt")
	if err := os.WriteFile(path, []byte("one\nTWO\nthree\n"), 0o644); err != nil {
		t.Fatal(err)
	}

	lines, err := FileDiff(dir, path)
	if err != nil {
		t.Fatalf("FileDiff: %v", err)
	}
	joined := strings.Join(lines, "\n")
	for _, want := range []string{"@@", "-two", "+TWO", " one", " three"} {
		if !strings.Contains(joined, want) {
			t.Errorf("expected diff to contain %q, got:\n%s", want, joined)
		}
	}
	if strings.HasSuffix(joined, "\n") {
		t.Error("expected no trailing blank line")
	}
}

// A relative path means "relative to dir", the way it does for every git
// call in this package — not relative to the test binary's own working
// directory, which is where a bare os.ReadFile would look.
func TestUntrackedPathsAreResolvedAgainstDir(t *testing.T) {
	dir := newLineHunksTestRepo(t)
	writeAndCommit(t, dir, "committed.txt", "x\n")
	if err := os.WriteFile(filepath.Join(dir, "new.txt"), []byte("alpha\n"), 0o644); err != nil {
		t.Fatal(err)
	}

	lines, err := FileDiff(dir, "new.txt")
	if err != nil {
		t.Fatalf("FileDiff with a relative path: %v", err)
	}
	if !strings.Contains(strings.Join(lines, "\n"), "+alpha") {
		t.Errorf("got %+v", lines)
	}

	hunks, err := FileHunks(dir, "new.txt")
	if err != nil {
		t.Fatalf("FileHunks with a relative path: %v", err)
	}
	if hunks[0] != LineAdded {
		t.Errorf("got %+v, want line 0 added", hunks)
	}
}

func TestFileDiffUntrackedFileRendersAsAllAdded(t *testing.T) {
	dir := newLineHunksTestRepo(t)
	writeAndCommit(t, dir, "committed.txt", "x\n")

	path := filepath.Join(dir, "new.txt")
	if err := os.WriteFile(path, []byte("alpha\nbeta\n"), 0o644); err != nil {
		t.Fatal(err)
	}

	lines, err := FileDiff(dir, path)
	if err != nil {
		t.Fatalf("FileDiff: %v", err)
	}
	joined := strings.Join(lines, "\n")
	if !strings.Contains(joined, "@@ -0,0 +1,2 @@") {
		t.Errorf("expected a whole-file hunk header, got:\n%s", joined)
	}
	if !strings.Contains(joined, "+alpha") || !strings.Contains(joined, "+beta") {
		t.Errorf("expected every line marked added, got:\n%s", joined)
	}
}
