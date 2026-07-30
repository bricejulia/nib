package editor

import (
	"testing"

	"github.com/bricejulia/kiwi/internal/layout"
)

// ctrlSpace builds the Ctrl+Space key the way App.translateKey actually
// produces it — Named: layout.KeySpace, not a raw Text: " " — since
// Ctrl+Space is registered as a Named-key trigger (see layout.KeySpace's
// doc comment); unlike ctrlKey (navigate_test.go), which is right for
// triggers with no Named form ("g", "]", etc.).
func ctrlSpace() layout.Key {
	return layout.Key{Named: layout.KeySpace, Mods: layout.ModCtrl}
}

func TestBufferWordsDedupsAndCollectsIdentifiers(t *testing.T) {
	buf := &Buffer{Lines: []string{"foo := bar", "foo + baz_2"}}
	words := bufferWords(buf)

	want := map[string]bool{"foo": true, "bar": true, "baz_2": true}
	if len(words) != len(want) {
		t.Fatalf("got %v, want exactly %v", words, want)
	}
	for _, w := range words {
		if !want[w] {
			t.Errorf("unexpected word %q", w)
		}
	}
}

func TestCtrlSpaceOpensPopupFilteredByPrefix(t *testing.T) {
	v := NewView()
	v.tabs = []*tab{{buf: &Buffer{Lines: []string{"format", "formula", "other", "fo"}}}}
	v.active = 0
	v.activeTab().cursorLn = 3
	v.activeTab().cursorCol = 2 // end of "fo" on the last line
	v.HandleKey(layout.Key{Text: "i"})

	if !v.HandleKey(ctrlSpace()) {
		t.Fatal("expected Ctrl+Space to be consumed")
	}
	if v.completion == nil {
		t.Fatal("expected the completion popup to open")
	}
	want := []string{"format", "formula"}
	if len(v.completion.candidates) != len(want) {
		t.Fatalf("candidates = %v, want %v", v.completion.candidates, want)
	}
	for i, w := range want {
		if v.completion.candidates[i] != w {
			t.Errorf("candidates[%d] = %q, want %q", i, v.completion.candidates[i], w)
		}
	}
	if v.completion.prefixLen != 2 {
		t.Errorf("prefixLen = %d, want 2", v.completion.prefixLen)
	}
}

// TestCtrlSpaceWithNoPrefixStillOffersBufferWords covers a reported gap:
// autocomplete used to require at least one typed character, so pressing
// Ctrl+Space right after a "." or on a blank line silently did nothing.
// With no prefix, every buffer word is a candidate — the same thing vim's
// own Ctrl+n does.
func TestCtrlSpaceWithNoPrefixStillOffersBufferWords(t *testing.T) {
	v := NewView()
	v.tabs = []*tab{{buf: &Buffer{Lines: []string{"format", ""}}}}
	v.active = 0
	v.activeTab().cursorLn = 1
	v.HandleKey(layout.Key{Text: "i"})

	v.HandleKey(ctrlSpace())

	if v.completion == nil {
		t.Fatal("expected a popup even with nothing typed before the cursor")
	}
	if len(v.completion.candidates) == 0 {
		t.Fatal("expected at least one candidate from the buffer")
	}
	if v.completion.prefixLen != 0 {
		t.Errorf("prefixLen = %d, want 0 so accepting doesn't delete anything", v.completion.prefixLen)
	}
}

func TestCtrlSpaceOnEmptyBufferIsNoop(t *testing.T) {
	v := NewView()
	v.tabs = []*tab{{buf: &Buffer{Lines: []string{""}}}}
	v.active = 0
	v.HandleKey(layout.Key{Text: "i"})

	v.HandleKey(ctrlSpace())

	if v.completion != nil {
		t.Fatal("expected no popup when the buffer has no words to offer")
	}
}

func TestCompletionUpDownMovesSelection(t *testing.T) {
	v := NewView()
	v.tabs = []*tab{{buf: &Buffer{Lines: []string{"alpha", "alps", "al"}}}}
	v.active = 0
	v.activeTab().cursorLn = 2
	v.activeTab().cursorCol = 2
	v.HandleKey(layout.Key{Text: "i"})
	v.HandleKey(ctrlSpace())
	if v.completion == nil || len(v.completion.candidates) != 2 {
		t.Fatalf("setup: expected 2 candidates, got %+v", v.completion)
	}

	v.HandleKey(layout.Key{Named: layout.KeyDown})
	if v.completion.selected != 1 {
		t.Fatalf("selected = %d, want 1 after Down", v.completion.selected)
	}
	v.HandleKey(layout.Key{Named: layout.KeyDown})
	if v.completion.selected != 0 {
		t.Fatalf("selected = %d, want 0 (wrapped around)", v.completion.selected)
	}
	v.HandleKey(layout.Key{Named: layout.KeyUp})
	if v.completion.selected != 1 {
		t.Fatalf("selected = %d, want 1 (wrapped the other way)", v.completion.selected)
	}
}

