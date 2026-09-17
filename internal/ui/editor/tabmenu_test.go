package editor

import (
	"os"
	"path/filepath"
	"testing"

	"github.com/bricejulia/nib/internal/layout"
)

func rightClick(col, row int) layout.Mouse {
	return layout.Mouse{Col: col, Row: row, Button: layout.MouseRight, EventType: layout.EventPress, Clicks: 1}
}

func TestTabMenuKeyboardNavigationAndActivation(t *testing.T) {
	v := multiTabView("a.go", "b.go", "c.go")
	spans := tabBarSpans(v.tabs)
	v.HandleMouse(rightClick(spans[2].start+1, 0)) // menu for c.go (index 2), not the active tab

	if v.tabMenu.selected != 0 {
		t.Fatalf("selected = %d, want 0 on open", v.tabMenu.selected)
	}
	v.HandleKey(layout.Key{Named: layout.KeyDown})
	if v.tabMenu.selected != 1 {
		t.Fatalf("selected = %d, want 1 after one Down", v.tabMenu.selected)
	}
	v.HandleKey(layout.Key{Named: layout.KeyUp})
	if v.tabMenu.selected != 0 {
		t.Fatalf("selected = %d, want 0 after Up", v.tabMenu.selected)
	}

	// Enter on item 0 ("Close") should close tabs[2] (c.go), the tab the
	// menu was opened for — not v.active.
	v.HandleKey(layout.Key{Named: layout.KeyEnter})
	if v.tabMenu != nil {
		t.Error("menu should close after activating an item")
	}
	if got := tabPaths(v.tabs); len(got) != 2 || got[0] != "a.go" || got[1] != "b.go" {
		t.Errorf("tabs = %v, want [a.go b.go]", got)
	}
}

func TestTabMenuEscDismissesWithoutActing(t *testing.T) {
	v := multiTabView("a.go", "b.go")
	spans := tabBarSpans(v.tabs)
	v.HandleMouse(rightClick(spans[0].start+1, 0))

	v.HandleKey(layout.Key{Named: layout.KeyEsc})
	if v.tabMenu != nil {
		t.Error("Esc should dismiss the menu")
	}
	if len(v.tabs) != 2 {
		t.Error("Esc must not close anything")
	}
}

func TestTabMenuUnrecognizedKeyDismissesAndFallsThrough(t *testing.T) {
	v := multiTabView("a.go", "b.go")
	spans := tabBarSpans(v.tabs)
	v.HandleMouse(rightClick(spans[0].start+1, 0))

	// "]" isn't a menu key (move_up/move_down/insert_newline/normal_mode) —
	// it should dismiss the menu AND still run as next_tab afterward.
	v.HandleKey(layout.Key{Text: "]"})
	if v.tabMenu != nil {
		t.Error("an unrecognized key should dismiss the menu")
	}
	if v.active != 1 {
		t.Errorf("active = %d, want 1 (\"]\" should still fall through to next_tab)", v.active)
	}
}

func TestTabMenuHoverHighlightsRowWithoutActivating(t *testing.T) {
	v := multiTabView("a.go", "b.go")
	spans := tabBarSpans(v.tabs)
	v.HandleMouse(rightClick(spans[0].start+1, 0))
	v.Render(newFakeWindow(80, 10)) // populates tabMenu.rect, needed to hit-test hover against it

	menu := v.tabMenu
	hoverRow := menu.rect.row + 2 // "Close All"
	hoverCol := menu.rect.col + 1
	if !v.HandleMouse(layout.Mouse{Col: hoverCol, Row: hoverRow, Button: layout.MouseNone, EventType: layout.EventMotion}) {
		t.Fatal("hovering the menu should be consumed")
	}
	if v.tabMenu == nil {
		t.Fatal("hovering must not activate anything or close the menu")
	}
	if v.tabMenu.selected != 2 {
		t.Errorf("selected = %d, want 2 (the hovered row)", v.tabMenu.selected)
	}
	if len(v.tabs) != 2 {
		t.Error("hovering must not close any tab")
	}

	// Move off the menu entirely: selection should stay put (no highlight
	// for "nothing"), and the menu should stay open — only a click or key
	// dismisses/activates it.
	v.HandleMouse(layout.Mouse{Col: 0, Row: 0, Button: layout.MouseNone, EventType: layout.EventMotion})
	if v.tabMenu == nil {
		t.Fatal("hovering outside the menu must not dismiss it")
	}
	if v.tabMenu.selected != 2 {
		t.Errorf("selected = %d, want unchanged 2 after hovering outside the menu", v.tabMenu.selected)
	}
}

func TestTabMenuClickOnRowActivatesItem(t *testing.T) {
	v := multiTabView("a.go", "b.go")
	spans := tabBarSpans(v.tabs)
	v.HandleMouse(rightClick(spans[0].start+1, 0))
	v.Render(newFakeWindow(80, 10)) // populates tabMenu.rect, needed to hit-test a click against it

	menu := v.tabMenu
	// Click the row for "Close All" (index 2 in the fixed item order).
	clickRow := menu.rect.row + 2
	clickCol := menu.rect.col + 1
	if !v.HandleMouse(layout.Mouse{Col: clickCol, Row: clickRow, Button: layout.MouseLeft, EventType: layout.EventPress, Clicks: 1}) {
		t.Fatal("a click on a menu row should be consumed")
	}
	if v.tabMenu != nil {
		t.Error("menu should close after a row click")
	}
	if len(v.tabs) != 0 {
		t.Errorf("tabs = %v, want none (Close All)", tabPaths(v.tabs))
	}
}

