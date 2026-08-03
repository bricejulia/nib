// Package debuglog is a small in-process ring buffer that any package can
// append messages to via Debug/Info/Warn/Error, without importing the UI
// layer. The debug view (internal/ui/debug) reads it back out to render a
// log pane toggled by a keyboard shortcut — see cmd/nib/main.go.
package debuglog

import (
	"fmt"
	"sync"
	"time"
)

// Level is a message's severity. Levels are ordered by increasing
// severity, so a min-level filter (e.g. in the debug view) can compare
// with >=.
type Level int

const (
	LevelDebug Level = iota
	LevelInfo
	LevelWarn
	LevelError
)

// String renders the level as a fixed-width label for display, e.g. in
// the debug view.
func (l Level) String() string {
	switch l {
	case LevelDebug:
		return "DEBUG"
	case LevelInfo:
		return "INFO"
	case LevelWarn:
		return "WARN"
	case LevelError:
		return "ERROR"
	default:
		return "?"
	}
}

// Entry is one recorded debug message.
type Entry struct {
	Time  time.Time
	Level Level
	Text  string
}

// maxEntries bounds memory use: once full, the oldest entry is dropped for
// every new one appended, so a long-running session's log can't grow
// unbounded.
const maxEntries = 1000

var (
	mu      sync.Mutex
	entries []Entry
)

// log formats and appends a message at level. Safe to call from any
// goroutine — callers don't need to know whether they're on the UI thread
// (e.g. the filesystem watcher and finder's content-search both run on
// their own goroutines).
func log(level Level, format string, args ...any) {
	e := Entry{Time: time.Now(), Level: level, Text: fmt.Sprintf(format, args...)}

	mu.Lock()
	defer mu.Unlock()
	entries = append(entries, e)
	if len(entries) > maxEntries {
		entries = entries[len(entries)-maxEntries:]
	}
}

// Debug records a low-level message useful only while actively debugging
// (e.g. "fsnotify refresh fired").
func Debug(format string, args ...any) { log(LevelDebug, format, args...) }

// Info records a routine, expected event worth a record but not attention
// (e.g. "opened file X").
func Info(format string, args ...any) { log(LevelInfo, format, args...) }

// Warn records something unexpected that the app recovered from on its own
// (e.g. a feature falling back to a degraded mode).
func Warn(format string, args ...any) { log(LevelWarn, format, args...) }

// Error records a failure the user may need to know about.
func Error(format string, args ...any) { log(LevelError, format, args...) }

// Entries returns a snapshot of every recorded message, oldest first. The
// returned slice is a copy, so callers can range over it without holding
// the package's lock.
func Entries() []Entry {
	mu.Lock()
	defer mu.Unlock()
	out := make([]Entry, len(entries))
	copy(out, entries)
	return out
}
