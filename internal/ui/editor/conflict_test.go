package editor

import (
	"errors"
	"strings"
	"testing"

	"github.com/bricejulia/nib/internal/layout"
)

func conflictView(src string) *View {
	v := NewView()
	v.tabs = []*tab{{path: "/repo/f.go", buf: &Buffer{Lines: strings.Split(src, "\n")}}}
	v.active = 0
	return v
}

func linesOf(v *View) string { return strings.Join(v.activeTab().buf.Lines, "|") }

const twoConflicts = "a\n<<<<<<< HEAD\nmine\n=======\ntheirs\n>>>>>>> topic\nb\n<<<<<<< HEAD\nx\n=======\ny\n>>>>>>> topic\nc"

func TestConflictTakeSides(t *testing.T) {
	for key, want := range map[string]string{
		"O": "a|mine|b",
		"T": "a|theirs|b",
		"Y": "a|mine|theirs|b",
		"Z": "a|b",
	} {
		v := conflictView(twoConflicts)
		v.activeTab().cursorLn = 2
		pressKeys(v, key)
		if got := linesOf(v); !strings.HasPrefix(got, want+"|<<<<<<<") {
			t.Errorf("%s: lines = %q, want prefix %q", key, got, want)
		}
		if v.activeTab().cursorLn != 1 {
			t.Errorf("%s: cursorLn = %d, want 1 (where the region was)", key, v.activeTab().cursorLn)
		}
		if v.notice != "1 conflict left" {
			t.Errorf("%s: notice = %q", key, v.notice)
		}
	}
}

func TestConflictTakeIsOneUndoStep(t *testing.T) {
	v := conflictView(twoConflicts)
	v.activeTab().cursorLn = 4
	pressKeys(v, "Tu")
	if got := linesOf(v); got != strings.ReplaceAll(twoConflicts, "\n", "|") {
		t.Errorf("after undo = %q", got)
	}
}

func TestConflictTakeOutsideRegionIsANoOp(t *testing.T) {
	v := conflictView(twoConflicts)
	pressKeys(v, "O")
	if got := linesOf(v); got != strings.ReplaceAll(twoConflicts, "\n", "|") {
		t.Errorf("lines changed: %q", got)
	}
	if v.notice != "Not in a merge conflict" {
		t.Errorf("notice = %q", v.notice)
	}
}

func TestConflictNextPrevAndFileHandoff(t *testing.T) {
	v := conflictView(twoConflicts)
	alt := func(r string) layout.Key { return layout.Key{Codepoint: []rune(r)[0], Mods: layout.ModAlt} }
	if got := alt("n").String(); got != "Alt+n" {
		t.Fatalf("key string = %q", got)
	}
	v.HandleKey(alt("n"))
	if ln := v.activeTab().cursorLn; ln != 1 {
		t.Fatalf("first Alt+n: cursorLn = %d, want 1", ln)
	}
	v.HandleKey(alt("n"))
	if ln := v.activeTab().cursorLn; ln != 7 {
		t.Fatalf("second Alt+n: cursorLn = %d, want 7", ln)
	}

	var handedOff string
	v.OnConflictFileDone = func(path string, forward bool) bool {
		handedOff = path
		return false // nowhere else to go: wrap
	}
	v.HandleKey(alt("n"))
	if handedOff != "/repo/f.go" {
		t.Errorf("OnConflictFileDone got %q", handedOff)
	}
	if ln := v.activeTab().cursorLn; ln != 1 {
		t.Errorf("wrapped Alt+n: cursorLn = %d, want 1", ln)
	}
	v.HandleKey(alt("p"))
	if ln := v.activeTab().cursorLn; ln != 7 {
		t.Errorf("Alt+p from the first region: cursorLn = %d, want 7 (wrapped)", ln)
	}
}

func TestConflictAutoResolveWithBase(t *testing.T) {
	src := "<<<<<<< HEAD\nfoo(x, b)\n||||||| base\nfoo(a, b)\n=======\nfoo(a, y)\n>>>>>>> topic\n" +
		"<<<<<<< HEAD\np\n||||||| base\nq\n=======\nr\n>>>>>>> topic"
	v := conflictView(src)
	pressKeys(v, "M")
	if got := linesOf(v); got != "foo(x, y)|<<<<<<< HEAD|p|||||||| base|q|=======|r|>>>>>>> topic" {
		t.Errorf("lines = %q", got)
	}
	if v.notice != "Resolved 1 of 2 conflicts: 1 left" {
		t.Errorf("notice = %q", v.notice)
	}
	pressKeys(v, "u")
	if got := linesOf(v); got != strings.ReplaceAll(src, "\n", "|") {
		t.Errorf("undo didn't restore in one step: %q", got)
	}
}

func TestConflictAutoResolveWithoutBaseOnDirtyBufferRefuses(t *testing.T) {
	v := conflictView(twoConflicts)
	v.activeTab().buf.Dirty = true
	called := false
	v.RegenerateConflictsFunc = func(string) error { called = true; return errors.New("no") }
	pressKeys(v, "M")
	if called {
		t.Error("regenerated conflicts over unsaved edits")
	}
	if !strings.Contains(v.notice, "no base version") {
		t.Errorf("notice = %q", v.notice)
	}
}

func TestConflictRenderMarksRegion(t *testing.T) {
	v := conflictView(twoConflicts)
	w := newFakeWindow(80, 10)
	v.Render(w)
	// Row 0 is the tab bar, so buffer line 1 ("<<<<<<< HEAD") is row 2.
	if !strings.Contains(w.lines[2], "O left · T right · Y both · Z neither · M auto") {
		t.Errorf("no key hint on the opening marker: %q", w.lines[2])
	}
	if !strings.Contains(w.lines[3], "▌") {
		t.Errorf("no side bar in the gutter of a conflict line: %q", w.lines[3])
	}
	if strings.Contains(w.lines[1], "▌") {
		t.Errorf("side bar outside the region: %q", w.lines[1])
	}
}

func TestOnSavedGetsPathAndLines(t *testing.T) {
	path := t.TempDir() + "/f.txt"
	v := conflictView("resolved")
	v.activeTab().path = path
	v.activeTab().buf.Path = path
	v.activeTab().buf.mode = 0o644
	var gotPath, gotLines string
	v.OnSaved = func(p string, lines []string) { gotPath, gotLines = p, strings.Join(lines, "|") }
	v.saveActive()
	if gotPath != path || gotLines != "resolved" {
		t.Errorf("OnSaved(%q, %q)", gotPath, gotLines)
	}
}
