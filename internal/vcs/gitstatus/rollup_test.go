package gitstatus

import "testing"

func TestRollupMarksAncestorDirectories(t *testing.T) {
	direct := map[string]Status{
		"src/deep/nested/file.go": Modified,
	}
	got := Rollup(direct)

	for _, dir := range []string{"src", "src/deep", "src/deep/nested"} {
		if got[dir] != Modified {
			t.Errorf("expected %q rolled up to Modified, got %v", dir, got[dir])
		}
	}
	if got["src/deep/nested/file.go"] != Modified {
		t.Errorf("direct entry should be preserved")
	}
}

func TestRollupPrecedencePicksMostNotable(t *testing.T) {
	direct := map[string]Status{
		"dir/a.txt": Untracked,
		"dir/b.txt": Conflicted,
	}
	got := Rollup(direct)
	if got["dir"] != Conflicted {
		t.Errorf("expected dir rolled up to Conflicted (higher precedence), got %v", got["dir"])
	}
}

func TestRollupDoesNotTouchUnrelatedDirectories(t *testing.T) {
	direct := map[string]Status{
		"a/file.txt": Modified,
	}
	got := Rollup(direct)
	if _, ok := got["b"]; ok {
		t.Errorf("unrelated directory should not appear in rollup")
	}
}

func TestRollupEmptyInput(t *testing.T) {
	got := Rollup(map[string]Status{})
	if len(got) != 0 {
		t.Fatalf("expected empty map, got %+v", got)
	}
}

func TestRollupTopLevelFile(t *testing.T) {
	// A file with no parent directory (path.Dir returns ".") must not
	// produce a bogus "." entry.
	direct := map[string]Status{"root.txt": Modified}
	got := Rollup(direct)
	if _, ok := got["."]; ok {
		t.Errorf(`unexpected "." entry in rollup for a top-level file`)
	}
}
