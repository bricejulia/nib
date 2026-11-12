package sidepanel

import (
	"os"
	"path/filepath"
	"testing"

	"github.com/bricejulia/nib/internal/layout"
	"github.com/bricejulia/nib/internal/ui/filetree"
	"github.com/bricejulia/nib/internal/ui/finder"
)

func newTestPanel(t *testing.T) (*View, *finder.View) {
	t.Helper()
	dir := t.TempDir()
	if err := os.WriteFile(filepath.Join(dir, "main.go"), []byte("package main\n"), 0o644); err != nil {
		t.Fatal(err)
	}
	f := finder.New(dir)
	f.SetDocked(true)
	return New(filetree.New(dir), f), f
}

func press(v *View, keys ...layout.Key) {
	for _, k := range keys {
		v.HandleKey(k)
	}
}

var (
	next = layout.Key{Text: "]"}
	prev = layout.Key{Text: "["}
	esc  = layout.Key{Named: layout.KeyEsc}
)

func TestNextCyclesThroughAllFiveViewsAndWraps(t *testing.T) {
	v, _ := newTestPanel(t)
	want := []string{
		"Files [Changes] Find Content Replace",
		"Files Changes [Find] Content Replace",
		"Files Changes Find [Content] Replace",
		"Files Changes Find Content [Replace]",
		"[Files] Changes Find Content Replace",
	}
	if got := v.Title(); got != "[Files] Changes Find Content Replace" {
		t.Fatalf("initial title = %q", got)
	}
	for i, w := range want {
		press(v, next)
		if got := v.Title(); got != w {
			t.Fatalf("after %d × ]: title = %q, want %q", i+1, got, w)
		}
	}
}

func TestPrevCyclesBackwardsAndWraps(t *testing.T) {
	v, _ := newTestPanel(t)
	want := []string{
		"Files Changes Find Content [Replace]",
		"Files Changes Find [Content] Replace",
		"Files Changes [Find] Content Replace",
		"Files [Changes] Find Content Replace",
		"[Files] Changes Find Content Replace",
	}
	for i, w := range want {
		press(v, prev)
		if got := v.Title(); got != w {
			t.Fatalf("after %d × [: title = %q, want %q", i+1, got, w)
		}
	}
}

func TestEnteringSearchByCyclingLandsInBrowse(t *testing.T) {
	v, f := newTestPanel(t)
	press(v, next, next) // Find
	if f.Typing() {
		t.Fatal("cycling into a search view should land in browse, so ] keeps cycling")
	}
	if _, _, show := v.CursorPosition(); show {
		t.Error("no caret while browsing")
	}
}

func TestEscFromBrowseReturnsToFiles(t *testing.T) {
	v, _ := newTestPanel(t)
	press(v, next, next, next) // Content
	press(v, esc)
	if !v.TreeActive() || v.Title() != "[Files] Changes Find Content Replace" {
		t.Errorf("Esc in browse should return to Files, title = %q", v.Title())
	}
}

func TestOpenedFinderTypesBracketsAndKeepsStateAcrossCycling(t *testing.T) {
	v, f := newTestPanel(t)
	f.Open()
	v.ShowFinder()
	press(v, layout.Key{Text: "["}, layout.Key{Text: "m"})
	if !f.Typing() || v.Title() != "Files Changes [Find] Content Replace" {
		t.Fatalf("typing [ should not switch views, title = %q", v.Title())
	}

	press(v, esc, prev) // Changes
	if !v.TreeActive() {
		t.Fatal("[ from Find should go to the tree's Changes view")
	}
	press(v, next) // back to Find
	press(v, layout.Key{Text: "/"})
	press(v, layout.Key{Named: layout.KeyEnd})
	press(v, layout.Key{Text: "x"})
	if col, _, _ := v.CursorPosition(); col != len("> [mx") {
		t.Errorf("query should be kept across cycling: caret col = %d", col)
	}
}

func TestShowTreeAndFinder(t *testing.T) {
	v, _ := newTestPanel(t)
	v.ShowFinder()
	if !v.FinderActive() {
		t.Fatal("ShowFinder should show the finder")
	}
	v.ShowTree(filetree.ModeChanges)
	if !v.TreeActive() || v.Title() != "Files [Changes] Find Content Replace" {
		t.Errorf("ShowTree(Changes): title = %q", v.Title())
	}
}
