package filetree

import (
	"os"
	"path/filepath"
	"strings"
	"testing"

	"github.com/bricejulia/nib/internal/layout"
)

// promptFixture builds a small on-disk tree in a temp dir (never the shared
// testdata fixture, which these tests would mutate) and returns a View
// rendered once, so v.rows is populated:
//
//	sub/c.txt
//	a.txt
//	b.txt
func promptFixture(t *testing.T) (*View, string, *fakeWindow) {
	t.Helper()
	root := t.TempDir()
	if err := os.Mkdir(filepath.Join(root, "sub"), 0o755); err != nil {
		t.Fatal(err)
	}
	for _, p := range []string{"sub/c.txt", "a.txt", "b.txt"} {
		if err := os.WriteFile(filepath.Join(root, filepath.FromSlash(p)), nil, 0o644); err != nil {
			t.Fatal(err)
		}
	}
	v := New(root)
	w := newFakeWindow(40, 10)
	v.Render(w)
	return v, root, w
}

func typeKeys(v *View, s string) {
	for _, r := range s {
		v.HandleKey(layout.Key{Text: string(r)})
	}
}

// selectRow moves the cursor onto the row for name, failing the test if
// there isn't one.
func selectRow(t *testing.T, v *View, name string) {
	t.Helper()
	for i, row := range v.rows {
		if row.Node.Name == name {
			v.cursor = i
			return
		}
	}
	t.Fatalf("no row named %q in %d rows", name, len(v.rows))
}

func TestPromptReservesThePaneBottomRow(t *testing.T) {
	v, _, _ := promptFixture(t)

	// A pane exactly as tall as the tree has rows: with no prompt every row
	// is visible, and opening one has to cost the last of them.
	w := newFakeWindow(40, len(v.rows))
	v.Render(w)
	if last := w.lines[len(w.lines)-1]; !strings.Contains(last, "b.txt") {
		t.Fatalf("expected the last tree row on the bottom line, got %q", last)
	}

	v.HandleKey(layout.Key{Text: "a"})
	v.Render(w)

	bottom := w.lines[len(w.lines)-1]
	if !strings.HasPrefix(bottom, "new: ") {
		t.Errorf("bottom row = %q, want the prompt", bottom)
	}
	if strings.Contains(strings.Join(w.lines, "\n"), "b.txt") {
		t.Error("the bottom tree row should have been displaced by the prompt")
	}
}

func TestPromptCursorPositionOnlyWhilePrompting(t *testing.T) {
	v, _, w := promptFixture(t)
	selectRow(t, v, "a.txt") // a top-level file, so the create prefill is empty

	if _, _, ok := v.CursorPosition(); ok {
		t.Error("the tree should show no terminal cursor when not prompting")
	}

	v.HandleKey(layout.Key{Text: "a"})
	typeKeys(v, "x.go")
	v.Render(w)

	col, row, ok := v.CursorPosition()
	if !ok {
		t.Fatal("expected a cursor while prompting")
	}
	if want := len("new: ") + len("x.go"); col != want {
		t.Errorf("cursor col = %d, want %d", col, want)
	}
	if want := w.rows - 1; row != want {
		t.Errorf("cursor row = %d, want %d (the pane's bottom row)", row, want)
	}
}

// The prompt must consume every key: an unconsumed one bubbles to the global
// keymap, where "?" opens help, "Tab" moves focus, and "Ctrl+c" quits.
func TestPromptSwallowsEveryKey(t *testing.T) {
	v, _, _ := promptFixture(t)
	selectRow(t, v, "a.txt")
	before := v.cursor

	v.HandleKey(layout.Key{Text: "a"})

	keys := []layout.Key{
		{Text: "?"},
		{Named: layout.KeyTab},
		{Text: "c", Mods: layout.ModCtrl},
		{Named: layout.KeyTab, Mods: layout.ModShift},
		{Text: "j"},
		{Text: "a"},
		{Text: "d"},
		{Text: "r"},
		{Named: layout.KeyPageDown},
		{Named: layout.KeyShift},
	}
	for _, k := range keys {
		if !v.HandleKey(k) {
			t.Errorf("key %q was not consumed by the prompt", k.String())
		}
	}

	if v.cursor != before {
		t.Errorf("cursor moved to %d while prompting (was %d): j must type, not navigate", v.cursor, before)
	}
	// The printables land verbatim; Ctrl+c, Tab, PageDown and bare Shift do
	// not. The prefill is "" here (a.txt is a file at the top level).
	if got := string(v.promptBuf); got != "?jadr" {
		t.Errorf("prompt buffer = %q, want %q", got, "?jadr")
	}
}

