package main

import (
	"bytes"
	"errors"
	"os"
	"path/filepath"
	"slices"
	"strings"
	"testing"

	"github.com/bricejulia/nib/internal/version"
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
		{name: "a single missing path", args: []string{missing}, wantErr: true},
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

func TestParseArgs(t *testing.T) {
	tests := []struct {
		name       string
		args       []string
		wantPaths  []string
		wantExit   bool
		wantUsage  bool // errUsage
		wantStdout string
	}{
		{name: "no args", args: nil},
		{name: "paths pass through", args: []string{"a", "b"}, wantPaths: []string{"a", "b"}},
		{name: "-v", args: []string{"-v"}, wantExit: true, wantStdout: "nib " + version.Version},
		{name: "--version", args: []string{"--version"}, wantExit: true, wantStdout: "nib " + version.Version},
		{name: "-h", args: []string{"-h"}, wantExit: true, wantStdout: "Usage:"},
		{name: "--help", args: []string{"--help"}, wantExit: true, wantStdout: "Usage:"},
		{name: "unknown flag", args: []string{"-x"}, wantExit: true, wantUsage: true},
		{name: "-- ends flags", args: []string{"--", "-v"}, wantPaths: []string{"-v"}},
	}
	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			var stdout, stderr bytes.Buffer
			paths, exit, err := parseArgs(tt.args, &stdout, &stderr)
			if tt.wantUsage {
				if !errors.Is(err, errUsage) || !strings.Contains(stderr.String(), "Usage:") {
					t.Fatalf("parseArgs(%v) err = %v, stderr %q; want errUsage with usage", tt.args, err, stderr.String())
				}
				return
			}
			if err != nil {
				t.Fatalf("parseArgs(%v): %v", tt.args, err)
			}
			if exit != tt.wantExit || !slices.Equal(paths, tt.wantPaths) {
				t.Errorf("parseArgs(%v) = %v, %v; want %v, %v", tt.args, paths, exit, tt.wantPaths, tt.wantExit)
			}
			if !strings.Contains(stdout.String(), tt.wantStdout) {
				t.Errorf("parseArgs(%v) stdout = %q; want it to contain %q", tt.args, stdout.String(), tt.wantStdout)
			}
		})
	}
}
