package gitstatus

import "testing"

func TestSummaryCleanReturnsEmpty(t *testing.T) {
	if got := Summary(map[string]Status{}); got != "" {
		t.Fatalf("got %q, want empty", got)
	}
}

func TestSummaryCountsByStatusOmittingZero(t *testing.T) {
	direct := map[string]Status{
		"a.txt": Added,
		"b.txt": Modified,
		"c.txt": Renamed, // rolls up into the same count as Modified
		"d.txt": Untracked,
		"e.txt": Untracked,
	}
	got := Summary(direct)
	want := "+1 ~2 ?2"
	if got != want {
		t.Fatalf("got %q, want %q", got, want)
	}
}

func TestSummaryConflictedLeadsAndDeletedCounted(t *testing.T) {
	direct := map[string]Status{
		"a.txt": Conflicted,
		"b.txt": Deleted,
	}
	got := Summary(direct)
	want := "!1 -1"
	if got != want {
		t.Fatalf("got %q, want %q", got, want)
	}
}
