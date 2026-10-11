// Package workspace owns every editor pane open in a nib session and every
// operation that spans more than one of them: which pane is active,
// splitting/closing panes and moving tabs between them, opening files, and
// fanning out the events that concern every pane showing a file — renames,
// deletes, diagnostics, git gutter markers, external changes on disk —
// plus saving everything and capturing/restoring the session's layout.
//
// It never touches ui.App: focus changes come in through FocusChanged, the
// leaf to focus after a layout change goes out as a return value (or, for
// a change an editor pane itself triggered, through OnLayoutChanged), and
// the two things it needs to read from the app — the focused leaf and the
// on-screen pane rectangles — are injected as plain functions. That keeps
// the whole module testable with real editor panes and no terminal.
package workspace

import (
	"slices"

	"github.com/bricejulia/nib/internal/config"
	"github.com/bricejulia/nib/internal/layout"
	"github.com/bricejulia/nib/internal/lsp"
	"github.com/bricejulia/nib/internal/ui/editor"
	"github.com/bricejulia/nib/internal/vcs/gitstatus"
)

// PaneConfig is the part of the user's config every editor pane applies:
// its keybinding overrides, per-language indent style, and whether
// whitespace is drawn. See Workspace.ApplyConfig.
type PaneConfig struct {
	Keymap         map[string]string
	TabModes       map[string]config.TabMode
	ShowWhitespace bool
}

// Welcome is what an empty pane's welcome screen shows — see
// editor.View.SetWelcomeInfo. Branch is re-read on every render.
type Welcome struct {
	Version  string
	Folder   string
	Branch   func() string
	Keybinds []editor.WelcomeKeybind
}

// Options configures a Workspace. Every field is optional except Root.
type Options struct {
	// Root is the project folder, used to resolve the relative paths a
	// session is saved with.
	Root string
	// FirstLeafID is the layout.LeafID given to the first pane; every later
	// pane takes the next one up, so the caller must keep its own leaves
	// (file tree, status bar) below it.
	FirstLeafID layout.LeafID
	Config      PaneConfig
	// LSP and Highlighter are shared by every pane: one language server per
	// language for the whole session, and one tree-sitter worker. Nil
	// disables LSP features and highlights inline, respectively.
	LSP         *lsp.Manager
	Highlighter *editor.Highlighter
	// LineStatus computes path's git gutter markers (gitstatus.FileHunks in
	// production). An error leaves the gutter as it was — not a git repo,
	// path outside it, or git unavailable.
	LineStatus func(path string) (map[int]gitstatus.LineStatus, error)
	Welcome    Welcome
	// Focused reports the currently focused leaf (ui.App.FocusedLeaf), so
	// actions driven by current focus act on the right pane; nil means
	// "always the active pane".
	Focused func() (layout.LeafID, bool)
	// Rects reports each leaf's on-screen rectangle (ui.App.Rects), for
	// the tab menu's geometric "Move Right"/"Move Down".
	Rects func() map[layout.LeafID]layout.Rect
}

// Workspace is the set of editor panes in one nib session. All methods
// must be called on the UI goroutine.
type Workspace struct {
	// OnLayoutChanged, if set, is called after an editor pane's own tab
	// menu (Move Right, Split Right, Move to next pane, …) changed the
	// layout or moved a tab, with the leaf that should now have focus.
	// Changes made through Workspace's own methods return that leaf
	// instead.
	OnLayoutChanged func(focus layout.LeafID)

	opts     Options
	cfg      PaneConfig
	store    *editor.BufferStore
	register *editor.Register
	wire     func(*editor.View)

	// root is a one-child container around the editor panes' own subtree:
	// layout.Split and layout.Close both need the pane they act on to have
	// a parent (and Close a grandparent), which a lone pane at the top of
	// the subtree wouldn't otherwise have.
	root        *layout.SplitNode
	panes       map[layout.LeafID]*pane
	active      *pane
	lastFocused layout.LeafID
	nextID      layout.LeafID
}

type pane struct {
	leaf *layout.LeafNode
	view *editor.View
}

// New creates a Workspace holding one empty pane, which starts out active.
func New(opts Options) *Workspace {
	w := &Workspace{
		opts:     opts,
		cfg:      opts.Config,
		store:    editor.NewBufferStore(),
		register: editor.NewRegister(),
		panes:    map[layout.LeafID]*pane{},
		nextID:   opts.FirstLeafID,
	}
	first := w.newPane()
	w.add(first)
	w.root = &layout.SplitNode{
		Dir:      layout.Horizontal,
		Children: []layout.Child{{Node: first.leaf, Hint: layout.Ratio(1)}},
	}
	w.active = first
	return w
}

// Root is the layout node holding every editor pane, for the caller to
// place in its own layout tree. It stays the same node for the
// Workspace's whole lifetime, even across Split, ClosePane and Restore.
func (w *Workspace) Root() layout.Node { return w.root }

