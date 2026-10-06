// Package trustprompt is the modal asking, the first time nib opens a
// folder, whether nib may keep per-project state there (see
// internal/session) — nothing is written into a project the user hasn't
// trusted. See cmd/nib/main.go's session wiring.
package trustprompt

import "github.com/bricejulia/nib/internal/layout"

// View asks whether to trust a folder. Its keys are fixed rather than
// user-configurable, like quitconfirm's: answering it writes into the
// user's project, so a misconfigured binding must never turn "no" into
// "yes".
type View struct {
	root string

	// OnTrust is called on "y".
	OnTrust func()
	// OnDecline is called on "n" or Esc. Declining isn't remembered: the
	// prompt shows again on the next launch in this folder.
	OnDecline func()
}

// New creates an unshown prompt; call Show before displaying it as an
// overlay.
func New() *View { return &View{} }

// Show primes the prompt with the folder being asked about.
func (v *View) Show(root string) {
	v.root = root
}

func (v *View) Title() string { return "Trust this folder?" }

func (v *View) Render(w layout.Window) {
	w.Clear()
	row := 0
	line := func(text string, style layout.Style) {
		w.Println(row, layout.Segment{Text: text, Style: style})
		row++
	}

	line("Trust this folder?", layout.Style{Attr: layout.AttrBold})
	line("  "+v.root, layout.Style{})
	row++
	line("nib will create a .nib folder here to remember", layout.Style{})
	line("your open files and splits between sessions.", layout.Style{})
	row++
	line("[y] Trust", layout.Style{})
	line("[n] Don't trust", layout.Style{})
}

// HandleKey always reports the key consumed: a modal should never leak
// input through to whatever is behind it.
func (v *View) HandleKey(k layout.Key) bool {
	if k.EventType == layout.EventRelease {
		return true
	}

	switch {
	case k.Named == "" && (k.Text == "y" || k.Text == "Y"):
		if v.OnTrust != nil {
			v.OnTrust()
		}
	case k.Named == layout.KeyEsc, k.Named == "" && (k.Text == "n" || k.Text == "N"):
		if v.OnDecline != nil {
			v.OnDecline()
		}
	}
	return true
}
