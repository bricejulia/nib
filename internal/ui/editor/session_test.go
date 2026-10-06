package editor

import "testing"

func TestTabStatesRoundTripThroughRestoreTabs(t *testing.T) {
	sample := fixturePath(t, "editor_sample.txt")
	other := fixturePath(t, "no_trailing_newline.txt")

	src := NewView()
	src.Open(sample)
	src.activeTab().cursorLn = 2
	src.activeTab().cursorCol = 3
	src.activeTab().topLine = 1
	src.Open(other)
	src.Open(sample) // re-activate the first tab

	states, active := src.TabStates()
	if len(states) != 2 || active != 0 {
		t.Fatalf("expected 2 tabs with the first active, got %d tabs, active=%d", len(states), active)
	}

	dst := NewView()
	dst.RestoreTabs(states, active)

	got, gotActive := dst.TabStates()
	if gotActive != active {
		t.Errorf("active: got %d, want %d", gotActive, active)
	}
	if len(got) != len(states) {
		t.Fatalf("tabs: got %d, want %d", len(got), len(states))
	}
	for i := range states {
		if got[i] != states[i] {
			t.Errorf("tab %d: got %+v, want %+v", i, got[i], states[i])
		}
	}
}

func TestRestoreTabsClampsCursorToBuffer(t *testing.T) {
	v := NewView()
	v.RestoreTabs([]TabState{{Path: fixturePath(t, "editor_sample.txt"), Line: 500, Col: 500, TopLine: 900}}, 0)

	tab := v.activeTab()
	if tab == nil {
		t.Fatal("expected the restored tab to be active")
	}
	if tab.cursorLn != len(tab.buf.Lines)-1 {
		t.Errorf("cursorLn: got %d, want last line %d", tab.cursorLn, len(tab.buf.Lines)-1)
	}
	if tab.topLine > tab.cursorLn {
		t.Errorf("topLine %d must not be below the cursor line %d", tab.topLine, tab.cursorLn)
	}
}

func TestRestoreTabsOutOfRangeActiveFallsBackToFirst(t *testing.T) {
	v := NewView()
	v.RestoreTabs([]TabState{
		{Path: fixturePath(t, "editor_sample.txt")},
		{Path: fixturePath(t, "no_trailing_newline.txt")},
	}, 7)

	if v.ActiveIndex() != 0 {
		t.Errorf("expected the first tab to be active, got %d", v.ActiveIndex())
	}
}