func TestPromptTypesSpacesIntoAName(t *testing.T) {
	v, root, _ := promptFixture(t)
	selectRow(t, v, "a.txt")
	v.HandleKey(layout.Key{Text: "a"})
	// How App.translateKey reports the space bar: promoted to Named "Space"
	// with Text left intact.
	typeKeys(v, "my")
	v.HandleKey(layout.Key{Text: " ", Named: layout.KeySpace})
	typeKeys(v, "file.txt")
	v.HandleKey(enterKey())

	if _, err := os.Stat(filepath.Join(root, "my file.txt")); err != nil {
		t.Fatalf("expected \"my file.txt\": %v", err)
	}
}

func TestPromptCreateCommitsAndSelectsTheNewFile(t *testing.T) {
	v, root, w := promptFixture(t)
	selectRow(t, v, "a.txt")

	v.HandleKey(layout.Key{Text: "a"})
	typeKeys(v, "new.go")
	v.HandleKey(enterKey())

	if _, err := os.Stat(filepath.Join(root, "new.go")); err != nil {
		t.Fatalf("expected new.go on disk: %v", err)
	}
	if v.prompt != promptNone {
		t.Error("the prompt should have closed on a successful commit")
	}
	v.Render(w)
	if got := v.rows[v.cursor].Node.Name; got != "new.go" {
		t.Errorf("cursor on %q, want the newly created new.go", got)
	}
}

func TestPromptCreateTrailingSlashMakesADirectory(t *testing.T) {
	v, root, _ := promptFixture(t)
	selectRow(t, v, "a.txt")

	v.HandleKey(layout.Key{Text: "a"})
	typeKeys(v, "pkg/")
	v.HandleKey(enterKey())

	info, err := os.Stat(filepath.Join(root, "pkg"))
	if err != nil {
		t.Fatalf("stat: %v", err)
	}
	if !info.IsDir() {
		t.Error("a trailing slash should have created a directory")
	}
}

func TestPromptCreateOpensTheNewFile(t *testing.T) {
	v, root, _ := promptFixture(t)
	selectRow(t, v, "a.txt")

	var opened string
	v.OnOpen = func(path string) { opened = path }

	v.HandleKey(layout.Key{Text: "a"})
	typeKeys(v, "new.go")
	v.HandleKey(enterKey())

	if want := filepath.Join(root, "new.go"); opened != want {
		t.Errorf("OnOpen called with %q, want %q", opened, want)
	}
}

func TestPromptCreateDirectoryDoesNotOpenIt(t *testing.T) {
	v, _, _ := promptFixture(t)
	selectRow(t, v, "a.txt")

	var opened string
	v.OnOpen = func(path string) { opened = path }

	v.HandleKey(layout.Key{Text: "a"})
	typeKeys(v, "pkg/")
	v.HandleKey(enterKey())

	if opened != "" {
		t.Errorf("OnOpen called with %q, want no call for a directory", opened)
	}
}

