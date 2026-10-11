package workspace

import (
	"os"
	"path/filepath"
	"slices"
	"testing"

	"github.com/bricejulia/nib/internal/layout"
	"github.com/bricejulia/nib/internal/ui/editor"
	"github.com/bricejulia/nib/internal/vcs/gitstatus"
)

// fixture is a project folder in t.TempDir() plus a Workspace on it, with
// a LineStatus that counts calls instead of running git.
type fixture struct {
	t        *testing.T
	root     string
	ws       *Workspace
	gutterOf map[string]int
}

func newFixture(t *testing.T, opts Options) *fixture {
	t.Helper()
	f := &fixture{t: t, root: t.TempDir(), gutterOf: map[string]int{}}
	opts.Root = f.root
	opts.FirstLeafID = 10
	opts.LineStatus = func(path string) (map[int]gitstatus.LineStatus, error) {
		f.gutterOf[path]++
		return nil, nil
	}
	f.ws = New(opts)
	return f
}

// file writes name (relative to the project root) with content and
// returns its absolute path.
func (f *fixture) file(name, content string) string {
	f.t.Helper()
	path := filepath.Join(f.root, name)
	if err := os.MkdirAll(filepath.Dir(path), 0o755); err != nil {
		f.t.Fatal(err)
	}
	if err := os.WriteFile(path, []byte(content), 0o644); err != nil {
		f.t.Fatal(err)
	}
	return path
}

// edit makes path's buffer in the active pane dirty.
func (f *fixture) edit(path string) {
	f.t.Helper()
	buf := f.ws.Active().BufferForPath(path)
	if buf == nil {
		f.t.Fatalf("%s is not open in the active pane", path)
	}
	buf.InsertText(0, 0, "edited ")
}

// focus makes leaf id the focused pane, as the app's focus-change handler
// would.
func (f *fixture) focus(id layout.LeafID) {
	f.ws.FocusChanged(id)
}

func (f *fixture) split(dir layout.Direction) layout.LeafID {
	f.t.Helper()
	id, ok := f.ws.Split(dir)
	if !ok {
		f.t.Fatal("Split failed")
	}
	return id
}

func readFile(t *testing.T, path string) string {
	t.Helper()
	b, err := os.ReadFile(path)
	if err != nil {
		t.Fatal(err)
	}
	return string(b)
}

func TestOpenShowsFileInActivePaneAndRefreshesItsGutter(t *testing.T) {
	f := newFixture(t, Options{})
	a := f.file("a.txt", "alpha\n")

	leaf := f.ws.Open(a)

	if leaf != f.ws.ActiveLeaf() {
		t.Errorf("Open returned leaf %d, want the active pane %d", leaf, f.ws.ActiveLeaf())
	}
	if got := f.ws.Active().ActivePath(); got != a {
		t.Errorf("active path = %q, want %q", got, a)
	}
	if f.gutterOf[a] != 1 {
		t.Errorf("LineStatus called %d times for %s, want 1", f.gutterOf[a], a)
	}
}

func TestSplitShowsTheSameBufferInTheNewPane(t *testing.T) {
	f := newFixture(t, Options{})
	a := f.file("a.txt", "alpha\n")
	f.ws.Open(a)
	first := f.ws.Active()

	id := f.split(layout.Horizontal)
	f.focus(id)

	if f.ws.Active() == first {
		t.Fatal("focusing the new pane should make it active")
	}
	if got := f.ws.Active().ActivePath(); got != a {
		t.Errorf("new pane shows %q, want %q", got, a)
	}
	if first.BufferForPath(a) != f.ws.Active().BufferForPath(a) {
		t.Error("both panes should share one Buffer for the same file")
	}
}

func TestRepathRetargetsTabsInEveryPane(t *testing.T) {
	f := newFixture(t, Options{})
	a := f.file("dir/a.txt", "alpha\n")
	f.ws.Open(a)
	f.focus(f.split(layout.Vertical))

	newDir := filepath.Join(f.root, "renamed")
	if err := os.Rename(filepath.Join(f.root, "dir"), newDir); err != nil {
		t.Fatal(err)
	}
	f.ws.Repath(filepath.Join(f.root, "dir"), newDir)

	moved := filepath.Join(newDir, "a.txt")
	v, ok := f.ws.FindView(moved)
	if !ok {
		t.Fatalf("no pane has %s open after the rename", moved)
	}
	if _, ok := f.ws.FindView(a); ok {
		t.Errorf("a pane still has the old path %s open", a)
	}
	if v.BufferForPath(moved) == nil {
		t.Error("renamed tab lost its buffer")
	}
}

