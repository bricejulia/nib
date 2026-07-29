// Package watch wraps fsnotify to deliver a single debounced refresh signal
// for a project tree, distinguishing "the git index/HEAD changed" (re-run
// git status) from "some other file changed" (re-scan the file tree).
package watch

import (
	"os"
	"path/filepath"
	"strings"
	"time"

	"github.com/fsnotify/fsnotify"
)

// RefreshEvent reports what kind of change was observed after debouncing.
type RefreshEvent struct {
	GitChanged bool // .git/HEAD or .git/index touched -> re-run git status
	FSChanged  bool // project tree touched -> re-scan the affected lazy-loaded Node
}

// Watcher watches a project tree and its repo's .git directory (HEAD/index
// only — never the rest of .git), debouncing bursts of events into a
// single merged RefreshEvent delivered on Events().
type Watcher struct {
	root     string
	gitDir   string
	fsw      *fsnotify.Watcher
	debounce time.Duration
	ignored  map[string]bool
	events   chan RefreshEvent
	done     chan struct{}
}

// New starts watching root. debounce is the quiet period after the last
// observed change before a RefreshEvent is emitted (~200ms is a reasonable
// default). If root is not a git repository, git-status refresh events are
// simply never produced — the file-tree watch still works.
func New(root string, debounce time.Duration) (*Watcher, error) {
	fsw, err := fsnotify.NewWatcher()
	if err != nil {
		return nil, err
	}

	ignored, err := ignoredDirs(root)
	if err != nil {
		ignored = map[string]bool{}
	}

	w := &Watcher{
		root:     root,
		gitDir:   filepath.Join(root, ".git"),
		fsw:      fsw,
		debounce: debounce,
		ignored:  ignored,
		events:   make(chan RefreshEvent, 1),
		done:     make(chan struct{}),
	}

	if err := w.addProjectTree(root); err != nil {
		fsw.Close()
		return nil, err
	}
	_ = fsw.Add(w.gitDir) // no .git directory: fine, just no git-change events

	go w.loop()
	return w, nil
}

// Events returns the channel RefreshEvents are delivered on.
func (w *Watcher) Events() <-chan RefreshEvent { return w.events }

// Close stops the watcher and releases the underlying fsnotify handle.
func (w *Watcher) Close() error {
	close(w.done)
	return w.fsw.Close()
}

// addProjectTree adds a non-recursive fsnotify watch on every directory
// under dir, skipping .git (watched separately) and anything ignoredDirs
// reported — so vendor/, node_modules/, and friends are never walked or
// watched.
func (w *Watcher) addProjectTree(dir string) error {
	return filepath.WalkDir(dir, func(path string, d os.DirEntry, err error) error {
		if err != nil {
			return nil // a permission error on one subtree shouldn't abort the whole walk
		}
		if !d.IsDir() {
			return nil
		}
		if path == w.gitDir {
			return filepath.SkipDir
		}
		if rel, ok := relPath(w.root, path); ok && w.ignored[rel] {
			return filepath.SkipDir
		}
		return w.fsw.Add(path)
	})
}

func relPath(root, path string) (string, bool) {
	rel, err := filepath.Rel(root, path)
	if err != nil || rel == "." || strings.HasPrefix(rel, "..") {
		return "", false
	}
	return filepath.ToSlash(rel), true
}

// loop runs entirely on one goroutine: the debounce timer's channel is
// read in the same select as fsnotify's Events/Errors, so the pending
// RefreshEvent accumulator is never touched concurrently.
func (w *Watcher) loop() {
	var timer *time.Timer
	var timerC <-chan time.Time
	var pending RefreshEvent

	for {
		select {
		case ev, ok := <-w.fsw.Events:
			if !ok {
				return
			}
			w.classify(ev, &pending)
			if timer == nil {
				timer = time.NewTimer(w.debounce)
			} else {
				if !timer.Stop() {
					select {
					case <-timer.C:
					default:
					}
				}
				timer.Reset(w.debounce)
			}
			timerC = timer.C

		case <-timerC:
			w.events <- pending
			pending = RefreshEvent{}
			timerC = nil

		case <-w.fsw.Errors:
			// Nothing sensible to do with a single watch error in Step 0;
			// the watcher keeps running on its remaining watched paths.

		case <-w.done:
			if timer != nil {
				timer.Stop()
			}
			return
		}
	}
}

func (w *Watcher) classify(ev fsnotify.Event, pending *RefreshEvent) {
	dir := filepath.Dir(ev.Name)
	base := filepath.Base(ev.Name)

	if dir == w.gitDir && (base == "HEAD" || base == "index") {
		pending.GitChanged = true
		return
	}
	if ev.Name == w.gitDir || strings.HasPrefix(ev.Name, w.gitDir+string(filepath.Separator)) {
		return // the rest of .git is deliberately not watched/reported
	}

	pending.FSChanged = true

	if ev.Has(fsnotify.Create) {
		if fi, err := os.Stat(ev.Name); err == nil && fi.IsDir() {
			if rel, ok := relPath(w.root, ev.Name); !ok || !w.ignored[rel] {
				_ = w.fsw.Add(ev.Name)
			}
		}
	}
}
