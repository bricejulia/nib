package editor

import (
	"testing"

	"github.com/bricejulia/nib/internal/layout"
)

func segText(segs []layout.Segment) string {
	s := ""
	for _, seg := range segs {
		s += seg.Text
	}
	return s
}

func TestHighlightLineColorsStringsCommentsAndNumbers(t *testing.T) {
	segs := highlightLine(`x = "hi" // trailing comment`)
	joined := segText(segs)
	if joined != `x = "hi" // trailing comment` {
		t.Fatalf("segments must reconstruct the original line losslessly, got %q", joined)
	}

	var foundString, foundComment bool
	for _, s := range segs {
		if s.Text == `"hi"` {
			foundString = true
			if s.Style.Foreground != layout.ColorGreen {
				t.Errorf("string literal should be green, got %+v", s.Style)
			}
		}
		if s.Text == "// trailing comment" {
			foundComment = true
			if s.Style.Attr&layout.AttrDim == 0 {
				t.Errorf("comment should be dim, got %+v", s.Style)
			}
		}
	}
	if !foundString {
		t.Errorf("expected a segment exactly matching the string literal, got %+v", segs)
	}
	if !foundComment {
		t.Errorf("expected a segment exactly matching the comment, got %+v", segs)
	}
}

func TestHighlightLineNumberVsIdentifierWithTrailingDigits(t *testing.T) {
	segs := highlightLine("var1 = 42")
	joined := segText(segs)
	if joined != "var1 = 42" {
		t.Fatalf("segments must reconstruct the original line losslessly, got %q", joined)
	}

	for _, s := range segs {
		if s.Text == "var1" && s.Style.Foreground == layout.ColorMagenta {
			t.Errorf(`identifier "var1" should not be colored as a number: %+v`, s)
		}
	}
	var foundNumber bool
	for _, s := range segs {
		if s.Text == "42" {
			foundNumber = true
			if s.Style.Foreground != layout.ColorMagenta {
				t.Errorf("standalone number should be magenta, got %+v", s.Style)
			}
		}
	}
	if !foundNumber {
		t.Errorf("expected a segment exactly matching the standalone number, got %+v", segs)
	}
}

func TestHighlightLinePlainTextHasNoSpecialStyle(t *testing.T) {
	segs := highlightLine("plain text with no special tokens")
	for _, s := range segs {
		if s.Style != (layout.Style{}) {
			t.Errorf("expected plain default style, got %+v in %+v", s.Style, segs)
		}
	}
}
