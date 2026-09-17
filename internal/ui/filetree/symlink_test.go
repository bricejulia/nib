package filetree

import (
	"os"
	"path/filepath"
	"strings"
	"testing"

	"github.com/bricejulia/nib/internal/layout"
)

func mustSymlink(t *testing.T, target, link string) {
	t.Helper()
	if err := os.Symlink(target, link); err != nil {
		t.Fatalf("os.Symlink(%q, %q): %v", target, link, err)
	}
}

func TestResolveSymlinkChainFileInsideRoot(t *testing.T) {
	root := t.TempDir()
	real := filepath.Join(root, "real.txt")
	if err := os.WriteFile(real, []byte("x"), 0o644); err != nil {
		t.Fatal(err)
	}
	link := filepath.Join(root, "link.txt")
	mustSymlink(t, real, link)

	got, isDir, state := resolveSymlinkChain(root, link)
	if state != LinkOK {
		t.Fatalf("state = %v, want LinkOK", state)
	}
	if isDir {
		t.Error("expected isDir false for a file target")
	}
	if got != real {
		t.Errorf("resolved = %q, want %q", got, real)
	}
}

func TestResolveSymlinkChainDirInsideRoot(t *testing.T) {
	root := t.TempDir()
	realDir := filepath.Join(root, "realdir")
	if err := os.Mkdir(realDir, 0o755); err != nil {
		t.Fatal(err)
	}
	link := filepath.Join(root, "link")
	mustSymlink(t, realDir, link)

	got, isDir, state := resolveSymlinkChain(root, link)
	if state != LinkOK || !isDir || got != realDir {
		t.Errorf("got (%q, %v, %v), want (%q, true, LinkOK)", got, isDir, state, realDir)
	}
}

func TestResolveSymlinkChainBroken(t *testing.T) {
	root := t.TempDir()
	link := filepath.Join(root, "link")
	mustSymlink(t, filepath.Join(root, "does-not-exist"), link)

	_, _, state := resolveSymlinkChain(root, link)
	if state != LinkBroken {
		t.Fatalf("state = %v, want LinkBroken", state)
	}
}

func TestResolveSymlinkChainOutsideRootIsBlocked(t *testing.T) {
	root := t.TempDir()
	outside := t.TempDir()
	target := filepath.Join(outside, "somewhere.txt")
	if err := os.WriteFile(target, []byte("x"), 0o644); err != nil {
		t.Fatal(err)
	}
	link := filepath.Join(root, "link")
	mustSymlink(t, target, link)

	_, _, state := resolveSymlinkChain(root, link)
	if state != LinkBlocked {
		t.Fatalf("state = %v, want LinkBlocked (outside root)", state)
	}
}

func TestResolveSymlinkChainFollowsMultipleHops(t *testing.T) {
	root := t.TempDir()
	real := filepath.Join(root, "real.txt")
	if err := os.WriteFile(real, []byte("x"), 0o644); err != nil {
		t.Fatal(err)
	}
	hop2 := filepath.Join(root, "hop2")
	hop1 := filepath.Join(root, "hop1")
	mustSymlink(t, real, hop2)
	mustSymlink(t, hop2, hop1)

	got, isDir, state := resolveSymlinkChain(root, hop1)
	if state != LinkOK || isDir || got != real {
		t.Errorf("got (%q, %v, %v), want (%q, false, LinkOK)", got, isDir, state, real)
	}
}

func TestResolveSymlinkChainReadlinkLoopIsBlocked(t *testing.T) {
	root := t.TempDir()
	a := filepath.Join(root, "a")
	b := filepath.Join(root, "b")
	mustSymlink(t, b, a)
	mustSymlink(t, a, b)

	_, _, state := resolveSymlinkChain(root, a)
	if state != LinkBlocked {
		t.Fatalf("state = %v, want LinkBlocked (readlink cycle)", state)
	}
}

func TestEnsureLoadedClassifiesFileSymlink(t *testing.T) {
	root := t.TempDir()
	real := filepath.Join(root, "real.txt")
	if err := os.WriteFile(real, []byte("x"), 0o644); err != nil {
		t.Fatal(err)
	}
	mustSymlink(t, real, filepath.Join(root, "link.txt"))

	r := NewRoot(root)
	if err := r.EnsureLoaded(); err != nil {
		t.Fatal(err)
	}
	link := r.child("link.txt")
	if link == nil {
		t.Fatal("expected a link.txt child")
	}
	if !link.IsSymlink || link.IsDir || link.LinkState != LinkOK {
		t.Errorf("link.txt: IsSymlink=%v IsDir=%v LinkState=%v, want true/false/LinkOK",
			link.IsSymlink, link.IsDir, link.LinkState)
	}
	if link.LinkTarget != real {
		t.Errorf("LinkTarget = %q, want %q", link.LinkTarget, real)
	}
}

