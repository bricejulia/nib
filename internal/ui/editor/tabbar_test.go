package editor

import (
	"os"
	"path/filepath"
	"testing"

	"github.com/bricejulia/nib/internal/layout"
)

// multiTabView builds a pane with one tab per path, sized so mouse-coordinate
// math has room to work with — the tab-bar-click counterpart to
// selectionView (selection_test.go), which only ever needs a single tab.
func multiTabView(paths ...string) *View {
	v := NewView()
	for _, p := range paths {
		v.tabs = append(v.tabs, &tab{path: p, buf: &Buffer{Lines: []string{""}}})
	}
	v.active = 0
	v.lastWidth, v.lastHeight = 80, 10
	return v
}

func tabPaths(tabs []*tab) []string {
	paths := make([]string, len(tabs))
	for i, t := range tabs {
		paths[i] = t.path
	}
	return paths
}

func middleClick(col, row int) layout.Mouse {
	return layout.Mouse{Col: col, Row: row, Button: layout.MouseMiddle, EventType: layout.EventPress, Clicks: 1}
}

func TestClickOnTabSwitchesActiveTab(t *testing.T) {
	v := multiTabView("a.go", "b.go", "c.go")
	spans := tabBarSpans(v.tabs)

	if !v.HandleMouse(press(spans[2].start+1, 0, 1)) {
		t.Fatal("a click on a tab should be consumed")
	}
	if v.active != 2 {
		t.Errorf("active = %d, want 2", v.active)
	}
}

func TestClickOnAlreadyActiveTabIsNoOpButStillConsumed(t *testing.T) {
	v := multiTabView("a.go", "b.go")
	spans := tabBarSpans(v.tabs)

	if !v.HandleMouse(press(spans[0].start+1, 0, 1)) {
		t.Fatal("a click on the active tab should still be consumed")
	}
	if v.active != 0 {
		t.Errorf("active = %d, want 0 (unchanged)", v.active)
	}
}

func TestClickOnSeparatorIsNotConsumed(t *testing.T) {
	v := multiTabView("a.go", "b.go")
	spans := tabBarSpans(v.tabs)
	// The single column right at spans[0].end is the "|" separator, not
	// part of either tab's span.
	if v.HandleMouse(press(spans[0].end, 0, 1)) {
		t.Error("a click on the separator between tabs should not be consumed")
	}
}

func TestRightClickOnTabOpensMenuWithoutSwitching(t *testing.T) {
	// See tabmenu_test.go for the menu's own behavior once open.
	v := multiTabView("a.go", "b.go")
	spans := tabBarSpans(v.tabs)
	m := layout.Mouse{Col: spans[1].start + 1, Row: 0, Button: layout.MouseRight, EventType: layout.EventPress, Clicks: 1}
	if !v.HandleMouse(m) {
		t.Error("a right-click on a tab should be consumed (it opens the context menu)")
	}
	if v.active != 0 {
		t.Errorf("active = %d, want 0 (right-click must not switch tabs)", v.active)
	}
	if v.tabMenu == nil {
		t.Fatal("expected the context menu to be open")
	}
	if v.tabMenu.target != 1 {
		t.Errorf("menu target = %d, want 1 (the tab actually clicked)", v.tabMenu.target)
	}
}

func TestDragReordersTabsLive(t *testing.T) {
	v := multiTabView("a.go", "b.go", "c.go")
	spans := tabBarSpans(v.tabs)

	// Press on a.go (index 0), drag to where c.go (index 2) currently sits.
	if !v.HandleMouse(press(spans[0].start+1, 0, 1)) {
		t.Fatal("press should be consumed")
	}
	destCol := spans[2].start + 1
	if !v.HandleMouse(motion(destCol, 0)) {
		t.Fatal("drag motion should be consumed")
	}

	got := tabPaths(v.tabs)
	want := []string{"b.go", "c.go", "a.go"}
	if got[0] != want[0] || got[1] != want[1] || got[2] != want[2] {
		t.Errorf("mid-drag order = %v, want %v", got, want)
	}
	if v.active != 2 {
		t.Errorf("active = %d, want 2 (the dragged tab's new position)", v.active)
	}

	if !v.HandleMouse(release(destCol, 0)) {
		t.Fatal("release should be consumed while a tab drag is in progress")
	}
	if v.tabDragging {
		t.Error("tabDragging should be cleared on release")
	}
	// Order survives the release — a tab drag commits live, not on release.
	got = tabPaths(v.tabs)
	if got[0] != want[0] || got[1] != want[1] || got[2] != want[2] {
		t.Errorf("post-release order = %v, want %v", got, want)
	}
}

