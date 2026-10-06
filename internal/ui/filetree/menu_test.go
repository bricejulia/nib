package filetree

import (
	"path/filepath"
	"strings"
	"testing"

	"github.com/bricejulia/nib/internal/layout"
	"github.com/bricejulia/nib/internal/vcs/gitstatus"
)

func rightClick(col, row int) layout.Mouse {
	return layout.Mouse{Col: col, Row: row, Button: layout.MouseRight, EventType: layout.EventPress, Clicks: 1}
}

func leftClick(col, row int) layout.Mouse {
	return layout.Mouse{Col: col, Row: row, Button: layout.MouseLeft, EventType: layout.EventPress, Clicks: 1}
}

// rowIndex returns the index of the row named name, failing the test if
// there isn't one.
func rowIndex(t *testing.T, v *View, name string) int {
	t.Helper()
	for i, row := range v.rows {
		if row.Node.Name == name {
			return i
		}
	}
	t.Fatalf("no row named %q", name)
	return -1
}

// menuFixture is promptFixture with the "sub" directory expanded, so the
// tree is: sub, sub/c.txt, a.txt, b.txt — and a CopyFunc recording what
// was copied.
func menuFixture(t *testing.T) (*View, string, *fakeWindow, *[]string) {
	t.Helper()
	v, root, w := promptFixture(t)
	selectRow(t, v, "sub")
	v.HandleKey(enterKey())
	v.Render(w)
	var copied []string
	v.CopyFunc = func(s string) { copied = append(copied, s) }
	return v, root, w, &copied
}

// clickItem right-clicks name's row, then left-clicks the menu item
// labelled label.
func clickItem(t *testing.T, v *View, w *fakeWindow, name, label string) {
	t.Helper()
	idx := rowIndex(t, v, name)
	if !v.HandleMouse(rightClick(2, idx)) {
		t.Fatal("right-click on a row was not consumed")
	}
	v.Render(w)
	for i, it := range v.menu.items {
		if it.label() == label {
			v.HandleMouse(leftClick(v.menu.col+1, v.menu.row+i))
			return
		}
	}
	t.Fatalf("menu has no %q item", label)
}

func TestRightClickOpensMenuAndSelectsRow(t *testing.T) {
	v, _, w, _ := menuFixture(t)
	idx := rowIndex(t, v, "b.txt")

	if !v.HandleMouse(rightClick(3, idx)) {
		t.Fatal("expected the right-click to be consumed")
	}
	if v.menu == nil {
		t.Fatal("expected the menu to open")
	}
	if v.cursor != idx {
		t.Errorf("cursor = %d, want the clicked row %d", v.cursor, idx)
	}
	v.Render(w)
	if !strings.Contains(strings.Join(w.lines, "\n"), "⤷ Copy Relative Path") {
		t.Errorf("menu not rendered:\n%s", strings.Join(w.lines, "\n"))
	}
}

func TestRightClickBelowRowsDoesNothing(t *testing.T) {
	v, _, _, _ := menuFixture(t)
	if v.HandleMouse(rightClick(1, len(v.rows)+1)) {
		t.Error("a right-click on empty space should not be consumed")
	}
	if v.menu != nil {
		t.Error("menu should stay closed")
	}
}

func TestLeftClickWithoutMenuIsNotConsumed(t *testing.T) {
	v, _, _, _ := menuFixture(t)
	if v.HandleMouse(leftClick(1, 0)) {
		t.Error("a left click should fall through to App's default handling")
	}
}

func TestMenuCopyPaths(t *testing.T) {
	v, root, w, copied := menuFixture(t)

	clickItem(t, v, w, "c.txt", "Copy Path")
	clickItem(t, v, w, "c.txt", "Copy Relative Path")

	want := []string{filepath.Join(root, "sub", "c.txt"), "sub/c.txt"}
	if len(*copied) != 2 || (*copied)[0] != want[0] || (*copied)[1] != want[1] {
		t.Errorf("copied = %q, want %q", *copied, want)
	}
	if v.menu != nil {
		t.Error("menu should close after acting")
	}
	if v.Notice() != "copied sub/c.txt" {
		t.Errorf("Notice = %q", v.Notice())
	}
}

