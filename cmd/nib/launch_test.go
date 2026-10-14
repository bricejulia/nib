package main

import (
	"os"
	"path/filepath"
	"slices"
	"testing"
)

func TestLaunchTarget(t *testing.T) {
	dir := t.TempDir()
	sub := filepath.Join(dir, "sub")
	a := filepath.Join(dir, "a.json")
	b := filepath.Join(sub, "b.csv")
	if err := os.Mkdir(sub, 0o755); err != nil {
		t.Fatal(err)
	}
	for _, f := range []string{a, b} {
		if err := os.WriteFile(f, []byte("x"), 0o644); err != nil {
			t.Fatal(err)
		}
	}
	missing := filepath.Join(dir, "missing.txt")
	cwd, err := os.Getwd()
	if err != nil {
		t.Fatal(err)
	}

	tests := []struct {
		name      string
		args      []string
		wantRoot  string
		wantFiles []string
		wantErr   bool
	}{
		{name: "no args is the current folder", args: nil, wantRoot: cwd},
		{name: "a folder", args: []string{sub}, wantRoot: sub},
		{name: "a single missing path stays folder mode", args: []string{missing}, wantRoot: missing},
		{name: "one file", args: []string{a}, wantRoot: dir, wantFiles: []string{a}},
		{name: "several files root at the first", args: []string{b, a}, wantRoot: sub, wantFiles: []string{b, a}},
		{name: "a folder among files", args: []string{a, sub}, wantErr: true},
		{name: "a missing file among files", args: []string{a, missing}, wantErr: true},
	}
	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			root, files, err := launchTarget(tt.args)
			if tt.wantErr {
				if err == nil {
					t.Fatalf("launchTarget(%v) = %q, %v; want an error", tt.args, root, files)
				}
				return
			}
			if err != nil {
				t.Fatalf("launchTarget(%v): %v", tt.args, err)
			}
			if root != tt.wantRoot || !slices.Equal(files, tt.wantFiles) {
				t.Errorf("launchTarget(%v) = %q, %v; want %q, %v", tt.args, root, files, tt.wantRoot, tt.wantFiles)
			}
		})
	}
}