func TestTabMenuOutsideClickDismissesWithoutActing(t *testing.T) {
	v := multiTabView("a.go", "b.go")
	spans := tabBarSpans(v.tabs)
	v.HandleMouse(rightClick(spans[0].start+1, 0))

	// Far outside the menu's rect.
	if !v.HandleMouse(layout.Mouse{Col: 70, Row: 9, Button: layout.MouseLeft, EventType: layout.EventPress, Clicks: 1}) {
		t.Fatal("the menu should still consume the dismissing click")
	}
	if v.tabMenu != nil {
		t.Error("a click outside the menu should dismiss it")
	}
	if len(v.tabs) != 2 {
		t.Error("an outside click must not close anything")
	}
}

func TestTabMenuCloseOthersKeepsOnlyTheTargetTab(t *testing.T) {
	v := multiTabView("a.go", "b.go", "c.go")
	v.active = 2 // active tab differs from the one right-clicked
	spans := tabBarSpans(v.tabs)
	v.HandleMouse(rightClick(spans[0].start+1, 0)) // target a.go (index 0)

	v.tabMenu.selected = 1 // "Close Others"
	v.HandleKey(layout.Key{Named: layout.KeyEnter})

	if got := tabPaths(v.tabs); len(got) != 1 || got[0] != "a.go" {
		t.Errorf("tabs = %v, want just [a.go]", got)
	}
}

func TestTabMenuMoveToNextPaneCallsCallbackWithTarget(t *testing.T) {
	v := multiTabView("a.go", "b.go")
	spans := tabBarSpans(v.tabs)
	v.HandleMouse(rightClick(spans[1].start+1, 0)) // target b.go (index 1)

	var gotIndex = -1
	v.OnMoveTabToNextPane = func(index int) { gotIndex = index }

	v.tabMenu.selected = 3 // "Move to next pane"
	v.HandleKey(layout.Key{Named: layout.KeyEnter})

	if gotIndex != 1 {
		t.Errorf("OnMoveTabToNextPane index = %d, want 1", gotIndex)
	}
}

func TestTabMenuCloseAllWithDirtyTabAsksForConfirmation(t *testing.T) {
	v := multiTabView("a.go", "b.go")
	v.tabs[1].buf.Dirty = true
	var gotPaths []string
	v.OnRequestCloseDirtyTabs = func(paths []string, _, _ func()) { gotPaths = paths }

	spans := tabBarSpans(v.tabs)
	v.HandleMouse(rightClick(spans[0].start+1, 0))
	v.tabMenu.selected = 2 // "Close All"
	v.HandleKey(layout.Key{Named: layout.KeyEnter})

	if len(gotPaths) != 1 || gotPaths[0] != "b.go" {
		t.Fatalf("OnRequestCloseDirtyTabs paths = %v, want [b.go]", gotPaths)
	}
	if len(v.tabs) != 2 {
		t.Error("nothing should close until the batch confirm resolves")
	}
}

func TestRequestCloseTabsSaveAllLeavesAFailedSaveOpen(t *testing.T) {
	// Regression guard: the "save all, then close" callback must only
	// close the tabs that actually saved — not the whole batch
	// unconditionally, which would silently discard a tab whose save
	// failed or conflicted.
	dir := t.TempDir()
	okPath := filepath.Join(dir, "ok.go")
	if err := os.WriteFile(okPath, []byte("v1"), 0o644); err != nil {
		t.Fatal(err)
	}
	// Under a directory that doesn't exist, so Buffer.Save's os.WriteFile
	// has nowhere to write and saveTab reports failure.
	badPath := filepath.Join(dir, "missing", "bad.go")

	v := NewView()
	v.Open(okPath) // properly stats the file, so HasDiskConflict won't false-positive
	v.tabs[0].buf.Lines = []string{"v2"}
	v.tabs[0].buf.Dirty = true
	v.tabs = append(v.tabs, &tab{path: badPath, buf: &Buffer{Path: badPath, Lines: []string{"x"}, Dirty: true}})
	v.active = 0

	var onSaveAll func()
	v.OnRequestCloseDirtyTabs = func(_ []string, saveAll, _ func()) { onSaveAll = saveAll }

	v.requestCloseTabs(append([]*tab(nil), v.tabs...))
	if onSaveAll == nil {
		t.Fatal("OnRequestCloseDirtyTabs was not called")
	}
	onSaveAll()

	if len(v.tabs) != 1 || v.tabs[0].path != badPath {
		t.Fatalf("tabs = %v, want only the failed save (%s) left open", tabPaths(v.tabs), badPath)
	}
	if !v.tabs[0].buf.Dirty {
		t.Error("the failed tab's unsaved change must not be discarded")
	}
}
