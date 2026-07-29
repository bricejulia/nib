package finder

import (
	"strings"
	"testing"

	"github.com/bricejulia/kiwi/internal/layout"
	"github.com/bricejulia/kiwi/internal/vcs/gitstatus"
)

type fakeWindow struct {
	cols, rows int
	lines      []string
	styles     []layout.Style // the first segment's style per row
}

func newFakeWindow(cols, rows int) *fakeWindow {
	return &fakeWindow{cols: cols, rows: rows, lines: make([]string, rows), styles: make([]layout.Style, rows)}
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
	if len(segs) > 0 {
		w.styles[row] = segs[0].Style
	}
}
func (w *fakeWindow) Clear() {
	for i := range w.lines {
		w.lines[i] = ""
		w.styles[i] = layout.Style{}
	}
}

func newTestView(items ...string) *View {
	v := New("/project")
	v.items = items
	v.refilter()
	return v
}

func TestViewListsAllItemsWithEmptyQuery(t *testing.T) {
	v := newTestView("a.go", "b.go", "c.go")
	w := newFakeWindow(40, 10)
	v.Render(w)

	for i, want := range []string{"a.go", "b.go", "c.go"} {
		if !strings.Contains(w.lines[1+i], want) {
			t.Errorf("row %d: got %q, want to contain %q", 1+i, w.lines[1+i], want)
		}
	}
}

func TestViewFiltersAsQueryIsTyped(t *testing.T) {
	v := newTestView("main.go", "utils.go", "main_test.go")
	v.HandleKey(layout.Key{Text: "m"})
	v.HandleKey(layout.Key{Text: "a"})
	v.HandleKey(layout.Key{Text: "i"})
	v.HandleKey(layout.Key{Text: "n"})

	w := newFakeWindow(40, 10)
	v.Render(w)

	joined := strings.Join(w.lines, "\n")
	if !strings.Contains(joined, "main.go") || !strings.Contains(joined, "main_test.go") {
		t.Errorf("expected both main.go and main_test.go to match \"main\":\n%s", joined)
	}
	if strings.Contains(joined, "utils.go") {
		t.Errorf("utils.go should not match \"main\":\n%s", joined)
	}
	if !strings.HasPrefix(w.lines[0], "> main") {
		t.Errorf("expected the prompt row to show the typed query, got %q", w.lines[0])
	}
}

func TestViewBackspaceRemovesLastQueryRuneAndRefilters(t *testing.T) {
	v := newTestView("main.go", "utils.go")
	v.HandleKey(layout.Key{Text: "m"})
	v.HandleKey(layout.Key{Text: "x"}) // "mx" matches nothing
	w := newFakeWindow(40, 10)
	v.Render(w)
	if !strings.Contains(w.lines[1], "no matches") {
		t.Fatalf("expected no matches for \"mx\", got %q", w.lines[1])
	}

	v.HandleKey(layout.Key{Named: layout.KeyBackspace}) // back to "m"
	v.Render(w)
	if !strings.Contains(w.lines[1], "main.go") {
		t.Errorf("expected main.go to match \"m\" after backspace, got %q", w.lines[1])
	}
}

func TestViewUpDownMovesCursorAndClamps(t *testing.T) {
	v := newTestView("a.go", "b.go", "c.go")

	v.HandleKey(layout.Key{Named: layout.KeyUp}) // already at 0, must clamp
	if v.cursor != 0 {
		t.Fatalf("cursor should clamp at 0, got %d", v.cursor)
	}

	v.HandleKey(layout.Key{Named: layout.KeyDown})
	v.HandleKey(layout.Key{Named: layout.KeyDown})
	if v.cursor != 2 {
		t.Fatalf("cursor = %d, want 2", v.cursor)
	}
	v.HandleKey(layout.Key{Named: layout.KeyDown}) // must clamp at last item
	if v.cursor != 2 {
		t.Fatalf("cursor should clamp at 2 (last item), got %d", v.cursor)
	}
}

