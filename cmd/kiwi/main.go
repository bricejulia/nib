// Command kiwi is the terminal editor's entrypoint. Step 0 wires up the
// window tree, focus routing, and a two-pane split (file tree | editor).
package main

import (
	"fmt"
	"os"
	"path/filepath"
	"time"

	"github.com/bricejulia/kiwi/internal/debuglog"
	"github.com/bricejulia/kiwi/internal/layout"
	"github.com/bricejulia/kiwi/internal/ui"
	"github.com/bricejulia/kiwi/internal/ui/debug"
	"github.com/bricejulia/kiwi/internal/ui/editor"
	"github.com/bricejulia/kiwi/internal/ui/filetree"
	"github.com/bricejulia/kiwi/internal/ui/finder"
	"github.com/bricejulia/kiwi/internal/ui/statusbar"
	"github.com/bricejulia/kiwi/internal/vcs/gitstatus"
	"github.com/bricejulia/kiwi/internal/vcs/watch"
)

// watchDebounce is the quiet period after the last observed filesystem
// change before a refresh fires.
const watchDebounce = 200 * time.Millisecond

func main() {
	if err := run(); err != nil {
		fmt.Fprintln(os.Stderr, "kiwi:", err)
		os.Exit(1)
	}
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

	treeView := filetree.New(absRoot)
	editorView := editor.NewView()
	statusBarView := statusbar.New()
	statusBarView.TextFunc = editorView.StatusText

	fileTreeLeaf := &layout.LeafNode{ID: 1, View: treeView}
	editorLeaf := &layout.LeafNode{ID: 2, View: editorView}
	statusBarLeaf := &layout.LeafNode{ID: 3, View: statusBarView}

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

	finderView := finder.New(absRoot)
	finderView.OnClose = app.CloseOverlay
	finderView.OnSelect = func(absPath string, line int) {
		editorView.OpenAtLine(absPath, line)
		app.FocusLeaf(editorLeaf.ID)
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
	debugView.OnClose = app.CloseOverlay
	openDebugLog := func() { app.ShowOverlay(debugView) }

	global := map[string]func(){
		"Ctrl+c": app.Quit,
		"Tab":    app.CycleFocus,
		// Double-tap Shift opens the same finder, but that only works on
		// terminals reporting bare modifier keypresses (kitty keyboard
		// protocol) — Ctrl+P is the conventional fallback everywhere else.
		"Ctrl+p": openFinder,
		"Ctrl+d": openDebugLog,
	}
	app.SetGlobalKeymap(global)

	treeView.OnOpen = func(path string) {
		editorView.Open(path)
		app.FocusLeaf(editorLeaf.ID)
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
			}
		case finder.SearchResult:
			finderView.ApplyContentResult(e)
		}
	})

	if watcher, err := watch.New(absRoot, watchDebounce); err == nil {
		defer watcher.Close()

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
