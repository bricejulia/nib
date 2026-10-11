// Command nib is the terminal editor's entrypoint. Step 0 wires up the
// window tree, focus routing, and a two-pane split (file tree | editor).
package main

import (
	"fmt"
	"maps"
	"os"
	"os/exec"
	"os/signal"
	"path/filepath"
	"strings"
	"syscall"
	"time"

	"github.com/bricejulia/nib/internal/config"
	"github.com/bricejulia/nib/internal/debuglog"
	"github.com/bricejulia/nib/internal/layout"
	"github.com/bricejulia/nib/internal/lsp"
	"github.com/bricejulia/nib/internal/memwatch"
	"github.com/bricejulia/nib/internal/session"
	"github.com/bricejulia/nib/internal/theme"
	"github.com/bricejulia/nib/internal/ui"
	"github.com/bricejulia/nib/internal/ui/actionpopup"
	"github.com/bricejulia/nib/internal/ui/closetabconfirm"
	"github.com/bricejulia/nib/internal/ui/codeactionpopup"
	"github.com/bricejulia/nib/internal/ui/debug"
	"github.com/bricejulia/nib/internal/ui/diffview"
	"github.com/bricejulia/nib/internal/ui/editor"
	"github.com/bricejulia/nib/internal/ui/filetree"
	"github.com/bricejulia/nib/internal/ui/finder"
	"github.com/bricejulia/nib/internal/ui/help"
	"github.com/bricejulia/nib/internal/ui/historyview"
	"github.com/bricejulia/nib/internal/ui/memprompt"
	"github.com/bricejulia/nib/internal/ui/quitconfirm"
	"github.com/bricejulia/nib/internal/ui/refview"
	"github.com/bricejulia/nib/internal/ui/reloadconfirm"
	"github.com/bricejulia/nib/internal/ui/statusbar"
	"github.com/bricejulia/nib/internal/ui/trustprompt"
	"github.com/bricejulia/nib/internal/vcs/gitblame"
	"github.com/bricejulia/nib/internal/vcs/githistory"
	"github.com/bricejulia/nib/internal/vcs/gitstatus"
	"github.com/bricejulia/nib/internal/vcs/watch"
	"github.com/bricejulia/nib/internal/version"
	"github.com/bricejulia/nib/internal/workspace"
)

// watchDebounce is the quiet period after the last observed filesystem
// change before a refresh fires.
const watchDebounce = 200 * time.Millisecond

// fileHistoryLimit caps how many commits the file-history overlay lists
// (see openFileHistory): enough to reach well back into any file's past,
// while keeping the `git log` behind opening it quick on a huge repository.
const fileHistoryLimit = 500

// memWatchThreshold/memWatchInterval configure the background memory
// watchdog (see internal/memwatch) — how much Go heap nib can use before
// offering to close its largest open file, and how often to check.
// memWatchThreshold is just the fallback default: the user can override it
// with a "memwatch = <MiB>" config line (see config.MemWatchThresholdMiB),
// applied where memWatcher is constructed below. A change to that line
// takes effect on next restart, like config-driven LSP server changes —
// memWatcher isn't rebuilt by the Ctrl+l config reload. memWatchInterval
// and memPromptSnooze stay hardcoded, like highlightTimeoutMicros/
// maxTreeSitterBytes/maxRenderLineRunes in internal/ui/editor.
const (
	memWatchThreshold uint64        = 500 << 20 // 500MiB
	memWatchInterval  time.Duration = 2 * time.Second
	memPromptSnooze   time.Duration = 5 * time.Minute
)

// memoryThresholdEvent is posted (via app.Post, from memWatcher's own
// goroutine — see its setup below) onto the main event loop whenever
// memWatchThreshold is crossed, so the prompt it triggers is shown from
// the UI goroutine like every other custom event nib handles.
type memoryThresholdEvent struct {
	heapBytes uint64
}

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
	// to claim for redo. Redo now lives on Shift+U instead (see
	// editor.DefaultKeybinds) — bare "r" is vim's own replace-char-under-
	// cursor gesture — specifically to free Ctrl+r up: a bare
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
	// Moves the active tab into the next/previous editor pane (see
	// Workspace.MoveActiveTab). NOT Alt+]/Alt+[ (the obvious pairing with
	// next_tab/prev_tab's own "]"/"["): outside the kitty keyboard
	// protocol, Alt+<printable> degrades to the classic "ESC then the
	// literal character" meta encoding (the same one Alt+b/Alt+f already
	// rely on below) — fine for a letter, but "]"/"[" specifically collide
	// with OSC/CSI, the terminal's own escape-sequence introducers (the
	// same class of problem "Ctrl+g", not "Ctrl+[", avoids — see its own
	// comment). Confirmed by hand: raw ESC+"]" never reached the app at
	// all. Ctrl+Alt+Right/Left (named keys, not bare printables) don't
	// have that ambiguity, and pair with the existing Ctrl+Alt+t
	// (toggle_tab_mode) precedent for a dual-modifier structural action.
	{Trigger: "Ctrl+Alt+Right", Action: "move_tab_next_pane"},
	{Trigger: "Ctrl+Alt+Left", Action: "move_tab_prev_pane"},
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
	// Bare Ctrl+j, not Ctrl+Shift+A (JetBrains' own "Find Action" mnemonic,
	// tried first here): outside the kitty keyboard protocol, a terminal
	// has no way to report Shift held alongside Ctrl+<letter> — Ctrl+Shift+A
	// arrives indistinguishable from plain Ctrl+A on most terminals/
	// multiplexers, which editor.DefaultKeybinds already binds to
	// "trigger_signature_help", silently stealing the key whenever an
	// editor pane is focused. A bare Ctrl+<letter> needs no modifier
	// disambiguation and collides with nothing else's keymap.
	{Trigger: "Ctrl+j", Action: "open_action_popup"},
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

