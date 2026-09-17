package editor

import "testing"

func TestTransferTabToMovesTabAndActivatesItInDest(t *testing.T) {
	src := multiTabView("a.go", "b.go")
	dst := multiTabView("x.go")

	src.TransferTabTo(1, dst) // move b.go out of src

	if got := tabPaths(src.tabs); len(got) != 1 || got[0] != "a.go" {
		t.Fatalf("src tabs = %v, want [a.go]", got)
	}
	if got := tabPaths(dst.tabs); len(got) != 2 || got[0] != "x.go" || got[1] != "b.go" {
		t.Fatalf("dst tabs = %v, want [x.go b.go]", got)
	}
	if dst.active != 1 {
		t.Errorf("dst.active = %d, want 1 (the moved tab, appended and activated)", dst.active)
	}
}

func TestTransferTabToActivatesExistingTabInsteadOfDuplicating(t *testing.T) {
	src := multiTabView("a.go", "shared.go")
	dst := multiTabView("shared.go", "y.go")

	src.TransferTabTo(1, dst) // "shared.go" is already open in dst

	if got := tabPaths(src.tabs); len(got) != 1 || got[0] != "a.go" {
		t.Fatalf("src tabs = %v, want [a.go] (its copy should close)", got)
	}
	if got := tabPaths(dst.tabs); len(got) != 2 || got[0] != "shared.go" || got[1] != "y.go" {
		t.Fatalf("dst tabs = %v, want unchanged [shared.go y.go] (no duplicate)", got)
	}
	if dst.active != 0 {
		t.Errorf("dst.active = %d, want 0 (dst's own existing shared.go tab, now activated)", dst.active)
	}
}

func TestTransferTabToOnlyTabLeavesSourcePaneOpenAndEmpty(t *testing.T) {
	src := multiTabView("a.go")
	dst := multiTabView("x.go")
	var emptied bool
	src.OnAllTabsClosed = func() { emptied = true }

	src.TransferTabTo(0, dst)

	if len(src.tabs) != 0 {
		t.Errorf("src tabs = %v, want none", tabPaths(src.tabs))
	}
	if !emptied {
		t.Error("OnAllTabsClosed should fire, same as any other path to zero tabs")
	}
	if got := tabPaths(dst.tabs); len(got) != 2 || got[1] != "a.go" {
		t.Fatalf("dst tabs = %v, want [x.go a.go]", got)
	}
}

func TestTransferTabToIgnoresOutOfRangeOrNilOrSelf(t *testing.T) {
	v := multiTabView("a.go", "b.go")
	other := multiTabView("x.go")

	v.TransferTabTo(-1, other)
	v.TransferTabTo(5, other)
	v.TransferTabTo(0, nil)
	v.TransferTabTo(0, v)

	if got := tabPaths(v.tabs); len(got) != 2 || got[0] != "a.go" || got[1] != "b.go" {
		t.Errorf("tabs = %v, want unchanged [a.go b.go]", got)
	}
	if got := tabPaths(other.tabs); len(got) != 1 || got[0] != "x.go" {
		t.Errorf("other tabs = %v, want unchanged [x.go]", got)
	}
}
