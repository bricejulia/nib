package workspace

import (
	"os"
	"sort"
	"strings"

	"github.com/bricejulia/nib/internal/debuglog"
	"github.com/bricejulia/nib/internal/lsp"
	"github.com/bricejulia/nib/internal/ui/editor"
)

// The same file can be open in several panes at once, each with its own
// tab for it but all sharing one Buffer (see editor.BufferStore), so
// everything below visits every pane and lets each one ignore paths it
// doesn't have open.

// refreshLineStatus recomputes path's git gutter markers and applies them
// to every pane showing it.
func (w *Workspace) refreshLineStatus(path string) {
	if w.opts.LineStatus == nil {
		return
	}
	lines, err := w.opts.LineStatus(path)
	if err != nil {
		return // not a git repo, path outside it, or git unavailable
	}
	for _, p := range w.panes {
		p.view.ApplyLineStatus(path, lines)
	}
}

// RefreshAllLineStatus recomputes the git gutter of every open file — for
// whenever something not scoped to one file may have made them stale: a
// git change (staging, commit, checkout) or any filesystem change.
func (w *Workspace) RefreshAllLineStatus() {
	for _, p := range w.panes {
		for _, path := range p.view.OpenPaths() {
			w.refreshLineStatus(path)
		}
	}
}

// ApplyDiagnostics shows a language server's diagnostics for path in every
// pane that has it open.
func (w *Workspace) ApplyDiagnostics(path string, diags []lsp.Diagnostic) {
	for _, p := range w.panes {
		p.view.ApplyDiagnostics(path, diags)
	}
}

// Repath retargets every tab on oldPath (or, for a directory, under it) to
// newPath after a rename or move. The shared Buffer is re-keyed once.
func (w *Workspace) Repath(oldPath, newPath string) {
	for _, p := range w.panes {
		p.view.Repath(oldPath, newPath)
	}
}

// CloseDeleted handles path (a file or directory) being gone from disk:
// clean tabs on it close outright; a dirty one stays open, detached, so
// ":w" can recreate the file.
func (w *Workspace) CloseDeleted(path string) {
	var detached []string
	for _, p := range w.panes {
		detached = append(detached, p.view.CloseTabsUnder(path)...)
	}
	if len(detached) > 0 {
		// The durable record of what happened; the panes themselves show
		// it as "-- DELETED --" and a ✗ in the tab bar.
		debuglog.Warn("deleted %s: %d open buffer(s) with unsaved changes stay open (\":w\" recreates the file, \":q!\" discards): %s",
			path, len(detached), strings.Join(detached, ", "))
	}
}

// SyncWithDisk checks every open file against disk after the filesystem
// watcher reported that something changed (it never says what):
//   - a file deleted outside nib is handled like CloseDeleted;
//   - a file changed outside nib is flagged in its tabs, and, when it has
//     no unsaved edits to lose, silently reloaded;
//   - a dirty one is only flagged — saving it asks the user (see
//     editor.View.OnSaveConflict).
func (w *Workspace) SyncWithDisk() {
	for _, path := range w.openPathsNotYetDetached() {
		info, err := os.Stat(path)
		if os.IsNotExist(err) {
			w.CloseDeleted(path)
			continue
		}
		if err != nil {
			continue // unreadable for some other reason; leave state as-is
		}
		for _, p := range w.panes {
			p.view.NoteDiskStat(path, info)
		}
		if buf := w.bufferForPath(path); buf != nil && buf.ExternallyModified && !buf.Dirty {
			if err := buf.Reload(); err != nil {
				debuglog.Warn("auto-reload %s: %v", path, err)
				continue
			}
			for _, p := range w.panes {
				p.view.RefreshBuffer(buf)
				p.view.MarkJustReloaded(buf)
			}
			w.refreshLineStatus(path)
		}
	}
}

// openPathsNotYetDetached lists every open file, deduplicated, except ones
// already known to be gone (see editor.View.CloseTabsUnder) — there's no
// need to stat those again on every refresh.
func (w *Workspace) openPathsNotYetDetached() []string {
	seen := map[string]bool{}
	var paths []string
	for _, p := range w.panes {
		detached := map[string]bool{}
		for _, path := range p.view.DetachedPaths() {
			detached[path] = true
		}
		for _, path := range p.view.OpenPaths() {
			if seen[path] || detached[path] {
				continue
			}
			seen[path] = true
			paths = append(paths, path)
		}
	}
	return paths
}

func (w *Workspace) bufferForPath(path string) *editor.Buffer {
	for _, p := range w.panes {
		if buf := p.view.BufferForPath(path); buf != nil {
			return buf
		}
	}
	return nil
}

// DirtyPaths lists every open file with unsaved changes, deduplicated and
// sorted.
func (w *Workspace) DirtyPaths() []string {
	seen := map[string]bool{}
	var paths []string
	for _, p := range w.panes {
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

// SaveAll saves every file with unsaved changes, logging write failures,
// and returns the ones it skipped because they changed on disk since nib
// last synced with them, for the caller to resolve with the user — see
// KeepMine and ReloadFromDisk. A Buffer shared by two panes is no longer
// Dirty by the time the second pane is visited, so nothing is written
// twice.
func (w *Workspace) SaveAll() []editor.SaveConflict {
	var conflicts []editor.SaveConflict
	for _, p := range w.panes {
		res := p.view.SaveDirtyTabs()
		for path, err := range res.Failed {
			debuglog.Error("save %s: %v", path, err)
		}
		conflicts = append(conflicts, res.Conflicts...)
	}
	return conflicts
}

// KeepMine resolves c by overwriting disk with the buffer.
func (w *Workspace) KeepMine(c editor.SaveConflict) {
	if err := c.Buf.Save(); err != nil {
		debuglog.Error("save %s: %v", c.Path, err)
		return
	}
	// Save just cleared ExternallyModified, which is what lets the gutter
	// accept fresh markers again — nothing else would ask for them.
	w.refreshLineStatus(c.Path)
}

// ReloadFromDisk resolves c by discarding the buffer's edits for what's on
// disk.
func (w *Workspace) ReloadFromDisk(c editor.SaveConflict) {
	if err := c.Buf.Reload(); err != nil {
		debuglog.Error("reload %s: %v", c.Path, err)
		return
	}
	for _, p := range w.panes {
		p.view.RefreshBuffer(c.Buf)
	}
	w.refreshLineStatus(c.Path)
}

// LargestBuffer returns the path and byte size of the largest open buffer,
// or ("", 0) if nothing is open — the likeliest culprit for high memory use.
func (w *Workspace) LargestBuffer() (path string, sizeBytes int) {
	for _, p := range w.panes {
		if pPath, pSize := p.view.LargestBuffer(); pSize > sizeBytes {
			path, sizeBytes = pPath, pSize
		}
	}
	return path, sizeBytes
}

// CloseTabByPath closes the first tab found on path, reporting whether
// there was one.
func (w *Workspace) CloseTabByPath(path string) bool {
	for _, p := range w.panes {
		if p.view.CloseTabByPath(path) {
			return true
		}
	}
	return false
}
