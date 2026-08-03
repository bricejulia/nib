// Package quitconfirm is a small modal shown on every quit attempt, as a
// safety net against an accidental Ctrl+C — with details on what unsaved
// work is at stake whenever there is any. See cmd/nib/main.go's
// confirmQuit.
package quitconfirm

import (
	"fmt"

	"github.com/bricejulia/nib/internal/layout"
)

// View asks for quit confirmation: with no unsaved files, that's just a
// plain "quit or cancel"; with unsaved files, it lists them and offers to
// save everything and quit, discard and quit anyway, or cancel back to
// editing. Its keys are fixed rather than user-configurable, unlike
// finder/help/debug/diffview: it's a safety dialog, so a misconfigured
// "cancel" binding must never be able to remap itself onto "quit" — the
// same reasoning behind filetree's hardcoded delete-confirmation prompt.
type View struct {
	paths []string

	// OnSaveAndQuit is called on "s": save every unsaved file, then quit.
	// Only offered (and only needed) when there's at least one unsaved
	// file — see Render.
	OnSaveAndQuit func()
	// OnDiscardAndQuit is called on "q": quit, discarding any unsaved
	// files. With none, there's nothing to discard, so this is just quit.
	OnDiscardAndQuit func()
	// OnCancel is called on Esc, dismissing the modal without quitting.
	OnCancel func()
}

// New creates an unshown confirm view; call Show before displaying it as
// an overlay.
func New() *View { return &View{} }

// Show primes the dialog with the paths of every unsaved file, to be
// listed as given — the caller decides absolute vs. project-relative. An
// empty (or nil) paths means "nothing unsaved": Render/Title fall back to
// a plain quit confirmation with no file list or save option.
func (v *View) Show(paths []string) {
	v.paths = paths
}

func (v *View) Title() string {
	if len(v.paths) == 0 {
		return "Quit nib?"
	}
	return "Unsaved changes"
}

func (v *View) Render(w layout.Window) {
	w.Clear()
	row := 0
	line := func(text string, style layout.Style) {
		w.Println(row, layout.Segment{Text: text, Style: style})
		row++
	}

	if len(v.paths) == 0 {
		line("Quit nib?", layout.Style{Attr: layout.AttrBold})
		row++
		line("[q] Quit", layout.Style{})
		line("[Esc] Cancel", layout.Style{})
		return
	}

	noun := "file"
	if len(v.paths) != 1 {
		noun = "files"
	}
	line(fmt.Sprintf("%d unsaved %s:", len(v.paths), noun), layout.Style{Attr: layout.AttrBold})
	for _, p := range v.paths {
		line("  "+p, layout.Style{})
	}
	row++
	line("[s] Save all and quit", layout.Style{})
	line("[q] Quit without saving", layout.Style{})
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
	case k.Named == "" && (k.Text == "s" || k.Text == "S"):
		if v.OnSaveAndQuit != nil {
			v.OnSaveAndQuit()
		}
	case k.Named == "" && (k.Text == "q" || k.Text == "Q"):
		if v.OnDiscardAndQuit != nil {
			v.OnDiscardAndQuit()
		}
	}
	return true
}
