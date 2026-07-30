// Command kiwi is the terminal editor's entrypoint. Step 0 wires up the
// window tree, focus routing, and a two-pane split (file tree | editor).
package main

import (
	"fmt"
	"os"
	"os/exec"
	"path/filepath"
	"strings"
	"time"

	"github.com/bricejulia/kiwi/internal/config"
	"github.com/bricejulia/kiwi/internal/debuglog"
	"github.com/bricejulia/kiwi/internal/layout"
	"github.com/bricejulia/kiwi/internal/lsp"
	"github.com/bricejulia/kiwi/internal/ui"
	"github.com/bricejulia/kiwi/internal/ui/debug"
	"github.com/bricejulia/kiwi/internal/ui/editor"
	"github.com/bricejulia/kiwi/internal/ui/filetree"
	"github.com/bricejulia/kiwi/internal/ui/finder"
	"github.com/bricejulia/kiwi/internal/ui/help"
	"github.com/bricejulia/kiwi/internal/ui/statusbar"
	"github.com/bricejulia/kiwi/internal/vcs/gitstatus"
	"github.com/bricejulia/kiwi/internal/vcs/watch"
	"github.com/bricejulia/kiwi/internal/version"
)

// watchDebounce is the quiet period after the last observed filesystem
// change before a refresh fires.
const watchDebounce = 200 * time.Millisecond

// mainShortcutsHint is the fixed reminder shown left-aligned in the status
// bar — see internal/ui/help for the full keybinding reference (opened via
// "?", included at the end here).
const mainShortcutsHint = "Tab Switch pane  ·  Ctrl+P Finder  ·  Ctrl+D Debug  ·  ? Help  ·  Ctrl+C Quit  ·  Ctrl+O Config"

// globalDefaultKeybinds are kiwi's built-in global keybindings,
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

// mergedLSPServers combines kiwi's built-in language-server registry with
// any "lsp" lines from the user's config, with the config winning — so a
// user can both add a language kiwi ships no default for and replace one
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

// configTemplateScopes is every scope's built-in keybindings, in the
// order the generated template config file lists them — see
// config.EnsureFile.
var configTemplateScopes = []config.Scope{
	{Name: "global", Defaults: globalDefaultKeybinds},
	{Name: "editor", Defaults: editor.DefaultKeybinds},
	{Name: "filetree", Defaults: filetree.DefaultKeybinds},
	{Name: "finder", Defaults: finder.DefaultKeybinds},
	{Name: "debug", Defaults: debug.DefaultKeybinds},
	{Name: "help", Defaults: help.DefaultKeybinds},
}

