package filetree

import (
	"strings"
	"testing"

	"github.com/bricejulia/kiwi/internal/layout"
)

// fakeWindow is an in-memory layout.Window double so View.Render is
// testable without a live terminal.
type fakeWindow struct {
	cols, rows int
	lines      []string
}

func newFakeWindow(cols, rows int) *fakeWindow {
	return &fakeWindow{cols: cols, rows: rows, lines: make([]string, rows)}
}

func (w *fakeWindow) Size() (int, int) { return w.cols, w.rows }
func (w *fakeWindow) Println(row int, segs ...layout.Segment) {
	if row < 0 || row >= len(w.lines) {
		return
	}
	text := ""
	for _, s := range segs {
		text += s.Text
	}
	w.lines[row] = text
}
func (w *fakeWindow) Clear() {
	for i := range w.lines {
		w.lines[i] = ""
	}
}

func downKey() layout.Key  { return layout.Key{Named: layout.KeyDown} }
func upKey() layout.Key    { return layout.Key{Named: layout.KeyUp} }
func enterKey() layout.Key { return layout.Key{Named: layout.KeyEnter} }
func leftKey() layout.Key  { return layout.Key{Named: layout.KeyLeft} }

func TestViewRenderShowsFixtureEntries(t *testing.T) {
	v := New(fixtureRoot(t))
	w := newFakeWindow(40, 10)
	v.Render(w)

	joined := strings.Join(w.lines, "\n")
	for _, want := range []string{"sub", "vendor", "a.txt", "b.txt"} {
		if !strings.Contains(joined, want) {
			t.Errorf("rendered output missing %q:\n%s", want, joined)
		}
	}
}

func TestViewHandleKeyDownMovesCursorAndReflectsInRender(t *testing.T) {
	v := New(fixtureRoot(t))
	w := newFakeWindow(40, 10)
	v.Render(w) // populates rows

	if !v.HandleKey(downKey()) {
		t.Fatal("Down should be consumed by the file tree")
	}
	if v.cursor != 1 {
		t.Fatalf("cursor = %d, want 1", v.cursor)
	}

	v.HandleKey(upKey())
	if v.cursor != 0 {
		t.Fatalf("cursor = %d, want 0 after Up", v.cursor)
	}
}

func TestViewHandleKeyCursorClampsAtBounds(t *testing.T) {
	v := New(fixtureRoot(t))
	w := newFakeWindow(40, 10)
	v.Render(w)

	v.HandleKey(upKey()) // already at 0, must clamp, not go negative
	if v.cursor != 0 {
		t.Fatalf("cursor should clamp at 0, got %d", v.cursor)
	}

	for i := 0; i < 100; i++ {
		v.HandleKey(downKey())
	}
	if v.cursor != len(v.rows)-1 {
		t.Fatalf("cursor should clamp at last row (%d), got %d", len(v.rows)-1, v.cursor)
	}
}

func TestViewEnterOnDirectoryExpandsAndLazyLoads(t *testing.T) {
	v := New(fixtureRoot(t))
	w := newFakeWindow(40, 10)
	v.Render(w)

	// Row 0 is "sub" (dirs sort first). Enter should expand it and load
	// its children (c.txt) without walking anything eagerly beforehand.
	if v.rows[0].Node.Name != "sub" {
		t.Fatalf("expected row 0 to be %q, got %q", "sub", v.rows[0].Node.Name)
	}
	v.HandleKey(enterKey())
	v.Render(w) // re-flatten after the dirty flag was set

	joined := strings.Join(w.lines, "\n")
	if !strings.Contains(joined, "c.txt") {
		t.Errorf("expected sub/c.txt to appear after expanding sub:\n%s", joined)
	}
}

func TestViewLeftCollapsesExpandedDirectory(t *testing.T) {
	v := New(fixtureRoot(t))
	w := newFakeWindow(40, 10)
	v.Render(w)
	v.HandleKey(enterKey()) // expand "sub"
	v.Render(w)

	v.HandleKey(leftKey()) // cursor still on "sub" row 0; collapse it
	v.Render(w)

	joined := strings.Join(w.lines, "\n")
	if strings.Contains(joined, "c.txt") {
		t.Errorf("c.txt should be hidden again after collapsing sub:\n%s", joined)
	}
}