func TestEnsureLoadedClassifiesDirSymlinkAsExpandable(t *testing.T) {
	root := t.TempDir()
	realDir := filepath.Join(root, "realdir")
	if err := os.MkdirAll(filepath.Join(realDir, "sub"), 0o755); err != nil {
		t.Fatal(err)
	}
	mustSymlink(t, realDir, filepath.Join(root, "link"))

	r := NewRoot(root)
	if err := r.EnsureLoaded(); err != nil {
		t.Fatal(err)
	}
	link := r.child("link")
	if link == nil {
		t.Fatal("expected a link child")
	}
	if !link.IsSymlink || !link.IsDir || link.LinkState != LinkOK {
		t.Fatalf("link: IsSymlink=%v IsDir=%v LinkState=%v, want true/true/LinkOK",
			link.IsSymlink, link.IsDir, link.LinkState)
	}

	// A regression guard for the very gap this feature fixes: before, a
	// symlinked directory was always classified as a flat file.
	if err := link.EnsureLoaded(); err != nil {
		t.Fatal(err)
	}
	if link.child("sub") == nil {
		t.Error("expected the followed symlink to expose its target's children")
	}
}

func TestEnsureLoadedClassifiesBrokenSymlink(t *testing.T) {
	root := t.TempDir()
	mustSymlink(t, filepath.Join(root, "gone"), filepath.Join(root, "link"))

	r := NewRoot(root)
	if err := r.EnsureLoaded(); err != nil {
		t.Fatal(err)
	}
	link := r.child("link")
	if link == nil || !link.IsSymlink || link.IsDir || link.LinkState != LinkBroken {
		t.Fatalf("link: IsSymlink=%v IsDir=%v LinkState=%v, want true/false/LinkBroken",
			link.IsSymlink, link.IsDir, link.LinkState)
	}
}

func TestEnsureLoadedClassifiesOutsideRootSymlinkAsBlocked(t *testing.T) {
	root := t.TempDir()
	outside := t.TempDir()
	if err := os.MkdirAll(filepath.Join(outside, "dir"), 0o755); err != nil {
		t.Fatal(err)
	}
	mustSymlink(t, filepath.Join(outside, "dir"), filepath.Join(root, "link"))

	r := NewRoot(root)
	if err := r.EnsureLoaded(); err != nil {
		t.Fatal(err)
	}
	link := r.child("link")
	// Blocked, and crucially not expandable: IsDir must stay false even
	// though the real target is a directory, or activate() would happily
	// walk outside the project.
	if link == nil || !link.IsSymlink || link.IsDir || link.LinkState != LinkBlocked {
		t.Fatalf("link: IsSymlink=%v IsDir=%v LinkState=%v, want true/false/LinkBlocked",
			link.IsSymlink, link.IsDir, link.LinkState)
	}
}

func TestEnsureLoadedDetectsAncestorCycle(t *testing.T) {
	root := t.TempDir()
	sub := filepath.Join(root, "sub")
	if err := os.Mkdir(sub, 0o755); err != nil {
		t.Fatal(err)
	}
	// "sub/loop" points back at "sub" itself — expanding it would show
	// sub's own contents (including loop) again, forever.
	mustSymlink(t, sub, filepath.Join(sub, "loop"))

	r := NewRoot(root)
	if err := r.EnsureLoaded(); err != nil {
		t.Fatal(err)
	}
	subNode := r.child("sub")
	if err := subNode.EnsureLoaded(); err != nil {
		t.Fatal(err)
	}
	loop := subNode.child("loop")
	if loop == nil || !loop.IsSymlink || loop.IsDir || loop.LinkState != LinkBlocked {
		t.Fatalf("loop: IsSymlink=%v IsDir=%v LinkState=%v, want true/false/LinkBlocked",
			loop.IsSymlink, loop.IsDir, loop.LinkState)
	}
}

// symlinkFixture builds a temp project with one of each kind of symlink
// the tree distinguishes, and returns a rendered View.
func symlinkFixture(t *testing.T) (*View, string) {
	t.Helper()
	root := t.TempDir()
	if err := os.WriteFile(filepath.Join(root, "real.txt"), []byte("x"), 0o644); err != nil {
		t.Fatal(err)
	}
	realDir := filepath.Join(root, "realdir")
	if err := os.Mkdir(realDir, 0o755); err != nil {
		t.Fatal(err)
	}
	if err := os.WriteFile(filepath.Join(realDir, "child.txt"), []byte("x"), 0o644); err != nil {
		t.Fatal(err)
	}
	outside := t.TempDir()

	mustSymlink(t, filepath.Join(root, "real.txt"), filepath.Join(root, "ok-link"))
	mustSymlink(t, realDir, filepath.Join(root, "ok-dir-link"))
	mustSymlink(t, filepath.Join(root, "missing"), filepath.Join(root, "broken-link"))
	mustSymlink(t, outside, filepath.Join(root, "blocked-link"))

	v := New(root)
	w := newFakeWindow(60, 10)
	v.Render(w)
	return v, root
}

