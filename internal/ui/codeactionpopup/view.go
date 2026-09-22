// Package codeactionpopup is a "Code Actions" popup meant to be shown as
// a modal overlay (see ui.App.ShowOverlay/CloseOverlay): a fixed,
// already-resolved list of the language server's suggested fixes/
// refactors at the cursor, Up/Down to move the selection, Enter to apply
// it, Esc to close without applying anything.
//
// Mechanically identical to internal/ui/actionpopup's Up/Down/Enter/Esc
// list minus the query box — there's nothing to filter, the server
// already scoped the list to the cursor — and, like internal/ui/refview,
// fed fresh per invocation rather than actionpopup's fixed compile-time
// entries.
package codeactionpopup

import (
	"github.com/bricejulia/nib/internal/config"
	"github.com/bricejulia/nib/internal/layout"
	"github.com/bricejulia/nib/internal/lsp"
)

// DefaultKeybinds are this popup's built-in keybindings, overridable via
// the user config's "codeactions" scope (see internal/config).
var DefaultKeybinds = config.Defaults{
	{Trigger: "Esc", Action: "close"},
	{Trigger: "Down", Action: "move_down"},
	{Trigger: "Up", Action: "move_up"},
	{Trigger: "Enter", Action: "execute"},
}

// View is the code-actions popup's content: a selectable, scrollable list
// of server-suggested actions.
type View struct {
	actions []lsp.CodeAction

	cursor              int
	scrollTop, lastRows int

	// OnClose is called when Esc is pressed, dismissing the overlay.
	OnClose func()
	// OnExecute is called with the selected action when Enter is pressed
	// on a non-empty list. Applying its Edit is the caller's job — this
	// package only knows how to list and select, the same split
	// actionpopup.View makes between searching/selecting and dispatching.
	OnExecute func(action lsp.CodeAction)

	keymap map[string]string
}

// New creates an empty code-actions popup; call Open before showing it.
func New() *View {
	return &View{keymap: DefaultKeybinds.Resolve(nil)}
}

// SetKeymap merges the user config's "codeactions" scope overrides on top
// of DefaultKeybinds, replacing the popup's active keymap.
func (v *View) SetKeymap(overrides map[string]string) {
	v.keymap = DefaultKeybinds.Resolve(overrides)
}

func (v *View) Title() string { return "Code Actions" }

// Open resets the list and selection to actions.
func (v *View) Open(actions []lsp.CodeAction) {
	v.actions = actions
	v.cursor = 0
	v.scrollTop = 0
}

func (v *View) Render(w layout.Window) {
	_, rows := w.Size()
	w.Clear()
	v.lastRows = rows

	if len(v.actions) == 0 {
		return
	}

	if v.cursor < v.scrollTop {
		v.scrollTop = v.cursor
	}
	if v.cursor >= v.scrollTop+rows {
		v.scrollTop = v.cursor - rows + 1
	}
	if v.scrollTop < 0 {
		v.scrollTop = 0
	}

	for i := 0; i < rows; i++ {
		idx := v.scrollTop + i
		if idx >= len(v.actions) {
			break
		}
		seg := layout.Segment{Text: v.actions[idx].Title}
		if idx == v.cursor {
			seg.Style.Attr |= layout.AttrReverse
		}
		w.Println(i, seg)
	}
}

// ScrollState implements layout.Scrollable.
func (v *View) ScrollState() layout.ScrollState {
	return layout.ScrollState{Top: v.scrollTop, Viewport: v.lastRows, Total: len(v.actions)}
}

// ScrollTo implements layout.ScrollTarget, same clamped-assignment shape
// as actionpopup.View's.
func (v *View) ScrollTo(top int) {
	maxTop := len(v.actions) - v.lastRows
	if maxTop < 0 {
		maxTop = 0
	}
	if top < 0 {
		top = 0
	}
	if top > maxTop {
		top = maxTop
	}
	v.scrollTop = top
}

// HandleKey always reports the key consumed: a modal should never leak
// input through to whatever is behind it. Unlike actionpopup.View, there's
// no query box to route unclaimed keys into, so an unrecognized key is
// simply swallowed.
func (v *View) HandleKey(k layout.Key) bool {
	if k.EventType == layout.EventRelease {
		return true
	}

	switch v.keymap[k.String()] {
	case "close":
		if v.OnClose != nil {
			v.OnClose()
		}
	case "move_down":
		if v.cursor < len(v.actions)-1 {
			v.cursor++
		}
	case "move_up":
		if v.cursor > 0 {
			v.cursor--
		}
	case "execute":
		if len(v.actions) > 0 && v.OnExecute != nil {
			v.OnExecute(v.actions[v.cursor])
		}
	}
	return true
}
