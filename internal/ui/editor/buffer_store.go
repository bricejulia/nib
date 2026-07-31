package editor

// BufferStore shares one *Buffer per path across every View that opens
// it — the standard editor model (a buffer is the shared document; a tab
// is just a view into it), so the same file open in two split panes is
// the same in-memory document: an edit or Save in one pane is immediately
// visible in the other, and neither can silently clobber the other's
// unsaved work. Reference-counted, so a Buffer sticks around only as long
// as at least one tab anywhere still has it open.
//
// Not safe for concurrent use — like every other piece of View/Buffer
// state, it's only ever touched from the single UI event-loop goroutine
// (external events, e.g. the fsnotify watcher's own goroutine, are
// already marshaled back onto that goroutine via App.Post before
// touching any View/Buffer state), so a lock here would be pure ceremony.
type BufferStore struct {
	bufs map[string]*storedBuffer
}

type storedBuffer struct {
	buf   *Buffer
	count int
}

// NewBufferStore returns an empty store.
func NewBufferStore() *BufferStore {
	return &BufferStore{bufs: map[string]*storedBuffer{}}
}

// Open returns the shared *Buffer for path, incrementing its reference
// count. The first call for a given path loads it fresh (see Load); every
// subsequent call, from any View sharing this store, returns the SAME
// *Buffer instead of reading the file again. Each successful call must be
// paired with exactly one Release(path) when the tab that opened it
// closes. A Load failure is propagated as-is and registers nothing — a
// path currently unopenable simply gets retried fresh on the next Open.
func (s *BufferStore) Open(path string) (*Buffer, error) {
	if sb, ok := s.bufs[path]; ok {
		sb.count++
		return sb.buf, nil
	}
	buf, err := Load(path)
	if err != nil {
		return nil, err
	}
	s.bufs[path] = &storedBuffer{buf: buf, count: 1}
	return buf, nil
}

// Release drops one reference to path's Buffer, evicting it from the
// store once nothing references it anymore (Go's GC reclaims the Buffer
// itself once every tab pointing to it is gone too). A no-op for a path
// that was never successfully Open'd through this store — safe to call
// on any tab regardless of how its buffer came to exist.
func (s *BufferStore) Release(path string) {
	sb, ok := s.bufs[path]
	if !ok {
		return
	}
	sb.count--
	if sb.count <= 0 {
		delete(s.bufs, path)
	}
}

// Rekey moves buf's entry from oldPath to newPath after the file was
// renamed or moved on disk, carrying its reference count across wholesale
// (the entry itself moves, so there's no count arithmetic to get wrong) and
// repathing the Buffer so its Path — what Save writes to — can never
// disagree with the key it's filed under.
//
// Reports whether newPath now maps to buf. That deliberately includes the
// case where a previous call already moved it: the same rename is fanned out
// to every View sharing this store (each has its own tab to fix up), so only
// the first call does work and the rest must be harmless no-ops rather than
// failures.
//
// False means the store could not represent the move — newPath is taken by a
// DIFFERENT buffer, or buf was never registered under oldPath — and the
// caller must then leave its tab's path alone: a tab whose path disagrees
// with the store leaks the entry forever, because its eventual Release would
// miss. Two entries are never merged: they're distinct Buffers with
// different content and different saved baselines, and evicting one while
// tabs elsewhere still point at it would corrupt the survivor's count.
func (s *BufferStore) Rekey(buf *Buffer, oldPath, newPath string) bool {
	if buf == nil || oldPath == newPath {
		return false
	}
	if sb, ok := s.bufs[newPath]; ok {
		return sb.buf == buf // already moved by another View, or a real collision
	}
	sb, ok := s.bufs[oldPath]
	if !ok || sb.buf != buf {
		return false
	}
	delete(s.bufs, oldPath)
	s.bufs[newPath] = sb
	buf.Repath(newPath)
	return true
}

// Len reports how many distinct paths currently have a live Buffer
// tracked — test/diagnostic use.
func (s *BufferStore) Len() int {
	return len(s.bufs)
}
