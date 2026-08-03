package editor

import (
	"strings"
	"testing"
)

func TestStripMarkdownRemovesCodeFenceDelimiters(t *testing.T) {
	in := "```go\nfunc Foo() string\n```\n\nDoes the foo thing."
	got := stripMarkdown(in)
	if strings.Contains(got, "```") {
		t.Errorf("fence delimiters survived: %q", got)
	}
	if !strings.Contains(got, "func Foo() string") {
		t.Errorf("expected the fenced content kept, got %q", got)
	}
	if !strings.Contains(got, "Does the foo thing.") {
		t.Errorf("expected the prose kept, got %q", got)
	}
}

func TestStripMarkdownRemovesHorizontalRule(t *testing.T) {
	in := "summary\n\n---\n\ndetails"
	got := stripMarkdown(in)
	if strings.Contains(got, "---") {
		t.Errorf("horizontal rule survived: %q", got)
	}
	if !strings.Contains(got, "summary") || !strings.Contains(got, "details") {
		t.Errorf("expected the surrounding text kept, got %q", got)
	}
}

func TestStripMarkdownConvertsLinksToLabel(t *testing.T) {
	in := "see [InvalidUnmarshalError](file:///path/decode.go#158,6) for details"
	got := stripMarkdown(in)
	if strings.Contains(got, "file://") {
		t.Errorf("link URL survived: %q", got)
	}
	if !strings.Contains(got, "InvalidUnmarshalError") {
		t.Errorf("expected the link's label kept, got %q", got)
	}
}

func TestStripMarkdownRemovesBoldMarkers(t *testing.T) {
	got := stripMarkdown("this is **bold** text")
	if strings.Contains(got, "**") {
		t.Errorf("bold markers survived: %q", got)
	}
	if !strings.Contains(got, "bold") {
		t.Errorf("expected the bold text's content kept, got %q", got)
	}
}

func TestStripMarkdownLeavesUnderscoresAndBackticksAlone(t *testing.T) {
	// snake_case identifiers and inline-code backticks are common enough in
	// real Go text that stripping single underscores/backticks risks
	// corrupting them — see stripMarkdown's doc comment.
	in := "the `my_var` parameter"
	got := stripMarkdown(in)
	if got != in {
		t.Errorf("got %q, want unchanged %q", got, in)
	}
}

// TestStripMarkdownHandlesRealGoplsHoverShape is a regression test for the
// exact shape gopls sends for a stdlib function: a fenced signature, a
// horizontal rule, then prose containing a markdown link to a local file.
func TestStripMarkdownHandlesRealGoplsHoverShape(t *testing.T) {
	in := "```go\nfunc json.Unmarshal(data []byte, v any) error\n```\n\n---\n\nUnmarshal parses the JSON-encoded data and stores the result in the value pointed to by v. If v is nil or not a pointer, Unmarshal returns an [InvalidUnmarshalError](file:///opt/homebrew/opt/go/libexec/src/encoding/json/decode.go#158,6)."
	got := stripMarkdown(in)
	for _, artifact := range []string{"```", "file://", "](", "\n---\n"} {
		if strings.Contains(got, artifact) {
			t.Errorf("markdown artifact %q survived in: %q", artifact, got)
		}
	}
	if !strings.Contains(got, "func json.Unmarshal(data []byte, v any) error") {
		t.Errorf("expected the signature kept, got %q", got)
	}
	if !strings.Contains(got, "InvalidUnmarshalError") {
		t.Errorf("expected the link label kept, got %q", got)
	}
}