func TestMiddleClickClosesCleanTab(t *testing.T) {
	v := multiTabView("a.go", "b.go")
	spans := tabBarSpans(v.tabs)

	if !v.HandleMouse(middleClick(spans[1].start+1, 0)) {
		t.Fatal("a middle-click on a tab should be consumed")
	}
	if got := tabPaths(v.tabs); len(got) != 1 || got[0] != "a.go" {
		t.Errorf("tabs = %v, want just [a.go]", got)
	}
}

func TestMiddleClickOnDirtyTabAsksForConfirmationInsteadOfClosing(t *testing.T) {
	v := multiTabView("a.go", "b.go")
	v.tabs[1].buf.Dirty = true

	var gotPaths []string
	var onDiscard func()
	v.OnRequestCloseDirtyTabs = func(paths []string, onSaveAll, discardAll func()) {
		gotPaths = paths
		onDiscard = discardAll
	}

	spans := tabBarSpans(v.tabs)
	if !v.HandleMouse(middleClick(spans[1].start+1, 0)) {
		t.Fatal("a middle-click on a dirty tab should still be consumed")
	}
	if len(gotPaths) != 1 || gotPaths[0] != "b.go" {
		t.Fatalf("OnRequestCloseDirtyTabs paths = %v, want [b.go]", gotPaths)
	}
	if len(v.tabs) != 2 {
		t.Fatal("the tab must not close until the caller confirms")
	}

	onDiscard()
	if got := tabPaths(v.tabs); len(got) != 1 || got[0] != "a.go" {
		t.Errorf("after discard, tabs = %v, want just [a.go]", got)
	}
}

func TestMiddleClickOnDirtyTabSaveCallbackSavesThenCloses(t *testing.T) {
	dir := t.TempDir()
	path := filepath.Join(dir, "b.go")
	if err := os.WriteFile(path, []byte("v1"), 0o644); err != nil {
		t.Fatal(err)
	}

	v := multiTabView("a.go")
	v.Open(path)
	v.tabs[1].buf.Lines = []string{"v2"}
	v.tabs[1].buf.Dirty = true

	var onSave func()
	v.OnRequestCloseDirtyTabs = func(_ []string, saveAll, _ func()) {
		onSave = saveAll
	}

	spans := tabBarSpans(v.tabs)
	v.HandleMouse(middleClick(spans[1].start+1, 0))
	if onSave == nil {
		t.Fatal("OnRequestCloseDirtyTabs was not called")
	}

	onSave()
	if len(v.tabs) != 1 || v.tabs[0].path != "a.go" {
		t.Fatalf("after save, tabs = %v, want just [a.go]", tabPaths(v.tabs))
	}
	data, err := os.ReadFile(path)
	if err != nil {
		t.Fatal(err)
	}
	if string(data) != "v2" {
		t.Errorf("file content = %q, want %q (save should have written it before closing)", data, "v2")
	}
}

func TestMiddleClickOnDirtyTabWithNoHandlerRefusesSilently(t *testing.T) {
	// No OnRequestCloseDirtyTabs wired — e.g. a bare NewView(), same as every
	// other test in this package. Matches ":q" on a dirty buffer: refuse,
	// don't discard silently.
	v := multiTabView("a.go")
	v.tabs[0].buf.Dirty = true
	spans := tabBarSpans(v.tabs)

	v.HandleMouse(middleClick(spans[0].start+1, 0))
	if len(v.tabs) != 1 {
		t.Errorf("tabs = %v, want the dirty tab left open", tabPaths(v.tabs))
	}
}
