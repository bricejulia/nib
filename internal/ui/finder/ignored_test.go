package finder

import (
	"os"
	"os/exec"
	"path/filepath"
	"strings"
	"testing"

	"github.com/bricejulia/nib/internal/layout"
)

// newIgnoredTestRepo builds a git repo with a tracked file, a git-ignored
// node_modules/pkg/index.js, and nib's own .nib/ state — each containing
// "needle" — for include-ignored tests.
func newIgnoredTestRepo(t *testing.T) string {
	t.Helper()
	dir := t.TempDir()
	run := func(args ...string) {
		t.Helper()
		cmd := exec.Command("git", args...)
		cmd.Dir = dir
		if out, err := cmd.CombinedOutput(); err != nil {
			t.Fatalf("git %v: %v\n%s", args, err, out)
		}
	}
	write := func(rel, content string) {
		t.Helper()
		p := filepath.Join(dir, rel)
		if err := os.MkdirAll(filepath.Dir(p), 0o755); err != nil {
			t.Fatal(err)
		}
		if err := os.WriteFile(p, []byte(content), 0o644); err != nil {
			t.Fatal(err)
		}
	}
	run("init", "-q")
	run("config", "user.email", "test@example.com")
	run("config", "user.name", "test")
	write(".gitignore", "node_modules/\n")
	write("main.go", "package main\n// needle\n")
	run("add", ".gitignore", "main.go")
	run("commit", "-q", "-m", "init")
	write("node_modules/pkg/index.js", "// needle\n")
	write(".nib/.gitignore", "*\n")
	write(".nib/scratch/notes.md", "needle\n")
	return dir
}

func contains(paths []string, want string) bool {
	for _, p := range paths {
		if p == want {
			return true
		}
	}
	return false
}

const ignoredFile = "node_modules/pkg/index.js"

func TestListFilesIncludeIgnored(t *testing.T) {
	dir := newIgnoredTestRepo(t)
	if got := listFiles(dir, "", false); contains(got, ignoredFile) {
		t.Errorf("default listing includes ignored file: %v", got)
	}
	got := listFiles(dir, "", true)
	if !contains(got, ignoredFile) {
		t.Errorf("includeIgnored listing misses %s: %v", ignoredFile, got)
	}
	for _, p := range got {
		if strings.HasPrefix(p, nibStatePrefix) {
			t.Errorf("listing includes nib state %q", p)
		}
	}
}

func TestEffectiveIncludeIgnoredFollowsScope(t *testing.T) {
	dir := newIgnoredTestRepo(t)
	if effectiveIncludeIgnored(dir, "", false) {
		t.Error("unscoped, toggle off: want false")
	}
	if !effectiveIncludeIgnored(dir, "", true) {
		t.Error("toggle on: want true")
	}
	if !effectiveIncludeIgnored(dir, "node_modules/pkg", false) {
		t.Error("scope inside ignored dir: want true")
	}
}

func TestSearchContentIncludeIgnored(t *testing.T) {
	dir := newIgnoredTestRepo(t)
	paths := func(include bool) []string {
		ms, err := searchContent(dir, "", "needle", include)
		if err != nil {
			t.Fatalf("searchContent: %v", err)
		}
		var out []string
		for _, m := range ms {
			out = append(out, m.path)
		}
		return out
	}
	if got := paths(false); contains(got, ignoredFile) {
		t.Errorf("default search hits ignored file: %v", got)
	}
	got := paths(true)
	if !contains(got, ignoredFile) || !contains(got, "main.go") {
		t.Errorf("includeIgnored search = %v, want main.go and %s", got, ignoredFile)
	}
	if contains(got, ".nib/scratch/notes.md") {
		t.Errorf("includeIgnored search hits nib state: %v", got)
	}
}

func TestToggleIgnoredKeyReindexesAndResetsOnOpen(t *testing.T) {
	dir := newIgnoredTestRepo(t)
	v := New(dir)
	v.Open()
	ctrlT := layout.Key{Text: "t", Mods: layout.ModCtrl}

	if contains(v.items, ignoredFile) {
		t.Fatal("ignored file listed before toggling")
	}
	v.HandleKey(ctrlT)
	if !contains(v.items, ignoredFile) {
		t.Fatalf("ignored file missing after Ctrl+T: %v", v.items)
	}
	if want := "Find File (+ignored)"; v.Title() != want {
		t.Errorf("Title() = %q, want %q", v.Title(), want)
	}
	v.HandleKey(ctrlT)
	if contains(v.items, ignoredFile) {
		t.Fatal("ignored file still listed after toggling off")
	}

	v.HandleKey(ctrlT)
	v.Open()
	if v.includeIgnored || contains(v.items, ignoredFile) {
		t.Error("Open should reset the include-ignored toggle")
	}
}

func TestOpenScopedIntoIgnoredDirListsIt(t *testing.T) {
	dir := newIgnoredTestRepo(t)
	v := New(dir)
	v.OpenScoped(filepath.Join(dir, "node_modules", "pkg"))
	if !contains(v.items, ignoredFile) {
		t.Errorf("scoped into ignored dir, items = %v", v.items)
	}
}
