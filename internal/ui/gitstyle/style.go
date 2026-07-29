// Package gitstyle is the shared presentation for git file status — a
// single-letter marker and a Style — used by both the file tree and the
// fuzzy finder so a file's status renders identically wherever it shows
// up.
package gitstyle

import (
	"github.com/bricejulia/kiwi/internal/layout"
	"github.com/bricejulia/kiwi/internal/vcs/gitstatus"
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