// A create prefilled from a collapsed directory has to expand the ancestors
// and land the cursor on the new entry — Refresh alone would not even
// re-read a collapsed-but-loaded folder.
func TestPromptCreateInsideACollapsedDirectoryRevealsIt(t *testing.T) {
	v, root, w := promptFixture(t)
	selectRow(t, v, "sub")
	// Load "sub" (so it's loaded but collapsed) by expanding and collapsing.
	v.HandleKey(enterKey())
	v.Render(w)
	v.HandleKey(leftKey())
	v.Render(w)
	if v.rows[v.cursor].Node.Expanded {
		t.Fatal("expected sub to be collapsed for this test")
	}

	v.HandleKey(layout.Key{Text: "a"})
	if got := string(v.promptBuf); got != "sub/" {
		t.Fatalf("prefill = %q, want %q", got, "sub/")
	}
	typeKeys(v, "deep/new.go")
	v.HandleKey(enterKey())

	if _, err := os.Stat(filepath.Join(root, "sub", "deep", "new.go")); err != nil {
		t.Fatalf("expected sub/deep/new.go: %v", err)
	}
	v.Render(w)
	if got := v.rows[v.cursor].Node.Name; got != "new.go" {
		t.Errorf("cursor on %q, want new.go", got)
	}
	joined := strings.Join(w.lines, "\n")
	if !strings.Contains(joined, "deep") {
		t.Errorf("expected the ancestors expanded:\n%s", joined)
	}
}

func TestPromptEscapeLeavesNoTrace(t *testing.T) {
	v, root, _ := promptFixture(t)
	before := v.cursor

	v.HandleKey(layout.Key{Text: "a"})
	typeKeys(v, "nope.go")
	v.HandleKey(layout.Key{Named: layout.KeyEsc})

	if v.prompt != promptNone {
		t.Error("Esc should have closed the prompt")
	}
	if _, err := os.Stat(filepath.Join(root, "nope.go")); !os.IsNotExist(err) {
		t.Error("Esc must not create anything")
	}
	if v.cursor != before {
		t.Errorf("cursor = %d, want it unchanged at %d", v.cursor, before)
	}
}

func TestPromptCaretEditing(t *testing.T) {
	v, _, _ := promptFixture(t)
	selectRow(t, v, "a.txt")

	v.HandleKey(layout.Key{Text: "r"})
	if got := string(v.promptBuf); got != "a.txt" {
		t.Fatalf("rename prefill = %q, want %q", got, "a.txt")
	}

	v.HandleKey(layout.Key{Named: layout.KeyHome})
	typeKeys(v, "sub/")
	if got := string(v.promptBuf); got != "sub/a.txt" {
		t.Fatalf("after Home + typing: %q", got)
	}

	v.HandleKey(layout.Key{Named: layout.KeyEnd})
	typeKeys(v, "!")
	if got := string(v.promptBuf); got != "sub/a.txt!" {
		t.Fatalf("after End + typing: %q", got)
	}

	v.HandleKey(layout.Key{Named: layout.KeyBackspace})
	v.HandleKey(layout.Key{Named: layout.KeyLeft})
	v.HandleKey(layout.Key{Named: layout.KeyLeft})
	v.HandleKey(layout.Key{Named: layout.KeyBackspace})
	if got := string(v.promptBuf); got != "sub/a.xt" {
		t.Errorf("after Left Left Backspace: %q, want %q", got, "sub/a.xt")
	}
	if v.promptCaret != len("sub/a.") {
		t.Errorf("caret = %d, want %d", v.promptCaret, len("sub/a."))
	}
}

func TestPromptRenameMovesTheFileAndNotifies(t *testing.T) {
	v, root, w := promptFixture(t)
	selectRow(t, v, "a.txt")

	var gotOld, gotNew string
	v.OnPathMoved = func(oldPath, newPath string) { gotOld, gotNew = oldPath, newPath }
	mutated := 0
	v.OnMutated = func() { mutated++ }

	v.HandleKey(layout.Key{Text: "r"})
	for range "a.txt" {
		v.HandleKey(layout.Key{Named: layout.KeyBackspace})
	}
	typeKeys(v, "renamed.txt")
	v.HandleKey(enterKey())

	if _, err := os.Stat(filepath.Join(root, "renamed.txt")); err != nil {
		t.Fatalf("expected renamed.txt: %v", err)
	}
	if _, err := os.Stat(filepath.Join(root, "a.txt")); !os.IsNotExist(err) {
		t.Error("a.txt should be gone")
	}
	if gotOld != filepath.Join(root, "a.txt") || gotNew != filepath.Join(root, "renamed.txt") {
		t.Errorf("OnPathMoved(%q, %q), want the old and new absolute paths", gotOld, gotNew)
	}
	if mutated != 1 {
		t.Errorf("OnMutated called %d times, want 1", mutated)
	}
	v.Render(w)
	if got := v.rows[v.cursor].Node.Name; got != "renamed.txt" {
		t.Errorf("cursor on %q, want renamed.txt", got)
	}
}

