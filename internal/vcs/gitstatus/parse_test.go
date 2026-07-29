package gitstatus

import "testing"

// realPorcelainV2Fixture was captured from a real `git status --porcelain=v2
// -z --untracked-files=all` run covering: a staged-added file (.gitignore),
// a staged rename (src/main.go -> src/renamed.go), a staged-added file
// (staged.txt), a modified-in-worktree tracked file (tracked.txt), and two
// untracked entries (a loose file and a file inside a new untracked
// directory).
const realPorcelainV2Fixture = "1 A. N... 000000 100644 100644 0000000000000000000000000000000000000000 48b8bf9072d8716346ec810e5a1808305c97d50f .gitignore\x00" +
	"2 R. N... 100644 100644 100644 c6d5f5462d9d70f2b02d1bbdd672da30c0209fb1 c6d5f5462d9d70f2b02d1bbdd672da30c0209fb1 R100 src/renamed.go\x00" +
	"src/main.go\x00" +
	"1 A. N... 000000 100644 100644 0000000000000000000000000000000000000000 3e757656cf36eca53338e520d134963a44f793f8 staged.txt\x00" +
	"1 .M N... 100644 100644 100644 2d00bd505971a8bc7318d98e003aee708a367c85 2d00bd505971a8bc7318d98e003aee708a367c85 tracked.txt\x00" +
	"? loose.txt\x00" +
	"? newdir/a.txt\x00"

func TestParsePorcelainV2RealFixture(t *testing.T) {
	got, err := parsePorcelainV2([]byte(realPorcelainV2Fixture))
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}

	want := map[string]Status{
		".gitignore":     Added,
		"src/renamed.go": Renamed,
		"staged.txt":     Added,
		"tracked.txt":    Modified,
		"loose.txt":      Untracked,
		"newdir/a.txt":   Untracked,
	}

	if len(got) != len(want) {
		t.Fatalf("got %d entries, want %d: %+v", len(got), len(want), got)
	}
	for path, wantStatus := range want {
		gotStatus, ok := got[path]
		if !ok {
			t.Errorf("missing entry for %q", path)
			continue
		}
		if gotStatus != wantStatus {
			t.Errorf("%q: got status %v, want %v", path, gotStatus, wantStatus)
		}
	}

	// The rename record's origPath chunk (src/main.go) must be consumed,
	// not treated as its own untracked-looking record.
	if _, ok := got["src/main.go"]; ok {
		t.Errorf("origPath chunk %q leaked into the result as its own entry", "src/main.go")
	}
}

func TestParsePorcelainV2Empty(t *testing.T) {
	got, err := parsePorcelainV2([]byte(""))
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}
	if len(got) != 0 {
		t.Fatalf("expected empty map, got %+v", got)
	}
}

func TestParsePorcelainV2ConflictedEntry(t *testing.T) {
	// u XY sub m1 m2 m3 mW h1 h2 h3 path
	raw := "u UU N... 100644 100644 100644 100644 " +
		"aaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaa " +
		"bbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbb " +
		"cccccccccccccccccccccccccccccccccccccccc conflict.txt\x00"

	got, err := parsePorcelainV2([]byte(raw))
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}
	if got["conflict.txt"] != Conflicted {
		t.Errorf("got %v, want Conflicted", got["conflict.txt"])
	}
}

func TestParsePorcelainV2DeletedEntry(t *testing.T) {
	raw := "1 D. N... 100644 000000 000000 " +
		"aaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaa " +
		"0000000000000000000000000000000000000000 gone.txt\x00"

	got, err := parsePorcelainV2([]byte(raw))
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}
	if got["gone.txt"] != Deleted {
		t.Errorf("got %v, want Deleted", got["gone.txt"])
	}
}
