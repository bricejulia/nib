package editor

import (
	"testing"

	"github.com/bricejulia/nib/internal/layout"
)

// manyTabsView builds a pane with enough tabs that they don't all fit in
// narrowCols, so tab-bar scrolling actually has something to do.
func manyTabsView(n int) *View {
	paths := make([]string, n)
	for i := range paths {
		paths[i] = string(rune('a'+i)) + ".go"
	}
	v := multiTabView(paths...)
	v.lastWidth, v.lastHeight = narrowCols, 10
	return v
}

const narrowCols = 20 // fits only 2-3 of manyTabsView's short tab names at once

func TestActiveTabAutoScrollsIntoView(t *testing.T) {
	v := manyTabsView(10)
	w := newFakeWindow(narrowCols, 10)
	v.Render(w) // active tab (index 0) starts in view; establishes the baseline

	v.active = 9 // jump straight to the last tab, e.g. via repeated NextTab
	v.Render(w)

	spans := tabBarSpans(v.tabs)
	active := spans[9]
	if active.start < v.tabScrollLeft || active.end > v.tabScrollLeft+narrowCols {
		t.Errorf("active tab span %v not fully inside visible window [%d, %d)",
			active, v.tabScrollLeft, v.tabScrollLeft+narrowCols)
	}
}

func TestTabBarSegmentsShowScrollIndicatorsWhenClipped(t *testing.T) {
	v := manyTabsView(10)
	v.active = 5
	w := newFakeWindow(narrowCols, 10)
	v.Render(w)

	row := w.lines[0]
	if v.tabScrollLeft > 0 && !containsRune(row, '‹') {
		t.Errorf("row 0 = %q, expected a ‹ indicator since content is hidden to the left", row)
	}
	spans := tabBarSpans(v.tabs)
	total := spans[len(spans)-1].end
	if total > v.tabScrollLeft+narrowCols && !containsRune(row, '›') {
		t.Errorf("row 0 = %q, expected a › indicator since content is hidden to the right", row)
	}
}

func containsRune(s string, r rune) bool {
	for _, c := range s {
		if c == r {
			return true
		}
	}
	return false
}

func TestWheelOverTabBarScrollsByOneTab(t *testing.T) {
	v := manyTabsView(10)
	w := newFakeWindow(narrowCols, 10)
	v.Render(w)
	before := v.tabScrollLeft

	wheelDown := layout.Mouse{Col: 5, Row: 0, Button: layout.MouseWheelDown, EventType: layout.EventPress}
	if !v.HandleMouse(wheelDown) {
		t.Fatal("a wheel event over the tab bar should be consumed")
	}
	if v.tabScrollLeft <= before {
		t.Errorf("tabScrollLeft = %d, want it to have increased from %d", v.tabScrollLeft, before)
	}
	spans := tabBarSpans(v.tabs)
	found := false
	for _, sp := range spans {
		if sp.start == v.tabScrollLeft {
			found = true
			break
		}
	}
	if !found {
		t.Errorf("tabScrollLeft = %d lands mid-tab, want it to land exactly on a tab's start (whole-tab granularity)", v.tabScrollLeft)
	}

	afterDown := v.tabScrollLeft
	wheelUp := layout.Mouse{Col: 5, Row: 0, Button: layout.MouseWheelUp, EventType: layout.EventPress}
	if !v.HandleMouse(wheelUp) {
		t.Fatal("a wheel event over the tab bar should be consumed")
	}
	if v.tabScrollLeft >= afterDown {
		t.Errorf("tabScrollLeft = %d, want it to have decreased from %d", v.tabScrollLeft, afterDown)
	}
}

func TestWheelOverBodyStillScrollsText(t *testing.T) {
	// Regression guard: only row 0 gets the new tab-bar wheel behavior —
	// everywhere else, the wheel still bubbles to App's Up/Down-key
	// translation (see isWheelButton's doc comment).
	v := manyTabsView(3)
	m := layout.Mouse{Col: 5, Row: 3, Button: layout.MouseWheelDown, EventType: layout.EventPress}
	if v.HandleMouse(m) {
		t.Error("a wheel event over the file body should still be left to App")
	}
}

func TestDragPastVisibleEdgeScrollsTabBarToRevealMore(t *testing.T) {
	v := manyTabsView(10)
	v.active = 5
	w := newFakeWindow(narrowCols, 10)
	v.Render(w) // scrolls so tab 5 is in view, hiding tabs before it

	scrollBefore := v.tabScrollLeft
	if scrollBefore == 0 {
		t.Fatal("test setup: expected some tabs already scrolled out of view on the left")
	}

	// Press on the active tab (guaranteed visible after Render), converting
	// its content-space span back into rendered-column space via the same
	// translation HandleMouse itself reverses (see tabBarContentColumn) —
	// press()/motion() work in on-screen column space, not content space.
	hasLeft, _, _ := tabBarScrollFlags(v.tabs, v.tabScrollLeft, narrowCols)
	spans := tabBarSpans(v.tabs)
	renderedCol := spans[5].start + 1 - v.tabScrollLeft
	if hasLeft {
		renderedCol++
	}

	v.HandleMouse(press(renderedCol, 0, 1))
	v.HandleMouse(motion(0, 0)) // drag to the left edge

	if v.tabScrollLeft >= scrollBefore {
		t.Errorf("tabScrollLeft = %d, want less than %d (dragging to the left edge should reveal more)", v.tabScrollLeft, scrollBefore)
	}
}
