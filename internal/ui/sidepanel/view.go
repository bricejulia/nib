// Package sidepanel is the files panel: one pane holding the file tree and
// the finder, shown as a single strip of views cycled with "[" / "]":
//
//	Files ↔ Changes ↔ Find File ↔ Find in Files ↔ Replace ↔ (Files)
//
// The first two are filetree.View's own modes, the last three finder.View's.
// Each child cycles its own views and hands over to the other past its ends
// (their OnViewOverflow hooks), so their keymaps — and the user's overrides
// of them — keep working unchanged. Every layout.View method, and every
// optional pane interface App looks for, is delegated to whichever child is
// showing. Pastes reach the active child through HandleKey, App's own
// fallback for a pane that isn't a layout.Paster (neither child is).
package sidepanel

import (
	"strings"

	"github.com/bricejulia/nib/internal/layout"
	"github.com/bricejulia/nib/internal/ui/filetree"
	"github.com/bricejulia/nib/internal/ui/finder"
)

// View is the files panel. Create it with New.
type View struct {
	tree   *filetree.View
	finder *finder.View

	finderShown bool
}

// New builds the panel around tree and f, taking over their view-overflow
// hooks and f's OnClose (Esc in browse returns to the Files view). The
// panel starts on the tree.
func New(tree *filetree.View, f *finder.View) *View {
	v := &View{tree: tree, finder: f}
	tree.OnViewOverflow = func(dir int) {
		if dir > 0 {
			f.ResumeFirst()
		} else {
			f.ResumeLast()
		}
		v.finderShown = true
	}
	f.OnViewOverflow = func(dir int) {
		if dir > 0 {
			tree.SetMode(filetree.ModeFiles)
		} else {
			tree.SetMode(filetree.ModeChanges)
		}
		v.finderShown = false
	}
	f.OnClose = func() { v.ShowTree(filetree.ModeFiles) }
	return v
}

// ShowTree switches the panel to the file tree, in mode.
func (v *View) ShowTree(mode filetree.Mode) {
	v.tree.SetMode(mode)
	v.finderShown = false
}

// ShowFinder switches the panel to the finder, as the caller last left it
// (typically right after one of its Open* calls).
func (v *View) ShowFinder() {
	v.finderShown = true
}

// TreeActive reports whether the file tree, rather than the finder, is
// showing.
func (v *View) TreeActive() bool {
	return !v.finderShown
}

// FinderActive reports whether the finder is showing.
func (v *View) FinderActive() bool {
	return v.finderShown
}

func (v *View) active() layout.View {
	if v.finderShown {
		return v.finder
	}
	return v.tree
}

// viewNames are the strip's labels, in cycle order.
var viewNames = [...]string{"Files", "Changes", "Find", "Content", "Replace"}

// activeIndex is the showing view's position in viewNames.
func (v *View) activeIndex() int {
	if !v.finderShown {
		if v.tree.CurrentMode() == filetree.ModeChanges {
			return 1
		}
		return 0
	}
	switch v.finder.CurrentMode() {
	case finder.ModeContent:
		return 3
	case finder.ModeReplace:
		return 4
	default:
		return 2
	}
}

// Title is the view strip, the showing view in brackets:
// "Files [Changes] Find Content Replace".
func (v *View) Title() string {
	active := v.activeIndex()
	parts := make([]string, len(viewNames))
	for i, name := range viewNames {
		if i == active {
			name = "[" + name + "]"
		}
		parts[i] = name
	}
	return strings.Join(parts, " ")
}

// Render implements layout.View.
func (v *View) Render(w layout.Window) { v.active().Render(w) }

// HandleKey implements layout.View.
func (v *View) HandleKey(k layout.Key) bool { return v.active().HandleKey(k) }

// CursorPosition implements layout.CursorProvider.
func (v *View) CursorPosition() (int, int, bool) {
	if cp, ok := v.active().(layout.CursorProvider); ok {
		return cp.CursorPosition()
	}
	return 0, 0, false
}

// ScrollState implements layout.Scrollable.
func (v *View) ScrollState() layout.ScrollState {
	if s, ok := v.active().(layout.Scrollable); ok {
		return s.ScrollState()
	}
	return layout.ScrollState{}
}

// ScrollTo implements layout.ScrollTarget.
func (v *View) ScrollTo(top int) {
	if s, ok := v.active().(layout.ScrollTarget); ok {
		s.ScrollTo(top)
	}
}

// ScrollMarks implements layout.ScrollMarker.
func (v *View) ScrollMarks() []layout.ScrollMark {
	if s, ok := v.active().(layout.ScrollMarker); ok {
		return s.ScrollMarks()
	}
	return nil
}

// HandleMouse implements layout.MouseHandler.
func (v *View) HandleMouse(m layout.Mouse) bool {
	if h, ok := v.active().(layout.MouseHandler); ok {
		return h.HandleMouse(m)
	}
	return false
}