func TestSetKeymapAddsCtrlBoundActionWithoutBreakingTyping(t *testing.T) {
	v := newTestView("a.go", "b.go", "c.go")
	v.SetKeymap(map[string]string{"Ctrl+n": "move_down"})

	v.HandleKey(layout.Key{Text: "n", Mods: layout.ModCtrl})
	if v.cursor != 1 {
		t.Fatalf("cursor = %d, want 1 (Ctrl+n bound to move_down)", v.cursor)
	}

	// Plain "n" (no modifier) must still be typed into the query, not
	// treated as a binding — overriding Ctrl+n must not affect it.
	v.HandleKey(layout.Key{Text: "n"})
	if string(v.query) != "n" {
		t.Fatalf("query = %q, want \"n\" (plain typing unaffected by the Ctrl+n override)", string(v.query))
	}
}

func TestSetKeymapOverridingAPlainLetterStopsItFromBeingTyped(t *testing.T) {
	v := newTestView("a.go", "b.go", "c.go")
	v.SetKeymap(map[string]string{"j": "move_down"})

	v.HandleKey(layout.Key{Text: "j"})
	if v.cursor != 1 {
		t.Fatalf("cursor = %d, want 1 (j remapped to move_down)", v.cursor)
	}
	if len(v.query) != 0 {
		t.Fatalf("query = %q, want empty: a plain-letter override intentionally stops that letter from being typed", string(v.query))
	}
}

func TestViewEnterSelectsHighlightedItemAndCloses(t *testing.T) {
	v := newTestView("a.go", "b.go", "c.go")
	v.HandleKey(layout.Key{Named: layout.KeyDown}) // move to "b.go"

	var selected string
	var selectedLine int
	closed := false
	v.OnSelect = func(absPath string, line int) { selected = absPath; selectedLine = line }
	v.OnClose = func() { closed = true }

	v.HandleKey(layout.Key{Named: layout.KeyEnter})

	if selected != "/project/b.go" {
		t.Errorf("got selected=%q, want %q", selected, "/project/b.go")
	}
	if selectedLine != 0 {
		t.Errorf("file-mode selection should report line=0 (no specific line), got %d", selectedLine)
	}
	if !closed {
		t.Error("expected OnClose to be called after Enter")
	}
}

func TestViewEscClosesWithoutSelecting(t *testing.T) {
	v := newTestView("a.go")
	selected := false
	closed := false
	v.OnSelect = func(string, int) { selected = true }
	v.OnClose = func() { closed = true }

	v.HandleKey(layout.Key{Named: layout.KeyEsc})

	if selected {
		t.Error("Esc should not trigger OnSelect")
	}
	if !closed {
		t.Error("expected OnClose to be called after Esc")
	}
}

func TestViewHandleKeyAlwaysReportsConsumed(t *testing.T) {
	v := newTestView("a.go")
	// Even a key the finder does nothing special with (e.g. an F-key-like
	// stray Named value) must be swallowed — nothing should leak through
	// a modal.
	if !v.HandleKey(layout.Key{Named: "F5"}) {
		t.Error("HandleKey should always return true (modal swallows all input)")
	}
}

func TestViewCursorPositionAfterPrompt(t *testing.T) {
	v := newTestView("a.go")
	v.HandleKey(layout.Key{Text: "a"})
	v.HandleKey(layout.Key{Text: "b"})

	col, row, ok := v.CursorPosition()
	if !ok {
		t.Fatal("expected ok=true")
	}
	if row != 0 {
		t.Errorf("cursor should be on the prompt row (0), got %d", row)
	}
	if col != len("> ab") {
		t.Errorf("cursor col = %d, want %d (right after the typed query)", col, len("> ab"))
	}
}

func TestViewShowsGitStatusMarkerAndColor(t *testing.T) {
	v := newTestView("main.go", "utils.go")
	v.ApplyStatus(map[string]gitstatus.Status{
		"main.go": gitstatus.Modified,
	})

	w := newFakeWindow(40, 10)
	v.Render(w)

	mainRow := indexOf(w.lines, "main.go")
	utilsRow := indexOf(w.lines, "utils.go")
	if mainRow < 0 || utilsRow < 0 {
		t.Fatalf("expected both files listed, got %v", w.lines)
	}

	if !strings.Contains(w.lines[mainRow], "M ") {
		t.Errorf("expected main.go's row to show the \"M\" marker, got %q", w.lines[mainRow])
	}
	if w.styles[mainRow].Foreground != layout.ColorYellow {
		t.Errorf("expected main.go styled yellow (modified), got %+v", w.styles[mainRow])
	}
	if w.styles[utilsRow] != (layout.Style{}) {
		t.Errorf("expected utils.go (no status) to use the default style, got %+v", w.styles[utilsRow])
	}
}

