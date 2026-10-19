package lsp

import (
	"encoding/json"
	"regexp"
	"testing"
)

func TestRegisterRecordsWatchedFileGlobs(t *testing.T) {
	c := &Client{root: "/project", watchers: map[string][]*regexp.Regexp{}}
	opts, _ := json.Marshal(map[string]any{
		"watchers": []map[string]any{
			{"globPattern": "**/*.{go,mod}"},
			{"globPattern": map[string]any{"baseUri": "file:///x", "pattern": "*"}}, // RelativePattern: ignored
		},
	})
	c.register(RegistrationParams{Registrations: []Registration{
		{ID: "w1", Method: methodDidChangeWatchedFiles, RegisterOptions: opts},
		{ID: "other", Method: "textDocument/formatting"},
	}})

	c.mu.Lock()
	defer c.mu.Unlock()
	if len(c.watchers) != 1 || len(c.watchers["w1"]) != 1 {
		t.Fatalf("watchers = %v, want one glob under w1", c.watchers)
	}
	for path, want := range map[string]bool{
		"/project/a/b.go":  true,
		"/project/go.mod":  true,
		"/project/README":  false,
		"/elsewhere/c.txt": false,
	} {
		if got := c.watching(path); got != want {
			t.Errorf("watching(%q) = %v, want %v", path, got, want)
		}
	}
}

func TestDidChangeWatchedFilesSendsNothingBeforeRegistration(t *testing.T) {
	// conn is nil: sending anything would panic.
	c := &Client{root: "/project", watchers: map[string][]*regexp.Regexp{}}
	if err := c.didChangeWatchedFiles([]FileChange{{Path: "/project/a.go", Type: FileChanged}}); err != nil {
		t.Fatalf("didChangeWatchedFiles: %v", err)
	}
}
