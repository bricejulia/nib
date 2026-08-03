package editor

import (
	"regexp"
	"strings"
)

// markdownFenceRe matches a code-fence delimiter line, with or without a
// language tag ("```" or "```go") — these are dropped, not the code
// between them.
var markdownFenceRe = regexp.MustCompile("^```[a-zA-Z0-9_+-]*$")

// markdownRuleRe matches a markdown horizontal rule sitting alone on its
// own line.
var markdownRuleRe = regexp.MustCompile(`^(-{3,}|\*{3,}|_{3,})$`)

// markdownLinkRe matches a markdown link, capturing its label.
var markdownLinkRe = regexp.MustCompile(`\[([^\]]*)\]\([^)]*\)`)

// markdownBoldRe matches **bold** text, capturing its content.
var markdownBoldRe = regexp.MustCompile(`\*\*([^*]+)\*\*`)

// stripMarkdown renders s — a server's hover/signature-help text, which is
// routinely LSP MarkupContent with kind "markdown" (gopls always sends
// markdown) — as plain text fit for nib's popup, which has no markdown
// renderer of its own: code-fence delimiters, links, horizontal rules, and
// bold markers are removed, while the underlying text (a fenced sample's
// code, a link's label) is kept.
//
// Deliberately leaves single underscores/asterisks (italic) and
// inline-code backticks alone: both collide too often with ordinary Go
// text — snake_case identifiers, pointer dereferences, backtick raw
// string literals inside a fenced code sample — to strip safely. This is
// a targeted cleanup of the syntax gopls actually emits, not a markdown
// renderer.
func stripMarkdown(s string) string {
	s = markdownLinkRe.ReplaceAllString(s, "$1")
	s = markdownBoldRe.ReplaceAllString(s, "$1")

	lines := strings.Split(s, "\n")
	out := make([]string, 0, len(lines))
	for _, line := range lines {
		trimmed := strings.TrimSpace(line)
		if markdownFenceRe.MatchString(trimmed) || markdownRuleRe.MatchString(trimmed) {
			continue
		}
		out = append(out, line)
	}
	return strings.Join(out, "\n")
}