func indexOf(lines []string, substr string) int {
	for i, l := range lines {
		if strings.Contains(l, substr) {
			return i
		}
	}
	return -1
}

func TestViewEnterWithNoMatchesDoesNotCallOnSelect(t *testing.T) {
	v := newTestView("main.go")
	v.HandleKey(layout.Key{Text: "z"})
	v.HandleKey(layout.Key{Text: "z"}) // "zz" matches nothing

	called := false
	v.OnSelect = func(string, int) { called = true }
	closed := false
	v.OnClose = func() { closed = true }

	v.HandleKey(layout.Key{Named: layout.KeyEnter})

	if called {
		t.Error("OnSelect should not fire when there are no matches")
	}
	if !closed {
		t.Error("Enter should still close the modal even with no matches")
	}
}

func TestViewRightArrowRevealsTruncatedPath(t *testing.T) {
	longPath := "some/very/deeply/nested/directory/structure/that/is/quite/long/main.go"
	v := newTestView(longPath)

	w := newFakeWindow(30, 10)
	v.Render(w)
	if strings.Contains(w.lines[1], "main.go") {
		t.Fatalf("expected the filename to be truncated off in a narrow window, got %q", w.lines[1])
	}

	for i := 0; i < 10; i++ {
		v.HandleKey(layout.Key{Named: layout.KeyRight})
		v.Render(w)
	}
	if !strings.Contains(w.lines[1], "main.go") {
		t.Errorf("expected repeated Right presses to scroll far enough to reveal main.go, got %q", w.lines[1])
	}
}

func TestViewLeftArrowScrollsBack(t *testing.T) {
	longPath := "some/very/deeply/nested/directory/structure/that/is/quite/long/main.go"
	v := newTestView(longPath)
	w := newFakeWindow(30, 10)
	v.Render(w)

	for i := 0; i < 10; i++ {
		v.HandleKey(layout.Key{Named: layout.KeyRight})
	}
	v.Render(w)
	scrolledFar := v.hScroll

	v.HandleKey(layout.Key{Named: layout.KeyLeft})
	v.Render(w)
	if v.hScroll >= scrolledFar {
		t.Errorf("expected Left to scroll back, hScroll went from %d to %d", scrolledFar, v.hScroll)
	}
}

func TestViewHScrollClampsToSelectedRowWidth(t *testing.T) {
	v := newTestView("short.go")
	w := newFakeWindow(80, 10) // window much wider than the content

	for i := 0; i < 20; i++ {
		v.HandleKey(layout.Key{Named: layout.KeyRight})
	}
	v.Render(w)

	if v.hScroll != 0 {
		t.Errorf("expected hScroll to clamp to 0 when the row already fits entirely, got %d", v.hScroll)
	}
}

func TestViewHScrollResetsWhenCursorMoves(t *testing.T) {
	v := newTestView(
		"some/very/deeply/nested/path/one/main.go",
		"b.go",
	)
	w := newFakeWindow(30, 10)
	v.Render(w)

	for i := 0; i < 10; i++ {
		v.HandleKey(layout.Key{Named: layout.KeyRight})
	}
	v.Render(w)
	if v.hScroll == 0 {
		t.Fatal("expected hScroll to have advanced before moving the cursor")
	}

	v.HandleKey(layout.Key{Named: layout.KeyDown})
	if v.hScroll != 0 {
		t.Errorf("expected hScroll to reset to 0 after moving to a different row, got %d", v.hScroll)
	}
}

func TestViewHScrollResetsWhenQueryChanges(t *testing.T) {
	v := newTestView("some/very/deeply/nested/path/one/main.go")
	w := newFakeWindow(30, 10)
	v.Render(w)

	for i := 0; i < 10; i++ {
		v.HandleKey(layout.Key{Named: layout.KeyRight})
	}
	if v.hScroll == 0 {
		t.Fatal("expected hScroll to have advanced before typing")
	}

	v.HandleKey(layout.Key{Text: "m"})
	if v.hScroll != 0 {
		t.Errorf("expected hScroll to reset to 0 after the query changes, got %d", v.hScroll)
	}
}

