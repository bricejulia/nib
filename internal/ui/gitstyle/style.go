// Package gitstyle is the shared presentation for git status — file-level
// (a single-letter marker and a Style, used by the file tree and fuzzy
// finder) and line-level (a gutter glyph and a Style, used by the editor)
// — so a status renders identically wherever it shows up.
package gitstyle

import (
	"github.com/bricejulia/nib/internal/layout"
	"github.com/bricejulia/nib/internal/vcs/gitstatus"
)

// Marker is the single-character indicator shown next to a path.
func Marker(s gitstatus.Status) string {
	switch s {
	case gitstatus.Modified:
		return "M"
	case gitstatus.Added:
		return "A"
	case gitstatus.Deleted:
		return "D"
	case gitstatus.Renamed:
		return "R"
	case gitstatus.Untracked:
		return "?"
	case gitstatus.Conflicted:
		return "!"
	default:
		return " "
	}
}

// Style colors a path by its status: yellow=modified, green=added,
// red=deleted, cyan=renamed, dim=untracked, bold bright-red=conflicted.
func Style(s gitstatus.Status) layout.Style {
	switch s {
	case gitstatus.Modified:
		return layout.Style{Foreground: layout.ColorYellow}
	case gitstatus.Added:
		return layout.Style{Foreground: layout.ColorGreen}
	case gitstatus.Deleted:
		return layout.Style{Foreground: layout.ColorRed}
	case gitstatus.Renamed:
		return layout.Style{Foreground: layout.ColorCyan}
	case gitstatus.Untracked:
		return layout.Style{Attr: layout.AttrDim}
	case gitstatus.Conflicted:
		return layout.Style{Foreground: layout.ColorBrightRed, Attr: layout.AttrBold}
	default:
		return layout.Style{}
	}
}

// LineMarker is the single-character editor-gutter glyph for a line's git
// diff status (see gitstatus.LineStatus): "+"/"~" sit on the changed line
// itself, while "_" — like vim-gitgutter's convention — rides on the line
// immediately after a deletion, since a pure deletion leaves no line of
// its own to mark.
func LineMarker(s gitstatus.LineStatus) string {
	switch s {
	case gitstatus.LineAdded:
		return "+"
	case gitstatus.LineModified:
		return "~"
	case gitstatus.LineDeletedBefore:
		return "_"
	default:
		return " "
	}
}

// LineStyle colors a line's gutter marker the same way Style colors a
// file's: green=added, yellow=modified, red=deleted.
func LineStyle(s gitstatus.LineStatus) layout.Style {
	switch s {
	case gitstatus.LineAdded:
		return layout.Style{Foreground: layout.ColorGreen}
	case gitstatus.LineModified:
		return layout.Style{Foreground: layout.ColorYellow}
	case gitstatus.LineDeletedBefore:
		return layout.Style{Foreground: layout.ColorRed}
	default:
		return layout.Style{}
	}
}
