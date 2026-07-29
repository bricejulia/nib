package editor

import (
	"path/filepath"
	"strings"
	"testing"
)

func fixturePath(t *testing.T, name string) string {
	t.Helper()
	abs, err := filepath.Abs(filepath.Join("../../../testdata", name))
	if err != nil {
		t.Fatal(err)
	}
	return abs
}

func TestLoadSplitsLinesWithoutTrailingNewline(t *testing.T) {
	buf, err := Load(fixturePath(t, "editor_sample.txt"))
	if err != nil {
		t.Fatalf("Load: %v", err)
	}
	if len(buf.Lines) != 4 {
		t.Fatalf("got %d lines, want 4: %+v", len(buf.Lines), buf.Lines)
	}
	if buf.Lines[0] != "line one" {
		t.Errorf("line 0 = %q", buf.Lines[0])
	}
	if buf.Lines[1] != "\ttabbed line" {
		t.Errorf("line 1 should retain its raw tab, got %q", buf.Lines[1])
	}
}

func TestLoadSourceMatchesLinesJoinedForTrailingNewlineFile(t *testing.T) {
	buf, err := Load(fixturePath(t, "editor_sample.txt")) // has a trailing \n on disk
	if err != nil {
		t.Fatalf("Load: %v", err)
	}
	want := strings.Join(buf.Lines, "\n")
	if string(buf.Source) != want {
		t.Errorf("Source = %q, want %q (Lines joined by \\n)", buf.Source, want)
	}
}

func TestLoadSourceMatchesLinesJoinedForNoTrailingNewlineFile(t *testing.T) {
	buf, err := Load(fixturePath(t, "no_trailing_newline.txt")) // no trailing \n on disk
	if err != nil {
		t.Fatalf("Load: %v", err)
	}
	if len(buf.Lines) != 1 || buf.Lines[0] != "no trailing newline" {
		t.Fatalf("got Lines=%+v", buf.Lines)
	}
	want := strings.Join(buf.Lines, "\n")
	if string(buf.Source) != want {
		t.Errorf("Source = %q, want %q", buf.Source, want)
	}
}

func TestLoadMissingFile(t *testing.T) {
	_, err := Load(fixturePath(t, "does-not-exist.txt"))
	if err == nil {
		t.Fatal("expected an error loading a missing file")
	}
}