func TestCloseDeletedClosesCleanTabsAndDetachesDirtyOnes(t *testing.T) {
	f := newFixture(t, Options{})
	clean := f.file("clean.txt", "c\n")
	dirty := f.file("dirty.txt", "d\n")
	f.ws.Open(clean)
	f.ws.Open(dirty)
	f.edit(dirty)

	for _, p := range []string{clean, dirty} {
		if err := os.Remove(p); err != nil {
			t.Fatal(err)
		}
		f.ws.CloseDeleted(p)
	}

	if got := f.ws.Active().OpenPaths(); !slices.Equal(got, []string{dirty}) {
		t.Errorf("open paths = %v, want only the dirty file", got)
	}
	if got := f.ws.Active().DetachedPaths(); !slices.Equal(got, []string{dirty}) {
		t.Errorf("detached paths = %v, want the dirty file", got)
	}
}

func TestSyncWithDiskReloadsCleanFlagsDirtyAndClosesDeleted(t *testing.T) {
	f := newFixture(t, Options{})
	clean := f.file("clean.txt", "old\n")
	dirty := f.file("dirty.txt", "old\n")
	gone := f.file("gone.txt", "old\n")
	for _, p := range []string{clean, dirty, gone} {
		f.ws.Open(p)
	}
	f.edit(dirty)

	f.file("clean.txt", "changed outside nib\n")
	f.file("dirty.txt", "changed outside nib\n")
	if err := os.Remove(gone); err != nil {
		t.Fatal(err)
	}
	f.ws.SyncWithDisk()

	v := f.ws.Active()
	if buf := v.BufferForPath(clean); buf == nil || buf.Lines[0] != "changed outside nib" {
		t.Errorf("clean buffer should have been reloaded, got %v", buf)
	}
	buf := v.BufferForPath(dirty)
	if buf == nil || !buf.ExternallyModified {
		t.Fatal("dirty buffer should be flagged as changed on disk")
	}
	if buf.Lines[0] != "edited old" {
		t.Errorf("dirty buffer's edits were lost: %q", buf.Lines[0])
	}
	if v.BufferForPath(gone) != nil {
		t.Error("the deleted file's clean tab should have closed")
	}
}

func TestDirtyPathsIsDeduplicatedAndSorted(t *testing.T) {
	f := newFixture(t, Options{})
	b := f.file("b.txt", "b\n")
	a := f.file("a.txt", "a\n")
	f.ws.Open(b)
	f.ws.Open(a)
	f.edit(a)
	f.edit(b)
	f.split(layout.Horizontal) // a is now open in two panes

	if got, want := f.ws.DirtyPaths(), []string{a, b}; !slices.Equal(got, want) {
		t.Errorf("DirtyPaths = %v, want %v", got, want)
	}
}

func TestSaveAllWritesOnceAndReturnsConflicts(t *testing.T) {
	f := newFixture(t, Options{})
	a := f.file("a.txt", "a")
	b := f.file("b.txt", "b")
	f.ws.Open(b)
	f.ws.Open(a)
	f.edit(a)
	f.edit(b)
	f.split(layout.Horizontal)
	f.file("b.txt", "changed outside nib\n")

	conflicts := f.ws.SaveAll()

	if len(conflicts) != 1 || conflicts[0].Path != b {
		t.Fatalf("conflicts = %v, want exactly %s", conflicts, b)
	}
	if got := readFile(t, a); got != "edited a" {
		t.Errorf("a.txt = %q, want the saved edit", got)
	}
	if got := f.ws.DirtyPaths(); !slices.Equal(got, []string{b}) {
		t.Errorf("still dirty = %v, want only the conflicted file", got)
	}

	f.ws.KeepMine(conflicts[0])
	if got := readFile(t, b); got != "edited b" {
		t.Errorf("after KeepMine b.txt = %q, want the buffer's content", got)
	}
}

func TestReloadFromDiskDiscardsEdits(t *testing.T) {
	f := newFixture(t, Options{})
	a := f.file("a.txt", "a\n")
	f.ws.Open(a)
	f.edit(a)
	f.file("a.txt", "from disk\n")

	f.ws.ReloadFromDisk(editor.SaveConflict{Path: a, Buf: f.ws.Active().BufferForPath(a)})

	if buf := f.ws.Active().BufferForPath(a); buf.Dirty || buf.Lines[0] != "from disk" {
		t.Errorf("buffer = %q dirty=%v, want disk content, clean", buf.Lines[0], buf.Dirty)
	}
}

