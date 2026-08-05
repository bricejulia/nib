package filetree

import (
	"testing"

	"github.com/bricejulia/nib/internal/vcs/gitstatus"
)

func TestBuildChangesTreeEmpty(t *testing.T) {
	root := buildChangesTree("/proj", map[string]gitstatus.Status{})
	if root.Name != "proj" || !root.IsDir || len(root.Children) != 0 {
		t.Fatalf("empty direct map should yield a childless root, got %+v", root)
	}
}

func TestBuildChangesTreePrunesToOnlyDirtyPaths(t *testing.T) {
	direct := map[string]gitstatus.Status{
		"a.txt":         gitstatus.Modified,
		"sub/dirty.txt": gitstatus.Untracked,
		// "sub/clean.txt" is deliberately absent: a directory with no dirty
		// descendants (or a clean file inside a dirty one) must not appear.
	}
	root := buildChangesTree("/proj", direct)

	if len(root.Children) != 2 {
		t.Fatalf("want 2 top-level entries (a.txt, sub), got %d: %+v", len(root.Children), root.Children)
	}

	a := root.child("a.txt")
	if a == nil || a.IsDir || a.Status != gitstatus.Modified {
		t.Fatalf("a.txt: want a Modified file node, got %+v", a)
	}
	if a.Parent != root {
		t.Fatal("a.txt: Parent should be root")
	}

	sub := root.child("sub")
	if sub == nil || !sub.IsDir || !sub.Loaded || !sub.Expanded {
		t.Fatalf("sub: want a loaded, expanded directory node, got %+v", sub)
	}
	// The directory rolls up to its dirtiest descendant's status.
	if sub.Status != gitstatus.Untracked {
		t.Fatalf("sub: want rolled-up status Untracked, got %v", sub.Status)
	}
	if len(sub.Children) != 1 {
		t.Fatalf("sub: want exactly the one dirty child, got %+v", sub.Children)
	}
	dirty := sub.child("dirty.txt")
	if dirty == nil || dirty.Status != gitstatus.Untracked || dirty.Parent != sub {
		t.Fatalf("sub/dirty.txt: want an Untracked leaf parented to sub, got %+v", dirty)
	}
}

func TestBuildChangesTreeIncludesDeletedFiles(t *testing.T) {
	// A deleted-but-tracked file has nothing left on disk to os.Stat — this
	// is exactly the case the disk-backed ModeFiles tree can't show, and
	// the reason buildChangesTree works from git's reported paths alone.
	root := buildChangesTree("/proj", map[string]gitstatus.Status{
		"gone.txt": gitstatus.Deleted,
	})
	gone := root.child("gone.txt")
	if gone == nil || gone.Status != gitstatus.Deleted {
		t.Fatalf("want a Deleted leaf for gone.txt, got %+v", gone)
	}
}

func TestBuildChangesTreeSortsDirsFirstThenAlphabetically(t *testing.T) {
	root := buildChangesTree("/proj", map[string]gitstatus.Status{
		"z.txt":       gitstatus.Modified,
		"a_dir/f.txt": gitstatus.Modified,
		"a.txt":       gitstatus.Modified,
	})
	if len(root.Children) != 3 {
		t.Fatalf("want 3 top-level entries, got %d", len(root.Children))
	}
	if !root.Children[0].IsDir || root.Children[0].Name != "a_dir" {
		t.Fatalf("want a_dir first (dirs before files), got %+v", root.Children[0])
	}
	if root.Children[1].Name != "a.txt" || root.Children[2].Name != "z.txt" {
		t.Fatalf("want files alphabetical after dirs, got [%s, %s]", root.Children[1].Name, root.Children[2].Name)
	}
}

func TestBuildChangesTreeSharesDirectoryAcrossMultipleDirtyFiles(t *testing.T) {
	root := buildChangesTree("/proj", map[string]gitstatus.Status{
		"sub/one.txt": gitstatus.Modified,
		"sub/two.txt": gitstatus.Added,
	})
	if len(root.Children) != 1 {
		t.Fatalf("want a single shared sub directory, got %+v", root.Children)
	}
	sub := root.Children[0]
	if len(sub.Children) != 2 {
		t.Fatalf("want both dirty files under the one sub node, got %+v", sub.Children)
	}
}
