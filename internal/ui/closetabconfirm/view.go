// Package closetabconfirm is a small modal shown when a mouse-driven close
// (middle-click on a tab — see editor.View.OnRequestCloseDirtyTab) targets a
// tab with unsaved changes. It is quitconfirm's single-file shape: unlike
// ":q" on a dirty buffer, which just refuses silently (a debug-log line, no
// visible feedback — fine for a command-line typo, not for a mouse gesture),
// this gives the user a real choice. See cmd/nib/main.go's wireEditorPane.
package closetabconfirm

import (
	"github.com/bricejulia/nib/internal/layout"
)

// View asks whether to save or discard one dirty tab before closing it. Its
// keys are fixed rather than user-configurable, matching quitconfirm and
// reloadconfirm: a safety dialog's "cancel" must never be remappable onto
// "discard".
type View struct {
	path string

	// OnSave is called on "s": save this file, then close its tab.
	OnSave func()
	// OnDiscard is called on "d": close the tab, discarding the change.
	OnDiscard func()
	// OnCancel is called on Esc, dismissing the modal without closing.
	OnCancel func()
}

// New creates an unshown confirm view; call Show before displaying it as an
// overlay.
func New() *View { return &View{} }

// Show primes the dialog with the tab's path, to be shown as given — the
// caller decides absolute vs. project-relative.
func (v *View) Show(path string) {
	v.path = path
}

func (v *View) Title() string { return "Unsaved changes" }

func (v *View) Render(w layout.Window) {
	w.Clear()
	row := 0
	line := func(text string, style layout.Style) {
		w.Println(row, layout.Segment{Text: text, Style: style})
		row++
	}

	line(v.path+" has unsaved changes", layout.Style{Attr: layout.AttrBold})
	row++
	line("[s] Save and close", layout.Style{})
	line("[d] Discard and close", layout.Style{})
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
		if v.OnSave != nil {
			v.OnSave()
		}
	case k.Named == "" && (k.Text == "d" || k.Text == "D"):
		if v.OnDiscard != nil {
			v.OnDiscard()
		}
	}
	return true
}
