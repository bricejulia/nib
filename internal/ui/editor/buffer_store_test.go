package editor

import (
	"os"
	"path/filepath"
	"testing"
)

func TestBufferStoreOpenReturnsSamePointerForSamePath(t *testing.T) {
	path := fixturePath(t, "editor_sample.txt")
	s := NewBufferStore()

	b1, err := s.Open(path)
	if err != nil {
		t.Fatalf("Open: %v", err)
	}
	b2, err := s.Open(path)
	if err != nil {
		t.Fatalf("Open: %v", err)
	}

	if b1 != b2 {
		t.Fatal("expected the same *Buffer for the same path")
	}
	if s.Len() != 1 {
		t.Fatalf("Len() = %d, want 1", s.Len())
	}
}

func TestBufferStoreDifferentPathsAreIndependent(t *testing.T) {
	s := NewBufferStore()

	b1, err := s.Open(fixturePath(t, "editor_sample.txt"))
	if err != nil {
		t.Fatalf("Open: %v", err)
	}
	b2, err := s.Open(fixturePath(t, "no_trailing_newline.txt"))
	if err != nil {
		t.Fatalf("Open: %v", err)
	}

	if b1 == b2 {
		t.Fatal("expected distinct Buffers for distinct paths")
	}
	if s.Len() != 2 {
		t.Fatalf("Len() = %d, want 2", s.Len())
	}
}

func TestBufferStoreEvictsOnceEveryReferenceIsReleased(t *testing.T) {
	path := fixturePath(t, "editor_sample.txt")
	s := NewBufferStore()

	if _, err := s.Open(path); err != nil {
		t.Fatalf("Open: %v", err)
	}
	if _, err := s.Open(path); err != nil { // second reference
		t.Fatalf("Open: %v", err)
	}

	s.Release(path)
	if s.Len() != 1 {
		t.Fatalf("Len() = %d, want 1 (one reference still held)", s.Len())
	}

	s.Release(path)
	if s.Len() != 0 {
		t.Fatalf("Len() = %d, want 0 (last reference released)", s.Len())
	}
}

func TestBufferStoreReopenAfterEvictionReloadsFromDisk(t *testing.T) {
	dir := t.TempDir()
	path := filepath.Join(dir, "f.txt")
	if err := os.WriteFile(path, []byte("v1"), 0o644); err != nil {
		t.Fatal(err)
	}

	s := NewBufferStore()
	b1, err := s.Open(path)
	if err != nil {
		t.Fatalf("Open: %v", err)
	}
	s.Release(path)
	if s.Len() != 0 {
		t.Fatalf("Len() = %d, want 0 after releasing the only reference", s.Len())
	}

	if err := os.WriteFile(path, []byte("v2"), 0o644); err != nil {
		t.Fatal(err)
	}

	b2, err := s.Open(path)
	if err != nil {
		t.Fatalf("Open: %v", err)
	}
	if b1 == b2 {
		t.Fatal("expected a fresh *Buffer after eviction, not the stale cached one")
	}
	if b2.Lines[0] != "v2" {
		t.Fatalf("Lines[0] = %q, want %q (freshly reloaded from disk)", b2.Lines[0], "v2")
	}
}

func TestBufferStoreOpenPropagatesLoadErrorAndRegistersNothing(t *testing.T) {
	s := NewBufferStore()

	buf, err := s.Open(fixturePath(t, "does-not-exist.txt"))
	if err == nil {
		t.Fatal("expected an error for a missing file")
	}
	if buf != nil {
		t.Fatal("expected a nil Buffer on load failure")
	}
	if s.Len() != 0 {
		t.Fatalf("Len() = %d, want 0 (a failed load must not register anything)", s.Len())
	}
}

func TestBufferStoreReleaseOnUnknownPathIsNoop(t *testing.T) {
	s := NewBufferStore()
	s.Release("/never/opened.txt") // must not panic
	if s.Len() != 0 {
		t.Fatalf("Len() = %d, want 0", s.Len())
	}
}