// paneConfig is the part of cfg every editor pane applies — see
// workspace.PaneConfig.
func paneConfig(cfg *config.Config) workspace.PaneConfig {
	return workspace.PaneConfig{
		Keymap:         cfg.Overrides("editor"),
		TabModes:       derivedTabModes(cfg),
		ShowWhitespace: cfg.ShowWhitespace(),
	}
}

// derivedTabModes merges the user config's "tabmode" lines over nib's
// built-in per-language defaults (config.DefaultTabModes), the same "config wins"
// shape mergedLSPServers uses for language servers — see
// editor.View.SetTabModeDefaults, which every editor pane's Open derives
// each newly opened file's indent style from.
func derivedTabModes(cfg *config.Config) map[string]config.TabMode {
	modes := maps.Clone(config.DefaultTabModes)
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
	{Name: "history", Defaults: historyview.DefaultKeybinds},
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

	// If the given path is a file rather than a directory, open the
	// workspace at its parent directory instead and remember the file so
	// it can be opened automatically once the editor is constructed. A
	// missing path (os.Stat error) or a directory falls through
	// unchanged — today's behavior for both is preserved exactly.
	var openOnStart string
	if info, statErr := os.Stat(absRoot); statErr == nil && !info.IsDir() {
		openOnStart = absRoot
		absRoot = filepath.Dir(absRoot)
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
	treeView.SetShowIcons(cfg.ShowIcons())

	// lspManager is shared by every editor pane: one language
	// server per language for the whole session, with each open file
	// announced to it exactly once no matter how many panes show it. Servers
	// are spawned lazily on the first file of their language, and a language
	// with no configured server (or a missing binary) just means the editor
	// keeps using its tree-sitter features there. See internal/lsp.
	lspManager := lsp.NewManager(absRoot)
	lspManager.SetServers(mergedLSPServers(cfg))
	defer lspManager.Shutdown()

	statusBarView := statusbar.New()
	statusBarView.Hint = mainShortcutsHint

	// gitBranch/gitSummary are refreshed by refreshGitStatus (on startup
	// and whenever the watcher reports a git change) rather than shelled
	// out to on every render, which would run `git` on every keystroke.
	var gitBranch, gitSummary string

	// app needs the layout tree, and the tree needs the workspace's panes,
	// so it's assigned just below; everything here that reads it only runs
	// once the app exists.
	var app *ui.App

	// highlighter is shared by every editor pane, like the buffers it
	// highlights: a highlight belongs to the Buffer, so a file open in two
	// panes is parsed once and both show the result. It exists at all
	// because a tree-sitter re-parse is far too slow to run on a keystroke
	// (236ms per key on an 1800-line Go file), so edits queue the work here
	// and it lands back through Post like everything else off the UI
	// goroutine. See editor.Highlighter.
	highlighter := editor.NewHighlighter(func(ev interface{}) { app.Post(ev) })

	// ws holds every editor pane — see internal/workspace. Opening the same
	// file in two panes gives them the SAME Buffer, and a yank in one can be
	// put in another, like vim's buffers and registers.
	ws := workspace.New(workspace.Options{
		Root:        absRoot,
		FirstLeafID: 3, // 1 and 2 are the file tree and status bar
		Config:      paneConfig(cfg),
		LSP:         lspManager,
		Highlighter: highlighter,
		LineStatus: func(path string) (map[int]gitstatus.LineStatus, error) {
			return gitstatus.FileHunks(absRoot, path)
		},
		Welcome: workspace.Welcome{
			Version:  version.Version,
			Folder:   filepath.Base(absRoot),
			Branch:   func() string { return gitBranch },
			Keybinds: welcomeKeybinds,
		},
		Focused: func() (layout.LeafID, bool) { return app.FocusedLeaf() },
		Rects:   func() map[layout.LeafID]layout.Rect { return app.Rects() },
	})

	fileTreeLeaf := &layout.LeafNode{ID: 1, View: treeView}
	statusBarLeaf := &layout.LeafNode{ID: 2, View: statusBarView}

	panes := &layout.SplitNode{
		Dir: layout.Horizontal,
		Children: []layout.Child{
			{Node: fileTreeLeaf, Hint: layout.Fixed(defaultTreeWidth)},
			{Node: ws.Root(), Hint: layout.Ratio(1)},
		},
	}
	tree := &layout.SplitNode{
		Dir: layout.Vertical,
		Children: []layout.Child{
			{Node: panes, Hint: layout.Ratio(1)},
			{Node: statusBarLeaf, Hint: layout.Fixed(1)},
		},
	}

	app, err = ui.NewApp(tree, nil)
	if err != nil {
		return err
	}
	defer app.Close()
	// Closed before the app, so no highlight result is posted to a closed
	// event loop.
	defer highlighter.Close()

	statusBarView.TextFunc = func() string {
		parts := make([]string, 0, 6)
		// A symlink activate() just refused to expand/open, or a path the
		// context menu just copied — see filetree.View.Notice — only shown
		// while the tree pane itself is focused, the same scoping openFinder
		// below uses for "which pane's selection does this apply to".
		if id, ok := app.FocusedLeaf(); ok && id == fileTreeLeaf.ID {
			if notice := treeView.Notice(); notice != "" {
				parts = append(parts, notice)
			}
		}
		if cursor := ws.Active().StatusText(); cursor != "" {
			parts = append(parts, cursor)
		}
		// The active file's indent style — see editor.View.TabModeStatus.
		if tabMode := ws.Active().TabModeStatus(); tabMode != "" {
			parts = append(parts, tabMode)
		}
		// The active file's language plus whether a language server is
		// running for it — see editor.View.LanguageStatus.
		if lang := ws.Active().LanguageStatus(); lang != "" {
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

	// Language servers answer on their own goroutines, so everything they
	// produce (diagnostics, definition responses) has to be marshaled onto
	// the UI event loop before it touches any View — the same discipline
	// the filesystem watcher and the finder's content search already use.
	lspManager.Post = app.Post

	// lastFocusedLeaf is only used to know whether the file tree just LOST
	// focus, immediately below; the workspace tracks the same for its panes.
	var lastFocusedLeaf layout.LeafID
	app.SetFocusChangeHandler(func(id layout.LeafID) {
		// A pane losing focus leaves Insert/Command mode, and the one gaining
		// it becomes the active pane — see Workspace.FocusChanged.
		ws.FocusChanged(id)
		// Same reason as a pane leaving Insert mode, for the file tree's own
		// create/rename/copy/delete prompt: a half-typed filename left behind
		// by a click elsewhere would sit swallowing every key the next time
		// the tree got focus back. See filetree.View.CancelPrompt.
		if lastFocusedLeaf == fileTreeLeaf.ID && lastFocusedLeaf != id {
			treeView.CancelPrompt()
		}
		lastFocusedLeaf = id
	})

	finderView := finder.New(absRoot)
	finderView.SetKeymap(cfg.Overrides("finder"))
	finderView.OnClose = app.CloseOverlay
	finderView.OnSelect = func(absPath string, line int) {
		app.FocusLeaf(ws.OpenAtLine(absPath, line))
	}
	// Content search runs `git grep` on its own goroutine (can take real
	// time on a large project) and delivers its result back through Post
	// rather than blocking the UI thread — see finder.View.Post.
	finderView.Post = app.Post
	// openFinder scopes to the selected folder when the file tree is
	// focused (the fix for "search scoped to a folder" — see
	// filetree.View.SelectedDir/finder.View.OpenScoped), and searches the
	// whole project otherwise, exactly as before. The scope field it
	// pre-fills stays editable once the popup is open (Ctrl+K), so this is
	// only a starting value, not the only way to change it.
	openFinder := func() {
		if id, ok := app.FocusedLeaf(); ok && id == fileTreeLeaf.ID {
			finderView.OpenScoped(treeView.SelectedDir())
		} else {
			finderView.Open() // re-index the project's files fresh on every open
		}
		app.ShowOverlay(finderView)
	}
	app.SetDoubleShiftHandler(openFinder)

	// refView is Find References' LSP-backed results list (see
	// openFindReferences below): a plain, already-resolved list to pick
	// from, so unlike finderView it needs no query/search plumbing of its
	// own — see internal/ui/refview's package doc for why that's a
	// separate small package rather than an extension of finder.View.
	refView := refview.New(absRoot)
	refView.SetKeymap(cfg.Overrides("references"))
	refView.OnClose = app.CloseOverlay
	refView.OnSelect = func(loc lsp.Location) {
		app.CloseOverlay()
		path := loc.Path()
		if path == "" {
			return
		}
		app.FocusLeaf(ws.OpenAtLine(path, loc.Range.Start.Line+1))
	}

	debugView := debug.New()
	debugView.SetKeymap(cfg.Overrides("debug"))
	debugView.OnClose = app.CloseOverlay
	openDebugLog := func() { app.ShowOverlay(debugView) }

	helpView := help.New(version.Version)
	helpView.SetKeymap(cfg.Overrides("help"))
	helpView.OnClose = app.CloseOverlay
	openHelp := func() { app.ShowOverlay(helpView) }

	// actionPopupView is the "Find Action" popup (Ctrl+j): type to
	// filter a list of named commands, Enter to run the selected one. Its
	// OnExecute is wired up further down, once the actions map exists
	// (see there for why).
	actionPopupView := actionpopup.New()
	actionPopupView.SetKeymap(cfg.Overrides("actionpopup"))
	actionPopupView.OnClose = app.CloseOverlay
	openActionPopup := func() {
		actionPopupView.Open(
			globalDefaultKeybinds.Resolve(cfg.Overrides("global")),
			editor.DefaultKeybinds.Resolve(cfg.Overrides("editor")),
		)
		app.ShowOverlay(actionPopupView)
	}

	// refreshGitStatus is defined further down (it needs finderView/etc.
	// that exist by then), but caView.OnExecute just below and the editor
	// panes' LSP callbacks (see OnApplyWorkspaceEdit in ws.Wire) only ever
	// run later, from a user action — by which point it's assigned.
	// Forward-declared so those closures can close over it now; same trick
	// reloadConfig uses below.
	var refreshGitStatus func()

	// caView is the code-actions popup ("A" in an editor pane): a list of
	// server-suggested fixes/refactors for the cursor position, fed fresh
	// per request — see internal/ui/codeactionpopup.
	caView := codeactionpopup.New()
	caView.SetKeymap(cfg.Overrides("codeactions"))
	caView.OnClose = app.CloseOverlay
	caView.OnExecute = func(action lsp.CodeAction) {
		app.CloseOverlay()
		if action.Edit == nil {
			return
		}
		res := editor.ApplyWorkspaceEdit(*action.Edit, ws.FindView, ws.Active())
		for path, err := range res.Failed {
			debuglog.Error("apply code action: %s: %v", path, err)
		}
		refreshGitStatus()
	}

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

	// The file history ("L" in an editor pane or the file tree): the
	// commits that touched a file, each one's diff previewed as it's
	// selected — see internal/ui/historyview. Uncommitted changes, when
	// there are any, head the list as their own entry, so the history runs
	// all the way up to what's on disk now.
	historyView := historyview.New()
	historyView.SetKeymap(cfg.Overrides("history"))
	historyView.OnClose = app.CloseOverlay
	openFileHistory := func(path string) {
		title := path
		if rel, err := filepath.Rel(absRoot, path); err == nil {
			title = rel
		}
		commits, err := githistory.Log(absRoot, path, fileHistoryLimit)
		if err != nil {
			// The overlay's own empty state already says "not a git
			// repository"; the log keeps git's actual reason.
			debuglog.Warn("history %s: %v", path, err)
		}

		var entries []historyview.Entry
		working, err := gitstatus.FileDiff(absRoot, path)
		if err != nil {
			debuglog.Warn("history %s: working-tree diff: %v", path, err)
		}
		if len(working) > 0 {
			entries = append(entries, historyview.Entry{ID: "working", When: "uncommitted", Summary: "Changes not yet committed"})
		}
		offset := len(entries)
		for _, c := range commits {
			entries = append(entries, historyview.Entry{
				ID:      c.ShortHash,
				When:    c.Time.Format("2006-01-02"),
				Author:  c.Author,
				Summary: c.Summary,
			})
		}

		historyView.DiffFunc = func(i int) []string {
			if i < offset {
				return working
			}
			j := i - offset
			// The file's path in this commit's parent: the next-older
			// commit's, or its own for the oldest one listed.
			prev := commits[j].Path
			if j+1 < len(commits) {
				prev = commits[j+1].Path
			}
			lines, err := githistory.CommitDiff(absRoot, commits[j], prev)
			if err != nil {
				debuglog.Warn("history %s: show %s: %v", path, commits[j].ShortHash, err)
				return []string{"(diff unavailable — git failed; see Ctrl+D)"}
			}
			return lines
		}
		historyView.Open(title, entries)
		app.ShowOverlay(historyView)
	}

	// reloadconfirmView is the "Keep mine / Reload from disk / Cancel"
	// prompt shown when a save conflicts with a change made outside nib
	// (see editor.View.OnSaveConflict / editor.Buffer.HasDiskConflict).
	// pendingConflicts is the queue it's draining, one path at a time —
	// populated with either a single entry (an interactive save hitting a
	// conflict) or several (saveAllDirty's leftover conflicts, below).
	// onConflictsResolved, if set, runs once the queue empties — e.g.
	// quitConfirmView.OnSaveAndQuit (below) uses this to defer app.Quit()
	// until every conflict is actually resolved, not just kicked off.
	reloadconfirmView := reloadconfirm.New()
	var pendingConflicts []editor.SaveConflict
	var onConflictsResolved func()

	showNextConflict := func() {
		c := pendingConflicts[0]
		rel := c.Path
		if r, err := filepath.Rel(absRoot, c.Path); err == nil {
			rel = r
		}
		reloadconfirmView.Show(rel, len(pendingConflicts)-1)
		app.ShowOverlay(reloadconfirmView)
	}
	advanceConflictQueue := func() {
		pendingConflicts = pendingConflicts[1:]
		if len(pendingConflicts) == 0 {
			app.CloseOverlay()
			done := onConflictsResolved
			onConflictsResolved = nil
			if done != nil {
				done()
			}
			return
		}
		showNextConflict()
	}
	reloadconfirmView.OnCancel = func() {
		// Cancel abandons the WHOLE remaining queue, not just this one
		// file — matching quitconfirm's "Esc always means stay put": a
		// quit-and-save that hits a conflict does NOT quit.
		app.CloseOverlay()
		pendingConflicts = nil
		onConflictsResolved = nil
	}
	reloadconfirmView.OnKeepMine = func() {
		ws.KeepMine(pendingConflicts[0])
		advanceConflictQueue()
	}
	reloadconfirmView.OnReloadFromDisk = func() {
		ws.ReloadFromDisk(pendingConflicts[0])
		advanceConflictQueue()
	}
	// queueConflicts shows the first of conflicts (if any) and calls onDone
	// once every one of them has been resolved (immediately, if conflicts
	// is empty). The single code path both an interactive single-file save
	// (via the panes' OnSaveConflict, in ws.Wire below) and a save-all's batch
	// of conflicts (via saveAllDirty, further down) route through, so
	// there's exactly one queue-draining implementation.
	queueConflicts := func(conflicts []editor.SaveConflict, onDone func()) {
		if len(conflicts) == 0 {
			if onDone != nil {
				onDone()
			}
			return
		}
		pendingConflicts = conflicts
		onConflictsResolved = onDone
		showNextConflict()
	}

	// closetabconfirmView offers to save or discard one or more dirty tabs
	// before closing them — shown when a mouse-driven close (middle-click
	// on a tab, or "Close Others"/"Close All" from its context menu)
	// targets at least one dirty tab, via the panes'
	// OnRequestCloseDirtyTabs in ws.Wire below. Unlike ":q"/":qa" on a dirty buffer,
	// which just refuse silently in the debug log, a mouse action that
	// visibly does nothing reads as broken rather than refused, so this
	// offers a real choice instead. OnSaveAll/OnDiscardAll are re-pointed
	// at the specific tabs being closed on each request, the same pattern
	// memPromptView's targetPath uses further down.
	closetabconfirmView := closetabconfirm.New()
	closetabconfirmView.OnCancel = app.CloseOverlay

	// ws.Wire attaches every callback an editor pane needs to reach the
	// rest of the application — dialogs, git, the clipboard — to every pane,
	// including each one a split or session restore creates later, so a new
	// pane is never quietly missing a feature. Moving tabs between panes is
	// the workspace's own wiring.
	ws.Wire(func(v *editor.View) {
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
		v.OnShowFileHistory = openFileHistory
		// A save that finds its file changed on disk since nib last synced
		// with it (see editor.Buffer.HasDiskConflict) hands off here rather
		// than writing — queueConflicts shows reloadconfirmView, guarded
		// against clobbering whatever overlay is already up (e.g. two
		// panes racing to save the same shared Buffer at once), same guard
		// promptToFreeMemory uses below. onResolved maps directly onto
		// queueConflicts' onDone for this single-conflict batch: called
		// once Keep mine/Reload is chosen, never if the user cancels —
		// exactly the contract editor.View.OnSaveConflict documents, and
		// what lets a ":wq" that hit a conflict (see saveActiveThen)
		// correctly retry closing the tab afterward instead of not
		// quitting at all.
		v.OnSaveConflict = func(c editor.SaveConflict, onResolved func()) {
			if app.OverlayActive() {
				return
			}
			queueConflicts([]editor.SaveConflict{c}, onResolved)
		}
		// A mouse-driven close (middle-click, or "Close Others"/"Close All")
		// that would discard unsaved changes hands off here instead of
		// refusing — see closetabconfirmView above and
		// editor.View.OnRequestCloseDirtyTabs. Same "don't clobber
		// whatever's already up" guard OnSaveConflict uses.
		v.OnRequestCloseDirtyTabs = func(paths []string, onSaveAll, onDiscardAll func()) {
			if app.OverlayActive() {
				return
			}
			rel := make([]string, len(paths))
			for i, p := range paths {
				rel[i] = p
				if r, err := filepath.Rel(absRoot, p); err == nil {
					rel[i] = r
				}
			}
			closetabconfirmView.Show(rel)
			closetabconfirmView.OnSaveAll = func() {
				app.CloseOverlay()
				onSaveAll()
			}
			closetabconfirmView.OnDiscardAll = func() {
				app.CloseOverlay()
				onDiscardAll()
			}
			app.ShowOverlay(closetabconfirmView)
		}
		// Copying a mouse selection reaches the system clipboard through
		// here — the editor pane speaks no OSC 52 itself, same arrangement
		// as the git callbacks above. The yank register (SetRegister) is what
		// makes an in-nib "p" work, and is filled independently, so a
		// terminal that ignores OSC 52 costs only the crossing-out-of-nib
		// half of the copy.
		v.CopyFunc = app.CopyToClipboard
		// Rename ("R") and code actions ("A") both resolve to an
		// lsp.WorkspaceEdit that can span files this pane never opened, so
		// applying it needs ws.FindView (which pane, if any, has each affected
		// path open) — something only the workspace can answer.
		// Same "hand it back out, don't own it" split finderView's own
		// replace-all logic already makes (see OnReplaceAll below). v itself
		// is passed as the triggerView: whichever file ApplyWorkspaceEdit
		// can't find already open gets opened as a new tab right here, in
		// the pane the rename/action was actually invoked from.
		v.OnApplyWorkspaceEdit = func(edit lsp.WorkspaceEdit) {
			res := editor.ApplyWorkspaceEdit(edit, ws.FindView, v)
			for path, err := range res.Failed {
				debuglog.Error("apply rename: %s: %v", path, err)
			}
			refreshGitStatus()
		}
		// Showing the code-actions list is an overlay decision (this
		// package owns app.ShowOverlay, editor.View does not) — see caView
		// above, whose OnExecute applies the chosen action's edit through
		// the exact same ApplyWorkspaceEdit.
		v.OnCodeActions = func(actions []lsp.CodeAction) {
			caView.Open(actions)
			app.ShowOverlay(caView)
		}
	})
	// A tab-menu move or split inside a pane hands back where the tab
	// landed.
	ws.OnLayoutChanged = func(focus layout.LeafID) { rebuildAndFocus(app, focus) }

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

	// openFindReferences is Ctrl+F's global handler (see
	// globalDefaultKeybinds): tries the language server's real references
	// first (opening refView on a non-empty answer), and falls back to the
	// finder's content search — pre-filled with the word under the
	// cursor, exactly as this always behaved — both when no server could
	// take the request at all and when it answered with nothing found.
	// Same LSP-then-fallback shape goToDefinition uses (navigate.go).
	//
	// Like every focus-driven action, it acts on the workspace's target
	// pane: the focused one, or the last one focused when focus is
	// elsewhere (e.g. the file tree) — see Workspace.Target.
	openFindReferences := func() {
		v := ws.Target()
		word := v.WordUnderCursor()
		fallback := func() {
			finderView.OpenWithQuery(word)
			app.ShowOverlay(finderView)
		}
		dispatched := v.FindReferences(func(locs []lsp.Location, ok bool) {
			if !ok {
				fallback()
				return
			}
			refView.Open(locs)
			app.ShowOverlay(refView)
		})
		if !dispatched {
			fallback()
		}
	}
	// revealInTree is Ctrl+T's global handler: locates the current pane's
	// active file in the file tree and focuses it, so opening a
	// same-named file from the finder (e.g. one of several index.tsx)
	// shows exactly which one.
	revealInTree := func() {
		path := ws.Target().ActivePath()
		if path == "" {
			return
		}
		if treeView.Reveal(path) {
			app.FocusLeaf(fileTreeLeaf.ID)
		}
	}
	// splitPane and closePane are the split_right/split_down and close_pane
	// handlers; moveActiveTab is Alt+]/Alt+['s. Each acts on the target
	// pane and then focuses wherever the workspace says.
	splitPane := func(dir layout.Direction) {
		if id, ok := ws.Split(dir); ok {
			rebuildAndFocus(app, id)
		}
	}
	closePane := func() {
		if id, ok := ws.ClosePane(); ok {
			rebuildAndFocus(app, id)
		}
	}
	moveActiveTab := func(dir int) {
		if id, ok := ws.MoveActiveTab(dir); ok {
			app.FocusLeaf(id)
		}
	}

	refreshGitStatus = func() {
		direct, err := gitstatus.RunPorcelain(absRoot)
		if err != nil {
			return // not a git repo, or git unavailable: leave markers as-is
		}
		// The file tree also shows directories, so it needs the rolled-up
		// map; the finder only ever lists files, so the direct per-file
		// map is the right (and precise) one for it.
		treeView.ApplyStatus(gitstatus.Rollup(direct))
		treeView.ApplyChanges(direct)
		finderView.ApplyStatus(direct)
		gitSummary = gitstatus.Summary(direct)
		if branch, err := gitstatus.CurrentBranch(absRoot); err == nil {
			gitBranch = branch
		}
		ws.RefreshAllLineStatus()
	}
	refreshGitStatus()

	// Search-and-replace is finder.View's third mode (Tab-cycled, alongside
	// filename/content search), so it shares finderView's own overlay
	// rather than getting a separate one — see finder.View.Replace.
	finderView.Replace().SetKeymap(cfg.Overrides("replace"))
	finderView.Replace().OnClose = app.CloseOverlay
	// Reuses the same async plumbing finderView.Post already relies on —
	// see finder.ReplaceView.refilter.
	finderView.Replace().Post = app.Post
	finderView.Replace().OnReplaceAll = func(search, replacement string, occs []editor.Occurrence) {
		res := editor.Apply(search, replacement, occs, ws.FindView)
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
	// shortcut openFindReferences gives content-search mode — scoped to the
	// selected folder when the file tree is focused, mirroring openFinder.
	openReplace := func() {
		if id, ok := app.FocusedLeaf(); ok && id == fileTreeLeaf.ID {
			finderView.OpenReplaceScoped(treeView.SelectedDir())
		} else {
			finderView.OpenReplace()
		}
		app.ShowOverlay(finderView)
	}

	// saveAllDirty saves every unsaved file, then calls onDone — immediately
	// if nothing conflicted, or once every conflict found along the way has
	// been resolved through reloadconfirmView (see queueConflicts).
	saveAllDirty := func(onDone func()) {
		queueConflicts(ws.SaveAll(), onDone)
	}

	// quitConfirmView warns before quitting would silently discard unsaved
	// changes, instead of app.Quit firing unconditionally — see
	// internal/ui/quitconfirm.
	quitConfirmView := quitconfirm.New()
	quitConfirmView.OnCancel = app.CloseOverlay
	quitConfirmView.OnSaveAndQuit = func() {
		app.CloseOverlay()
		// app.Quit only fires once every conflict saveAllDirty finds is
		// actually resolved; Cancel on any of them (see reloadconfirmView)
		// aborts the quit entirely, same as declining quitConfirmView
		// itself would.
		saveAllDirty(app.Quit)
	}
	quitConfirmView.OnDiscardAndQuit = func() {
		app.CloseOverlay()
		app.Quit()
	}
	confirmQuit := func() {
		dirty := ws.DirtyPaths()
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

	// memThreshold is the heap size past which memWatcher (started near the
	// end of this function) prompts: memWatchThreshold unless the config
	// sets its own.
	memThreshold := memWatchThreshold
	if mib := cfg.MemWatchThresholdMiB(); mib > 0 {
		memThreshold = uint64(mib) << 20
	}

	// memPromptView offers to close whichever open file is using the most
	// memory, shown when memWatcher reports nib's own heap crossing
	// memThreshold — see
	// internal/memwatch and internal/ui/memprompt. targetPath is captured
	// by promptToFreeMemory below and read back by OnConfirmClose, since
	// the file being offered up isn't necessarily the one the user is
	// currently looking at.
	memPromptView := memprompt.New()
	var targetPath string
	// snoozeUntil is set whenever the user dismisses the prompt (either
	// button), so a noisy heap sample bouncing back above memThreshold
	// a few seconds later doesn't immediately reopen it — see
	// promptToFreeMemory below.
	var snoozeUntil time.Time
	memPromptView.OnCancel = func() {
		snoozeUntil = time.Now().Add(memPromptSnooze)
		app.CloseOverlay()
	}
	memPromptView.OnConfirmClose = func() {
		snoozeUntil = time.Now().Add(memPromptSnooze)
		app.CloseOverlay()
		ws.CloseTabByPath(targetPath)
	}
	promptToFreeMemory := func(heapBytes uint64) {
		if app.OverlayActive() {
			return // don't clobber whatever the user's already looking at
		}
		if time.Now().Before(snoozeUntil) {
			return // user already saw and handled this recently
		}
		path, size := ws.LargestBuffer()
		if path == "" {
			return
		}
		targetPath = path
		debuglog.Warn("memory: heap ~%d MiB exceeds ~%d MiB, prompting to close %s (~%d MiB)",
			heapBytes>>20, memThreshold>>20, path, size>>20)
		memPromptView.Show(path, size, int(heapBytes))
		app.ShowOverlay(memPromptView)
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
		"open_action_popup":    openActionPopup,
		"split_right":          func() { splitPane(layout.Horizontal) },
		"split_down":           func() { splitPane(layout.Vertical) },
		"close_pane":           closePane,
		"reveal_in_tree":       revealInTree,
		"move_tab_next_pane":   func() { moveActiveTab(1) },
		"move_tab_prev_pane":   func() { moveActiveTab(-1) },
	}

	// actionPopupView.OnExecute closes the popup first — restoring
	// whichever pane was really focused underneath it, since
	// ShowOverlay/CloseOverlay never touch the FocusManager — then
	// dispatches the selected command: global commands run straight
	// through the actions map just built, editor commands through
	// editor.View.ExecuteAction on the workspace's target pane, the same
	// "act on the editor pane you were just in" fallback
	// openFindReferences/revealInTree above already rely on.
	actionPopupView.OnExecute = func(id string) {
		app.CloseOverlay()
		if fn, ok := actions[id]; ok {
			fn()
			return
		}
		ws.Target().ExecuteAction(id)
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
		treeView.SetShowIcons(cfg.ShowIcons())
		ws.ApplyConfig(paneConfig(cfg))
		finderView.SetKeymap(cfg.Overrides("finder"))
		finderView.Replace().SetKeymap(cfg.Overrides("replace"))
		debugView.SetKeymap(cfg.Overrides("debug"))
		helpView.SetKeymap(cfg.Overrides("help"))
		actionPopupView.SetKeymap(cfg.Overrides("actionpopup"))
		diffView.SetKeymap(cfg.Overrides("diff"))
		historyView.SetKeymap(cfg.Overrides("history"))
		lspManager.SetServers(mergedLSPServers(cfg))
		rebuildGlobalKeymap()

		debuglog.Info("config reloaded from %s", cfgPath)
	}
	actions["reload_config"] = reloadConfig

	rebuildGlobalKeymap()

	treeView.OnOpen = func(path string) {
		app.FocusLeaf(ws.Open(path))
	}

	// OnPathMoved/OnPathDeleted are how the file tree's own create/rename/
	// delete actions reach the rest of the app. Both are called on the UI
	// goroutine AFTER the filesystem operation succeeded, with absolute
	// paths, and either path may be a directory — a folder rename moves
	// every file under it, which editor.View.Repath handles by prefix
	// itself, so nothing here does path arithmetic. The file tree updates
	// its own node tree, so neither of these calls Refresh.
	treeView.OnPathMoved = ws.Repath
	treeView.OnPathDeleted = ws.CloseDeleted
	treeView.OnShowHistory = openFileHistory
	// The context menu's "Copy Path" items — same clipboard route as the
	// editor panes' CopyFunc.
	treeView.CopyFunc = app.CopyToClipboard
	// Run last, after the tabs are carrying their new paths: refreshGitStatus
	// ends in ws.RefreshAllLineStatus, and ApplyLineStatus matches on a tab's
	// path — so doing this first would leave a moved file's gutter blank
	// until something else invalidated it.
	treeView.OnMutated = refreshGitStatus

	// Registered unconditionally (not just when the watcher starts):
	// finder.SearchResult must be handled regardless of whether fsnotify
	// watching is available, or content-search results posted via
	// finderView.Post would have nowhere to go.
	app.SetCustomEventHandler(func(ev interface{}) {
		switch e := ev.(type) {
		case terminateEvent:
			// Quit normally rather than dying mid-loop, so run() still
			// gets to save the session (see the end of run).
			app.Quit()
		case watch.RefreshEvent:
			debuglog.Debug("fsnotify refresh: gitChanged=%v fsChanged=%v", e.GitChanged, e.FSChanged)
			if e.FSChanged {
				treeView.Refresh()
				// RefreshEvent only says "something changed", never what, so
				// every open file is checked against disk: deleted ones close
				// (or detach, if dirty), changed clean ones reload, changed
				// dirty ones are flagged. See Workspace.SyncWithDisk.
				ws.SyncWithDisk()
			}
			// A plain unstaged edit (including this app's own Save) never
			// touches .git/index or HEAD, so it's only ever reported as
			// FSChanged, not GitChanged — but it still changes both the
			// file's porcelain status (clean -> modified, i.e. the marker
			// the file tree and finder show) and its line-level diff, so a
			// bare FSChanged has to re-run the same refresh a GitChanged
			// does. refreshGitStatus ends by calling ws.RefreshAllLineStatus,
			// so the line-level gutters are covered by this too.
			if e.GitChanged || e.FSChanged {
				refreshGitStatus()
			}
		case finder.SearchResult:
			finderView.ApplyContentResult(e)
		case finder.ReplaceSearchResult:
			finderView.Replace().ApplyReplaceSearchResult(e)
		case finder.FileListResult:
			finderView.ApplyFileListResult(e)
		case lsp.DiagnosticsEvent:
			ws.ApplyDiagnostics(e.Path, e.Diagnostics)
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
		case memoryThresholdEvent:
			promptToFreeMemory(e.heapBytes)
		}
	})

	// memWatcher periodically samples nib's own heap and, past
	// memThreshold, posts memoryThresholdEvent onto the event loop —
	// see internal/memwatch and promptToFreeMemory above. Mirrors the
	// fsnotify watcher goroutine just below: a background goroutine that
	// only ever talks to the UI by posting, never touching the workspace or
	// any View directly.
	memWatcher := memwatch.New(memThreshold, memWatchInterval, func(heap uint64) {
		app.Post(memoryThresholdEvent{heapBytes: heap})
	})
	memWatcher.Start()
	defer memWatcher.Close()

	if watcher, err := watch.New(absRoot, watchDebounce); err == nil {
		defer func() { _ = watcher.Close() }()

		// A directory reachable only through a symlink was never walked by
		// the watcher's startup scan (see watch.Watcher.AddDir) — ask it to
		// watch one the first time the tree actually expands into it, so an
		// external change inside it after that point still triggers a
		// refresh.
		treeView.OnDirExpanded = func(real string) { _ = watcher.AddDir(real) }

		go func() {
			for re := range watcher.Events() {
				app.Post(re)
			}
		}()
	} else {
		debuglog.Warn("filesystem watcher unavailable: %v", err)
	}

	if openOnStart != "" {
		app.FocusLeaf(ws.Open(openOnStart))
		treeView.Reveal(openOnStart)
	}

	// Per-project sessions (see internal/session): only when nib was
	// launched on a folder — opening a single file (e.g. `nib ~/.zshrc`)
	// must never ask to trust, or write into, whatever folder it lives in.
	// A trusted folder gets its last session back now and saved again on
	// exit; an untrusted one is asked about, every launch until trusted.
	trusted := false
	if openOnStart == "" {
		trusted = session.Trusted(absRoot)
		if trusted {
			restoreSession(absRoot, func(sess *session.Session) {
				if sess.TreeWidth > 0 {
					panes.Children[0].Hint = layout.Fixed(sess.TreeWidth)
				}
				focused, ok := ws.Restore(sess)
				if !ok {
					return
				}
				rebuildAndFocus(app, focused)
				if path := ws.Active().ActivePath(); path != "" {
					treeView.Reveal(path)
				}
			})
		} else {
			trustView := trustprompt.New()
			trustView.Show(absRoot)
			trustView.OnTrust = func() {
				app.CloseOverlay()
				if err := session.Trust(absRoot); err != nil {
					debuglog.Warn("trust folder: %v", err)
					return
				}
				trusted = true
			}
			trustView.OnDecline = app.CloseOverlay
			app.ShowOverlay(trustView)
		}
	}

	// Closing the terminal (SIGHUP) or a plain `kill` (SIGTERM) would
	// otherwise end the process before run() reaches the session save
	// below — route both through an ordinary quit instead.
	sigs := make(chan os.Signal, 1)
	signal.Notify(sigs, syscall.SIGHUP, syscall.SIGTERM)
	defer signal.Stop(sigs)
	go func() {
		<-sigs
		app.Post(terminateEvent{})
	}()

	runErr := app.Run()
	if trusted {
		sess := ws.Capture()
		// A dragged tree width is worth keeping even with nothing open.
		if w := panes.Children[0].Hint.Fixed; w != defaultTreeWidth {
			if sess == nil {
				sess = &session.Session{}
			}
			sess.TreeWidth = w
		}
		if err := session.Save(absRoot, sess); err != nil {
			debuglog.Warn("save session: %v", err)
		}
	}
	return runErr
}

// defaultTreeWidth is the file tree's width in columns until the user drags
// its border (see ui.App.beginResizeDrag); a dragged width is then kept in
// the session.
const defaultTreeWidth = 50

// terminateEvent is posted to the event loop when nib receives SIGHUP or
// SIGTERM, so the quit happens on the UI goroutine like any other.
type terminateEvent struct{}

// restoreSession loads root's saved session and hands it to apply, logging
// (rather than failing startup on) a session that can't be read — nib
// then just starts with the default empty layout.
func restoreSession(root string, apply func(*session.Session)) {
	sess, err := session.Load(root)
	if err != nil {
		debuglog.Warn("restore session: %v", err)
		return
	}
	if sess != nil {
		apply(sess)
	}
}