func TestMenuRenameOpensPrompt(t *testing.T) {
	v, _, w, _ := menuFixture(t)
	clickItem(t, v, w, "c.txt", "Rename / Move")
	if v.prompt != promptRename || v.promptField.String() != "sub/c.txt" {
		t.Errorf("prompt = %v %q, want rename prefilled with sub/c.txt", v.prompt, v.promptField.String())
	}
}

func TestMenuDeleteOpensConfirm(t *testing.T) {
	v, _, w, _ := menuFixture(t)
	clickItem(t, v, w, "a.txt", "Delete")
	if v.prompt != promptConfirm || filepath.Base(v.promptTarget) != "a.txt" {
		t.Errorf("prompt = %v on %q, want a y/N confirm for a.txt", v.prompt, v.promptTarget)
	}

	v.HandleKey(layout.Key{Named: layout.KeyEsc})
	clickItem(t, v, w, "sub", "Delete")
	if v.prompt != promptConfirmYes {
		t.Errorf("prompt = %v, want the type-\"yes\" confirm for a non-empty dir", v.prompt)
	}
}

func TestMenuInChangesModeOnlyCopies(t *testing.T) {
	v, _, w, _ := menuFixture(t)
	v.ApplyChanges(map[string]gitstatus.Status{"a.txt": gitstatus.Modified})
	v.SetMode(ModeChanges)
	v.Render(w)

	v.HandleMouse(rightClick(1, 0))
	if v.menu == nil {
		t.Fatal("expected the menu to open")
	}
	if len(v.menu.items) != 2 {
		t.Errorf("items = %v, want only the two copy actions", v.menu.items)
	}
}

func TestMenuKeyboard(t *testing.T) {
	v, _, _, copied := menuFixture(t)
	v.HandleMouse(rightClick(1, rowIndex(t, v, "a.txt")))

	v.HandleKey(upKey()) // wraps to the last item
	if v.menu.selected != len(v.menu.items)-1 {
		t.Errorf("selected = %d after Up from 0, want wrap to last", v.menu.selected)
	}
	v.HandleKey(downKey())
	v.HandleKey(downKey())
	if v.menu.selected != 1 {
		t.Errorf("selected = %d, want 1", v.menu.selected)
	}
	v.HandleKey(enterKey())
	if len(*copied) != 1 || (*copied)[0] != "a.txt" {
		t.Errorf("copied = %q, want the relative path", *copied)
	}

	v.HandleMouse(rightClick(1, rowIndex(t, v, "a.txt")))
	if !v.HandleKey(layout.Key{Text: "d"}) {
		t.Error("a key while the menu is open should be consumed")
	}
	if v.menu != nil || v.prompt != promptNone {
		t.Error("an unrelated key should close the menu without triggering its binding")
	}
}

func TestMenuMouseHoverAndOutsideClick(t *testing.T) {
	v, _, w, copied := menuFixture(t)
	v.HandleMouse(rightClick(1, rowIndex(t, v, "a.txt")))
	v.Render(w)

	v.HandleMouse(layout.Mouse{Col: v.menu.col, Row: v.menu.row + 2, Button: layout.MouseNone, EventType: layout.EventMotion})
	if v.menu.selected != 2 {
		t.Errorf("selected = %d after hovering item 2", v.menu.selected)
	}

	v.HandleMouse(leftClick(w.cols-1, w.rows-1))
	if v.menu != nil {
		t.Error("a click outside the menu should close it")
	}
	if len(*copied) != 0 {
		t.Error("closing the menu should not act")
	}
}

func TestCancelPromptClosesMenu(t *testing.T) {
	v, _, _, _ := menuFixture(t)
	v.HandleMouse(rightClick(1, 0))
	v.CancelPrompt()
	if v.menu != nil {
		t.Error("CancelPrompt should close the menu too")
	}
}
