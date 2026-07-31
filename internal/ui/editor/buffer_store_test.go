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

func TestBufferStoreRekeyMovesTheEntryAndRepathsTheBuffer(t *testing.T) {
	dir := t.TempDir()
	oldPath := filepath.Join(dir, "a.txt")
	newPath := filepath.Join(dir, "b.txt")
	if err := os.WriteFile(oldPath, []byte("hi"), 0o644); err != nil {
		t.Fatal(err)
	}
	s := NewBufferStore()
	buf, err := s.Open(oldPath)
	if err != nil {
		t.Fatalf("Open: %v", err)
	}

	if !s.Rekey(buf, oldPath, newPath) {
		t.Fatal("Rekey should have succeeded")
	}
	if s.Len() != 1 {
		t.Fatalf("Len() = %d, want 1", s.Len())
	}
	if buf.Path != newPath {
		t.Errorf("buf.Path = %q, want %q — Save would otherwise recreate the old file", buf.Path, newPath)
	}
	again, err := s.Open(newPath)
	if err != nil {
		t.Fatalf("Open(newPath): %v", err)
	}
	if again != buf {
		t.Error("the new path should resolve to the same *Buffer")
	}
}

func TestBufferStoreRekeyCarriesTheReferenceCount(t *testing.T) {
	path := fixturePath(t, "editor_sample.txt")
	moved := filepath.Join(filepath.Dir(path), "moved.txt")
	s := NewBufferStore()
	buf, err := s.Open(path)
	if err != nil {
		t.Fatalf("Open: %v", err)
	}
	if _, err := s.Open(path); err != nil { // a second pane
		t.Fatalf("Open: %v", err)
	}

	if !s.Rekey(buf, path, moved) {
		t.Fatal("Rekey should have succeeded")
	}
	s.Release(moved)
	if s.Len() != 1 {
		t.Fatalf("Len() = %d after one Release, want 1 (the count must have survived the move)", s.Len())
	}
	s.Release(moved)
	if s.Len() != 0 {
		t.Fatalf("Len() = %d after both Releases, want 0", s.Len())
	}
}

// The same rename is fanned out to every View sharing the store, so the
// second and later calls have to be harmless no-ops rather than failures.
func TestBufferStoreRekeyIsIdempotentForASecondCaller(t *testing.T) {
	path := fixturePath(t, "editor_sample.txt")
	moved := filepath.Join(filepath.Dir(path), "moved.txt")
	s := NewBufferStore()
	buf, err := s.Open(path)
	if err != nil {
		t.Fatalf("Open: %v", err)
	}

	if !s.Rekey(buf, path, moved) {
		t.Fatal("first Rekey should have succeeded")
	}
	if !s.Rekey(buf, path, moved) {
		t.Error("a second Rekey for the same buffer should report success, not failure")
	}
	if s.Len() != 1 {
		t.Errorf("Len() = %d, want 1", s.Len())
	}
}

func TestBufferStoreRekeyRefusesADifferentBufferAtTheDestination(t *testing.T) {
	a := fixturePath(t, "editor_sample.txt")
	b := fixturePath(t, "no_trailing_newline.txt")
	s := NewBufferStore()
	bufA, err := s.Open(a)
	if err != nil {
		t.Fatalf("Open: %v", err)
	}
	if _, err := s.Open(b); err != nil {
		t.Fatalf("Open: %v", err)
	}

	if s.Rekey(bufA, a, b) {
		t.Fatal("Rekey should refuse to displace a different buffer")
	}
	if s.Len() != 2 {
		t.Errorf("Len() = %d, want 2 (both entries intact)", s.Len())
	}
	if bufA.Path != a {
		t.Errorf("bufA.Path = %q, want it unchanged at %q", bufA.Path, a)
	}
}

func TestBufferStoreRekeyRejectsAnUnknownOldPath(t *testing.T) {
	path := fixturePath(t, "editor_sample.txt")
	s := NewBufferStore()
	buf, err := s.Open(path)
	if err != nil {
		t.Fatalf("Open: %v", err)
	}

	if s.Rekey(buf, filepath.Join(filepath.Dir(path), "never-opened.txt"), filepath.Join(filepath.Dir(path), "x.txt")) {
		t.Error("Rekey should fail for a path the store doesn't hold")
	}
	if s.Rekey(nil, path, "/x.txt") {
		t.Error("Rekey(nil, ...) should fail")
	}
	if s.Rekey(buf, path, path) {
		t.Error("Rekey to the same path should report no move")
	}
}

// A rename can change the language outright, and highlighting is keyed on
// the extension — so the cached tree-sitter output has to be rebuilt, not
// carried over.
func TestBufferStoreRekeyRecomputesHighlightingWhenTheLanguageChanges(t *testing.T) {
	dir := t.TempDir()
	txt := filepath.Join(dir, "x.txt")
	if err := os.WriteFile(txt, []byte("package main\n\nfunc main() {}\n"), 0o644); err != nil {
		t.Fatal(err)
	}
	s := NewBufferStore()
	buf, err := s.Open(txt)
	if err != nil {
		t.Fatalf("Open: %v", err)
	}
	if buf.highlighted != nil {
		t.Fatal("a .txt file should have no tree-sitter highlighting to start with")
	}

	if !s.Rekey(buf, txt, filepath.Join(dir, "x.go")) {
		t.Fatal("Rekey should have succeeded")
	}
	if buf.highlighted == nil {
		t.Error("renaming .txt -> .go should have produced Go highlighting")
	}
}