// Editing an earlier segment of the prefilled path moves the file.
func TestPromptRenameEarlierSegmentMovesIntoAnotherDirectory(t *testing.T) {
	v, root, w := promptFixture(t)
	selectRow(t, v, "a.txt")

	v.HandleKey(layout.Key{Text: "r"})
	v.HandleKey(layout.Key{Named: layout.KeyHome})
	typeKeys(v, "sub/")
	v.HandleKey(enterKey())

	if _, err := os.Stat(filepath.Join(root, "sub", "a.txt")); err != nil {
		t.Fatalf("expected sub/a.txt: %v", err)
	}
	v.Render(w)
	if got := v.rows[v.cursor].Node.Name; got != "a.txt" {
		t.Fatalf("cursor on %q, want the moved a.txt", got)
	}
	if got := v.rows[v.cursor].Depth; got != 1 {
		t.Errorf("depth = %d, want 1 (inside sub, which should be expanded)", got)
	}
}

func TestPromptRefusalKeepsThePromptOpen(t *testing.T) {
	v, root, w := promptFixture(t)
	selectRow(t, v, "a.txt")
	mutated := 0
	v.OnMutated = func() { mutated++ }

	v.HandleKey(layout.Key{Text: "r"})
	for range "a.txt" {
		v.HandleKey(layout.Key{Named: layout.KeyBackspace})
	}
	typeKeys(v, "b.txt") // already exists
	v.HandleKey(enterKey())

	if v.prompt != promptRename {
		t.Fatal("a refused rename should leave the prompt open")
	}
	if v.promptErr == "" {
		t.Error("expected an inline error message")
	}
	if got := string(v.promptBuf); got != "b.txt" {
		t.Errorf("typed text = %q, want it preserved", got)
	}
	if _, err := os.Stat(filepath.Join(root, "a.txt")); err != nil {
		t.Error("the source should be untouched")
	}
	if mutated != 0 {
		t.Errorf("OnMutated called %d times, want 0", mutated)
	}

	v.Render(w)
	bottom := w.lines[w.rows-1]
	if !strings.Contains(bottom, errExists.Error()) {
		t.Errorf("prompt row = %q, want it to show %q", bottom, errExists.Error())
	}
	// The message renders AFTER the buffer so it can't shift the caret.
	if !strings.HasPrefix(bottom, "rename: b.txt") {
		t.Errorf("prompt row = %q, want the buffer before the message", bottom)
	}

	// Typing again clears the message.
	typeKeys(v, "2")
	if v.promptErr != "" {
		t.Error("editing should clear the inline error")
	}
}

// Renaming an expanded directory must not collapse it: EnsureLoaded carries
// state over by name, so without moveNodeInTree/Retarget the rename reads as
// a delete plus a create.
func TestPromptRenameKeepsAnExpandedDirectoryExpanded(t *testing.T) {
	v, root, w := promptFixture(t)
	selectRow(t, v, "sub")
	v.HandleKey(enterKey()) // expand
	v.Render(w)
	if !strings.Contains(strings.Join(w.lines, "\n"), "c.txt") {
		t.Fatal("expected sub expanded with c.txt visible")
	}

	selectRow(t, v, "sub")
	v.HandleKey(layout.Key{Text: "r"})
	for range "sub" {
		v.HandleKey(layout.Key{Named: layout.KeyBackspace})
	}
	typeKeys(v, "pkg")
	v.HandleKey(enterKey())

	if _, err := os.Stat(filepath.Join(root, "pkg", "c.txt")); err != nil {
		t.Fatalf("expected pkg/c.txt on disk: %v", err)
	}
	v.Render(w)
	n := v.rows[v.cursor].Node
	if n.Name != "pkg" {
		t.Fatalf("cursor on %q, want pkg", n.Name)
	}
	if !n.Expanded {
		t.Error("the renamed directory should still be expanded")
	}
	if n.Path != filepath.Join(root, "pkg") {
		t.Errorf("node path = %q, want the new path", n.Path)
	}
	joined := strings.Join(w.lines, "\n")
	if !strings.Contains(joined, "c.txt") {
		t.Errorf("expected the subtree still rendered:\n%s", joined)
	}
	for _, c := range n.Children {
		if want := filepath.Join(root, "pkg", c.Name); c.Path != want {
			t.Errorf("child path = %q, want %q", c.Path, want)
		}
	}
}

