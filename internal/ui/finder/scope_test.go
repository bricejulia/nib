package finder

import (
	"os"
	"os/exec"
	"path/filepath"
	"testing"
	"time"

	"github.com/bricejulia/nib/internal/layout"
)

// newScopedTestRepo builds a small git repo with a "needle" match both at
// the top level and inside a "sub" subdirectory, for folder-scoped
// file-listing and content-search tests.
func newScopedTestRepo(t *testing.T) string {
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
	run("init", "-q")
	run("config", "user.email", "test@example.com")
	run("config", "user.name", "test")

	if err := os.MkdirAll(filepath.Join(dir, "sub"), 0o755); err != nil {
		t.Fatal(err)
	}
	if err := os.WriteFile(filepath.Join(dir, "top.go"), []byte("package top\n// needle\n"), 0o644); err != nil {
		t.Fatal(err)
	}
	if err := os.WriteFile(filepath.Join(dir, "sub", "inner.go"), []byte("package sub\n// needle\n"), 0o644); err != nil {
		t.Fatal(err)
	}
	run("add", "top.go", "sub/inner.go")
	run("commit", "-q", "-m", "init")
	return dir
}

func TestNormalizeScopeTrimsSlashesAndDot(t *testing.T) {
	cases := map[string]string{
		"":        "",
		".":       "",
		"/":       "",
		"src":     "src",
		"/src/":   "src",
		"src/sub": "src/sub",
	}
	for in, want := range cases {
		if got := normalizeScope(in); got != want {
			t.Errorf("normalizeScope(%q) = %q, want %q", in, got, want)
		}
	}
}

func TestRelScopeOutsideRootReturnsEmpty(t *testing.T) {
	if got := relScope("/project", "/project/sub"); got != "sub" {
		t.Errorf("relScope = %q, want %q", got, "sub")
	}
	if got := relScope("/project", "/project"); got != "" {
		t.Errorf("relScope(root, root) = %q, want empty", got)
	}
	if got := relScope("/project", "/elsewhere"); got != "" {
		t.Errorf("relScope outside root = %q, want empty", got)
	}
}

func TestOpenScopedPreFillsScopeFieldAndNarrowsFileResults(t *testing.T) {
	dir := newScopedTestRepo(t)
	v := New(dir)
	v.OpenScoped(filepath.Join(dir, "sub"))

	if got := v.scopeField.String(); got != "sub" {
		t.Fatalf("scopeField = %q, want %q", got, "sub")
	}
	if len(v.items) != 1 || v.items[0] != "sub/inner.go" {
		t.Fatalf("items = %v, want only sub/inner.go", v.items)
	}
}

func TestOpenScopedOutsideRootLeavesScopeEmpty(t *testing.T) {
	dir := newScopedTestRepo(t)
	v := New(dir)
	v.OpenScoped(t.TempDir())
	if got := v.scopeField.String(); got != "" {
		t.Errorf("scopeField = %q, want empty for a directory outside the project", got)
	}
}

func TestFocusScopeTogglesTypingTargetAndEnterReturnsToQuery(t *testing.T) {
	v := newTestView("main.go")

	v.HandleKey(layout.Key{Text: "k", Mods: layout.ModCtrl})
	if v.focus != focusScope {
		t.Fatalf("expected Ctrl+K to switch focus to focusScope, got %v", v.focus)
	}

	for _, r := range "sub" {
		v.HandleKey(layout.Key{Text: string(r)})
	}
	if got := v.scopeField.String(); got != "sub" {
		t.Errorf("scopeField = %q, want %q (typed while focused on scope)", got, "sub")
	}
	if v.query.Len() != 0 {
		t.Errorf("query should stay empty while typing into the scope field, got %q", v.query.String())
	}

	v.HandleKey(layout.Key{Named: layout.KeyEnter})
	if v.focus != focusQuery {
		t.Fatalf("expected Enter to return focus to the query field, got %v", v.focus)
	}
	v.HandleKey(layout.Key{Text: "m"})
	if v.query.String() != "m" {
		t.Errorf("expected typing to reach the query field again after Enter, got query=%q", v.query.String())
	}
}

