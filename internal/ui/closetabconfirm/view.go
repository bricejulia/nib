// Package closetabconfirm is a small modal shown when a mouse-driven close
// (middle-click on a tab, or "Close Others"/"Close All" from its context
// menu — see editor.View.OnRequestCloseDirtyTabs) would otherwise silently
// discard unsaved changes. Unlike ":q"/":qa" on a dirty buffer, which just
// refuse silently (a debug-log line, no visible feedback — fine for a
// command-line typo, not for a mouse gesture), this gives a real choice.
// See cmd/nib/main.go's wireEditorPane.
package closetabconfirm

import (
	"fmt"

	"github.com/bricejulia/nib/internal/layout"
)

// View asks whether to save or discard one or more dirty tabs before
// closing them — quitconfirm's own single-vs-many shape, reused here for a
// single dirty tab (middle-click) and for a batch of them (Close
// Others/Close All). Its keys are fixed rather than user-configurable,
// matching quitconfirm and reloadconfirm: a safety dialog's "cancel" must
// never be remappable onto "discard".
type View struct {
	paths []string

	// OnSaveAll is called on "s": save every listed file, then close their
	// tabs.
	OnSaveAll func()
	// OnDiscardAll is called on "d": close the tabs, discarding every
	// listed change.
	OnDiscardAll func()
	// OnCancel is called on Esc, dismissing the modal without closing
	// anything.
	OnCancel func()
}

// New creates an unshown confirm view; call Show before displaying it as an
// overlay.
func New() *View { return &View{} }

// Show primes the dialog with the paths of every dirty tab that would be
// closed, to be listed as given — the caller decides absolute vs.
// project-relative.
func (v *View) Show(paths []string) {
	v.paths = paths
}

func (v *View) Title() string { return "Unsaved changes" }

func (v *View) Render(w layout.Window) {
	w.Clear()
	row := 0
	line := func(text string, style layout.Style) {
		w.Println(row, layout.Segment{Text: text, Style: style})
		row++
	}

	if len(v.paths) == 1 {
		line(v.paths[0]+" has unsaved changes", layout.Style{Attr: layout.AttrBold})
	} else {
		noun := "files"
		line(fmt.Sprintf("%d unsaved %s:", len(v.paths), noun), layout.Style{Attr: layout.AttrBold})
		for _, p := range v.paths {
			line("  "+p, layout.Style{})
		}
	}
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
		if v.OnSaveAll != nil {
			v.OnSaveAll()
		}
	case k.Named == "" && (k.Text == "d" || k.Text == "D"):
		if v.OnDiscardAll != nil {
			v.OnDiscardAll()
		}
	}
	return true
}