func TestViewTabTogglesModeAndTitle(t *testing.T) {
	v := newTestView("main.go")
	if v.Title() != "Find File" {
		t.Fatalf("expected initial title %q, got %q", "Find File", v.Title())
	}

	v.HandleKey(layout.Key{Named: layout.KeyTab})
	if v.mode != modeContent {
		t.Fatal("Tab should switch to content mode")
	}
	if v.Title() != "Find in Files" {
		t.Errorf("expected title %q in content mode, got %q", "Find in Files", v.Title())
	}

	v.HandleKey(layout.Key{Named: layout.KeyTab})
	if v.mode != modeFiles {
		t.Fatal("a second Tab should switch back to file mode")
	}
}

func TestOpenWithQueryEntersContentModeWithQueryPreFilled(t *testing.T) {
	v := New("/project")

	v.OpenWithQuery("myIdentifier")

	if v.mode != modeContent {
		t.Fatal("expected OpenWithQuery to switch to content-search mode")
	}
	if string(v.query) != "myIdentifier" {
		t.Errorf("query = %q, want %q", string(v.query), "myIdentifier")
	}
}

func TestViewOpenAlwaysResetsToFileMode(t *testing.T) {
	v := newTestView("main.go")
	v.HandleKey(layout.Key{Named: layout.KeyTab}) // switch to content mode
	v.Open()
	if v.mode != modeFiles {
		t.Error("Open should always reset back to file mode")
	}
}

func TestViewContentModeRequiresMinimumQueryLength(t *testing.T) {
	v := newTestView("main.go")
	v.HandleKey(layout.Key{Named: layout.KeyTab}) // content mode
	v.HandleKey(layout.Key{Text: "a"})            // 1 char: below minContentQueryLen

	w := newFakeWindow(60, 10)
	v.Render(w)
	if !strings.Contains(w.lines[1], "type at least") {
		t.Errorf("expected a prompt to type more characters, got %q", w.lines[1])
	}
}

func TestViewContentModeSearchesFileContentsViaGitGrep(t *testing.T) {
	dir := newContentSearchRepo(t)
	v := New(dir)
	v.HandleKey(layout.Key{Named: layout.KeyTab}) // content mode
	for _, r := range "needle" {
		v.HandleKey(layout.Key{Text: string(r)})
	}

	w := newFakeWindow(80, 10)
	v.Render(w)

	joined := strings.Join(w.lines, "\n")
	if !strings.Contains(joined, "haystack.go") || !strings.Contains(joined, "needle") {
		t.Errorf("expected a match from haystack.go containing \"needle\", got:\n%s", joined)
	}
	if !strings.Contains(joined, ":2:") {
		t.Errorf("expected the match to report line 2, got:\n%s", joined)
	}
}

func TestViewContentModeDimsThePathLinePrefix(t *testing.T) {
	dir := newContentSearchRepo(t)
	v := New(dir)
	v.HandleKey(layout.Key{Named: layout.KeyTab}) // content mode
	for _, r := range "needle" {
		v.HandleKey(layout.Key{Text: string(r)})
	}

	w := newFakeWindow(80, 10)
	v.Render(w)

	if w.styles[1].Attr&layout.AttrDim == 0 {
		t.Errorf("expected the path:line: prefix to be dim-styled, got %+v", w.styles[1])
	}
}

func TestViewContentModeSelectReportsPathAndLine(t *testing.T) {
	dir := newContentSearchRepo(t)
	v := New(dir)
	v.HandleKey(layout.Key{Named: layout.KeyTab})
	for _, r := range "needle" {
		v.HandleKey(layout.Key{Text: string(r)})
	}

	var gotPath string
	var gotLine int
	v.OnSelect = func(absPath string, line int) { gotPath, gotLine = absPath, line }
	v.HandleKey(layout.Key{Named: layout.KeyEnter})

	if gotLine != 2 {
		t.Errorf("expected line=2, got %d", gotLine)
	}
	if !strings.HasSuffix(gotPath, "haystack.go") {
		t.Errorf("expected the absolute path to haystack.go, got %q", gotPath)
	}
}
