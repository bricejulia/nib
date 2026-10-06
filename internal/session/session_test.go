package session

import (
	"os"
	"path/filepath"
	"reflect"
	"testing"

	"github.com/bricejulia/nib/internal/layout"
)

func TestTrustCreatesDirWithGitignore(t *testing.T) {
	root := t.TempDir()
	if Trusted(root) {
		t.Fatal("a fresh folder must not be trusted")
	}
	if err := Trust(root); err != nil {
		t.Fatal(err)
	}
	if !Trusted(root) {
		t.Fatal("expected the folder to be trusted after Trust")
	}
	data, err := os.ReadFile(filepath.Join(root, DirName, ".gitignore"))
	if err != nil {
		t.Fatal(err)
	}
	if string(data) != "*\n" {
		t.Errorf(".gitignore: got %q, want %q", data, "*\n")
	}
}

func TestLoadMissingIsNotAnError(t *testing.T) {
	root := t.TempDir()
	s, err := Load(root)
	if s != nil || err != nil {
		t.Fatalf("got (%v, %v), want (nil, nil)", s, err)
	}
}

func TestLoadCorruptOrUnknownVersionErrors(t *testing.T) {
	for name, content := range map[string]string{
		"corrupt": "{not json",
		"version": `{"version": 99}`,
	} {
		t.Run(name, func(t *testing.T) {
			root := t.TempDir()
			if err := Trust(root); err != nil {
				t.Fatal(err)
			}
			if err := os.WriteFile(filepath.Join(root, DirName, fileName), []byte(content), 0o644); err != nil {
				t.Fatal(err)
			}
			if _, err := Load(root); err == nil {
				t.Fatal("expected an error")
			}
		})
	}
}

func TestSaveLoadRoundTrip(t *testing.T) {
	root := t.TempDir()
	if err := Trust(root); err != nil {
		t.Fatal(err)
	}
	want := &Session{
		Focused: 1,
		Layout: &Node{Split: "horizontal", Children: []*Node{
			{Pane: &Pane{Active: 0, Tabs: []Tab{{Path: "a.go", Line: 3, Col: 2, Top: 1}}}},
			{Pane: &Pane{Active: 1, Tabs: []Tab{{Path: "b.go"}, {Path: "dir/c.go", Line: 10}}}},
		}},
	}
	if err := Save(root, want); err != nil {
		t.Fatal(err)
	}
	got, err := Load(root)
	if err != nil {
		t.Fatal(err)
	}
	if !reflect.DeepEqual(got, want) {
		t.Errorf("got %+v, want %+v", got, want)
	}

	if err := Save(root, nil); err != nil {
		t.Fatal(err)
	}
	if got, err := Load(root); got != nil || err != nil {
		t.Errorf("after Save(nil): got (%v, %v), want (nil, nil)", got, err)
	}
	entries, _ := os.ReadDir(Dir(root))
	for _, e := range entries {
		if filepath.Ext(e.Name()) == ".tmp" {
			t.Errorf("leftover temp file %s", e.Name())
		}
	}
}

func TestRelAbsPath(t *testing.T) {
	root := filepath.FromSlash("/proj")
	in := filepath.Join(root, "dir", "a.go")
	rel := RelPath(root, in)
	if rel != "dir/a.go" {
		t.Errorf("RelPath: got %q", rel)
	}
	if got := AbsPath(root, rel); got != in {
		t.Errorf("AbsPath: got %q, want %q", got, in)
	}
	outside := filepath.FromSlash("/elsewhere/b.go")
	if got := RelPath(root, outside); got != outside {
		t.Errorf("RelPath outside root: got %q, want it kept absolute", got)
	}
	if got := AbsPath(root, RelPath(root, outside)); got != outside {
		t.Errorf("AbsPath of an absolute path: got %q", got)
	}
}

// appTree mirrors cmd/nib/main.go's tree: a file tree beside the editor
// area, above a status bar.
func appTree(editor layout.Node) *layout.SplitNode {
	return &layout.SplitNode{Dir: layout.Vertical, Children: []layout.Child{
		{Node: &layout.SplitNode{Dir: layout.Horizontal, Children: []layout.Child{
			{Node: &layout.LeafNode{ID: 1}, Hint: layout.Fixed(50)},
			{Node: editor, Hint: layout.Ratio(1)},
		}}, Hint: layout.Ratio(1)},
		{Node: &layout.LeafNode{ID: 2}, Hint: layout.Fixed(1)},
	}}
}

