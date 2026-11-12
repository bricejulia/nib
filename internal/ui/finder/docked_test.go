package finder

import (
	"testing"

	"github.com/bricejulia/nib/internal/layout"
)

func newDockedTestView(items ...string) *View {
	v := newTestView(items...)
	v.SetDocked(true)
	return v
}

var (
	keyEsc   = layout.Key{Named: layout.KeyEsc}
	keyNext  = layout.Key{Text: "]"}
	keyPrev  = layout.Key{Text: "["}
	keyEnter = layout.Key{Named: layout.KeyEnter}
)

func TestDockedEnterSelectsWithoutClosingAndLandsInBrowse(t *testing.T) {
	v := newDockedTestView("a.go", "b.go")
	var selected string
	closed := false
	v.OnSelect = func(absPath string, _ int) { selected = absPath }
	v.OnClose = func() { closed = true }

	v.HandleKey(keyEnter)

	if selected != "/project/a.go" {
		t.Errorf("got selected=%q, want /project/a.go", selected)
	}
	if closed {
		t.Error("docked finder should stay open after a selection")
	}
	if v.Typing() {
		t.Error("after opening a result the finder should be in browse")
	}
}

func TestTypingTypesViewAndMotionKeys(t *testing.T) {
	v := newDockedTestView("a.go")
	for _, r := range "[j]/ik" {
		if !v.HandleKey(layout.Key{Text: string(r)}) {
			t.Fatalf("typing %q should be consumed", r)
		}
	}
	if got := v.query.String(); got != "[j]/ik" {
		t.Errorf("query = %q, want every key typed", got)
	}
	if v.mode != ModeFiles {
		t.Error("typing ] must not switch modes")
	}
}

func TestEscGoesToBrowseThenCloses(t *testing.T) {
	v := newDockedTestView("a.go")
	closed := false
	v.OnClose = func() { closed = true }

	v.HandleKey(keyEsc)
	if closed || v.Typing() {
		t.Fatalf("first Esc should enter browse: closed=%v typing=%v", closed, v.Typing())
	}
	if _, _, show := v.CursorPosition(); show {
		t.Error("no caret while browsing")
	}
	v.HandleKey(keyEsc)
	if !closed {
		t.Error("Esc in browse should close")
	}
}

func TestBrowseKeys(t *testing.T) {
	v := newDockedTestView("a.go", "b.go", "c.go")
	v.HandleKey(keyEsc)

	v.HandleKey(layout.Key{Text: "j"})
	if v.cursor != 1 {
		t.Errorf("j in browse: cursor = %d, want 1", v.cursor)
	}
	v.HandleKey(layout.Key{Text: "k"})
	if v.cursor != 0 {
		t.Errorf("k in browse: cursor = %d, want 0", v.cursor)
	}
	for _, key := range []string{"/", "i"} {
		v.HandleKey(keyEsc)
		v.HandleKey(layout.Key{Text: key})
		if !v.Typing() {
			t.Errorf("%s in browse should return to typing", key)
		}
	}
}

func TestBrowseCyclesModesKeepingTheQuery(t *testing.T) {
	v := newDockedTestView("main.go")
	v.HandleKey(layout.Key{Text: "ma"})
	v.HandleKey(keyEsc)

	v.HandleKey(keyNext)
	if v.mode != ModeContent || v.Typing() {
		t.Fatalf("] should go to content mode in browse, got mode=%v typing=%v", v.mode, v.Typing())
	}
	if v.query.String() != "ma" {
		t.Errorf("query = %q, want it kept across modes", v.query.String())
	}
	v.HandleKey(keyNext)
	if v.mode != ModeReplace || v.Typing() {
		t.Fatalf("] should go to replace mode in browse, got mode=%v typing=%v", v.mode, v.Typing())
	}
	v.HandleKey(keyPrev)
	if v.mode != ModeContent {
		t.Errorf("[ from replace should go back to content mode, got %v", v.mode)
	}
}

func TestViewOverflowHandsOverPastBothEnds(t *testing.T) {
	v := newDockedTestView("a.go")
	var dirs []int
	v.OnViewOverflow = func(dir int) { dirs = append(dirs, dir) }

	v.HandleKey(keyEsc)
	v.HandleKey(keyPrev) // before files
	v.HandleKey(keyNext)
	v.HandleKey(keyNext)
	v.HandleKey(keyNext) // past replace

	if len(dirs) != 2 || dirs[0] != -1 || dirs[1] != 1 {
		t.Errorf("overflows = %v, want [-1 1]", dirs)
	}
	if v.mode != ModeReplace {
		t.Errorf("an overflow should leave the mode alone, got %v", v.mode)
	}
}

func TestViewKeysWrapWithoutOverflowHook(t *testing.T) {
	v := newTestView("a.go")
	v.HandleKey(keyEsc)
	v.HandleKey(keyPrev)
	if v.mode != ModeReplace {
		t.Errorf("[ from files should wrap to replace, got %v", v.mode)
	}
}

func TestDockedUnusedKeysBubble(t *testing.T) {
	v := newDockedTestView("a.go")
	for _, k := range []layout.Key{
		{Named: "F5"},
		{Named: layout.KeyTab},
		{Named: layout.KeyTab, Mods: layout.ModShift},
		{Text: "c", Mods: layout.ModCtrl},
	} {
		if v.HandleKey(k) {
			t.Errorf("docked HandleKey(%s) while typing = true, want false", k.String())
		}
	}
	v.HandleKey(keyEsc)
	if v.HandleKey(layout.Key{Named: layout.KeyTab}) {
		t.Error("docked Tab while browsing should bubble (next pane)")
	}
}

func TestDockedReplaceModeKeys(t *testing.T) {
	v := newDockedTestView("a.go")
	v.HandleKey(keyEsc)
	v.HandleKey(keyNext)
	v.HandleKey(keyNext) // replace, browsing its results

	if v.HandleKey(layout.Key{Named: layout.KeyTab}) {
		t.Error("docked replace mode should let Tab bubble (next pane)")
	}
	v.HandleKey(layout.Key{Text: "/"})
	if !v.Typing() {
		t.Fatal("/ should return to the Find field")
	}
	v.HandleKey(layout.Key{Text: "]"})
	if v.replace.find.String() != "]" || v.mode != ModeReplace {
		t.Errorf("] in the Find field should be typed, got find=%q mode=%v", v.replace.find.String(), v.mode)
	}
	if v.HandleKey(layout.Key{Text: "d", Mods: layout.ModCtrl}) {
		t.Error("an unbound Ctrl key should bubble")
	}
	v.HandleKey(keyEsc)
	v.HandleKey(keyPrev)
	if v.mode != ModeContent {
		t.Errorf("[ from replace results should go to content mode, got %v", v.mode)
	}
}

func TestOverlayStillSwallowsEverything(t *testing.T) {
	v := newTestView("a.go")
	if !v.HandleKey(layout.Key{Named: layout.KeyTab, Mods: layout.ModShift}) {
		t.Error("as an overlay, Shift+Tab must not leak through")
	}
}