func TestPromptDeleteFileConfirmation(t *testing.T) {
	v, root, w := promptFixture(t)
	selectRow(t, v, "a.txt")

	v.HandleKey(layout.Key{Text: "d"})
	v.Render(w)
	if bottom := w.lines[w.rows-1]; !strings.HasPrefix(bottom, "delete a.txt? (y/N)") {
		t.Errorf("prompt row = %q", bottom)
	}

	// Anything but y cancels.
	v.HandleKey(layout.Key{Text: "n"})
	if v.prompt != promptNone {
		t.Error("expected the prompt closed")
	}
	if _, err := os.Stat(filepath.Join(root, "a.txt")); err != nil {
		t.Fatal("\"n\" must not delete")
	}

	var deleted string
	v.OnPathDeleted = func(path string) { deleted = path }
	selectRow(t, v, "a.txt")
	v.HandleKey(layout.Key{Text: "d"})
	v.HandleKey(layout.Key{Text: "y"})

	if _, err := os.Stat(filepath.Join(root, "a.txt")); !os.IsNotExist(err) {
		t.Error("\"y\" should have deleted a.txt")
	}
	if deleted != filepath.Join(root, "a.txt") {
		t.Errorf("OnPathDeleted(%q), want a.txt's absolute path", deleted)
	}
	v.Render(w)
	if v.cursor < 0 || v.cursor >= len(v.rows) {
		t.Errorf("cursor = %d, out of range for %d rows", v.cursor, len(v.rows))
	}
}

func TestPromptDeleteNonEmptyDirectoryNeedsTypedYes(t *testing.T) {
	v, root, w := promptFixture(t)
	selectRow(t, v, "sub")

	v.HandleKey(layout.Key{Text: "d"})
	v.Render(w)
	bottom := w.lines[w.rows-1]
	if !strings.Contains(bottom, "1 entries") {
		t.Errorf("prompt row = %q, want the entry count", bottom)
	}

	// A bare "y" is not enough here: it just types a character.
	v.HandleKey(layout.Key{Text: "y"})
	if v.prompt != promptConfirmYes {
		t.Fatal("a single y should not confirm a recursive delete")
	}
	if _, err := os.Stat(filepath.Join(root, "sub")); err != nil {
		t.Fatal("sub should still be there")
	}

	// A near miss cancels rather than deleting.
	typeKeys(v, "ep")
	v.HandleKey(enterKey())
	if v.prompt != promptNone {
		t.Error("expected the prompt closed")
	}
	if _, err := os.Stat(filepath.Join(root, "sub")); err != nil {
		t.Fatal("\"yep\" must not delete")
	}

	selectRow(t, v, "sub")
	v.HandleKey(layout.Key{Text: "d"})
	typeKeys(v, "yes")
	v.HandleKey(enterKey())

	if _, err := os.Stat(filepath.Join(root, "sub")); !os.IsNotExist(err) {
		t.Error("\"yes\" should have removed sub recursively")
	}
}

func TestPromptDeleteEmptyDirectoryUsesTheSimpleConfirmation(t *testing.T) {
	v, root, w := promptFixture(t)
	if err := os.Mkdir(filepath.Join(root, "empty"), 0o755); err != nil {
		t.Fatal(err)
	}
	v.Refresh()
	v.Render(w)
	selectRow(t, v, "empty")

	v.HandleKey(layout.Key{Text: "d"})
	if v.prompt != promptConfirm {
		t.Fatalf("prompt = %v, want the plain y/N form for an empty directory", v.prompt)
	}
	v.HandleKey(layout.Key{Text: "y"})
	if _, err := os.Stat(filepath.Join(root, "empty")); !os.IsNotExist(err) {
		t.Error("expected the empty directory removed")
	}
}

