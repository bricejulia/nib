// Package session persists a project's editor workspace — which files are
// open in which pane, where each cursor sits, and how the panes are split —
// into a hidden .nib folder at the project root, so reopening nib there
// picks up where the last run left off.
//
// The .nib folder doubles as the project's trust marker: nib only ever
// writes into a project the user explicitly trusted (see Trust), and a
// project without one is simply untrusted — there is no global list of
// trusted or untrusted folders.
//
// The package knows nothing about editor views: cmd/nib/main.go converts
// between editor.TabState and Tab, and walks/rebuilds the layout tree
// through Capture and Build.
package session

import (
	"encoding/json"
	"errors"
	"fmt"
	"io/fs"
	"os"
	"path/filepath"
	"strings"

	"github.com/bricejulia/nib/internal/layout"
)

// DirName is the per-project folder nib keeps its state in.
const DirName = ".nib"

const (
	fileName = "session.json"
	version  = 1
)

// Session is the on-disk shape of .nib/session.json.
type Session struct {
	Version int `json:"version"`
	// Focused is the index, in Layout's left-to-right/top-to-bottom pane
	// order, of the pane that had focus.
	Focused int   `json:"focused"`
	Layout  *Node `json:"layout"`
}

// Node is either a pane (Pane set) or a split (Split set to "horizontal"
// or "vertical", with two or more Children).
type Node struct {
	Split    string  `json:"split,omitempty"`
	Children []*Node `json:"children,omitempty"`
	Pane     *Pane   `json:"pane,omitempty"`
}

// Pane is one editor pane's tabs, in tab-bar order, and which one was
// active.
type Pane struct {
	Active int   `json:"active"`
	Tabs   []Tab `json:"tabs"`
}

// Tab is one open file. Path is relative to the project root, slash
// separated, so the project can be moved without losing its session; Line
// and Col are the 0-based cursor position and Top the first visible line.
type Tab struct {
	Path string `json:"path"`
	Line int    `json:"line"`
	Col  int    `json:"col"`
	Top  int    `json:"top"`
}

// Dir returns root's .nib folder.
func Dir(root string) string { return filepath.Join(root, DirName) }

// Trusted reports whether root has a .nib folder, i.e. whether the user
// already agreed to let nib keep state there.
func Trusted(root string) bool {
	info, err := os.Stat(Dir(root))
	return err == nil && info.IsDir()
}

// Trust creates root's .nib folder, with a .gitignore ignoring everything
// in it — the session is personal, never something to commit, and being
// git-ignored also keeps it out of the finder and the file watcher.
func Trust(root string) error {
	dir := Dir(root)
	if err := os.MkdirAll(dir, 0o755); err != nil {
		return err
	}
	return os.WriteFile(filepath.Join(dir, ".gitignore"), []byte("*\n"), 0o644)
}

// Load reads root's session. A missing file is not an error: it returns
// (nil, nil), the same as a freshly trusted project with nothing saved yet.
func Load(root string) (*Session, error) {
	data, err := os.ReadFile(filepath.Join(Dir(root), fileName))
	if errors.Is(err, fs.ErrNotExist) {
		return nil, nil
	}
	if err != nil {
		return nil, err
	}
	var s Session
	if err := json.Unmarshal(data, &s); err != nil {
		return nil, fmt.Errorf("parse %s: %w", fileName, err)
	}
	if s.Version != version {
		return nil, fmt.Errorf("%s: unsupported version %d", fileName, s.Version)
	}
	return &s, nil
}

// Save writes s as root's session, atomically (a temp file renamed into
// place) so a crash mid-write can never leave a truncated session behind.
// A nil s removes any saved session instead.
func Save(root string, s *Session) error {
	path := filepath.Join(Dir(root), fileName)
	if s == nil {
		if err := os.Remove(path); err != nil && !errors.Is(err, fs.ErrNotExist) {
			return err
		}
		return nil
	}
	s.Version = version
	data, err := json.MarshalIndent(s, "", "  ")
	if err != nil {
		return err
	}
	tmp, err := os.CreateTemp(Dir(root), fileName+".*.tmp")
	if err != nil {
		return err
	}
	defer func() { _ = os.Remove(tmp.Name()) }() // no-op once renamed
	if _, err := tmp.Write(append(data, '\n')); err != nil {
		_ = tmp.Close()
		return err
	}
	if err := tmp.Close(); err != nil {
		return err
	}
	return os.Rename(tmp.Name(), path)
}

