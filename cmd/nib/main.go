// Command nib is the terminal editor's entrypoint. Step 0 wires up the
// window tree, focus routing, and a two-pane split (file tree | editor).
package main

import (
	"fmt"
	"os"
	"os/exec"
	"path/filepath"
	"sort"
	"strings"
	"time"

	"github.com/bricejulia/nib/internal/config"
	"github.com/bricejulia/nib/internal/debuglog"
	"github.com/bricejulia/nib/internal/layout"
	"github.com/bricejulia/nib/internal/lsp"
	"github.com/bricejulia/nib/internal/theme"
	"github.com/bricejulia/nib/internal/ui"
	"github.com/bricejulia/nib/internal/ui/debug"
	"github.com/bricejulia/nib/internal/ui/diffview"
	"github.com/bricejulia/nib/internal/ui/editor"
	"github.com/bricejulia/nib/internal/ui/filetree"
	"github.com/bricejulia/nib/internal/ui/finder"
	"github.com/bricejulia/nib/internal/ui/help"
	"github.com/bricejulia/nib/internal/ui/quitconfirm"
	"github.com/bricejulia/nib/internal/ui/statusbar"
	"github.com/bricejulia/nib/internal/vcs/gitblame"
	"github.com/bricejulia/nib/internal/vcs/gitstatus"
	"github.com/bricejulia/nib/internal/vcs/watch"
	"github.com/bricejulia/nib/internal/version"
)

// watchDebounce is the quiet period after the last observed filesystem
// change before a refresh fires.
const watchDebounce = 200 * time.Millisecond

// mainShortcutsHint is the fixed reminder shown left-aligned in the status
// bar — see internal/ui/help for the full keybinding reference (opened via
// "?", included at the end here).
const mainShortcutsHint = "Tab Switch pane  ·  Ctrl+P Finder  ·  Ctrl+F Find refs  ·  Ctrl+R Replace  ·  Ctrl+D Debug  ·  ? Help  ·  Ctrl+C Quit  ·  Ctrl+O Config"

// welcomeKeybinds is the short key-binding reference shown centered in an
// editor pane that has no tabs open (see editor.View.SetWelcomeInfo) — a
// subset of globalDefaultKeybinds/editor.DefaultKeybinds picked for a
// first-time user staring at an empty pane, not the full reference (that's
// "?"/internal/ui/help).
var welcomeKeybinds = []editor.WelcomeKeybind{
	{Key: "Tab", Desc: "Switch pane"},
	{Key: "Ctrl+P", Desc: "Finder"},
	{Key: "Ctrl+F", Desc: "Find references"},
	{Key: "Ctrl+S", Desc: "Save"},
	{Key: "?", Desc: "Help"},
	{Key: "Ctrl+C", Desc: "Quit"},
}

// globalDefaultKeybinds are nib's built-in global keybindings,
// overridable via the user config's "global" scope (see internal/config).
// Each trigger's action name must have a matching entry in the actions
// map built in run(), or the (default or user-configured) binding for it
// is silently ignored.
var globalDefaultKeybinds = config.Defaults{
	{Trigger: "Ctrl+c", Action: "quit"},
	{Trigger: "Tab", Action: "focus_next"},
	{Trigger: "Shift+Tab", Action: "focus_prev"},
	// Double-tap Shift opens the same finder, but that only works on
	// terminals reporting bare modifier keypresses (kitty keyboard
	// protocol) — Ctrl+P is the conventional fallback everywhere else.
	{Trigger: "Ctrl+p", Action: "open_finder"},
	// "Find references": opens the finder's content search, pre-filled
	// with the word under the cursor when an editor pane happens to be
	// focused (see openFindReferences) — global, not editor-scoped, so it
	// also works from the file tree, unlike every other editor-only
	// gesture (B/D/H, go-to-definition, ...). A bare Ctrl+<letter>, so it's
	// reliably representable on any terminal.
	{Trigger: "Ctrl+f", Action: "open_find_references"},
	// Bare Ctrl+r, not Ctrl+Shift+r: the latter is only reliably
	// distinguishable from plain Ctrl+r on terminals reporting the kitty
	// keyboard protocol's full modifier state, which tmux doesn't negotiate
	// (its own "extended-keys" disambiguation defaults to off) — degrading
	// there to indistinguishable-from-Ctrl+r, which editor's own keymap used
	// to claim for redo. Redo now lives on bare "r" instead (see
	// editor.DefaultKeybinds) specifically to free this up: a bare
	// Ctrl+<letter> needs no modifier disambiguation and no macOS
	// Option-as-Alt terminal setting, so it's reliably representable
	// everywhere with zero configuration.
	{Trigger: "Ctrl+r", Action: "open_replace"},
	{Trigger: "Ctrl+d", Action: "open_debug"},
	{Trigger: "?", Action: "open_help"},
	// Not Ctrl+, (a common "settings" mnemonic elsewhere): outside the
	// kitty keyboard protocol, a terminal has no standard control code
	// for Ctrl held with punctuation, so it would silently never fire on
	// most terminals/multiplexers. Ctrl+letter is always representable.
	{Trigger: "Ctrl+o", Action: "open_config"},
	// These three are a reasonable starting point, not load-bearing —
	// like every other binding here, they're trivially remapped via the
	// user's own config file (Ctrl+O) if any of them collide with a given
	// terminal's own chrome shortcuts.
	{Trigger: "Ctrl+w", Action: "split_right"},
	{Trigger: "Ctrl+e", Action: "split_down"},
	{Trigger: "Ctrl+x", Action: "close_pane"},
	// Re-reads the config file and re-applies everything it drives —
	// every pane's keybindings, the global keymap, the LSP server
	// registry, and the theme — live, no restart needed. Also fires
	// automatically when the Ctrl+O editor exits (see openConfig); this
	// binding covers editing the file from elsewhere (another terminal,
	// tmux pane) while nib stays open.
	{Trigger: "Ctrl+l", Action: "reload_config"},
	// Reveals the active pane's file in the tree and focuses it — the
	// fix for "I opened index.tsx from the finder, but which one?" when a
	// project has many same-named files. Bare Ctrl+t, not a bare letter:
	// works the same from Insert mode as everywhere else, and "tree" is
	// free of collisions with every other pane's own keymap.
	{Trigger: "Ctrl+t", Action: "reveal_in_tree"},
}

