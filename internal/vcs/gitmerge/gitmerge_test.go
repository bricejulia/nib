package gitmerge

import (
	"os"
	"os/exec"
	"path/filepath"
	"slices"
	"strings"
	"testing"
)

// conflictedRepo returns a repo mid-merge with f.txt conflicted (default
// "merge" conflict style, so no base section) and ok.txt merged cleanly.
func conflictedRepo(t *testing.T) string {
	t.Helper()
	dir := t.TempDir()
	run := func(args ...string) {
		t.Helper()
		cmd := exec.Command("git", args...)
		cmd.Dir = dir
		cmd.Env = append(os.Environ(), "GIT_CONFIG_GLOBAL=/dev/null", "GIT_CONFIG_NOSYSTEM=1")
		if out, err := cmd.CombinedOutput(); err != nil && args[0] != "merge" {
			t.Fatalf("git %v: %v\n%s", args, err, out)
		}
	}
	write := func(name, content string) {
		t.Helper()
		if err := os.WriteFile(filepath.Join(dir, name), []byte(content), 0o644); err != nil {
			t.Fatal(err)
		}
	}
	run("init", "-q", "-b", "main")
	run("config", "user.email", "test@example.com")
	run("config", "user.name", "test")
	run("config", "merge.conflictStyle", "merge")
	write("f.txt", "base\n")
	write("ok.txt", "x\n")
	run("add", ".")
	run("commit", "-q", "-m", "base")
	run("checkout", "-q", "-b", "topic")
	write("f.txt", "topic\n")
	run("commit", "-qam", "topic")
	run("checkout", "-q", "main")
	write("f.txt", "main\n")
	write("ok.txt", "y\n")
	run("commit", "-qam", "main")
	run("merge", "-q", "topic")
	return dir
}

func TestConflictedAndStageRoundTrip(t *testing.T) {
	dir := conflictedRepo(t)
	got, err := Conflicted(dir)
	if err != nil || !slices.Equal(got, []string{"f.txt"}) {
		t.Fatalf("Conflicted = %q, %v", got, err)
	}
	if c, _ := IsConflicted(dir, "ok.txt"); c {
		t.Error("ok.txt reported conflicted")
	}

	if err := os.WriteFile(filepath.Join(dir, "f.txt"), []byte("resolved\n"), 0o644); err != nil {
		t.Fatal(err)
	}
	if err := Stage(dir, "f.txt"); err != nil {
		t.Fatal(err)
	}
	if c, _ := IsConflicted(dir, "f.txt"); c {
		t.Fatal("still conflicted after Stage")
	}
	if err := Unresolve(dir, "f.txt"); err != nil {
		t.Fatal(err)
	}
	if c, _ := IsConflicted(dir, "f.txt"); !c {
		t.Fatal("not conflicted after Unresolve")
	}
	data, _ := os.ReadFile(filepath.Join(dir, "f.txt"))
	if string(data) != "resolved\n" {
		t.Errorf("Unresolve touched the file: %q", data)
	}
}

func TestRegenerateWithBaseAddsBaseSection(t *testing.T) {
	dir := conflictedRepo(t)
	if err := RegenerateWithBase(dir, "f.txt"); err != nil {
		t.Fatal(err)
	}
	data, _ := os.ReadFile(filepath.Join(dir, "f.txt"))
	if !strings.Contains(string(data), "|||||||") || !strings.Contains(string(data), "base") {
		t.Errorf("no base section:\n%s", data)
	}
}
