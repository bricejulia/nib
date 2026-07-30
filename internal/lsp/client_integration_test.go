package lsp

import (
	"os"
	"os/exec"
	"path/filepath"
	"testing"
	"time"
)

// These tests spawn a REAL language server, so they're skipped wherever
// gopls isn't installed rather than failing — the same opportunistic
// approach internal/vcs/watch's tests take with real `git`. They're what
// actually proves the wire format, handshake, and subprocess lifecycle
// work; everything else in this package is tested against a fake.

// goplsProject writes a tiny two-file Go module and returns its directory.
// Two files specifically so go-to-definition has somewhere cross-file to
// land — the thing the tree-sitter fallback can never do.
func goplsProject(t *testing.T) string {
	t.Helper()
	dir := t.TempDir()

	write := func(name, content string) {
		if err := os.WriteFile(filepath.Join(dir, name), []byte(content), 0o644); err != nil {
			t.Fatal(err)
		}
	}
	write("go.mod", "module testproj\n\ngo 1.21\n")
	write("helper.go", "package main\n\nfunc Helper() int {\n\treturn 42\n}\n")
	write("main.go", "package main\n\nfunc main() {\n\t_ = Helper()\n}\n")
	return dir
}

func requireGopls(t *testing.T) {
	t.Helper()
	if _, err := exec.LookPath("gopls"); err != nil {
		t.Skip("gopls not installed; skipping real language-server integration test")
	}
}

func TestRealGoplsInitializeHandshake(t *testing.T) {
	requireGopls(t)
	dir := goplsProject(t)

	c, err := newClient(dir, []string{"gopls"}, nil)
	if err != nil {
		t.Fatalf("newClient (initialize handshake): %v", err)
	}
	if err := c.Close(); err != nil {
		t.Errorf("Close: %v", err)
	}
}

func TestRealGoplsPublishesDiagnostics(t *testing.T) {
	requireGopls(t)
	dir := t.TempDir()
	if err := os.WriteFile(filepath.Join(dir, "go.mod"), []byte("module testproj\n\ngo 1.21\n"), 0o644); err != nil {
		t.Fatal(err)
	}
	// A genuine compile error, so gopls has something to complain about.
	bad := filepath.Join(dir, "main.go")
	if err := os.WriteFile(bad, []byte("package main\n\nfunc main() {\n\tvar x int = \"not an int\"\n}\n"), 0o644); err != nil {
		t.Fatal(err)
	}

	diags := make(chan PublishDiagnosticsParams, 8)
	c, err := newClient(dir, []string{"gopls"}, func(p PublishDiagnosticsParams) {
		select {
		case diags <- p:
		default: // don't block the read goroutine if the test has moved on
		}
	})
	if err != nil {
		t.Fatalf("newClient: %v", err)
	}
	defer c.Close()

	source, err := os.ReadFile(bad)
	if err != nil {
		t.Fatal(err)
	}
	if err := c.didOpen(bad, "go", string(source), 1); err != nil {
		t.Fatalf("didOpen: %v", err)
	}

	// gopls publishes for several URIs (and may send an empty set first),
	// so wait for a non-empty set for the file we care about.
	deadline := time.After(30 * time.Second)
	for {
		select {
		case p := <-diags:
			if uriToPath(p.URI) == bad && len(p.Diagnostics) > 0 {
				return // got a real diagnostic: the notification path works
			}
		case <-deadline:
			t.Fatal("timed out waiting for diagnostics on the erroneous file")
		}
	}
}

// TestRealGoplsCompletesStructMembers is the test that actually proves
// member completion works — the case buffer-word scanning fundamentally
// cannot answer, because only the server knows what type "obj." is.
func TestRealGoplsCompletesStructMembers(t *testing.T) {
	requireGopls(t)
	dir := t.TempDir()
	write := func(name, content string) {
		t.Helper()
		if err := os.WriteFile(filepath.Join(dir, name), []byte(content), 0o644); err != nil {
			t.Fatal(err)
		}
	}
	write("go.mod", "module testproj\n\ngo 1.21\n")
	// Line 8 (0-based) is "\tobj." — completion is requested right after
	// the dot, with no partial member name typed at all.
	main := "package main\n" +
		"\n" +
		"type Thing struct {\n" +
		"\tAlpha int\n" +
		"\tBeta  string\n" +
		"}\n" +
		"\n" +
		"func main() {\n" +
		"\tobj := Thing{}\n" +
		"\tobj.\n" +
		"}\n"
	mainPath := filepath.Join(dir, "main.go")
	write("main.go", main)

	c, err := newClient(dir, []string{"gopls"}, nil)
	if err != nil {
		t.Fatalf("newClient: %v", err)
	}
	defer c.Close()
	if err := c.didOpen(mainPath, "go", main, 1); err != nil {
		t.Fatalf("didOpen: %v", err)
	}

	// Line 9 is "\tobj.", character 5 is just past the dot.
	var items []CompletionItem
	for attempt := 0; attempt < 12; attempt++ {
		items, err = c.completion(mainPath, 9, 5)
		if err == nil && len(items) > 0 {
			break
		}
		time.Sleep(500 * time.Millisecond)
	}
	if err != nil {
		t.Fatalf("completion: %v", err)
	}
	if len(items) == 0 {
		t.Fatal("expected gopls to return member completions after 'obj.'")
	}

	got := map[string]bool{}
	for _, it := range items {
		got[it.Text()] = true
	}
	for _, want := range []string{"Alpha", "Beta"} {
		if !got[want] {
			t.Errorf("expected %q among the completions, got %v", want, keysOf(got))
		}
	}
}

func keysOf(m map[string]bool) []string {
	out := make([]string, 0, len(m))
	for k := range m {
		out = append(out, k)
	}
	return out
}

func TestRealGoplsDefinitionFindsCrossFileTarget(t *testing.T) {
	requireGopls(t)
	dir := goplsProject(t)
	mainPath := filepath.Join(dir, "main.go")
	helperPath := filepath.Join(dir, "helper.go")

	c, err := newClient(dir, []string{"gopls"}, nil)
	if err != nil {
		t.Fatalf("newClient: %v", err)
	}
	defer c.Close()

	// Open both files so gopls has the package loaded.
	for _, p := range []string{mainPath, helperPath} {
		source, err := os.ReadFile(p)
		if err != nil {
			t.Fatal(err)
		}
		if err := c.didOpen(p, "go", string(source), 1); err != nil {
			t.Fatalf("didOpen %s: %v", p, err)
		}
	}

	// main.go line 3 (0-based) is "\t_ = Helper()"; character 7 lands
	// inside "Helper".
	var loc Location
	var found bool
	// gopls may still be loading the package on the first ask; retry
	// briefly rather than flaking.
	for attempt := 0; attempt < 10; attempt++ {
		loc, found, err = c.definition(mainPath, 3, 7)
		if err == nil && found {
			break
		}
		time.Sleep(500 * time.Millisecond)
	}
	if err != nil {
		t.Fatalf("definition: %v", err)
	}
	if !found {
		t.Fatal("expected gopls to find a definition for Helper()")
	}
	if got := loc.Path(); got != helperPath {
		t.Errorf("definition landed in %q, want %q (the OTHER file)", got, helperPath)
	}
	if loc.Range.Start.Line != 2 { // "func Helper() int {" is line 3, 0-based 2
		t.Errorf("definition line = %d, want 2", loc.Range.Start.Line)
	}
}