func TestCompletionEnterAcceptsAndClosesPopup(t *testing.T) {
	v := NewView()
	v.tabs = []*tab{{buf: &Buffer{Lines: []string{"format", "fo"}}}}
	v.active = 0
	v.activeTab().cursorLn = 1
	v.activeTab().cursorCol = 2
	v.HandleKey(layout.Key{Text: "i"})
	v.HandleKey(ctrlSpace())

	if !v.HandleKey(layout.Key{Named: layout.KeyEnter}) {
		t.Fatal("expected Enter to be consumed")
	}
	if v.completion != nil {
		t.Fatal("expected the popup to close after accepting")
	}
	if got := v.activeTab().buf.Lines[1]; got != "format" {
		t.Fatalf("Lines[1] = %q, want %q", got, "format")
	}
	if v.mode != modeInsert {
		t.Fatal("accepting a completion must not leave Insert mode")
	}
}

func TestCompletionAcceptedEditIsUndoableWithTheEnclosingSession(t *testing.T) {
	v := NewView()
	v.tabs = []*tab{{buf: &Buffer{Lines: []string{"format", "fo"}}}}
	v.active = 0
	v.activeTab().cursorLn = 1
	v.activeTab().cursorCol = 2
	v.HandleKey(layout.Key{Text: "i"})
	v.HandleKey(ctrlSpace())
	v.HandleKey(layout.Key{Named: layout.KeyEnter}) // accept "format"
	v.HandleKey(layout.Key{Named: layout.KeyEsc})   // commit the Insert session

	if !v.HandleKey(layout.Key{Text: "u"}) {
		t.Fatal("expected 'u' to be consumed")
	}
	if got := v.activeTab().buf.Lines[1]; got != "fo" {
		t.Fatalf("Lines[1] after undo = %q, want %q", got, "fo")
	}
}

func TestCompletionEscClosesPopupWithoutLeavingInsertMode(t *testing.T) {
	v := NewView()
	v.tabs = []*tab{{buf: &Buffer{Lines: []string{"format", "fo"}}}}
	v.active = 0
	v.activeTab().cursorLn = 1
	v.activeTab().cursorCol = 2
	v.HandleKey(layout.Key{Text: "i"})
	v.HandleKey(ctrlSpace())

	if !v.HandleKey(layout.Key{Named: layout.KeyEsc}) {
		t.Fatal("expected Esc to be consumed")
	}
	if v.completion != nil {
		t.Fatal("expected the first Esc to close the popup")
	}
	if v.mode != modeInsert {
		t.Fatal("the first Esc (with a popup open) must not exit Insert mode")
	}

	if !v.HandleKey(layout.Key{Named: layout.KeyEsc}) {
		t.Fatal("expected the second Esc to be consumed")
	}
	if v.mode != modeNormal {
		t.Fatal("the second Esc (popup already closed) should exit Insert mode")
	}
}

func TestTypingOnWithNoMatchesClosesPopup(t *testing.T) {
	v := NewView()
	v.tabs = []*tab{{buf: &Buffer{Lines: []string{"format", "fo"}}}}
	v.active = 0
	v.activeTab().cursorLn = 1
	v.activeTab().cursorCol = 2
	v.HandleKey(layout.Key{Text: "i"})
	v.HandleKey(ctrlSpace())
	if v.completion == nil {
		t.Fatal("setup: expected the popup to open")
	}

	v.HandleKey(layout.Key{Text: "z"}) // "foz" matches nothing
	if v.completion != nil {
		t.Fatal("expected the popup to close once the prefix matches nothing")
	}
	if got := v.activeTab().buf.Lines[1]; got != "foz" {
		t.Fatalf("Lines[1] = %q, want %q (typing continues normally)", got, "foz")
	}
}

func TestBackspaceRefiltersCompletion(t *testing.T) {
	v := NewView()
	v.tabs = []*tab{{buf: &Buffer{Lines: []string{"format", "flag", "fo"}}}}
	v.active = 0
	v.activeTab().cursorLn = 2
	v.activeTab().cursorCol = 2
	v.HandleKey(layout.Key{Text: "i"})
	v.HandleKey(ctrlSpace())
	if len(v.completion.candidates) != 1 { // only "format" matches "fo"
		t.Fatalf("setup: candidates = %v", v.completion.candidates)
	}

	v.HandleKey(layout.Key{Named: layout.KeyBackspace}) // buffer now "f"
	if v.completion == nil {
		t.Fatal("expected the popup to stay open with a shorter prefix")
	}
	if len(v.completion.candidates) != 2 { // "format" and "flag" both match "f"
		t.Fatalf("candidates after backspace = %v, want 2 matches", v.completion.candidates)
	}
}