func TestCaptureSkipsNonEditorLeavesAndCollapsesSplits(t *testing.T) {
	editor := &layout.SplitNode{Dir: layout.Vertical, Children: []layout.Child{
		{Node: &layout.LeafNode{ID: 10}, Hint: layout.Ratio(1)},
		{Node: &layout.SplitNode{Dir: layout.Horizontal, Children: []layout.Child{
			{Node: &layout.LeafNode{ID: 11}, Hint: layout.Ratio(1)},
			{Node: &layout.LeafNode{ID: 12}, Hint: layout.Ratio(1)}, // empty pane
		}}, Hint: layout.Ratio(1)},
	}}
	panes := map[layout.LeafID]*Pane{
		10: {Tabs: []Tab{{Path: "a.go"}}},
		11: {Tabs: []Tab{{Path: "b.go"}}},
	}
	s := Capture(appTree(editor), func(l *layout.LeafNode) (*Pane, bool) {
		p, ok := panes[l.ID]
		return p, ok
	}, 11)

	want := &Node{Split: "vertical", Children: []*Node{{Pane: panes[10]}, {Pane: panes[11]}}}
	if !reflect.DeepEqual(s.Layout, want) {
		t.Errorf("layout: got %+v, want %+v", s.Layout, want)
	}
	if s.Focused != 1 {
		t.Errorf("focused: got %d, want 1", s.Focused)
	}
}

func TestCaptureNothingOpenIsNil(t *testing.T) {
	s := Capture(appTree(&layout.LeafNode{ID: 10}), func(*layout.LeafNode) (*Pane, bool) { return nil, false }, 10)
	if s != nil {
		t.Errorf("got %+v, want nil", s)
	}
}

func TestBuildRestoresShapeAndFocus(t *testing.T) {
	s := &Session{Focused: 2, Layout: &Node{Split: "horizontal", Children: []*Node{
		{Pane: &Pane{Tabs: []Tab{{Path: "a.go"}}}},
		{Split: "vertical", Children: []*Node{
			{Pane: &Pane{Tabs: []Tab{{Path: "b.go"}}}},
			{Pane: &Pane{Tabs: []Tab{{Path: "c.go"}}}},
		}},
	}}}
	next := layout.LeafID(100)
	built := map[layout.LeafID]string{}
	root, focused := Build(s, func(p Pane) *layout.LeafNode {
		l := &layout.LeafNode{ID: next}
		built[next] = p.Tabs[0].Path
		next++
		return l
	})

	top, ok := root.(*layout.SplitNode)
	if !ok || top.Dir != layout.Horizontal || len(top.Children) != 2 {
		t.Fatalf("expected a horizontal split of 2, got %#v", root)
	}
	inner, ok := top.Children[1].Node.(*layout.SplitNode)
	if !ok || inner.Dir != layout.Vertical || len(inner.Children) != 2 {
		t.Fatalf("expected a nested vertical split of 2, got %#v", top.Children[1].Node)
	}
	if built[focused.ID] != "c.go" {
		t.Errorf("focused pane: got %q, want c.go", built[focused.ID])
	}
}

func TestBuildPrunesDroppedPanes(t *testing.T) {
	s := &Session{Focused: 1, Layout: &Node{Split: "horizontal", Children: []*Node{
		{Pane: &Pane{Tabs: []Tab{{Path: "keep.go"}}}},
		{Pane: &Pane{Tabs: []Tab{{Path: "gone.go"}}}},
	}}}
	root, focused := Build(s, func(p Pane) *layout.LeafNode {
		if p.Tabs[0].Path == "gone.go" {
			return nil
		}
		return &layout.LeafNode{ID: 1}
	})
	leaf, ok := root.(*layout.LeafNode)
	if !ok {
		t.Fatalf("expected the split to collapse into its surviving leaf, got %#v", root)
	}
	if focused != leaf {
		t.Error("expected focus to fall back to the surviving pane")
	}

	root, focused = Build(s, func(Pane) *layout.LeafNode { return nil })
	if root != nil || focused != nil {
		t.Errorf("all panes dropped: got (%v, %v), want (nil, nil)", root, focused)
	}
}
