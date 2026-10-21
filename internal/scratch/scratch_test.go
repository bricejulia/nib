package scratch

import (
	"os"
	"path/filepath"
	"testing"
	"time"
)

func TestCreateBlankNamePicksFirstFreeNumber(t *testing.T) {
	root := t.TempDir()
	first, err := Create(root, "")
	if err != nil {
		t.Fatal(err)
	}
	if filepath.Base(first) != "scratch-1.txt" {
		t.Fatalf("first = %q, want scratch-1.txt", first)
	}
	second, err := Create(root, "  ")
	if err != nil {
		t.Fatal(err)
	}
	if filepath.Base(second) != "scratch-2.txt" {
		t.Fatalf("second = %q, want scratch-2.txt", second)
	}
	if err := os.Remove(first); err != nil {
		t.Fatal(err)
	}
	third, err := Create(root, "")
	if err != nil {
		t.Fatal(err)
	}
	if filepath.Base(third) != "scratch-1.txt" {
		t.Fatalf("third = %q, want the freed scratch-1.txt", third)
	}
}

func TestCreateNamedReusesExistingFile(t *testing.T) {
	root := t.TempDir()
	path, err := Create(root, "q.sql")
	if err != nil {
		t.Fatal(err)
	}
	if path != filepath.Join(root, ".nib", "scratch", "q.sql") {
		t.Fatalf("path = %q", path)
	}
	if err := os.WriteFile(path, []byte("select 1"), 0o644); err != nil {
		t.Fatal(err)
	}
	again, err := Create(root, "q.sql")
	if err != nil {
		t.Fatal(err)
	}
	if again != path {
		t.Fatalf("again = %q, want %q", again, path)
	}
	if data, _ := os.ReadFile(path); string(data) != "select 1" {
		t.Fatalf("existing content was clobbered: %q", data)
	}
}

func TestCreateRejectsPaths(t *testing.T) {
	root := t.TempDir()
	for _, name := range []string{"../x", "a/b", `a\b`, ".", ".."} {
		if _, err := Create(root, name); err == nil {
			t.Errorf("Create(%q) succeeded, want an error", name)
		}
	}
}

func TestListMissingDir(t *testing.T) {
	entries, err := List(t.TempDir())
	if err != nil || entries != nil {
		t.Fatalf("List = %v, %v; want nil, nil", entries, err)
	}
}

func TestListNewestFirstSkipsDirs(t *testing.T) {
	root := t.TempDir()
	old, _ := Create(root, "old.md")
	recent, _ := Create(root, "new.md")
	now := time.Now()
	if err := os.Chtimes(old, now, now.Add(-time.Hour)); err != nil {
		t.Fatal(err)
	}
	if err := os.Chtimes(recent, now, now); err != nil {
		t.Fatal(err)
	}
	if err := os.Mkdir(filepath.Join(Dir(root), "sub"), 0o755); err != nil {
		t.Fatal(err)
	}
	entries, err := List(root)
	if err != nil {
		t.Fatal(err)
	}
	if len(entries) != 2 || entries[0].Name != "new.md" || entries[1].Name != "old.md" {
		t.Fatalf("entries = %+v", entries)
	}
}