func TestClosePaneRefusesTheLastPaneAndUnsavedChanges(t *testing.T) {
	f := newFixture(t, Options{})
	if _, ok := f.ws.ClosePane(); ok {
		t.Fatal("closing the only pane should be refused")
	}

	a := f.file("a.txt", "a\n")
	b := f.file("b.txt", "b\n")
	f.ws.Open(a)
	first := f.ws.ActiveLeaf()
	second := f.split(layout.Horizontal)
	f.focus(second)
	f.ws.Open(b)
	f.edit(b)

	if _, ok := f.ws.ClosePane(); ok {
		t.Fatal("closing a pane with unsaved changes should be refused")
	}

	if err := f.ws.Active().BufferForPath(b).Save(); err != nil {
		t.Fatal(err)
	}
	next, ok := f.ws.ClosePane()
	if !ok {
		t.Fatal("closing a clean pane should succeed")
	}
	if next != first || f.ws.ActiveLeaf() != first {
		t.Errorf("focus goes to %d (active %d), want the surviving pane %d", next, f.ws.ActiveLeaf(), first)
	}
	if f.ws.IsPane(second) {
		t.Error("the closed pane is still registered")
	}
	if got := layout.Leaves(f.ws.Root()); len(got) != 1 || got[0].ID != first {
		t.Errorf("layout leaves = %v, want just the surviving pane", got)
	}
}

func TestClosingAPaneReleasesItsBuffers(t *testing.T) {
	f := newFixture(t, Options{})
	a := f.file("a.txt", "a\n")
	b := f.file("b.txt", "old\n")
	f.ws.Open(a)
	f.focus(f.split(layout.Horizontal))
	f.ws.Open(b)
	if _, ok := f.ws.ClosePane(); !ok {
		t.Fatal("ClosePane failed")
	}

	f.file("b.txt", "new\n")
	f.ws.Open(b)

	if got := f.ws.Active().BufferForPath(b).Lines[0]; got != "new" {
		t.Errorf("reopened b.txt shows %q, want a fresh load from disk", got)
	}
}

func TestMoveActiveTabWrapsAroundPanes(t *testing.T) {
	f := newFixture(t, Options{})
	a := f.file("a.txt", "a\n")
	b := f.file("b.txt", "b\n")
	f.ws.Open(a)
	first := f.ws.ActiveLeaf()
	second := f.split(layout.Horizontal)
	f.ws.Open(b) // still in the first pane, which is active

	dest, ok := f.ws.MoveActiveTab(1)
	if !ok || dest != second {
		t.Fatalf("MoveActiveTab(1) = %d, %v; want the second pane %d", dest, ok, second)
	}
	f.focus(second)
	if f.ws.Active().ActivePath() != b {
		t.Fatalf("second pane's active tab = %q, want %q", f.ws.Active().ActivePath(), b)
	}

	if dest, ok := f.ws.MoveActiveTab(1); !ok || dest != first {
		t.Errorf("MoveActiveTab(1) from the last pane = %d, %v; want to wrap to %d", dest, ok, first)
	}
}

func TestTargetFollowsFocusAndFallsBackToActive(t *testing.T) {
	var focused layout.LeafID
	f := newFixture(t, Options{Focused: func() (layout.LeafID, bool) { return focused, true }})
	first := f.ws.Active()
	second := f.split(layout.Horizontal)

	focused = second // focused, but the focus-change handler hasn't run yet
	if f.ws.Target() == first {
		t.Error("Target should be the focused pane")
	}
	focused = 1 // e.g. the file tree
	if f.ws.Target() != first {
		t.Error("Target should fall back to the active pane when focus is elsewhere")
	}
}

func TestTabMenuMoveRightReportsWhereTheTabLanded(t *testing.T) {
	var rects map[layout.LeafID]layout.Rect
	f := newFixture(t, Options{Rects: func() map[layout.LeafID]layout.Rect { return rects }})
	var changed []layout.LeafID
	f.ws.OnLayoutChanged = func(id layout.LeafID) { changed = append(changed, id) }

	a := f.file("a.txt", "a\n")
	f.ws.Open(a)
	left := f.ws.Active()
	leftID := f.ws.ActiveLeaf()
	right := f.split(layout.Horizontal)
	rects = map[layout.LeafID]layout.Rect{
		leftID: {X: 0, Y: 0, W: 40, H: 20},
		right:  {X: 40, Y: 0, W: 40, H: 20},
	}

	if !left.CanMoveRight() {
		t.Fatal("the left pane should offer Move Right")
	}
	if left.CanMoveDown() {
		t.Error("there is no pane below, so Move Down should be hidden")
	}
	left.OnMoveRight(0)

	if !slices.Equal(changed, []layout.LeafID{right}) {
		t.Errorf("OnLayoutChanged calls = %v, want [%d]", changed, right)
	}
	if len(left.OpenPaths()) != 0 {
		t.Errorf("left pane still has %v open", left.OpenPaths())
	}
}

