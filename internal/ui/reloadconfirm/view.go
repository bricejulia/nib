// Package reloadconfirm is the three-way modal shown when a save would
// overwrite a file that changed on disk since nib last synced with it (see
// editor.Buffer.HasDiskConflict) — Keep mine (overwrite disk), Reload from
// disk (discard in-memory edits), or Cancel. Structurally a near-copy of
// internal/ui/quitconfirm/internal/ui/memprompt: same fixed,
// non-configurable keymap, since this is a safety dialog, not a feature to
// be remapped onto itself. See cmd/nib/main.go's reloadconfirm wiring for
// how several conflicts (e.g. from a save-all) are queued and resolved one
// at a time, re-priming this same View for each.
package reloadconfirm

import (
	"fmt"

	"github.com/bricejulia/nib/internal/layout"
)

// View asks how to resolve one file's save conflict. Only one target is
// ever shown at a time (see app.OverlayActive) — the caller owns any
// queue of further conflicts and re-calls Show for the next one from
// inside OnKeepMine/OnReloadFromDisk, the same way memprompt.View holds
// exactly the one target Show was last called with, no queue of its own.
type View struct {
	path      string
	remaining int // conflicts still queued behind this one; 0 means "the last one" — see Render

	// OnKeepMine is called on "k": overwrite disk with the in-memory
	// content. This view holds no *editor.Buffer, only a path, to stay as
	// decoupled from the editor package as quitconfirm/memprompt are — the
	// caller is expected to know which buffer path refers to and call its
	// now-clear-to-proceed Save() directly.
	OnKeepMine func()
	// OnReloadFromDisk is called on "r": discard in-memory edits and
	// reload the file's on-disk content.
	OnReloadFromDisk func()
	// OnCancel is called on Esc: dismiss without writing or reloading.
	OnCancel func()
}

// New creates an unshown prompt; call Show before displaying it as an
// overlay.
func New() *View { return &View{} }

// Show primes the dialog: path is the file in conflict (the caller
// decides absolute vs. project-relative, matching quitconfirm.Show).
// remaining is how many MORE conflicts are queued behind this one — 0 for
// a single-file save, or the last of a batch — shown as "(N more)" so a
// save-all with several conflicts doesn't read as stuck after each one is
// resolved.
func (v *View) Show(path string, remaining int) {
	v.path, v.remaining = path, remaining
}

func (v *View) Title() string { return "File changed on disk" }

func (v *View) Render(w layout.Window) {
	w.Clear()
	row := 0
	line := func(text string, style layout.Style) {
		w.Println(row, layout.Segment{Text: text, Style: style})
		row++
	}

	line(fmt.Sprintf("%s changed on disk since nib last read it.", v.path), layout.Style{Attr: layout.AttrBold})
	if v.remaining > 0 {
		noun := "file"
		if v.remaining != 1 {
			noun = "files"
		}
		line(fmt.Sprintf("(%d more conflicting %s queued)", v.remaining, noun), layout.Style{})
	}
	row++
	line("[k] Keep mine (overwrite disk)", layout.Style{})
	line("[r] Reload from disk (discard my edits)", layout.Style{})
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
	case k.Named == "" && (k.Text == "k" || k.Text == "K"):
		if v.OnKeepMine != nil {
			v.OnKeepMine()
		}
	case k.Named == "" && (k.Text == "r" || k.Text == "R"):
		if v.OnReloadFromDisk != nil {
			v.OnReloadFromDisk()
		}
	}
	return true
}
