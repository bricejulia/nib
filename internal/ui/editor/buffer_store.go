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

// Len reports how many distinct paths currently have a live Buffer
// tracked — test/diagnostic use.
func (s *BufferStore) Len() int {
	return len(s.bufs)
}