func TestTabMenuSplitRightMovesOnlyThatTab(t *testing.T) {
	f := newFixture(t, Options{})
	var changed []layout.LeafID
	f.ws.OnLayoutChanged = func(id layout.LeafID) { changed = append(changed, id) }
	a := f.file("a.txt", "a\n")
	b := f.file("b.txt", "b\n")
	f.ws.Open(a)
	f.ws.Open(b)
	v := f.ws.Active()

	v.OnSplitAndMoveRight(0) // a.txt

	if len(changed) != 1 || !f.ws.IsPane(changed[0]) {
		t.Fatalf("OnLayoutChanged calls = %v, want one new pane", changed)
	}
	if got := v.OpenPaths(); !slices.Equal(got, []string{b}) {
		t.Errorf("original pane has %v open, want only %s", got, b)
	}
	f.focus(changed[0])
	if got := f.ws.Active().OpenPaths(); !slices.Equal(got, []string{a}) {
		t.Errorf("new pane has %v open, want only %s", got, a)
	}
}

func TestWireRunsOnExistingAndFuturePanes(t *testing.T) {
	f := newFixture(t, Options{})
	var wired []*editor.View
	f.ws.Wire(func(v *editor.View) { wired = append(wired, v) })
	f.focus(f.split(layout.Vertical))

	if len(wired) != 2 || wired[1] != f.ws.Active() {
		t.Errorf("Wire ran on %d panes, want the first one and the new split", len(wired))
	}
}

func TestSessionRoundTrip(t *testing.T) {
	f := newFixture(t, Options{})
	a := f.file("a.txt", "a\n")
	b := f.file("b.txt", "b\n")
	c := f.file("c.txt", "c\n")
	gone := f.file("gone.txt", "g\n")
	f.ws.Open(a)
	f.ws.Open(gone)
	f.ws.Open(b)
	second := f.split(layout.Vertical)
	f.focus(second)
	f.ws.Open(c)

	sess := f.ws.Capture()
	if sess == nil {
		t.Fatal("Capture returned nil with files open")
	}
	if err := os.Remove(gone); err != nil {
		t.Fatal(err)
	}

	restored := New(Options{Root: f.root, FirstLeafID: 10})
	focus, ok := restored.Restore(sess)
	if !ok {
		t.Fatal("Restore failed")
	}
	if focus != restored.ActiveLeaf() {
		t.Errorf("Restore focused %d but active is %d", focus, restored.ActiveLeaf())
	}
	if got := restored.Active().OpenPaths(); !slices.Equal(got, []string{b, c}) {
		t.Errorf("focused pane has %v open, want %v", got, []string{b, c})
	}
	if got := restored.Active().ActivePath(); got != c {
		t.Errorf("focused pane's active tab = %q, want %q", got, c)
	}
	leaves := layout.Leaves(restored.Root())
	if len(leaves) != 2 {
		t.Fatalf("restored %d panes, want 2", len(leaves))
	}
	other, ok := restored.FindView(a)
	if !ok || slices.Contains(other.OpenPaths(), gone) {
		t.Errorf("first pane should have a.txt and b.txt without the deleted file, got %v", other.OpenPaths())
	}
}

func TestRestoreWithNothingLeftChangesNothing(t *testing.T) {
	f := newFixture(t, Options{})
	gone := f.file("gone.txt", "g\n")
	f.ws.Open(gone)
	sess := f.ws.Capture()
	if err := os.Remove(gone); err != nil {
		t.Fatal(err)
	}

	g := New(Options{Root: f.root, FirstLeafID: 10})
	root := g.Root()
	if _, ok := g.Restore(sess); ok {
		t.Error("Restore should report nothing restored")
	}
	if g.Root() != root || len(layout.Leaves(root)) != 1 {
		t.Error("Restore with nothing left should keep the single empty pane")
	}
}
