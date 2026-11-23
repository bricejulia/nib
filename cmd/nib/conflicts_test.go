package main

import "testing"

func TestPickNext(t *testing.T) {
	paths := []string{"/r/a", "/r/b", "/r/c"}
	cases := []struct {
		current string
		forward bool
		want    string
		ok      bool
	}{
		{"/r/a", true, "/r/b", true},
		{"/r/c", true, "/r/a", true},
		{"/r/a", false, "/r/c", true},
		{"/r/x", true, "/r/a", true},
		{"/r/x", false, "/r/c", true},
	}
	for _, c := range cases {
		got, ok := pickNext(paths, c.current, c.forward)
		if got != c.want || ok != c.ok {
			t.Errorf("pickNext(%q, %v) = %q, %v; want %q, %v", c.current, c.forward, got, ok, c.want, c.ok)
		}
	}
	if _, ok := pickNext([]string{"/r/a"}, "/r/a", true); ok {
		t.Error("only the current file: want ok false")
	}
	if _, ok := pickNext(nil, "/r/a", true); ok {
		t.Error("no files: want ok false")
	}
}
