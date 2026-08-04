package editor

import (
	"testing"

	"github.com/bricejulia/nib/internal/config"
	"github.com/bricejulia/nib/internal/layout"
)

func TestTabModeForPrefersLanguageOverDefault(t *testing.T) {
	v := NewView()
	v.SetTabModeDefaults(map[string]config.TabMode{
		"default": {UseSpaces: false, Width: 4},
		"yaml":    {UseSpaces: true, Width: 2},
	})
	if got := v.tabModeFor("yaml"); got != (config.TabMode{UseSpaces: true, Width: 2}) {
		t.Errorf("tabModeFor(yaml) = %+v, want spaces:2", got)
	}
	if got := v.tabModeFor("go"); got != (config.TabMode{UseSpaces: false, Width: 4}) {
		t.Errorf("tabModeFor(go) = %+v, want the default (tabs:4)", got)
	}
}

func TestTabModeForInheritsDefaultWidthWhenLanguageOmitsIt(t *testing.T) {
	v := NewView()
	v.SetTabModeDefaults(map[string]config.TabMode{
		"default": {UseSpaces: false, Width: 8},
		"python":  {UseSpaces: true}, // no ":<width>" in the config line
	})
	if got := v.tabModeFor("python"); got != (config.TabMode{UseSpaces: true, Width: 8}) {
		t.Errorf("tabModeFor(python) = %+v, want spaces, width inherited from default (8)", got)
	}
}

func TestTabModeForFallsBackToHardcodedTabsWhenUnconfigured(t *testing.T) {
	v := NewView() // never calls SetTabModeDefaults
	if got := v.tabModeFor("go"); got != (config.TabMode{UseSpaces: false, Width: 4}) {
		t.Errorf("tabModeFor(go) = %+v, want tabs:4 (nib's unconfigured default)", got)
	}
}

func TestOpenDerivesBufferIndentSettingsFromTabModeDefaults(t *testing.T) {
	v := NewView()
	v.SetTabModeDefaults(map[string]config.TabMode{"default": {UseSpaces: true, Width: 2}})
	v.Open(fixturePath(t, "editor_sample.txt")) // unrecognized extension -> "" language -> "default" entry

	buf := v.activeTab().buf
	if !buf.IndentUseSpaces || buf.IndentWidth != 2 {
		t.Errorf("buf indent = (spaces=%v, width=%d), want (true, 2)", buf.IndentUseSpaces, buf.IndentWidth)
	}
}

func TestOpenDoesNotRederiveIndentSettingsOnAnAlreadyLoadedBuffer(t *testing.T) {
	store := NewBufferStore()
	a := NewView()
	a.SetBufferStore(store)
	a.SetTabModeDefaults(map[string]config.TabMode{"default": {UseSpaces: true, Width: 2}})
	a.Open(fixturePath(t, "editor_sample.txt"))

	b := NewView()
	b.SetBufferStore(store) // shares the same cached Buffer as a
	b.SetTabModeDefaults(map[string]config.TabMode{"default": {UseSpaces: false, Width: 8}})
	b.Open(fixturePath(t, "editor_sample.txt"))

	buf := b.activeTab().buf
	if !buf.IndentUseSpaces || buf.IndentWidth != 2 {
		t.Errorf("buf indent = (spaces=%v, width=%d), want the FIRST pane's settings (true, 2) unchanged", buf.IndentUseSpaces, buf.IndentWidth)
	}
}

func TestInsertTabInsertsLiteralTabByDefault(t *testing.T) {
	v := NewView()
	v.tabs = []*tab{{buf: &Buffer{Lines: []string{""}}}}
	v.active = 0
	v.HandleKey(layout.Key{Text: "i"})
	v.HandleKey(layout.Key{Named: layout.KeyTab})

	if got := v.activeTab().buf.Lines[0]; got != "\t" {
		t.Errorf("line = %q, want a literal tab", got)
	}
}

func TestInsertTabInsertsSpacesWhenBufferUsesSpaces(t *testing.T) {
	v := NewView()
	v.tabs = []*tab{{buf: &Buffer{Lines: []string{""}, IndentUseSpaces: true, IndentWidth: 2}}}
	v.active = 0
	v.HandleKey(layout.Key{Text: "i"})
	v.HandleKey(layout.Key{Named: layout.KeyTab})

	if got := v.activeTab().buf.Lines[0]; got != "  " {
		t.Errorf("line = %q, want two spaces", got)
	}
}

func TestToggleTabModeFlipsIndentUseSpacesButNotWidth(t *testing.T) {
	v := NewView()
	v.tabs = []*tab{{buf: &Buffer{Lines: []string{""}, IndentUseSpaces: false, IndentWidth: 4}}}
	v.active = 0

	v.HandleKey(layout.Key{Mods: layout.ModCtrl | layout.ModAlt, Text: "t"})

	buf := v.activeTab().buf
	if !buf.IndentUseSpaces {
		t.Error("expected IndentUseSpaces to flip to true")
	}
	if buf.IndentWidth != 4 {
		t.Errorf("IndentWidth = %d, want unchanged (4)", buf.IndentWidth)
	}

	v.HandleKey(layout.Key{Mods: layout.ModCtrl | layout.ModAlt, Text: "t"})
	if v.activeTab().buf.IndentUseSpaces {
		t.Error("expected a second toggle to flip IndentUseSpaces back to false")
	}
}

func TestTabModeStatusFormatsSpacesAndTabs(t *testing.T) {
	v := NewView()
	v.tabs = []*tab{{buf: &Buffer{Lines: []string{""}, IndentUseSpaces: true, IndentWidth: 2}}}
	v.active = 0
	if got := v.TabModeStatus(); got != "spaces:2" {
		t.Errorf("TabModeStatus() = %q, want %q", got, "spaces:2")
	}

	v.activeTab().buf.IndentUseSpaces = false
	v.activeTab().buf.IndentWidth = 4
	if got := v.TabModeStatus(); got != "tabs:4" {
		t.Errorf("TabModeStatus() = %q, want %q", got, "tabs:4")
	}
}

func TestTabModeStatusEmptyWithNoFileOpen(t *testing.T) {
	v := NewView()
	if got := v.TabModeStatus(); got != "" {
		t.Errorf("TabModeStatus() = %q, want empty with no tabs open", got)
	}
}
