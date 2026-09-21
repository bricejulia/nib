package filetree

import (
	"errors"
	"io"
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

// copyPath duplicates src at dst — a file byte-for-byte with its original
// mode, a directory recursively — creating any missing parent directories
// on the way, same as movePath. Unlike movePath there's no case-only-rename
// exception to make: a copy onto its own case-insensitive alias is refused
// like any other existing target, since nothing about it is a rename.
//
// A directory copied into its own subtree is refused the same way a move
// into itself is (see movePath) — copying "sub" to "sub/inner/sub" would
// otherwise recurse into the very tree it's still writing.
func copyPath(src, dst string) error {
	if src == dst {
		return errExists
	}
	if _, inside := relPath(src, dst); inside {
		return errIntoSelf
	}
	if _, err := os.Lstat(dst); err == nil {
		return errExists
	}
	if err := os.MkdirAll(filepath.Dir(dst), newDirMode); err != nil {
		return err
	}
	if err := copyAny(src, dst); err != nil {
		if errors.Is(err, os.ErrExist) {
			return errExists
		}
		return err
	}
	return nil
}

// copyAny copies one filesystem entry at src to dst, dispatching on what
// src actually is. A symlink is recreated pointing at the same target
// rather than followed — resolving it and copying whatever it points to
// could reach outside the project entirely, and deletePath already treats
// a symlink as the link itself rather than its target for the same reason.
func copyAny(src, dst string) error {
	info, err := os.Lstat(src)
	if err != nil {
		return err
	}
	switch {
	case info.Mode()&os.ModeSymlink != 0:
		target, err := os.Readlink(src)
		if err != nil {
			return err
		}
		return os.Symlink(target, dst)
	case info.IsDir():
		return copyDir(src, dst, info.Mode().Perm())
	default:
		return copyFile(src, dst, info.Mode().Perm())
	}
}

// copyDir creates dst and copies every entry of src into it, recursively.
func copyDir(src, dst string, mode os.FileMode) error {
	if err := os.Mkdir(dst, mode); err != nil {
		return err
	}
	entries, err := os.ReadDir(src)
	if err != nil {
		return err
	}
	for _, e := range entries {
		if err := copyAny(filepath.Join(src, e.Name()), filepath.Join(dst, e.Name())); err != nil {
			return err
		}
	}
	return nil
}

// copyFile copies src's bytes to a newly created dst with mode, refusing
// (via O_EXCL) to clobber anything already there.
func copyFile(src, dst string, mode os.FileMode) error {
	in, err := os.Open(src)
	if err != nil {
		return err
	}
	defer func() { _ = in.Close() }()
	out, err := os.OpenFile(dst, os.O_RDWR|os.O_CREATE|os.O_EXCL, mode)
	if err != nil {
		return err
	}
	// The deferred close is only a backstop for an early return via
	// io.Copy's error; the real close below is the one whose error is
	// checked, since a buffered write failing at Close time is exactly
	// the kind of error this function exists to surface.
	defer func() { _ = out.Close() }()
	if _, err := io.Copy(out, in); err != nil {
		return err
	}
	return out.Close()
}

// deletePath removes abs. A directory that still has entries in it is only
// removed when recursive is true — the caller is expected to have asked for
// a stronger confirmation first (see beginDelete), since this is
// permanent: nib has no trash and no undo for it.
//
// Symlinks need no special case: os.Remove unlinks a symlink without
// touching whatever it points at, even a followed directory symlink whose
// target isn't empty — see beginDelete, which routes every symlink through
// the single-keypress confirm rather than the recursive one for exactly
// this reason.
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
