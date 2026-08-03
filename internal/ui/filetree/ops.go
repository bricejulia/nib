package filetree

import (
	"errors"
	"os"
	"path/filepath"
	"strings"
)

// File and directory modes for entries created from the tree, matching the
// two places nib already writes to the filesystem: editor.defaultSaveMode
// for files, and config.EnsureFile's os.MkdirAll for directories.
const (
	newFileMode = 0o644
	newDirMode  = 0o755
)

// The refusals a create/rename/delete can produce. They're sentinel errors
// rather than formatted strings because their text is shown verbatim in the
// prompt row (see failPrompt) — short, lowercase, and readable in a pane
// that may only be a few dozen columns wide.
var (
	errEmptyName   = errors.New("name is empty")
	errOutsideRoot = errors.New("outside the project")
	errExists      = errors.New("already exists")
	errNotEmpty    = errors.New("directory is not empty")
	errIntoSelf    = errors.New("cannot move a directory into itself")
)

// resolveInRoot turns a user-typed, project-root-relative path into an
// absolute one, reporting whether it named a directory — signalled by a
// trailing "/", the same shorthand file managers and shells use.
//
// Everything typed is interpreted relative to root, and anything that would
// land outside it is refused: the tree can only show what's inside the
// project, so creating a file it could never display would look like the
// operation silently did nothing. relPath does that containment check
// already (and, by returning false for ".", refuses the project root
// itself — which is what makes "rename the root" fail safely for free).
//
// "." and ".." segments need no special handling: filepath.Join normalizes
// them, so "sub/../x" resolves to "x" exactly as it would in a shell, and
// an escape like "../x" is caught by the containment check.
func resolveInRoot(root, typed string) (abs string, isDir bool, err error) {
	t := strings.TrimSpace(typed)
	t = filepath.ToSlash(t)
	isDir = strings.HasSuffix(t, "/")
	t = strings.TrimRight(t, "/")
	if t == "" {
		return "", false, errEmptyName
	}
	if filepath.IsAbs(t) {
		return "", false, errOutsideRoot
	}
	abs = filepath.Join(root, filepath.FromSlash(t))
	if _, ok := relPath(root, abs); !ok {
		return "", false, errOutsideRoot
	}
	return abs, isDir, nil
}

// createEntry creates abs — a directory if isDir, otherwise an empty file —
// along with any missing parent directories, and refuses to touch an entry
// that already exists.
//
// Existence is checked by the create call itself (O_EXCL for a file, Mkdir's
// own EEXIST for a directory) rather than by a preceding Stat: there's no
// window in which something else can appear at abs between the check and
// the write, and os.MkdirAll would silently succeed on an existing
// directory anyway.
func createEntry(abs string, isDir bool) error {
	if err := os.MkdirAll(filepath.Dir(abs), newDirMode); err != nil {
		return err
	}
	if isDir {
		if err := os.Mkdir(abs, newDirMode); err != nil {
			if errors.Is(err, os.ErrExist) {
				return errExists
			}
			return err
		}
		return nil
	}
	f, err := os.OpenFile(abs, os.O_RDWR|os.O_CREATE|os.O_EXCL, newFileMode)
	if err != nil {
		if errors.Is(err, os.ErrExist) {
			return errExists
		}
		return err
	}
	return f.Close()
}

// movePath renames src to dst, creating any missing parent directories on
// the way (so a move into a folder that doesn't exist yet just works), and
// refuses to overwrite anything already at dst — os.Rename would clobber it
// without a word, which for a file open in an editor pane would replace its
// content underneath the buffer.
//
// The one existing-dst case that IS allowed is a rename that only changes
// letter case on a case-insensitive filesystem (macOS, most Windows
// setups): there, Lstat("A.txt") happily answers for "a.txt", so a plain
// existence check would refuse a perfectly ordinary rename. os.SameFile
// tells the two apart by inode.
func movePath(src, dst string) error {
	if src == dst {
		return nil
	}
	if _, inside := relPath(src, dst); inside {
		return errIntoSelf
	}
	if di, err := os.Lstat(dst); err == nil {
		si, serr := os.Lstat(src)
		if serr != nil || !os.SameFile(si, di) {
			return errExists
		}
	}
	if err := os.MkdirAll(filepath.Dir(dst), newDirMode); err != nil {
		return err
	}
	return os.Rename(src, dst)
}

// deletePath removes abs. A directory that still has entries in it is only
// removed when recursive is true — the caller is expected to have asked for
// a stronger confirmation first (see beginDelete), since this is
// permanent: nib has no trash and no undo for it.
//
// Symlinks need no special case. os.ReadDir reports a symlink to a
// directory as a plain file, so the tree already treats it as one, and
// os.Remove unlinks the symlink without touching whatever it points at.
func deletePath(abs string, recursive bool) error {
	err := os.Remove(abs)
	if err == nil {
		return nil
	}
	if dirEntryCount(abs) == 0 {
		return err // not a "directory not empty" failure: permissions, gone already, ...
	}
	if !recursive {
		return errNotEmpty
	}
	return os.RemoveAll(abs)
}

// dirEntryCount reports how many entries abs holds, counting dotfiles, and
// 0 for anything that isn't a readable directory. Used both to decide
// whether a delete needs the stronger confirmation and to say how much is
// about to be lost.
func dirEntryCount(abs string) int {
	entries, err := os.ReadDir(abs)
	if err != nil {
		return 0
	}
	return len(entries)
}