func longNameTree(name string) *Node {
	return &Node{IsDir: true, Loaded: true, Children: []*Node{{Name: name}}}
}

func TestViewShiftRightRevealsTruncatedName(t *testing.T) {
	longName := "a-very-long-file-name-that-will-not-fit-in-a-narrow-pane.go"
	v := &View{root: longNameTree(longName), dirty: true}

	w := newFakeWindow(20, 10)
	v.Render(w)
	if strings.Contains(w.lines[0], longName) {
		t.Fatalf("expected the name to be truncated in a narrow (20-col) pane, got %q", w.lines[0])
	}

	for i := 0; i < 10; i++ {
		v.HandleKey(layout.Key{Named: layout.KeyRight, Mods: layout.ModShift})
		v.Render(w)
	}
	tail := longName[len(longName)-6:] // "ne.go"-ish tail
	if !strings.Contains(w.lines[0], tail) {
		t.Errorf("expected repeated Shift+Right to scroll far enough to reveal the file's tail %q, got %q", tail, w.lines[0])
	}
}

func TestViewPlainRightStillExpandsNotScrolls(t *testing.T) {
	// Guards the switch-case ordering: Shift+Right must not swallow plain
	// Right, which still means "expand/activate".
	v := New(fixtureRoot(t))
	w := newFakeWindow(40, 10)
	v.Render(w)

	if v.rows[0].Node.Name != "sub" || v.rows[0].Node.Expanded {
		t.Fatalf("expected row 0 to be a collapsed \"sub\" directory, got %+v", v.rows[0].Node)
	}
	v.HandleKey(layout.Key{Named: layout.KeyRight}) // no Shift
	if !v.rows[0].Node.Expanded {
		t.Error("plain Right should still expand the directory, not scroll")
	}
	if v.hScroll != 0 {
		t.Errorf("plain Right should not touch hScroll, got %d", v.hScroll)
	}
}

func TestViewShiftLeftScrollsBack(t *testing.T) {
	longName := "a-very-long-file-name-that-will-not-fit-in-a-narrow-pane.go"
	v := &View{root: longNameTree(longName), dirty: true}
	w := newFakeWindow(20, 10)
	v.Render(w)

	for i := 0; i < 10; i++ {
		v.HandleKey(layout.Key{Named: layout.KeyRight, Mods: layout.ModShift})
	}
	v.Render(w)
	scrolledFar := v.hScroll

	v.HandleKey(layout.Key{Named: layout.KeyLeft, Mods: layout.ModShift})
	v.Render(w)
	if v.hScroll >= scrolledFar {
		t.Errorf("expected Shift+Left to scroll back, hScroll went from %d to %d", scrolledFar, v.hScroll)
	}
}

func TestViewHScrollResetsWhenCursorMoves(t *testing.T) {
	longName := "a-very-long-file-name-that-will-not-fit-in-a-narrow-pane.go"
	root := &Node{IsDir: true, Loaded: true, Children: []*Node{
		{Name: longName},
		{Name: "b.txt"},
	}}
	v := &View{root: root, dirty: true}
	w := newFakeWindow(20, 10)
	v.Render(w)

	for i := 0; i < 10; i++ {
		v.HandleKey(layout.Key{Named: layout.KeyRight, Mods: layout.ModShift})
	}
	v.Render(w)
	if v.hScroll == 0 {
		t.Fatal("expected hScroll to have advanced before moving the cursor")
	}

	v.HandleKey(downKey())
	if v.hScroll != 0 {
		t.Errorf("expected hScroll to reset to 0 after moving to a different row, got %d", v.hScroll)
	}
}

func TestViewOnOpenCalledForFileRow(t *testing.T) {
	v := New(fixtureRoot(t))
	var opened string
	v.OnOpen = func(path string) { opened = path }

	w := newFakeWindow(40, 10)
	v.Render(w)

	// Move cursor to "a.txt" (dirs sub, vendor come first, then a.txt).
	v.HandleKey(downKey())
	v.HandleKey(downKey())
	if v.rows[v.cursor].Node.Name != "a.txt" {
		t.Fatalf("expected cursor on a.txt, got %q", v.rows[v.cursor].Node.Name)
	}
	v.HandleKey(enterKey())

	if opened == "" || !strings.HasSuffix(opened, "a.txt") {
		t.Errorf("OnOpen should have been called with a.txt's path, got %q", opened)
	}
}
