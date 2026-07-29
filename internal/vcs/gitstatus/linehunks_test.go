package gitstatus

import (
	"os"
	"os/exec"
	"path/filepath"
	"testing"
)

func newLineHunksTestRepo(t *testing.T) string {
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
	run("config", "user.email", "test@example.com")
	run("config", "user.name", "test")
	return dir
}

func writeAndCommit(t *testing.T, dir, name, content string) {
	t.Helper()
	if err := os.WriteFile(filepath.Join(dir, name), []byte(content), 0o644); err != nil {
		t.Fatal(err)
	}
	cmd := exec.Command("git", "add", name)
	cmd.Dir = dir
	if out, err := cmd.CombinedOutput(); err != nil {
		t.Fatalf("git add: %v\n%s", err, out)
	}
	cmd = exec.Command("git", "commit", "-q", "-m", "commit "+name)
	cmd.Dir = dir
	if out, err := cmd.CombinedOutput(); err != nil {
		t.Fatalf("git commit: %v\n%s", err, out)
	}
}

func TestFileHunksCleanFileReturnsEmpty(t *testing.T) {
	dir := newLineHunksTestRepo(t)
	writeAndCommit(t, dir, "f.txt", "a\nb\nc\n")

	got, err := FileHunks(dir, filepath.Join(dir, "f.txt"))
	if err != nil {
		t.Fatalf("FileHunks: %v", err)
	}
	if len(got) != 0 {
		t.Fatalf("expected no hunks for a clean file, got %+v", got)
	}
}

func TestFileHunksUntrackedFileMarksEveryLineAdded(t *testing.T) {
	dir := newLineHunksTestRepo(t)
	writeAndCommit(t, dir, "committed.txt", "x\n")

	path := filepath.Join(dir, "untracked.txt")
	if err := os.WriteFile(path, []byte("p\nq\nr\n"), 0o644); err != nil {
		t.Fatal(err)
	}

	got, err := FileHunks(dir, path)
	if err != nil {
		t.Fatalf("FileHunks: %v", err)
	}
	want := map[int]LineStatus{0: LineAdded, 1: LineAdded, 2: LineAdded}
	if len(got) != len(want) {
		t.Fatalf("got %+v, want %+v", got, want)
	}
	for i, s := range want {
		if got[i] != s {
			t.Errorf("line %d: got %v, want %v", i, got[i], s)
		}
	}
}

func TestFileHunksStagedNewFileMarksEveryLineAdded(t *testing.T) {
	dir := newLineHunksTestRepo(t)
	writeAndCommit(t, dir, "committed.txt", "x\n")

	path := filepath.Join(dir, "staged.txt")
	if err := os.WriteFile(path, []byte("p\nq\n"), 0o644); err != nil {
		t.Fatal(err)
	}
	cmd := exec.Command("git", "add", "staged.txt")
	cmd.Dir = dir
	if out, err := cmd.CombinedOutput(); err != nil {
		t.Fatalf("git add: %v\n%s", err, out)
	}

	got, err := FileHunks(dir, path)
	if err != nil {
		t.Fatalf("FileHunks: %v", err)
	}
	if got[0] != LineAdded || got[1] != LineAdded {
		t.Fatalf("got %+v, want every line LineAdded", got)
	}
}

func TestFileHunksModifiedLineIsMarkedModified(t *testing.T) {
	dir := newLineHunksTestRepo(t)
	writeAndCommit(t, dir, "f.txt", "a\nb\nc\n")

	path := filepath.Join(dir, "f.txt")
	if err := os.WriteFile(path, []byte("a\nB\nc\n"), 0o644); err != nil {
		t.Fatal(err)
	}

	got, err := FileHunks(dir, path)
	if err != nil {
		t.Fatalf("FileHunks: %v", err)
	}
	want := map[int]LineStatus{1: LineModified}
	if len(got) != len(want) || got[1] != LineModified {
		t.Fatalf("got %+v, want %+v", got, want)
	}
}

func TestFileHunksAddedLinesAreMarkedAdded(t *testing.T) {
	dir := newLineHunksTestRepo(t)
	writeAndCommit(t, dir, "f.txt", "a\nb\n")

	path := filepath.Join(dir, "f.txt")
	if err := os.WriteFile(path, []byte("a\nb\nc\nd\n"), 0o644); err != nil {
		t.Fatal(err)
	}

	got, err := FileHunks(dir, path)
	if err != nil {
		t.Fatalf("FileHunks: %v", err)
	}
	want := map[int]LineStatus{2: LineAdded, 3: LineAdded}
	if len(got) != len(want) {
		t.Fatalf("got %+v, want %+v", got, want)
	}
	for i, s := range want {
		if got[i] != s {
			t.Errorf("line %d: got %v, want %v", i, got[i], s)
		}
	}
}

func TestFileHunksDeletedLineMarksFollowingLine(t *testing.T) {
	dir := newLineHunksTestRepo(t)
	writeAndCommit(t, dir, "f.txt", "a\nb\nc\n")

	path := filepath.Join(dir, "f.txt")
	if err := os.WriteFile(path, []byte("a\nc\n"), 0o644); err != nil {
		t.Fatal(err)
	}

	got, err := FileHunks(dir, path)
	if err != nil {
		t.Fatalf("FileHunks: %v", err)
	}
	// "b" (old line 2) was deleted; the new file's line 2 ("c", 0-based
	// index 1) is what the deletion now sits immediately above.
	want := map[int]LineStatus{1: LineDeletedBefore}
	if len(got) != len(want) || got[1] != LineDeletedBefore {
		t.Fatalf("got %+v, want %+v", got, want)
	}
}

func TestFileHunksDeletionAtStartMarksFirstLine(t *testing.T) {
	dir := newLineHunksTestRepo(t)
	writeAndCommit(t, dir, "f.txt", "a\nb\n")

	path := filepath.Join(dir, "f.txt")
	if err := os.WriteFile(path, []byte("b\n"), 0o644); err != nil {
		t.Fatal(err)
	}

	got, err := FileHunks(dir, path)
	if err != nil {
		t.Fatalf("FileHunks: %v", err)
	}
	if got[0] != LineDeletedBefore {
		t.Fatalf("got %+v, want line 0 LineDeletedBefore", got)
	}
}

func TestFileHunksNonRepoErrors(t *testing.T) {
	dir := t.TempDir()
	path := filepath.Join(dir, "f.txt")
	if err := os.WriteFile(path, []byte("a\n"), 0o644); err != nil {
		t.Fatal(err)
	}
	if _, err := FileHunks(dir, path); err == nil {
		t.Fatal("expected an error outside a git repo")
	}
}