// RelPath converts an absolute file path into the root-relative,
// slash-separated form Tab.Path stores. Paths outside root are kept
// absolute.
func RelPath(root, path string) string {
	rel, err := filepath.Rel(root, path)
	if err != nil || rel == ".." || strings.HasPrefix(rel, ".."+string(filepath.Separator)) {
		return path
	}
	return filepath.ToSlash(rel)
}

// AbsPath is RelPath's inverse.
func AbsPath(root, path string) string {
	p := filepath.FromSlash(path)
	if filepath.IsAbs(p) {
		return p
	}
	return filepath.Join(root, p)
}

// Capture snapshots every editor pane under root. pane reports a leaf's
// Pane, or ok=false for leaves that aren't editor panes (the file tree,
// the status bar) and for panes with nothing open — both are left out,
// and any split left with a single child collapses into that child.
// focused is the leaf that had focus; if it isn't captured, Focused is 0.
// Returns nil if there are no panes worth saving.
func Capture(root layout.Node, pane func(*layout.LeafNode) (*Pane, bool), focused layout.LeafID) *Session {
	s := &Session{}
	count := 0
	var walk func(n layout.Node) *Node
	walk = func(n layout.Node) *Node {
		switch v := n.(type) {
		case *layout.LeafNode:
			p, ok := pane(v)
			if !ok {
				return nil
			}
			if v.ID == focused {
				s.Focused = count
			}
			count++
			return &Node{Pane: p}
		case *layout.SplitNode:
			var children []*Node
			for _, c := range v.Children {
				if cn := walk(c.Node); cn != nil {
					children = append(children, cn)
				}
			}
			switch len(children) {
			case 0:
				return nil
			case 1:
				return children[0]
			}
			return &Node{Split: dirName(v.Dir), Children: children}
		}
		return nil
	}
	s.Layout = walk(root)
	if s.Layout == nil {
		return nil
	}
	return s
}

// Build rebuilds s's layout as a layout tree, every split's children
// sharing its space equally. newLeaf turns each saved Pane into a leaf, or
// returns nil to drop it (e.g. when none of its files exist any more);
// splits left with a single child collapse into it. Returns the tree (nil
// if every pane was dropped) and the leaf to focus — the saved focused
// pane if it survived, otherwise the first one.
func Build(s *Session, newLeaf func(Pane) *layout.LeafNode) (layout.Node, *layout.LeafNode) {
	if s == nil || s.Layout == nil {
		return nil, nil
	}
	var focused, first *layout.LeafNode
	index := 0
	var walk func(n *Node) layout.Node
	walk = func(n *Node) layout.Node {
		if n.Pane != nil {
			i := index
			index++
			leaf := newLeaf(*n.Pane)
			if leaf == nil {
				return nil
			}
			if first == nil {
				first = leaf
			}
			if i == s.Focused {
				focused = leaf
			}
			return leaf
		}
		var children []layout.Child
		for _, c := range n.Children {
			if c == nil {
				continue
			}
			if cn := walk(c); cn != nil {
				children = append(children, layout.Child{Node: cn, Hint: layout.Ratio(1)})
			}
		}
		switch len(children) {
		case 0:
			return nil
		case 1:
			return children[0].Node
		}
		return &layout.SplitNode{Dir: parseDir(n.Split), Children: children}
	}
	root := walk(s.Layout)
	if focused == nil {
		focused = first
	}
	return root, focused
}

func dirName(d layout.Direction) string {
	if d == layout.Vertical {
		return "vertical"
	}
	return "horizontal"
}

func parseDir(s string) layout.Direction {
	if s == "vertical" {
		return layout.Vertical
	}
	return layout.Horizontal
}
