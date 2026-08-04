package editor

import (
	"strings"

	"github.com/mattn/go-runewidth"

	"github.com/bricejulia/nib/internal/layout"
	"github.com/bricejulia/nib/internal/theme"
)

// whitespaceStyle returns base with its Foreground/Attr overridden to the
// dim "structural, not content" look renderBody draws space/tab-fill
// glyphs in, keeping base's own Background — so a glyph drawn inside a
// selection or search highlight still shows that highlight's background
// rather than erasing it. Not a package var, for the same reason
// selectionStyle isn't one: a var would freeze on theme.Default before
// cmd/nib installs the user's theme.
func whitespaceStyle(base layout.Style) layout.Style {
	base.Foreground = theme.Get(theme.EditorWhitespace)
	base.Attr = layout.AttrDim
	return base
}

// expandTabsForDisplay is renderBody's whitespace-visualization
// counterpart to textwidth.ExpandTabsSegments: the same tab-stop
// expansion — one running display column threaded across every segment
// in order, so a tab's width still depends on the column it starts at —
// but marked as it goes: a literal ' ' becomes '·', and each '\t'
// becomes '→' followed by blank padding out to ITS OWN tab stop, all
// styled via whitespaceStyle.
//
// Marking tabs while expanding, rather than guessing at "runs of spaces"
// in the already-expanded output, is what makes several consecutive tab
// characters draw one arrow each — a first attempt at this expanded
// AND THEN scanned the result for space runs, which can't tell three
// adjacent tabs apart from one wide run of tab-fill, and drew only one
// arrow for all of them combined.
//
// Called INSTEAD of ExpandTabsSegments when showWhitespace is on — not
// alongside it.
func expandTabsForDisplay(segs []layout.Segment, tabWidth int) []layout.Segment {
	if tabWidth <= 0 {
		tabWidth = 8
	}
	out := make([]layout.Segment, 0, len(segs))
	col := 0
	for _, seg := range segs {
		var b strings.Builder
		flush := func() {
			if b.Len() > 0 {
				out = append(out, layout.Segment{Text: b.String(), Style: seg.Style})
				b.Reset()
			}
		}
		for _, r := range seg.Text {
			switch r {
			case '\t':
				flush()
				spaces := tabWidth - (col % tabWidth)
				out = append(out, layout.Segment{Text: "→", Style: whitespaceStyle(seg.Style)})
				if spaces > 1 {
					out = append(out, layout.Segment{Text: strings.Repeat(" ", spaces-1), Style: seg.Style})
				}
				col += spaces
			case ' ':
				flush()
				out = append(out, layout.Segment{Text: "·", Style: whitespaceStyle(seg.Style)})
				col++
			default:
				b.WriteRune(r)
				col += runewidth.RuneWidth(r)
			}
		}
		flush()
	}
	return out
}