// editorPane pairs an editor pane's window-tree leaf with its View, so
// split/close/open-file logic can look either up from the other. Multiple
// editorPanes can exist at once once the editor has been split.
type editorPane struct {
	leaf *layout.LeafNode
	view *editor.View
}

// rebuildAndFocus recomputes focus traversal order after the tree's
// leaves change, then immediately refocuses id — bundled into one call so
// the two can't accidentally be split apart. FocusManager.Rebuild's own
// fallback silently defaults to the first leaf (the file tree) the
// instant a closed leaf's ID is no longer present, and that's only ever
// correct here because every caller of Rebuild immediately follows it
// with an explicit refocus.
func rebuildAndFocus(app *ui.App, id layout.LeafID) {
	app.Rebuild()
	app.FocusLeaf(id)
}

// mergedLSPServers combines nib's built-in language-server registry with
// any "lsp" lines from the user's config, with the config winning — so a
// user can both add a language nib ships no default for and replace one
// it does (e.g. swapping PHP's Intelephense for Phpactor).
func mergedLSPServers(cfg *config.Config) map[string][]string {
	servers := make(map[string][]string, len(lsp.DefaultServers))
	for lang, command := range lsp.DefaultServers {
		servers[lang] = command
	}
	for lang, command := range cfg.Servers() {
		servers[lang] = command
		debuglog.Info("lsp: %s server configured as %v", lang, command)
	}
	return servers
}

// derivedTabModes merges the user config's "tabmode" lines over nib's own
// unconfigured default (real tabs, width 4), the same "config wins"
// shape mergedLSPServers uses for language servers — see
// editor.View.SetTabModeDefaults, which every editor pane's Open derives
// each newly opened file's indent style from.
func derivedTabModes(cfg *config.Config) map[string]config.TabMode {
	modes := map[string]config.TabMode{"default": {UseSpaces: false, Width: 4}}
	for lang, mode := range cfg.TabModes() {
		modes[lang] = mode
	}
	return modes
}

// resolveTheme merges the user's "theme = <name>" pick and any "color ="
// overrides into the theme every View's Render reads from — see
// internal/theme. Unknown role names (a color line with a typo'd role) are
// warned and skipped here, mirroring how an unknown "keybind" action name
// is warned and skipped in the global-keymap loop below: config.Parse
// validates only syntax, semantic validity against the closed role
// vocabulary internal/theme owns is checked once here.
func resolveTheme(cfg *config.Config) theme.Theme {
	overrides := make(map[theme.Role]layout.Color, len(cfg.ColorOverrides()))
	for roleName, color := range cfg.ColorOverrides() {
		role := theme.Role(roleName)
		if !theme.ValidRole(role) {
			debuglog.Warn("config: unknown theme role %q", roleName)
			continue
		}
		overrides[role] = color
	}
	if name := cfg.ThemeName(); name != "" {
		if _, ok := theme.Builtins[name]; !ok {
			debuglog.Warn("config: unknown theme %q, using %q", name, theme.DefaultName)
		}
	}
	return theme.Resolve(cfg.ThemeName(), overrides)
}

// configTemplateScopes is every scope's built-in keybindings, in the
// order the generated template config file lists them — see
// config.EnsureFile.
var configTemplateScopes = []config.Scope{
	{Name: "global", Defaults: globalDefaultKeybinds},
	{Name: "editor", Defaults: editor.DefaultKeybinds},
	{Name: "filetree", Defaults: filetree.DefaultKeybinds},
	{Name: "finder", Defaults: finder.DefaultKeybinds},
	{Name: "replace", Defaults: finder.ReplaceDefaultKeybinds},
	{Name: "debug", Defaults: debug.DefaultKeybinds},
	{Name: "diff", Defaults: diffview.DefaultKeybinds},
	{Name: "help", Defaults: help.DefaultKeybinds},
}