func TestScopeFieldNarrowsContentSearchViaGitGrep(t *testing.T) {
	dir := newScopedTestRepo(t)
	v := New(dir)
	v.HandleKey(layout.Key{Named: layout.KeyTab}) // content mode

	v.HandleKey(layout.Key{Text: "k", Mods: layout.ModCtrl})
	for _, r := range "sub" {
		v.HandleKey(layout.Key{Text: string(r)})
	}
	v.HandleKey(layout.Key{Named: layout.KeyEnter})

	for _, r := range "needle" {
		v.HandleKey(layout.Key{Text: string(r)})
	}

	if len(v.contentMatches) != 1 || v.contentMatches[0].path != "sub/inner.go" {
		t.Fatalf("contentMatches = %+v, want only sub/inner.go's match", v.contentMatches)
	}
}

func TestScopeCarriesForwardWhenTabbingIntoReplaceMode(t *testing.T) {
	v := New("/project")
	v.OpenScoped("/project/sub")

	v.HandleKey(layout.Key{Named: layout.KeyTab}) // content mode
	v.HandleKey(layout.Key{Named: layout.KeyTab}) // replace mode

	if got := v.replace.scopeField.String(); got != "sub" {
		t.Errorf("replace.scopeField = %q, want %q", got, "sub")
	}
}

func TestTitleShowsScopeWhenSet(t *testing.T) {
	v := New("/project")
	v.OpenScoped("/project/sub")
	if want := "Find File — sub"; v.Title() != want {
		t.Errorf("Title() = %q, want %q", v.Title(), want)
	}
}

func TestReplaceViewFocusScopeTogglesTypingTarget(t *testing.T) {
	v := NewReplaceView(t.TempDir())
	v.Open()

	v.HandleKey(layout.Key{Text: "k", Mods: layout.ModCtrl})
	if v.focus != focusReplaceScope {
		t.Fatalf("expected Ctrl+K to switch to focusReplaceScope, got %v", v.focus)
	}

	typeText(v, "sub")
	if got := v.scopeField.String(); got != "sub" {
		t.Errorf("scopeField = %q, want %q", got, "sub")
	}
	if v.find.String() != "" {
		t.Errorf("find should stay empty while typing into the scope field, got %q", v.find.String())
	}

	v.HandleKey(layout.Key{Named: layout.KeyEnter})
	if v.focus != focusFind {
		t.Errorf("expected Enter to return focus to Find, got %v", v.focus)
	}
}

// TestScopeEditInFileModeReindexesAsynchronously checks that, with Post
// wired up, editing the scope field in file mode doesn't run `git
// ls-files` inline (see applyScopeChange/FileListResult) — the same
// non-blocking contract refilterContent already has for content search.
func TestScopeEditInFileModeReindexesAsynchronously(t *testing.T) {
	withFastDebounce(t)
	dir := newScopedTestRepo(t)
	v := New(dir)
	c := &postCollector{}
	v.Post = c.post

	v.HandleKey(layout.Key{Text: "k", Mods: layout.ModCtrl})
	for _, r := range "sub" {
		v.HandleKey(layout.Key{Text: string(r)})
	}
	if !v.searching {
		t.Fatal("expected searching=true immediately after a scope edit")
	}

	waitUntil(t, time.Second, func() bool { return c.count() > 0 })
	result, ok := c.events[len(c.events)-1].(FileListResult)
	if !ok {
		t.Fatalf("expected a FileListResult to be posted, got %+v", c.events)
	}
	v.ApplyFileListResult(result)

	if v.searching {
		t.Error("expected searching=false after the result is applied")
	}
	if len(v.items) != 1 || v.items[0] != "sub/inner.go" {
		t.Errorf("items = %v, want only sub/inner.go", v.items)
	}
}

func TestReplaceViewScopeFieldNarrowsResultsToSubtree(t *testing.T) {
	dir := newScopedTestRepo(t) // "needle", not "todo" — reuse with a matching query below.
	v := NewReplaceView(dir)
	v.SetScope("sub")
	v.Open()
	typeText(v, "needle")

	files, _ := v.counts()
	if files != 1 {
		t.Fatalf("files = %d, want 1 (only sub/inner.go within scope): rows=%+v", files, v.rows)
	}
	for _, r := range v.rows {
		if r.isFile && r.path != "sub/inner.go" {
			t.Errorf("unexpected file header outside scope: %q", r.path)
		}
	}
}
