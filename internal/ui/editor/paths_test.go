package editor

import (
	"path/filepath"
	"testing"
)

func TestPathUnder(t *testing.T) {
	abs := func(parts ...string) string {
		return filepath.Join(append([]string{string(filepath.Separator), "a"}, parts...)...)
	}

	cases := []struct {
		target, p string
		want      bool
	}{
		{abs("foo"), abs("foo"), true},                 // the path itself
		{abs("foo"), abs("foo", "x.go"), true},         // one level under
		{abs("foo"), abs("foo", "sub", "x.go"), true},  // several levels under
		{abs("foo"), abs("foobar"), false},             // the sibling a bare HasPrefix would wrongly match
		{abs("foo"), abs("foobar", "x.go"), false},     //   ... and files inside it
		{abs("foo"), abs("bar", "x.go"), false},        // unrelated
		{abs("foo", "sub"), abs("foo", "x.go"), false}, // not under a deeper target
	}
	for _, c := range cases {
		if got := pathUnder(c.target, c.p); got != c.want {
			t.Errorf("pathUnder(%q, %q) = %v, want %v", c.target, c.p, got, c.want)
		}
	}
}

func TestMovedPath(t *testing.T) {
	sep := string(filepath.Separator)
	old := sep + filepath.Join("proj", "dir")
	new := sep + filepath.Join("proj", "dir2")

	if got, ok := movedPath(old, new, old); !ok || got != new {
		t.Errorf("the moved path itself: (%q, %v), want (%q, true)", got, ok, new)
	}

	p := filepath.Join(old, "sub", "x.go")
	want := filepath.Join(new, "sub", "x.go")
	if got, ok := movedPath(old, new, p); !ok || got != want {
		t.Errorf("a file under a moved directory: (%q, %v), want (%q, true)", got, ok, want)
	}

	for _, unaffected := range []string{
		sep + filepath.Join("proj", "dirx", "x.go"), // the HasPrefix trap
		sep + filepath.Join("proj", "other.go"),
	} {
		if got, ok := movedPath(old, new, unaffected); ok {
			t.Errorf("movedPath(%q) = (%q, true), want unaffected", unaffected, got)
		}
	}
}
