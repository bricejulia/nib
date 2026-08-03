package editor

import "github.com/bricejulia/nib/internal/layout"

// highlightLine is a placeholder, language-agnostic heuristic highlighter:
// it recognizes single-line comments, quoted strings, and numbers by
// simple character-class scanning, with no notion of grammar. It exists to
// give the editor pane some visual structure now; real syntax
// highlighting is a tree-sitter-based step planned later in the project
// (grammars as WASM under wazero, with language injection for embedded
// HTML/CSS/JS etc. inside PHP) — this is not meant to be language-correct,
// just a reasonable-looking stopgap until then.
func highlightLine(line string) []layout.Segment {
	commentStyle := layout.Style{Attr: layout.AttrDim, Foreground: layout.ColorBrightBlack}
	stringStyle := layout.Style{Foreground: layout.ColorGreen}
	numberStyle := layout.Style{Foreground: layout.ColorMagenta}
	plainStyle := layout.Style{}

	var out []layout.Segment
	push := func(text string, style layout.Style) {
		if text == "" {
			return
		}
		if n := len(out); n > 0 && out[n-1].Style == style {
			out[n-1].Text += text
			return
		}
		out = append(out, layout.Segment{Text: text, Style: style})
	}

	runes := []rune(line)
	n := len(runes)

	for i := 0; i < n; {
		r := runes[i]

		if isLineCommentStart(runes, i) {
			push(string(runes[i:]), commentStyle)
			break
		}

		if r == '"' || r == '\'' {
			j := i + 1
			for j < n {
				if runes[j] == '\\' && j+1 < n {
					j += 2
					continue
				}
				if runes[j] == r {
					j++
					break
				}
				j++
			}
			push(string(runes[i:j]), stringStyle)
			i = j
			continue
		}

		if isDigit(r) && (i == 0 || !isIdentRune(runes[i-1])) {
			j := i + 1
			for j < n && (isDigit(runes[j]) || runes[j] == '.') {
				j++
			}
			push(string(runes[i:j]), numberStyle)
			i = j
			continue
		}

		// Plain run: consume everything up to the next special
		// trigger — a comment start, a quote, or a digit that begins
		// a fresh token (not part of the identifier just scanned, so
		// "var1" stays one plain run instead of splitting into "var"
		// + a colored "1").
		j := i + 1
		for j < n {
			c := runes[j]
			if isLineCommentStart(runes, j) || c == '"' || c == '\'' {
				break
			}
			if isDigit(c) && !isIdentRune(runes[j-1]) {
				break
			}
			j++
		}
		push(string(runes[i:j]), plainStyle)
		i = j
	}

	return out
}

func isLineCommentStart(runes []rune, i int) bool {
	if runes[i] == '#' {
		return true
	}
	if i+1 >= len(runes) {
		return false
	}
	pair := runes[i : i+2]
	return (pair[0] == '/' && pair[1] == '/') || (pair[0] == '-' && pair[1] == '-')
}

func isDigit(r rune) bool { return r >= '0' && r <= '9' }

func isIdentRune(r rune) bool {
	return r == '_' ||
		(r >= 'a' && r <= 'z') ||
		(r >= 'A' && r <= 'Z') ||
		isDigit(r)
}