func main() {
	if err := run(); err != nil {
		fmt.Fprintln(os.Stderr, "nib:", err)
		os.Exit(1)
	}
}

// preferredEditor picks the interactive editor to shell out to for
// open_config, following the same $VISUAL/$EDITOR convention as git and
// most other terminal tools, with vi as a last-resort fallback that's
// present on nearly every Unix system.
func preferredEditor() string {
	if e := os.Getenv("VISUAL"); e != "" {
		return e
	}
	if e := os.Getenv("EDITOR"); e != "" {
		return e
	}
	return "vi"
}

func run() error {
	root := "."
	if len(os.Args) > 1 {
		root = os.Args[1]
	}
	absRoot, err := filepath.Abs(root)
	if err != nil {
		return err
	}

	// A missing or unloadable config just means every scope falls back
	// to its built-in defaults — cfg is safe to use as nil throughout
	// (see (*config.Config).Overrides).
	var cfg *config.Config
	cfgPath, err := config.Path()
	if err != nil {
		debuglog.Warn("resolve config path: %v", err)
	} else if c, err := config.Load(cfgPath); err != nil {
		debuglog.Warn("load config %s: %v", cfgPath, err)
	} else {
		cfg = c
	}
	theme.SetActive(resolveTheme(cfg))

	treeView := filetree.New(absRoot)
	treeView.SetKeymap(cfg.Overrides("filetree"))
	// bufferStore is shared by every editor pane (this one and any created
	// by trySplit below), so opening the same file in two panes gives them
	// the SAME Buffer — edits, dirty state, and undo are shared exactly
	// like vim's buffers-vs-windows model, instead of each pane silently
	// keeping its own independent copy. See editor.BufferStore.
	bufferStore := editor.NewBufferStore()

	// yankRegister is likewise shared by every editor pane, so a line cut
	// with "dd" in one pane can be put with "p" in another — vim's registers
	// are global to the session, not per-window. See editor.Register.
	yankRegister := editor.NewRegister()

	// lspManager is likewise shared by every editor pane: one language
	// server per language for the whole session, with each open file
	// announced to it exactly once no matter how many panes show it. Servers
	// are spawned lazily on the first file of their language, and a language
	// with no configured server (or a missing binary) just means the editor
	// keeps using its tree-sitter features there. See internal/lsp.
	lspManager := lsp.NewManager(absRoot)
	lspManager.SetServers(mergedLSPServers(cfg))
	defer lspManager.Shutdown()

	editorView := editor.NewView()
	editorView.SetKeymap(cfg.Overrides("editor"))
	editorView.SetTabModeDefaults(derivedTabModes(cfg))
	editorView.SetShowWhitespace(cfg.ShowWhitespace())
	editorView.SetBufferStore(bufferStore)
	editorView.SetRegister(yankRegister)
	editorView.SetLSPManager(lspManager)
	statusBarView := statusbar.New()
	statusBarView.Hint = mainShortcutsHint

	// gitBranch/gitSummary are refreshed by refreshGitStatus (on startup
	// and whenever the watcher reports a git change) rather than shelled
	// out to on every render, which would run `git` on every keystroke.
	var gitBranch, gitSummary string

	// applyWelcomeInfo wires up the empty-pane welcome screen (see
	// editor.View.SetWelcomeInfo): the opened folder's base name, plus a
	// closure over gitBranch above so a pane sitting empty still reflects the
	// current branch after refreshGitStatus updates it. Applied to every
	// editor pane, not just the first — see trySplit below, which can leave
	// a freshly split pane with no tab open too.
	applyWelcomeInfo := func(v *editor.View) {
		v.SetWelcomeInfo(version.Version, filepath.Base(absRoot), func() string { return gitBranch }, welcomeKeybinds)
	}
	applyWelcomeInfo(editorView)

	fileTreeLeaf := &layout.LeafNode{ID: 1, View: treeView}
	editorLeaf := &layout.LeafNode{ID: 2, View: editorView}
	statusBarLeaf := &layout.LeafNode{ID: 3, View: statusBarView}

	// editorPanes tracks every currently open editor pane (more than one
	// once the editor has been split); activeEditorPane is the last one
	// that genuinely had focus, kept in sync via SetFocusChangeHandler
	// below — needed because opening a file from the tree/finder fires
	// while focus is still on the tree/finder itself, never on an editor
	// pane directly.
	editorPanes := map[layout.LeafID]*editorPane{
		editorLeaf.ID: {leaf: editorLeaf, view: editorView},
	}
	activeEditorPane := editorPanes[editorLeaf.ID]
	nextLeafID := layout.LeafID(4) // 1/2/3 are already taken by the static tree above

	// refreshLineStatusFor recomputes path's git-diff gutter markers (see
	// gitstatus.FileHunks) and applies them to every editor pane that has
	// it open (path may be open in more than one pane at once — see
	// trySplit below). Cheap enough (one `git diff` shellout) to call
	// right after a file is opened, rather than waiting for the
	// debounced refresh from refreshAllLineStatus to catch up.
	refreshLineStatusFor := func(path string) {
		lines, err := gitstatus.FileHunks(absRoot, path)
		if err != nil {
			return // not a git repo, path outside it, or git unavailable
		}
		for _, p := range editorPanes {
			p.view.ApplyLineStatus(path, lines)
		}
	}
	// refreshAllLineStatus re-runs refreshLineStatusFor for every path
	// currently open in any editor pane — used whenever something not
	// scoped to a single just-opened file may have invalidated the
	// diffs: a git change (staging, commit, checkout) or any filesystem
	// change, since FileHunks reads the working-tree file directly and
	// so goes stale on a plain unstaged edit too (e.g. this app's own
	// Save, which never touches .git/index or HEAD).
	refreshAllLineStatus := func() {
		for _, p := range editorPanes {
			for _, path := range p.view.OpenPaths() {
				refreshLineStatusFor(path)
			}
		}
	}

	statusBarView.TextFunc = func() string {
		parts := make([]string, 0, 5)
		if cursor := activeEditorPane.view.StatusText(); cursor != "" {
			parts = append(parts, cursor)
		}
		// The active file's indent style — see editor.View.TabModeStatus.
		if tabMode := activeEditorPane.view.TabModeStatus(); tabMode != "" {
			parts = append(parts, tabMode)
		}
		// The active file's language plus whether a language server is
		// running for it — see editor.View.LanguageStatus.
		if lang := activeEditorPane.view.LanguageStatus(); lang != "" {
			parts = append(parts, lang)
		}
		if gitBranch != "" {
			branch := gitBranch
			if gitSummary != "" {
				branch += " " + gitSummary
			}
			parts = append(parts, branch)
		}
		parts = append(parts, "nib "+version.Version)
		return strings.Join(parts, "   ")
	}

	panes := &layout.SplitNode{
		Dir: layout.Horizontal,
		Children: []layout.Child{
			{Node: fileTreeLeaf, Hint: layout.Fixed(50)},
			{Node: editorLeaf, Hint: layout.Ratio(1)},
		},
	}
	tree := &layout.SplitNode{
		Dir: layout.Vertical,
		Children: []layout.Child{
			{Node: panes, Hint: layout.Ratio(1)},
			{Node: statusBarLeaf, Hint: layout.Fixed(1)},
		},
	}

	app, err := ui.NewApp(tree, nil)
	if err != nil {
		return err
	}
	defer app.Close()

	// Language servers answer on their own goroutines, so everything they
	// produce (diagnostics, definition responses) has to be marshaled onto
	// the UI event loop before it touches any View — the same discipline
	// the filesystem watcher and the finder's content search already use.
	lspManager.Post = app.Post

	// highlighter is shared by every editor pane, like bufferStore above and
	// for the same reason: a highlight belongs to the Buffer, so a file open
	// in two panes is parsed once and both show the result. It exists at all
	// because a tree-sitter re-parse is far too slow to run on a keystroke
	// (236ms per key on an 1800-line Go file), so edits queue the work here
	// and it lands back through Post like everything else off the UI
	// goroutine. See editor.Highlighter.
	highlighter := editor.NewHighlighter(app.Post)
	defer highlighter.Close()

	// lastFocusedLeaf is only used to know which editor pane (if any) just
	// LOST focus, immediately below — activeEditorPane (which pane last
	// genuinely had focus, full stop) is a separate, longer-lived thing
	// other callbacks below still need even once focus moves elsewhere
	// (e.g. onto the file tree).
	var lastFocusedLeaf layout.LeafID
	app.SetFocusChangeHandler(func(id layout.LeafID) {
		// A pane left mid-Insert/Command-mode when focus moves away (e.g.
		// a mouse click elsewhere, which — unlike Tab-cycling — never
		// routes a key through the losing pane's own HandleKey) must not
		// stay that way: with buffers now shared across panes (see
		// editor.BufferStore), two panes simultaneously mid-edit on the
		// same buffer would scramble undo history. See
		// editor.View.ExitEditingModes.
		if p, ok := editorPanes[lastFocusedLeaf]; ok && lastFocusedLeaf != id {
			p.view.ExitEditingModes()
		}
		// Same reason, for the file tree's own create/rename/delete prompt:
		// a half-typed filename left behind by a click elsewhere would sit
		// swallowing every key the next time the tree got focus back. See
		// filetree.View.CancelPrompt.
		if lastFocusedLeaf == fileTreeLeaf.ID && lastFocusedLeaf != id {
			treeView.CancelPrompt()
		}
		lastFocusedLeaf = id

		// The only place activeEditorPane is ever written: keeps it
		// pointed at whichever editor pane last genuinely had focus, for
		// Tab-cycling, mouse clicks, and FocusLeaf calls alike.
		if p, ok := editorPanes[id]; ok {
			activeEditorPane = p
		}
	})

	finderView := finder.New(absRoot)
	finderView.SetKeymap(cfg.Overrides("finder"))
	finderView.OnClose = app.CloseOverlay
	finderView.OnSelect = func(absPath string, line int) {
		activeEditorPane.view.OpenAtLine(absPath, line)
		app.FocusLeaf(activeEditorPane.leaf.ID)
		refreshLineStatusFor(absPath)
	}
	// Content search runs `git grep` on its own goroutine (can take real
	// time on a large project) and delivers its result back through Post
	// rather than blocking the UI thread — see finder.View.Post.
	finderView.Post = app.Post
	openFinder := func() {
		finderView.Open() // re-index the project's files fresh on every open
		app.ShowOverlay(finderView)
	}
	app.SetDoubleShiftHandler(openFinder)

	debugView := debug.New()
	debugView.SetKeymap(cfg.Overrides("debug"))
	debugView.OnClose = app.CloseOverlay
	openDebugLog := func() { app.ShowOverlay(debugView) }

	helpView := help.New(version.Version)
	helpView.SetKeymap(cfg.Overrides("help"))
	helpView.OnClose = app.CloseOverlay
	openHelp := func() { app.ShowOverlay(helpView) }

	// The whole-file diff ("D" in an editor pane) is a scrollable document,
	// so it gets the same modal-overlay treatment as the finder and debug
	// log rather than an in-pane popup — see internal/ui/diffview.
	diffView := diffview.New()
	diffView.SetKeymap(cfg.Overrides("diff"))
	diffView.OnClose = app.CloseOverlay
	openFileDiff := func(path string) {
		title := path
		if rel, err := filepath.Rel(absRoot, path); err == nil {
			title = rel
		}
		lines, err := gitstatus.FileDiff(absRoot, path)
		if err != nil {
			// Untracked files are already handled inside FileDiff, so
			// reaching here means git itself couldn't answer: no repository,
			// or no git. Say so in the overlay rather than opening an empty
			// one that would read as "no changes".
			debuglog.Warn("diff %s: %v", path, err)
			lines = []string{"(diff unavailable — not a git repository, or git failed; see Ctrl+D)"}
		}
		diffView.Show(title, lines)
		app.ShowOverlay(diffView)
	}

	// wireEditorPane attaches every callback an editor pane needs to reach
	// the rest of the application. Called for the initial pane below and for
	// each pane trySplit creates, so a new split is never quietly missing a
	// feature — which is exactly what a third hand-maintained copy of this
	// list would eventually do.
	wireEditorPane := func(v *editor.View) {
		// Keystrokes hand their re-highlighting to the shared worker rather
		// than parsing inline — see editor.View.SetHighlighter.
		v.SetHighlighter(highlighter)
		// Closing a pane's last tab (via ":q"/":qa"/etc.) leaves it showing
		// the "No file open" placeholder with nothing left to do in it, so
		// hand focus back to the file tree.
		v.OnAllTabsClosed = func() { app.FocusLeaf(fileTreeLeaf.ID) }
		// The git tooltips ("B" blame, "H" current-line diff): the editor
		// pane never shells out to git itself, so it asks through these,
		// exactly as its gutter markers arrive via ApplyLineStatus.
		v.BlameFunc = func(path string, line int) (gitblame.Info, error) {
			return gitblame.Line(absRoot, path, line)
		}
		v.HunkFunc = func(path string, line int) (gitstatus.Hunk, bool, error) {
			return gitstatus.FileHunkAt(absRoot, path, line)
		}
		v.OnShowFileDiff = openFileDiff
		// Copying a mouse selection reaches the system clipboard through
		// here — the editor pane speaks no OSC 52 itself, same arrangement
		// as the git callbacks above. The yank register (SetRegister) is what
		// makes an in-nib "p" work, and is filled independently, so a
		// terminal that ignores OSC 52 costs only the crossing-out-of-nib
		// half of the copy.
		v.CopyFunc = app.CopyToClipboard
	}
	wireEditorPane(editorView)

	// reloadConfig is forward-declared here (assigned for real once
	// everything it re-wires exists, near the actions map below) so
	// openConfig can call it on return — the only action another action
	// needs to invoke.
	var reloadConfig func()

	// open_config shells out to the user's editor, so it needs the real
	// terminal to itself — see ui.App.SuspendAndRun. Reloading afterward
	// (see reload_config) is what makes editing here take effect
	// immediately instead of needing a restart.
	openConfig := func() {
		if cfgPath == "" {
			return
		}
		if err := config.EnsureFile(cfgPath, configTemplateScopes); err != nil {
			debuglog.Warn("create config %s: %v", cfgPath, err)
			return
		}
		editorCmd := preferredEditor()
		err := app.SuspendAndRun(func() error {
			cmd := exec.Command(editorCmd, cfgPath)
			cmd.Stdin, cmd.Stdout, cmd.Stderr = os.Stdin, os.Stdout, os.Stderr
			return cmd.Run()
		})
		if err != nil {
			debuglog.Warn("open config %s in %s: %v", cfgPath, editorCmd, err)
		}
		reloadConfig()
	}

	// targetPane resolves the editor pane an action driven by CURRENT focus
	// should act on: the one currently focused, or activeEditorPane (the
	// last editor pane that genuinely had focus) when focus is elsewhere
	// entirely (e.g. the file tree) — so Ctrl+W/E/X and Ctrl+F act on "the
	// editor pane you were just in" rather than silently no-op'ing.
	targetPane := func() (*editorPane, bool) {
		if id, ok := app.FocusedLeaf(); ok {
			if p, ok := editorPanes[id]; ok {
				return p, true
			}
		}
		return activeEditorPane, true
	}
	// openFindReferences is Ctrl+F's global handler (see
	// globalDefaultKeybinds): it opens the finder's content search,
	// pre-filled with the word under the cursor when an editor pane
	// happens to be focused — the exact behavior "find references" always
	// had — or with an empty query otherwise, identical to how it already
	// behaved when no word was under the cursor.
	openFindReferences := func() {
		word := ""
		if p, ok := targetPane(); ok {
			word = p.view.WordUnderCursor()
		}
		finderView.OpenWithQuery(word)
		app.ShowOverlay(finderView)
	}
	// revealInTree is Ctrl+T's global handler: locates the current pane's
	// active file in the file tree and focuses it, so opening a
	// same-named file from the finder (e.g. one of several index.tsx)
	// shows exactly which one.
	revealInTree := func() {
		p, ok := targetPane()
		if !ok {
			return
		}
		path := p.view.ActivePath()
		if path == "" {
			return
		}
		if treeView.Reveal(path) {
			app.FocusLeaf(fileTreeLeaf.ID)
		}
	}
	trySplit := func(dir layout.Direction) {
		target, ok := targetPane()
		if !ok {
			return
		}
		newView := editor.NewView()
		newView.SetKeymap(cfg.Overrides("editor"))
		newView.SetTabModeDefaults(derivedTabModes(cfg))
		newView.SetShowWhitespace(cfg.ShowWhitespace())
		newView.SetBufferStore(bufferStore)
		newView.SetRegister(yankRegister)
		newView.SetLSPManager(lspManager)
		applyWelcomeInfo(newView)
		wireEditorPane(newView)
		newLeaf := &layout.LeafNode{ID: nextLeafID, View: newView}
		if !layout.Split(tree, target.leaf, dir, newLeaf) {
			return
		}
		nextLeafID++
		path := target.view.ActivePath()
		if path != "" {
			newView.Open(path) // new pane starts on the same file — the SAME Buffer, via bufferStore
		}
		editorPanes[newLeaf.ID] = &editorPane{leaf: newLeaf, view: newView}
		if path != "" {
			refreshLineStatusFor(path) // must run after registration above, so the new pane is in editorPanes to receive it
		}
		rebuildAndFocus(app, newLeaf.ID)
	}
	closeFocusedPane := func() {
		target, ok := targetPane()
		if !ok || len(editorPanes) == 1 {
			return // not in an editor pane, or it's the last one: no-op
		}
		// Refuse exactly like ":qa" would (see View.closeAllTabsCmd):
		// closing the pane releases these tabs' Buffer references, and an
		// unsaved buffer not held open by another pane loses its edits for
		// good.
		if dirty := target.view.DirtyPaths(); len(dirty) > 0 {
			debuglog.Warn("close pane: %d unsaved file(s) (save first): %s", len(dirty), strings.Join(dirty, ", "))
			return
		}
		// Release every open tab's Buffer reference before the pane
		// itself is discarded — otherwise bufferStore would keep each of
		// them alive (and, worse, hand back their now-orphaned stale
		// content instead of a fresh Load) forever, since nothing would
		// ever call Release for them again.
		target.view.CloseAllTabs()
		survivor, ok := layout.Close(tree, target.leaf)
		if !ok {
			return
		}
		delete(editorPanes, target.leaf.ID)
		rebuildAndFocus(app, layout.Leaves(survivor)[0].ID)
	}

	refreshGitStatus := func() {
		direct, err := gitstatus.RunPorcelain(absRoot)
		if err != nil {
			return // not a git repo, or git unavailable: leave markers as-is
		}
		// The file tree also shows directories, so it needs the rolled-up
		// map; the finder only ever lists files, so the direct per-file
		// map is the right (and precise) one for it.
		treeView.ApplyStatus(gitstatus.Rollup(direct))
		finderView.ApplyStatus(direct)
		gitSummary = gitstatus.Summary(direct)
		if branch, err := gitstatus.CurrentBranch(absRoot); err == nil {
			gitBranch = branch
		}
		refreshAllLineStatus()
	}
	refreshGitStatus()

	// findPane answers "is absPath open in some editor pane, and which
	// one" for editor.Apply, without that package needing to know about
	// panes, leaves, or the window tree at all — the same fan-out idiom
	// refreshLineStatusFor/refreshAllLineStatus already use (looping
	// editorPanes + OpenPaths), just stopping at the first match instead
	// of visiting every pane, since editor.View.ReplaceLines mutates the
	// buffer's own shared Lines rather than per-tab display state (see its
	// doc comment) and so must only ever be called once per path.
	findPane := func(absPath string) (*editor.View, bool) {
		for _, p := range editorPanes {
			for _, path := range p.view.OpenPaths() {
				if path == absPath {
					return p.view, true
				}
			}
		}
		return nil, false
	}

	// Search-and-replace is finder.View's third mode (Tab-cycled, alongside
	// filename/content search), so it shares finderView's own overlay
	// rather than getting a separate one — see finder.View.Replace.
	finderView.Replace().SetKeymap(cfg.Overrides("replace"))
	finderView.Replace().OnClose = app.CloseOverlay
	// Reuses the same async plumbing finderView.Post already relies on —
	// see finder.ReplaceView.refilter.
	finderView.Replace().Post = app.Post
	finderView.Replace().OnReplaceAll = func(search, replacement string, occs []editor.Occurrence) {
		res := editor.Apply(search, replacement, occs, findPane)
		for path, err := range res.Failed {
			debuglog.Error("replace in path %s: %v", path, err)
		}
		// Same call treeView.OnMutated already makes: a direct,
		// nib-initiated file mutation updates the tree/finder status
		// markers immediately rather than waiting on fsnotify's debounce.
		refreshGitStatus()
		finderView.Replace().ShowResult(res)
	}
	// openReplace jumps straight into replace mode, the same direct-to-mode
	// shortcut openFindReferences gives content-search mode.
	openReplace := func() {
		finderView.OpenReplace()
		app.ShowOverlay(finderView)
	}

	// dirtyPaths lists every currently-open file with unsaved changes,
	// across every editor pane, deduplicated (the same Buffer can be open
	// as a tab in more than one pane — see bufferStore above) and sorted
	// for a stable, scannable order in the quit-confirm dialog below.
	dirtyPaths := func() []string {
		seen := map[string]bool{}
		var paths []string
		for _, p := range editorPanes {
			for _, path := range p.view.DirtyPaths() {
				if seen[path] {
					continue
				}
				seen[path] = true
				paths = append(paths, path)
			}
		}
		sort.Strings(paths)
		return paths
	}
	// saveAllDirty saves every unsaved file across every editor pane. A
	// buffer shared by two panes is simply no longer Dirty by the time the
	// second pane's SaveDirtyTabs runs, so this never double-writes.
	saveAllDirty := func() {
		for _, p := range editorPanes {
			for path, err := range p.view.SaveDirtyTabs() {
				debuglog.Error("save %s: %v", path, err)
			}
		}
	}

	// quitConfirmView warns before quitting would silently discard unsaved
	// changes, instead of app.Quit firing unconditionally — see
	// internal/ui/quitconfirm.
	quitConfirmView := quitconfirm.New()
	quitConfirmView.OnCancel = app.CloseOverlay
	quitConfirmView.OnSaveAndQuit = func() {
		app.CloseOverlay()
		saveAllDirty()
		app.Quit()
	}
	quitConfirmView.OnDiscardAndQuit = func() {
		app.CloseOverlay()
		app.Quit()
	}
	confirmQuit := func() {
		dirty := dirtyPaths()
		rel := make([]string, len(dirty))
		for i, p := range dirty {
			rel[i] = p
			if r, err := filepath.Rel(absRoot, p); err == nil {
				rel[i] = r
			}
		}
		quitConfirmView.Show(rel)
		app.ShowOverlay(quitConfirmView)
	}

	actions := map[string]func(){
		"quit":                 confirmQuit,
		"focus_next":           app.CycleFocusNext,
		"focus_prev":           app.CycleFocusPrev,
		"open_finder":          openFinder,
		"open_find_references": openFindReferences,
		"open_replace":         openReplace,
		"open_debug":           openDebugLog,
		"open_help":            openHelp,
		"open_config":          openConfig,
		"split_right":          func() { trySplit(layout.Horizontal) },
		"split_down":           func() { trySplit(layout.Vertical) },
		"close_pane":           closeFocusedPane,
		"reveal_in_tree":       revealInTree,
	}

	// rebuildGlobalKeymap resolves globalDefaultKeybinds against cfg's
	// current "global" overrides and installs the result — factored out
	// so reloadConfig can rebuild it exactly the same way the initial
	// setup below does, instead of duplicating this loop.
	rebuildGlobalKeymap := func() {
		global := map[string]func(){}
		for trigger, action := range globalDefaultKeybinds.Resolve(cfg.Overrides("global")) {
			fn, ok := actions[action]
			if !ok {
				debuglog.Warn("config: unknown global action %q bound to %q", action, trigger)
				continue
			}
			global[trigger] = fn
		}
		app.SetGlobalKeymap(global)
	}

	// reloadConfig re-reads cfgPath and re-applies everything it drives —
	// every pane's keybindings, the global keymap, the LSP server
	// registry, and the theme — without restarting nib. A missing or
	// unloadable file is handled exactly like the initial load: warn and
	// leave whatever was already active in place, rather than clearing it.
	reloadConfig = func() {
		if cfgPath == "" {
			return
		}
		newCfg, err := config.Load(cfgPath)
		if err != nil {
			debuglog.Warn("reload config %s: %v", cfgPath, err)
			return
		}
		cfg = newCfg

		theme.SetActive(resolveTheme(cfg))

		treeView.SetKeymap(cfg.Overrides("filetree"))
		for _, p := range editorPanes {
			p.view.SetKeymap(cfg.Overrides("editor"))
			p.view.SetTabModeDefaults(derivedTabModes(cfg))
			p.view.SetShowWhitespace(cfg.ShowWhitespace())
		}
		finderView.SetKeymap(cfg.Overrides("finder"))
		finderView.Replace().SetKeymap(cfg.Overrides("replace"))
		debugView.SetKeymap(cfg.Overrides("debug"))
		helpView.SetKeymap(cfg.Overrides("help"))
		diffView.SetKeymap(cfg.Overrides("diff"))
		lspManager.SetServers(mergedLSPServers(cfg))
		rebuildGlobalKeymap()

		debuglog.Info("config reloaded from %s", cfgPath)
	}
	actions["reload_config"] = reloadConfig

	rebuildGlobalKeymap()

	treeView.OnOpen = func(path string) {
		activeEditorPane.view.Open(path)
		app.FocusLeaf(activeEditorPane.leaf.ID)
		refreshLineStatusFor(path)
	}

	// OnPathMoved/OnPathDeleted are how the file tree's own create/rename/
	// delete actions reach the rest of the app. Both are called on the UI
	// goroutine AFTER the filesystem operation succeeded, with absolute
	// paths, and either path may be a directory — a folder rename moves
	// every file under it, which editor.View.Repath handles by prefix
	// itself, so nothing here does path arithmetic.
	//
	// Fanned out over every editor pane for the same reason
	// refreshLineStatusFor and ApplyDiagnostics are: the same file can be
	// open in several panes, and each pane owns its own tab for it. The
	// store-level work (re-keying the shared Buffer) still happens exactly
	// once — see editor.BufferStore.Rekey. The file tree updates its own
	// node tree, so neither of these calls Refresh.
	treeView.OnPathMoved = func(oldPath, newPath string) {
		for _, p := range editorPanes {
			p.view.Repath(oldPath, newPath)
		}
	}
	treeView.OnPathDeleted = func(path string) {
		var detached []string
		for _, p := range editorPanes {
			detached = append(detached, p.view.CloseTabsUnder(path)...)
		}
		if len(detached) > 0 {
			// The durable record of what happened; the panes themselves show
			// it as "-- DELETED --" and a ✗ in the tab bar.
			debuglog.Warn("deleted %s: %d open buffer(s) with unsaved changes stay open (\":w\" recreates the file, \":q!\" discards): %s",
				path, len(detached), strings.Join(detached, ", "))
		}
	}
	// Run last, after the tabs are carrying their new paths: refreshGitStatus
	// ends in refreshAllLineStatus, and ApplyLineStatus matches on a tab's
	// path — so doing this first would leave a moved file's gutter blank
	// until something else invalidated it.
	treeView.OnMutated = refreshGitStatus

	// Registered unconditionally (not just when the watcher starts):
	// finder.SearchResult must be handled regardless of whether fsnotify
	// watching is available, or content-search results posted via
	// finderView.Post would have nowhere to go.
	app.SetCustomEventHandler(func(ev interface{}) {
		switch e := ev.(type) {
		case watch.RefreshEvent:
			debuglog.Debug("fsnotify refresh: gitChanged=%v fsChanged=%v", e.GitChanged, e.FSChanged)
			if e.FSChanged {
				treeView.Refresh()
			}
			// A plain unstaged edit (including this app's own Save) never
			// touches .git/index or HEAD, so it's only ever reported as
			// FSChanged, not GitChanged — but it still changes both the
			// file's porcelain status (clean -> modified, i.e. the marker
			// the file tree and finder show) and its line-level diff, so a
			// bare FSChanged has to re-run the same refresh a GitChanged
			// does. refreshGitStatus ends by calling refreshAllLineStatus,
			// so the line-level gutters are covered by this too.
			if e.GitChanged || e.FSChanged {
				refreshGitStatus()
			}
		case finder.SearchResult:
			finderView.ApplyContentResult(e)
		case finder.ReplaceSearchResult:
			finderView.Replace().ApplyReplaceSearchResult(e)
		case lsp.DiagnosticsEvent:
			// Fanned out to every pane, exactly like refreshLineStatusFor
			// does for git line status: ApplyDiagnostics is a no-op in panes
			// that don't have this file open, and a file open in two panes
			// needs the markers in both.
			for _, p := range editorPanes {
				p.view.ApplyDiagnostics(e.Path, e.Diagnostics)
			}
		case editor.HighlightResult:
			// Like DiagnosticsEvent, but with no fan-out to do: a highlight
			// belongs to the shared Buffer, not to a pane, so storing it
			// once updates every pane showing that file. Dropped here if the
			// buffer has been edited since — see editor.ApplyHighlightResult.
			editor.ApplyHighlightResult(e)
		case lsp.AsyncResult:
			// One generic case covers every request/response LSP feature:
			// the closure was built by whoever issued the request and
			// already knows what to do with the answer, so there's no
			// per-feature routing to keep in sync here.
			e.Apply()
		}
	})

	if watcher, err := watch.New(absRoot, watchDebounce); err == nil {
		defer func() { _ = watcher.Close() }()

		go func() {
			for re := range watcher.Events() {
				app.Post(re)
			}
		}()
	} else {
		debuglog.Warn("filesystem watcher unavailable: %v", err)
	}

	return app.Run()
}