// Deleting the bottom row — an expanded directory, so a whole subtree
// disappears at once — must leave the cursor in range.
func TestPromptDeleteLastRowKeepsTheCursorInRange(t *testing.T) {
	root := t.TempDir()
	if err := os.Mkdir(filepath.Join(root, "zsub"), 0o755); err != nil {
		t.Fatal(err)
	}
	if err := os.WriteFile(filepath.Join(root, "zsub", "c.txt"), nil, 0o644); err != nil {
		t.Fatal(err)
	}
	if err := os.WriteFile(filepath.Join(root, "a.txt"), nil, 0o644); err != nil {
		t.Fatal(err)
	}
	v := New(root)
	w := newFakeWindow(40, 10)
	v.Render(w)

	selectRow(t, v, "zsub")
	v.HandleKey(enterKey()) // expand, so its child is the last row
	v.Render(w)
	selectRow(t, v, "zsub")

	v.HandleKey(layout.Key{Text: "d"})
	typeKeys(v, "yes")
	v.HandleKey(enterKey())
	v.Render(w)

	if len(v.rows) != 1 {
		t.Fatalf("rows = %d, want 1 (a.txt)", len(v.rows))
	}
	if v.cursor != 0 {
		t.Errorf("cursor = %d, want 0", v.cursor)
	}
}

// The pending operation is remembered by path, not by *Node, so a refresh
// landing mid-prompt (the fsnotify watcher is debounced by 200ms and can
// fire between two keystrokes) can't retarget it.
func TestPromptSurvivesARefreshMidPrompt(t *testing.T) {
	v, root, w := promptFixture(t)
	selectRow(t, v, "a.txt")

	v.HandleKey(layout.Key{Text: "d"})
	v.Refresh() // rebuilds every expanded directory's child Nodes
	v.HandleKey(layout.Key{Text: "y"})
	v.Render(w)

	if _, err := os.Stat(filepath.Join(root, "a.txt")); !os.IsNotExist(err) {
		t.Error("the delete should still have hit a.txt")
	}
	if _, err := os.Stat(filepath.Join(root, "b.txt")); err != nil {
		t.Error("b.txt must not have been touched")
	}
}

func TestCancelPromptIsExportedForFocusChanges(t *testing.T) {
	v, _, _ := promptFixture(t)
	v.HandleKey(layout.Key{Text: "a"})
	typeKeys(v, "half-typed")

	v.CancelPrompt()

	if v.prompt != promptNone || len(v.promptBuf) != 0 {
		t.Error("CancelPrompt should reset the prompt entirely")
	}
	if _, _, ok := v.CursorPosition(); ok {
		t.Error("no cursor after cancelling")
	}
	// And normal navigation works again.
	before := v.cursor
	if !v.HandleKey(downKey()) {
		t.Fatal("expected the key consumed")
	}
	if v.cursor == before {
		t.Error("navigation should work again after cancelling")
	}
}

func TestCreateTargetDirResolution(t *testing.T) {
	v, root, w := promptFixture(t)

	// On a file: its own directory.
	selectRow(t, v, "a.txt")
	if got := v.createTargetDir(); got != root {
		t.Errorf("on a top-level file: %q, want %q", got, root)
	}

	// On a collapsed directory: inside it — you shouldn't have to open a
	// folder to put something in it.
	selectRow(t, v, "sub")
	if want := filepath.Join(root, "sub"); v.createTargetDir() != want {
		t.Errorf("on a collapsed dir: %q, want %q", v.createTargetDir(), want)
	}

	// On a nested file: that file's directory.
	v.HandleKey(enterKey())
	v.Render(w)
	selectRow(t, v, "c.txt")
	if want := filepath.Join(root, "sub"); v.createTargetDir() != want {
		t.Errorf("on a nested file: %q, want %q", v.createTargetDir(), want)
	}

	// Nothing selected: the project root.
	v.cursor = -1
	if got := v.createTargetDir(); got != root {
		t.Errorf("with no selection: %q, want %q", got, root)
	}
}
