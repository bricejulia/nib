package editor

import (
	"os"
	"strings"
)

// Buffer is the simplest possible read model for Step 0's read-only pane:
// no rope, no undo, no insert mode. Swapping in a rope/piece-table later
// replaces Buffer and the View's line-fetch code only, not the View's
// scroll/cursor/tab/width logic.
type Buffer struct {
	Lines  []string // one entry per line, no trailing newline
	Path   string
	Source []byte // raw bytes Lines was split from — same text, pre-split;
	// tree-sitter's Highlight() needs byte offsets into this, not Lines.
}

// Load reads path into a Buffer.
func Load(path string) (*Buffer, error) {
	data, err := os.ReadFile(path)
	if err != nil {
		return nil, err
	}
	text := string(data)
	text = strings.TrimSuffix(text, "\n")
	var lines []string
	if text == "" {
		lines = []string{""}
	} else {
		lines = strings.Split(text, "\n")
	}
	return &Buffer{Lines: lines, Path: path, Source: []byte(text)}, nil
}
