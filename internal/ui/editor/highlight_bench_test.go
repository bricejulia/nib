package editor

import (
	"os"
	"path/filepath"
	"strings"
	"testing"

	"github.com/bricejulia/nib/internal/layout"
)

// benchFile writes the first n lines of view.go (this package's own
// largest file — a realistic 1800-line Go source) to a temp file with the
// given extension, and returns its path. n <= 0 means the whole file.
func benchFile(b *testing.B, n int, ext string) string {
	b.Helper()
	data, err := os.ReadFile("view.go")
	if err != nil {
		b.Skip(err)
	}
	lines := strings.Split(string(data), "\n")
	if n > 0 && len(lines) > n {
		lines = lines[:n]
	}
	p := filepath.Join(b.TempDir(), "probe"+ext)
	if err := os.WriteFile(p, []byte(strings.Join(lines, "\n")), 0o644); err != nil {
		b.Fatal(err)
	}
	return p
}

// benchKeystrokes types into a file through the real key path and reports
// what one keystroke costs.
//
// This is the benchmark the whole highlighter exists for. With highlighting
// inline it measured 2.9ms/key at 300 lines, 100ms at 1000 and 236ms at
// 1822 — visible lag, growing with the file. With the work on the worker it
// should sit near the no-grammar control below (~45µs) at every size,
// because the keystroke no longer parses anything.
func benchKeystrokes(b *testing.B, lines int, ext string, async bool) {
	v := NewView()
	if async {
		h := NewHighlighter(nil) // results go nowhere; only the submit cost is on this goroutine
		defer h.Close()
		v.SetHighlighter(h)
	}
	v.Open(benchFile(b, lines, ext))
	v.HandleKey(layout.Key{Text: "i"}) // Insert mode

	b.ResetTimer()
	for i := 0; i < b.N; i++ {
		v.HandleKey(layout.Key{Text: "x"})
	}
}

func BenchmarkKeystroke300(b *testing.B)  { benchKeystrokes(b, 300, ".go", true) }
func BenchmarkKeystroke1000(b *testing.B) { benchKeystrokes(b, 1000, ".go", true) }
func BenchmarkKeystroke1800(b *testing.B) { benchKeystrokes(b, 0, ".go", true) }

// The control: an extension no grammar claims, so no highlighting happens
// at any point. Everything the other benchmarks cost ABOVE this number is
// attributable to syntax highlighting.
func BenchmarkKeystrokeNoGrammar1800(b *testing.B) { benchKeystrokes(b, 0, ".zzz", true) }

// What a keystroke cost before the worker existed, kept as the baseline the
// numbers above are meant to be compared against.
func BenchmarkKeystrokeInlineHighlight1800(b *testing.B) { benchKeystrokes(b, 0, ".go", false) }

// One full-buffer parse, the work the worker does off the keystroke path.
func BenchmarkHighlightSource1800(b *testing.B) {
	src, err := os.ReadFile("view.go")
	if err != nil {
		b.Skip(err)
	}
	for i := 0; i < b.N; i++ {
		if _, ok := highlightSource("probe.go", src); !ok {
			b.Fatal("parse stopped early")
		}
	}
}
