package watch

import (
	"os"
	"path/filepath"
	"testing"
	"time"
)

const testDebounce = 50 * time.Millisecond
const testTimeout = 2 * time.Second

func waitForEvent(t *testing.T, w *Watcher) (RefreshEvent, bool) {
	t.Helper()
	select {
	case ev := <-w.Events():
		return ev, true
	case <-time.After(testTimeout):
		return RefreshEvent{}, false
	}
}

func expectNoEvent(t *testing.T, w *Watcher, within time.Duration) {
	t.Helper()
	select {
	case ev := <-w.Events():
		t.Fatalf("expected no event within %v, got %+v", within, ev)
	case <-time.After(within):
	}
}

func TestWatcherDebouncesBurstOfWritesIntoOneEvent(t *testing.T) {
	dir := newTestRepo(t)
	w, err := New(dir, testDebounce)
	if err != nil {
		t.Fatalf("New: %v", err)
	}
	defer w.Close()

	target := filepath.Join(dir, "src", "main.go")
	for i := 0; i < 5; i++ {
		if err := os.WriteFile(target, []byte("package main // edit "+string(rune('a'+i))), 0o644); err != nil {
			t.Fatal(err)
		}
		time.Sleep(5 * time.Millisecond) // well under the debounce window
	}

	ev, ok := waitForEvent(t, w)
	if !ok {
		t.Fatal("expected a debounced RefreshEvent, got none")
	}
	if !ev.FSChanged {
		t.Errorf("expected FSChanged=true, got %+v", ev)
	}

	// A second, distinct event should not follow immediately — the burst
	// must have collapsed into exactly one.
	expectNoEvent(t, w, testDebounce*3)
}

func TestWatcherReportsGitChangedForHeadAndIndex(t *testing.T) {
	dir := newTestRepo(t)
	w, err := New(dir, testDebounce)
	if err != nil {
		t.Fatalf("New: %v", err)
	}
	defer w.Close()

	head := filepath.Join(dir, ".git", "HEAD")
	if err := os.WriteFile(head, []byte("ref: refs/heads/other\n"), 0o644); err != nil {
		t.Fatal(err)
	}

	ev, ok := waitForEvent(t, w)
	if !ok {
		t.Fatal("expected a RefreshEvent for a HEAD change, got none")
	}
	if !ev.GitChanged {
		t.Errorf("expected GitChanged=true, got %+v", ev)
	}
}

func TestWatcherIgnoresRestOfGitDirectory(t *testing.T) {
	dir := newTestRepo(t)
	w, err := New(dir, testDebounce)
	if err != nil {
		t.Fatalf("New: %v", err)
	}
	defer w.Close()

	// logs/ and objects/ are inside .git but are not HEAD/index; per the
	// "ignore the rest of .git" contract these must not surface events.
	logsDir := filepath.Join(dir, ".git", "logs")
	if err := os.MkdirAll(logsDir, 0o755); err != nil {
		t.Fatal(err)
	}
	if err := os.WriteFile(filepath.Join(logsDir, "some-log"), []byte("x"), 0o644); err != nil {
		t.Fatal(err)
	}

	expectNoEvent(t, w, testDebounce*3)
}

func TestWatcherDoesNotWatchIgnoredDirectories(t *testing.T) {
	dir := newTestRepo(t)
	w, err := New(dir, testDebounce)
	if err != nil {
		t.Fatalf("New: %v", err)
	}
	defer w.Close()

	// vendor/ is listed in .gitignore and must never have been added to
	// the fsnotify watch set.
	if err := os.WriteFile(filepath.Join(dir, "vendor", "pkg", "new.go"), []byte("x"), 0o644); err != nil {
		t.Fatal(err)
	}

	expectNoEvent(t, w, testDebounce*3)
}

func TestWatcherDetectsChangeInNewlyCreatedDirectory(t *testing.T) {
	dir := newTestRepo(t)
	w, err := New(dir, testDebounce)
	if err != nil {
		t.Fatalf("New: %v", err)
	}
	defer w.Close()

	newDir := filepath.Join(dir, "newpkg")
	if err := os.Mkdir(newDir, 0o755); err != nil {
		t.Fatal(err)
	}
	// Drain the event for the mkdir itself before testing that files
	// created inside it are also detected (proves it was added to the
	// watch set dynamically, not just that mkdir fired an event).
	waitForEvent(t, w)

	if err := os.WriteFile(filepath.Join(newDir, "file.go"), []byte("x"), 0o644); err != nil {
		t.Fatal(err)
	}
	ev, ok := waitForEvent(t, w)
	if !ok {
		t.Fatal("expected a RefreshEvent for a file created inside a newly created directory")
	}
	if !ev.FSChanged {
		t.Errorf("expected FSChanged=true, got %+v", ev)
	}
}

func TestNewOnNonRepoStillWatchesFilesystem(t *testing.T) {
	dir := t.TempDir() // not a git repo
	if err := os.WriteFile(filepath.Join(dir, "a.txt"), []byte("x"), 0o644); err != nil {
		t.Fatal(err)
	}

	w, err := New(dir, testDebounce)
	if err != nil {
		t.Fatalf("New should succeed even outside a git repo: %v", err)
	}
	defer w.Close()

	if err := os.WriteFile(filepath.Join(dir, "a.txt"), []byte("y"), 0o644); err != nil {
		t.Fatal(err)
	}
	ev, ok := waitForEvent(t, w)
	if !ok {
		t.Fatal("expected an FSChanged event even without a .git directory")
	}
	if !ev.FSChanged || ev.GitChanged {
		t.Errorf("got %+v, want FSChanged=true, GitChanged=false", ev)
	}
}