// Active is the editor pane that last had focus — the one opening a file
// from the file tree or finder lands in, even while focus is elsewhere.
func (w *Workspace) Active() *editor.View { return w.active.view }

// ActiveLeaf is Active's leaf ID, for focusing it.
func (w *Workspace) ActiveLeaf() layout.LeafID { return w.active.leaf.ID }

// IsPane reports whether id is one of this Workspace's editor panes.
func (w *Workspace) IsPane(id layout.LeafID) bool {
	_, ok := w.panes[id]
	return ok
}

// Target is the editor pane an action driven by current focus should act
// on: the focused one, or Active when focus is elsewhere entirely (e.g.
// the file tree) — so a global shortcut acts on "the editor pane you were
// just in" rather than silently doing nothing.
func (w *Workspace) Target() *editor.View { return w.target().view }

func (w *Workspace) target() *pane {
	if w.opts.Focused != nil {
		if id, ok := w.opts.Focused(); ok {
			if p, ok := w.panes[id]; ok {
				return p
			}
		}
	}
	return w.active
}

// FocusChanged tells the Workspace focus moved to leaf id, which need not
// be an editor pane. A pane losing focus leaves Insert/Command mode: two
// panes mid-edit on the same shared Buffer would scramble its undo
// history. A pane gaining it becomes Active.
func (w *Workspace) FocusChanged(id layout.LeafID) {
	if p, ok := w.panes[w.lastFocused]; ok && w.lastFocused != id {
		p.view.ExitEditingModes()
	}
	w.lastFocused = id
	if p, ok := w.panes[id]; ok {
		w.active = p
	}
}

// Wire installs fn as the hook that connects an editor pane to the rest of
// the application (dialogs, git, clipboard, …), running it on every pane
// that exists now and on every pane created later, so a new split is never
// missing a feature. Set once.
func (w *Workspace) Wire(fn func(*editor.View)) {
	w.wire = fn
	for _, p := range w.panes {
		fn(p.view)
	}
}

// ApplyConfig re-applies cfg to every pane, and to every pane created
// from here on — e.g. after the config file is reloaded.
func (w *Workspace) ApplyConfig(cfg PaneConfig) {
	w.cfg = cfg
	for _, p := range w.panes {
		w.configure(p.view)
	}
}

// Open opens path in the Active pane and refreshes its git gutter. Returns
// the leaf to focus.
func (w *Workspace) Open(path string) layout.LeafID {
	w.active.view.Open(path)
	w.refreshLineStatus(path)
	return w.active.leaf.ID
}

// OpenAtLine is Open as a navigation jump (see editor.View.OpenAtLine):
// the cursor lands on line (1-based) when line > 0, and the jump is
// recorded for jump-back either way.
func (w *Workspace) OpenAtLine(path string, line int) layout.LeafID {
	w.active.view.OpenAtLine(path, line)
	w.refreshLineStatus(path)
	return w.active.leaf.ID
}

// FindView reports which pane, if any, has path open — for applying an
// edit (replace-all, rename, code action) to an already-open buffer
// exactly once. See editor.Apply and editor.ApplyWorkspaceEdit.
func (w *Workspace) FindView(path string) (*editor.View, bool) {
	for _, p := range w.panes {
		if slices.Contains(p.view.OpenPaths(), path) {
			return p.view, true
		}
	}
	return nil, false
}

// newPane builds a fully configured and wired pane with the next leaf ID,
// without registering it — see add.
func (w *Workspace) newPane() *pane {
	v := editor.NewView()
	w.configure(v)
	v.SetBufferStore(w.store)
	v.SetRegister(w.register)
	v.SetLSPManager(w.opts.LSP)
	v.SetHighlighter(w.opts.Highlighter)
	wel := w.opts.Welcome
	v.SetWelcomeInfo(wel.Version, wel.Folder, wel.Branch, wel.Keybinds)
	w.wirePaneMoves(v)
	if w.wire != nil {
		w.wire(v)
	}
	p := &pane{leaf: &layout.LeafNode{ID: w.nextID, View: v}, view: v}
	w.nextID++
	return p
}

func (w *Workspace) add(p *pane) { w.panes[p.leaf.ID] = p }

func (w *Workspace) configure(v *editor.View) {
	v.SetKeymap(w.cfg.Keymap)
	v.SetTabModeDefaults(w.cfg.TabModes)
	v.SetShowWhitespace(w.cfg.ShowWhitespace)
}

func (w *Workspace) paneForView(v *editor.View) (*pane, bool) {
	for _, p := range w.panes {
		if p.view == v {
			return p, true
		}
	}
	return nil, false
}

func (w *Workspace) layoutChanged(focus layout.LeafID) {
	if w.OnLayoutChanged != nil {
		w.OnLayoutChanged(focus)
	}
}
