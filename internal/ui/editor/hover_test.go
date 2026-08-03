package editor

import (
	"strings"
	"testing"

	"github.com/bricejulia/kiwi/internal/layout"
)

func TestHoverKeyPopulatesHoverText(t *testing.T) {
	fake := &fakeLSP{ready: true, hoverText: "func greet(name string) string", hoverOK: true}
	v := NewView()
	v.lsp = fake
	v.Open(fixturePath(t, "highlight_sample.go"))
	tb := v.activeTab()
	tb.cursorLn, tb.cursorCol = 3, 6

	if !v.HandleKey(layout.Key{Text: "I"}) {
		t.Fatal("expected 'I' to be consumed")
	}
	if !fake.hoverDispatched {
		t.Fatal("expected a hover request when the server is ready")
	}
	fake.deliver(t)

	if v.hoverText != "func greet(name string) string" {
		t.Errorf("hoverText = %q", v.hoverText)
	}

	w := newFakeWindow(60, 10)
	v.Render(w)
	joined := strings.Join(w.lines, "\n")
	if !strings.Contains(joined, "func greet") {
		t.Errorf("expected the hover text rendered, got:\n%s", joined)
	}
}

func TestHoverKeyNoopWhenServerNotReady(t *testing.T) {
	fake := &fakeLSP{ready: false}
	v := NewView()
	v.lsp = fake
	v.Open(fixturePath(t, "highlight_sample.go"))

	v.HandleKey(layout.Key{Text: "I"})

	if fake.hoverDispatched {
		t.Fatal("expected no hover request when the server isn't ready")
	}
	if v.hoverText != "" {
		t.Errorf("hoverText = %q, want empty", v.hoverText)
	}
}

func TestHoverPopupDismissedByNextKeypress(t *testing.T) {
	fake := &fakeLSP{ready: true, hoverText: "some doc", hoverOK: true}
	v := NewView()
	v.lsp = fake
	v.Open(fixturePath(t, "highlight_sample.go"))

	v.HandleKey(layout.Key{Text: "I"})
	fake.deliver(t)
	if v.hoverText == "" {
		t.Fatal("setup: expected the popup open")
	}

	v.HandleKey(layout.Key{Text: "j"})

	if v.hoverText != "" {
		t.Error("expected the next keypress to dismiss the hover popup")
	}
}

func TestHoverStaleResponseIgnoredAfterTabSwitch(t *testing.T) {
	fake := &fakeLSP{ready: true, hoverText: "stale answer", hoverOK: true}
	v := NewView()
	v.lsp = fake
	v.Open(fixturePath(t, "highlight_sample.go"))

	v.HandleKey(layout.Key{Text: "I"})
	if !fake.hoverDispatched {
		t.Fatal("expected a hover request dispatched")
	}

	// The user opens another file before the server answers.
	v.Open(fixturePath(t, "no_trailing_newline.txt"))
	fake.deliver(t)

	if v.hoverText != "" {
		t.Errorf("hoverText = %q, want empty (the response arrived for a tab that's no longer active)", v.hoverText)
	}
}