func TestActivateOpensFollowedFileSymlinkWithItsOwnPath(t *testing.T) {
	v, root := symlinkFixture(t)
	var opened string
	v.OnOpen = func(path string) { opened = path }
	selectRow(t, v, "ok-link")

	v.HandleKey(enterKey())

	want := filepath.Join(root, "ok-link")
	if opened != want {
		t.Errorf("OnOpen called with %q, want the symlink's own path %q", opened, want)
	}
}

func TestActivateExpandsFollowedDirSymlinkAndFiresOnDirExpandedOnce(t *testing.T) {
	v, root := symlinkFixture(t)
	var expanded []string
	v.OnDirExpanded = func(real string) { expanded = append(expanded, real) }
	selectRow(t, v, "ok-dir-link")

	v.HandleKey(enterKey())
	v.ensureFresh()

	n := v.rows[v.cursor].Node
	if !n.Expanded || !n.Loaded {
		t.Fatal("expected the followed dir symlink to expand and load")
	}
	found := false
	for _, row := range v.rows {
		if row.Node.Name == "child.txt" {
			found = true
		}
	}
	if !found {
		t.Error("expected the symlinked directory's own child to appear in the flattened rows")
	}

	wantReal := filepath.Join(root, "realdir")
	if len(expanded) != 1 || expanded[0] != wantReal {
		t.Fatalf("OnDirExpanded calls = %v, want exactly one call with %q", expanded, wantReal)
	}

	// Collapsing and re-expanding must not fire it again — it's a "first
	// load" hook, not a toggle hook.
	v.HandleKey(enterKey()) // collapse
	v.HandleKey(enterKey()) // re-expand
	if len(expanded) != 1 {
		t.Errorf("OnDirExpanded fired %d times, want exactly 1 (only the first load)", len(expanded))
	}
}

func TestActivateRefusesBrokenSymlink(t *testing.T) {
	v, _ := symlinkFixture(t)
	opened := false
	v.OnOpen = func(string) { opened = true }
	selectRow(t, v, "broken-link")

	v.HandleKey(enterKey())

	if opened {
		t.Error("OnOpen must not be called for a broken symlink")
	}
	if !strings.Contains(v.BlockedNotice(), "broken-link") || !strings.Contains(v.BlockedNotice(), "missing") {
		t.Errorf("BlockedNotice = %q, want it to name the row and say the target is missing", v.BlockedNotice())
	}
}

func TestActivateRefusesOutsideRootSymlink(t *testing.T) {
	v, _ := symlinkFixture(t)
	opened := false
	v.OnOpen = func(string) { opened = true }
	selectRow(t, v, "blocked-link")

	v.HandleKey(enterKey())

	if opened {
		t.Error("OnOpen must not be called for a symlink whose target is outside the project")
	}
	if v.BlockedNotice() == "" {
		t.Error("expected a BlockedNotice explaining why the link wasn't followed")
	}
}

func TestBlockedNoticeIsOneShot(t *testing.T) {
	v, _ := symlinkFixture(t)
	selectRow(t, v, "broken-link")
	v.HandleKey(enterKey())
	if v.BlockedNotice() == "" {
		t.Fatal("expected a notice right after the refusal")
	}

	v.HandleKey(downKey()) // any subsequent key clears it
	if v.BlockedNotice() != "" {
		t.Errorf("expected BlockedNotice cleared after the next keypress, got %q", v.BlockedNotice())
	}
}

func TestBeginDeleteOnFollowedDirSymlinkUsesSingleConfirmation(t *testing.T) {
	v, root := symlinkFixture(t)
	selectRow(t, v, "ok-dir-link")

	v.HandleKey(layout.Key{Text: "d"})

	if v.prompt != promptConfirm {
		t.Fatalf("prompt = %v, want promptConfirm — a symlink is never a recursive delete even when it points at a non-empty directory", v.prompt)
	}
	if !strings.Contains(v.promptLabel(), "symlink") {
		t.Errorf("promptLabel() = %q, want it to say this deletes a symlink", v.promptLabel())
	}

	v.HandleKey(layout.Key{Text: "y"})

	if _, err := os.Lstat(filepath.Join(root, "ok-dir-link")); !os.IsNotExist(err) {
		t.Error("expected the symlink itself to be gone")
	}
	if _, err := os.Stat(filepath.Join(root, "realdir", "child.txt")); err != nil {
		t.Errorf("expected the symlink's target to be untouched, got: %v", err)
	}
}

func TestFormatRowShowsSymlinkGlyphAndTargetOnlyWhenFocused(t *testing.T) {
	v, _ := symlinkFixture(t)
	selectRow(t, v, "ok-link")
	focused := formatRow(v.rows[v.cursor], true)
	unfocused := formatRow(v.rows[v.cursor], false)

	if !strings.Contains(focused, "->") {
		t.Errorf("focused row = %q, want it to show the symlink target", focused)
	}
	if strings.Contains(unfocused, "->") {
		t.Errorf("unfocused row = %q, want no target suffix", unfocused)
	}
	if !strings.Contains(focused, "→") && !strings.Contains(unfocused, "→") {
		t.Error("expected the symlink glyph (→) in the icon slot regardless of focus")
	}
}
