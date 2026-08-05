// Package memprompt is a small modal offering to close whichever open
// file is using the most memory, shown when internal/memwatch reports
// nib's own heap crossing a threshold. See cmd/nib/main.go's
// memoryThresholdEvent handling. Structurally a near-copy of
// internal/ui/quitconfirm — same modal mechanism (a fixed, non-
// configurable keymap; a safety dialog, not a feature, so it can't be
// remapped onto itself the way finder/help/debug's keys can), one binary
// choice instead of three.
package memprompt

import (
	"fmt"

	"github.com/bricejulia/nib/internal/layout"
)

// View offers to close a specific file to free memory. Only one such
// prompt is ever shown at a time (see app.OverlayActive), so it holds
// exactly the one target Show was last called with — no queue, no list.
type View struct {
	path      string
	sizeBytes int
	heapBytes int

	// OnConfirmClose is called on "c": close the named file.
	OnConfirmClose func()
	// OnCancel is called on Esc, dismissing the modal without closing
	// anything.
	OnCancel func()
}

// New creates an unshown prompt; call Show before displaying it as an
// overlay.
func New() *View { return &View{} }

// Show primes the dialog: path is the file being offered up to close,
// sizeBytes its own Buffer.Source size, heapBytes nib's total heap size
// at the moment the threshold was crossed (see memwatch.Watcher).
func (v *View) Show(path string, sizeBytes, heapBytes int) {
	v.path = path
	v.sizeBytes = sizeBytes
	v.heapBytes = heapBytes
}

func (v *View) Title() string { return "High memory usage" }

func (v *View) Render(w layout.Window) {
	w.Clear()
	row := 0
	line := func(text string, style layout.Style) {
		w.Println(row, layout.Segment{Text: text, Style: style})
		row++
	}

	line(fmt.Sprintf("nib is using ~%d MiB of memory.", mib(v.heapBytes)), layout.Style{Attr: layout.AttrBold})
	line(fmt.Sprintf("%s alone accounts for ~%d MiB.", v.path, mib(v.sizeBytes)), layout.Style{})
	row++
	line("[c] Close it", layout.Style{})
	line("[Esc] Cancel", layout.Style{})
}

// HandleKey always reports the key consumed: a modal should never leak
// input through to whatever is behind it.
func (v *View) HandleKey(k layout.Key) bool {
	if k.EventType == layout.EventRelease {
		return true
	}

	switch {
	case k.Named == layout.KeyEsc:
		if v.OnCancel != nil {
			v.OnCancel()
		}
	case k.Named == "" && (k.Text == "c" || k.Text == "C"):
		if v.OnConfirmClose != nil {
			v.OnConfirmClose()
		}
	}
	return true
}

// mib rounds n bytes down to whole mebibytes for display — plenty of
// precision for a "your memory use is high" notice, which is never going
// to be showing single-digit values anyway.
func mib(n int) int {
	return n / (1 << 20)
}