func main() {
	if err := run(); err != nil {
		fmt.Fprintln(os.Stderr, "kiwi:", err)
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

	treeView := filetree.New(absRoot)
	treeView.SetKeymap(cfg.Overrides("filetree"))
	// bufferStore is shared by every editor pane (this one and any created
	// by trySplit below), so opening the same file in two panes gives them
	// the SAME Buffer — edits, dirty state, and undo are shared exactly
	// like vim's buffers-vs-windows model, instead of each pane silently
	// keeping its own independent copy. See editor.BufferStore.
	bufferStore := editor.NewBufferStore()

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
	editorView.SetBufferStore(bufferStore)
	editorView.SetLSPManager(lspManager)
	statusBarView := statusbar.New()
	statusBarView.Hint = mainShortcutsHint

	// gitBranch/gitSummary are refreshed by refreshGitStatus (on startup
	// and whenever the watcher reports a git change) rather than shelled
	// out to on every render, which would run `git` on every keystroke.
	var gitBranch, gitSummary string

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
		parts := make([]string, 0, 4)
		if cursor := activeEditorPane.view.StatusText(); cursor != "" {
			parts = append(parts, cursor)
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
		parts = append(parts, "kiwi "+version.Version)
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
		lastFocusedLeaf = id

		// The only place activeEditorPane is ever written: keeps it
		// pointed at whichever editor pane last genuinely had focus, for
		// Tab-cycling, mouse clicks, and FocusLeaf calls alike.
		if p, ok := editorPanes[id]; ok {
			activeEditorPane = p
		}
	})

	// Closing an editor pane's last tab (via ":q"/":qa"/etc.) leaves it
	// showing the "No file open" placeholder with nothing left to do in
	// it, so hand focus back to the file tree — for every editor pane,
	// not just this initial one (see trySplit below for split-created
	// panes getting the same wiring).
	editorView.OnAllTabsClosed = func() { app.FocusLeaf(fileTreeLeaf.ID) }

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

	// "Find references" (Ctrl+f) reuses the finder's own content-search
	// overlay, pre-seeded with the identifier under the cursor, rather
	// than a separate results picker — see finder.View.OpenWithQuery.
	// Wired for the initial pane here; trySplit below wires the same for
	// every split-created pane.
	editorView.OnFindReferences = func(word string) {
		finderView.OpenWithQuery(word)
		app.ShowOverlay(finderView)
	}

	debugView := debug.New()
	debugView.SetKeymap(cfg.Overrides("debug"))
	debugView.OnClose = app.CloseOverlay
	openDebugLog := func() { app.ShowOverlay(debugView) }

	helpView := help.New(version.Version)
	helpView.SetKeymap(cfg.Overrides("help"))
	helpView.OnClose = app.CloseOverlay
	openHelp := func() { app.ShowOverlay(helpView) }

	// open_config shells out to the user's editor, so it needs the real
	// terminal to itself — see ui.App.SuspendAndRun. Keybinding changes
	// only take effect on kiwi's next start (the config template says
	// so), so there's no need to reload anything on return.
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
	}

	// targetPane resolves the editor pane split/close should act on: the
	// one currently focused, or ok=false if focus isn't on any known
	// editor pane (e.g. it's on the file tree) — in which case split/close
	// are simply no-ops.
	targetPane := func() (*editorPane, bool) {
		id, ok := app.FocusedLeaf()
		if !ok {
			return nil, false
		}
		p, ok := editorPanes[id]
		return p, ok
	}
	trySplit := func(dir layout.Direction) {
		target, ok := targetPane()
		if !ok {
			return
		}
		newView := editor.NewView()
		newView.SetKeymap(cfg.Overrides("editor"))
		newView.SetBufferStore(bufferStore)
		newView.SetLSPManager(lspManager)
		newView.OnAllTabsClosed = func() { app.FocusLeaf(fileTreeLeaf.ID) }
		newView.OnFindReferences = func(word string) {
			finderView.OpenWithQuery(word)
			app.ShowOverlay(finderView)
		}
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

	actions := map[string]func(){
		"quit":        app.Quit,
		"focus_next":  app.CycleFocusNext,
		"focus_prev":  app.CycleFocusPrev,
		"open_finder": openFinder,
		"open_debug":  openDebugLog,
		"open_help":   openHelp,
		"open_config": openConfig,
		"split_right": func() { trySplit(layout.Horizontal) },
		"split_down":  func() { trySplit(layout.Vertical) },
		"close_pane":  closeFocusedPane,
	}
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

	treeView.OnOpen = func(path string) {
		activeEditorPane.view.Open(path)
		app.FocusLeaf(activeEditorPane.leaf.ID)
		refreshLineStatusFor(path)
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

	// Registered unconditionally (not just when the watcher starts):
	// finder.SearchResult must be handled regardless of whether fsnotify
	// watching is available, or content-search results posted via
	// finderView.Post would have nowhere to go.
	app.SetCustomEventHandler(func(ev interface{}) {
		switch e := ev.(type) {
		case watch.RefreshEvent:
			debuglog.Debug("fsnotify refresh: gitChanged=%v fsChanged=%v", e.GitChanged, e.FSChanged)
			if e.GitChanged {
				refreshGitStatus()
			}
			if e.FSChanged {
				treeView.Refresh()
				// A plain unstaged edit (including this app's own Save)
				// never touches .git/index or HEAD, so it's only ever
				// reported as FSChanged, not GitChanged — but it can
				// still change a file's line-level diff, so it has to be
				// recomputed here too, not just in refreshGitStatus.
				refreshAllLineStatus()
			}
		case finder.SearchResult:
			finderView.ApplyContentResult(e)
		case lsp.DiagnosticsEvent:
			// Fanned out to every pane, exactly like refreshLineStatusFor
			// does for git line status: ApplyDiagnostics is a no-op in panes
			// that don't have this file open, and a file open in two panes
			// needs the markers in both.
			for _, p := range editorPanes {
				p.view.ApplyDiagnostics(e.Path, e.Diagnostics)
			}
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
