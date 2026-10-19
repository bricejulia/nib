package lsp

import (
	"regexp"
	"strings"
)

// compileGlob turns an LSP glob pattern (the didChangeWatchedFiles
// registration syntax) into an anchored regexp over slash-separated
// paths. Supported: "*" (any run of characters within one segment), "?"
// (one character), "**" (any number of segments, including none), "{a,b}"
// alternatives (not nested), and "[...]" character classes. That covers
// what servers actually send — gopls registers "**/*.{go,mod,sum,work,s}"
// and friends. A malformed pattern yields nil, which matches nothing.
func compileGlob(pattern string) *regexp.Regexp {
	var b strings.Builder
	b.WriteString("^")
	inBraces := false
	for i := 0; i < len(pattern); i++ {
		c := pattern[i]
		switch c {
		case '*':
			if i+1 < len(pattern) && pattern[i+1] == '*' {
				i++
				if i+1 < len(pattern) && pattern[i+1] == '/' {
					i++
					b.WriteString("(?:.*/)?") // "**/": zero or more whole segments
				} else {
					b.WriteString(".*")
				}
			} else {
				b.WriteString("[^/]*")
			}
		case '?':
			b.WriteString("[^/]")
		case '{':
			if inBraces {
				return nil
			}
			inBraces = true
			b.WriteString("(?:")
		case '}':
			if !inBraces {
				return nil
			}
			inBraces = false
			b.WriteString(")")
		case ',':
			if inBraces {
				b.WriteString("|")
			} else {
				b.WriteString(",")
			}
		case '[':
			end := strings.IndexByte(pattern[i+1:], ']')
			if end < 0 {
				return nil
			}
			class := pattern[i+1 : i+1+end]
			if strings.HasPrefix(class, "!") {
				class = "^" + class[1:]
			}
			b.WriteString("[" + strings.ReplaceAll(class, `\`, `\\`) + "]")
			i += end + 1
		default:
			b.WriteString(regexp.QuoteMeta(string(c)))
		}
	}
	if inBraces {
		return nil
	}
	b.WriteString("$")
	re, err := regexp.Compile(b.String())
	if err != nil {
		return nil
	}
	return re
}
