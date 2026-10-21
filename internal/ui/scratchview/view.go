// Package scratchview is the scratch-file overlay (see internal/scratch),
// meant to be shown as a modal via ui.App.ShowOverlay/CloseOverlay. It has
// two modes: a one-line name prompt for creating a scratch file, and a
// live-filtered list of the project's existing ones to open or delete.
// Like actionpopup, it only searches and selects — creating, opening and
// deleting files is the caller's job (see cmd/nib/main.go).
package scratchview

import (
	"fmt"
	"strings"
	"time"

	"github.com/bricejulia/nib/internal/config"
	"github.com/bricejulia/nib/internal/layout"
	"github.com/bricejulia/nib/internal/scratch"
	"github.com/bricejulia/nib/internal/ui/textfield"
)

// Scope is the overlay's actions and their default keys, overridable via
// the user config's "scratch" scope (see internal/config). Narrow on
// purpose, like actionpopup.Scope: anything not listed here is typed into
// the query or name box. Ctrl+d doesn't open the debug log here — an
// overlay gets every key before the global keymap does.
var Scope = config.Scope{Name: "scratch", Title: "Scratch Files", Actions: config.Actions{
	{ID: "close", Keys: []string{"Esc"}, Desc: "Close"},
	{ID: "move_down", Keys: []string{"Down"}, Desc: "Move selection down"},
	{ID: "move_up", Keys: []string{"Up"}, Desc: "Move selection up"},
	{ID: "open", Keys: []string{"Enter"}, Desc: "Open the selected scratch file (or create the named one)"},
	{ID: "new", Keys: []string{"Ctrl+n"}, Desc: "Create a new scratch file named after the query"},
	{ID: "delete", Keys: []string{"Ctrl+d"}, Desc: "Delete the selected scratch file (confirm y/N)"},
}}

const (
	queryLabel = "Scratch: "
	nameLabel  = "New scratch: "
	// nameColumn is how wide a row's file name is padded to before its
	// age, so ages line up the way actionpopup's key hints do.
	nameColumn = 40
)

type mode int

const (
	modeList mode = iota
	modeCreate
)

// View is the scratch overlay's content.
type View struct {
	mode  mode
	field textfield.TextField // the list's query, or the new file's name

	entries  []scratch.Entry
	filtered []scratch.Entry
	cursor   int
	// confirming is the entry awaiting a y/N answer to "delete?", or nil.
	confirming *scratch.Entry

	scrollTop, lastRows int
	errText             string
	now                 func() time.Time

	// OnClose is called on Esc.
	OnClose func()
	// OnCreate is called with the typed name (possibly blank) when Enter
	// is pressed in the name prompt.
	OnCreate func(name string)
	// OnOpen is called with the selected entry's path on Enter.
	OnOpen func(path string)
	// OnDelete is called with the selected entry's path once its delete
	// is confirmed; the caller refreshes the list via ShowList.
	OnDelete func(path string)

	keymap map[string]string
}

// New creates an unshown overlay; call ShowCreate or ShowList first.
func New() *View {
	return &View{keymap: Scope.Actions.Resolve(nil), now: time.Now}
}

// SetKeymap merges the user config's "scratch" scope overrides on top of
// Scope.
func (v *View) SetKeymap(overrides map[string]string) {
	v.keymap = Scope.Actions.Resolve(overrides)
}

func (v *View) Title() string {
	if v.mode == modeCreate {
		return "New Scratch File"
	}
	return "Scratch Files"
}

// ShowCreate switches to the name prompt, pre-filled with name.
func (v *View) ShowCreate(name string) {
	v.mode = modeCreate
	v.field = textfield.New(name)
	v.errText = ""
	v.confirming = nil
}

// ShowList switches to the list of entries (expected newest first, as
// scratch.List returns them), keeping the current query when already in
// list mode so a delete doesn't reset the filter.
func (v *View) ShowList(entries []scratch.Entry) {
	if v.mode != modeList {
		v.field = textfield.TextField{}
	}
	v.mode = modeList
	v.entries = entries
	v.errText = ""
	v.confirming = nil
	v.refilter()
}

// Reset clears the list's query, for a fresh opening.
func (v *View) Reset() {
	v.field = textfield.TextField{}
	v.cursor = 0
	v.mode = modeList
}

// SetError shows err under the input row until the next keypress — e.g.
// an invalid name passed to OnCreate.
func (v *View) SetError(err error) {
	v.errText = ""
	if err != nil {
		v.errText = err.Error()
	}
}

func (v *View) refilter() {
	q := strings.ToLower(strings.TrimSpace(v.field.String()))
	v.filtered = v.entries
	if q != "" {
		v.filtered = nil
		for _, e := range v.entries {
			if strings.Contains(strings.ToLower(e.Name), q) {
				v.filtered = append(v.filtered, e)
			}
		}
	}
	if v.cursor >= len(v.filtered) {
		v.cursor = len(v.filtered) - 1
	}
	if v.cursor < 0 {
		v.cursor = 0
	}
}

func (v *View) label() string {
	if v.mode == modeCreate {
		return nameLabel
	}
	return queryLabel
}

func (v *View) Render(w layout.Window) {
	_, rows := w.Size()
	w.Clear()
	w.Println(0, layout.Segment{Text: v.label() + v.field.String()})

	dim := layout.Style{Attr: layout.AttrDim}
	row := 1
	if v.errText != "" {
		w.Println(row, layout.Segment{Text: v.errText, Style: dim})
		row++
	}
	if v.mode == modeCreate {
		if v.errText == "" {
			w.Println(row, layout.Segment{Text: "Enter to create (blank for scratch-<n>.txt) · Esc to cancel", Style: dim})
		}
		return
	}
	if v.confirming != nil {
		w.Println(row, layout.Segment{Text: fmt.Sprintf("Delete %s? [y/N]", v.confirming.Name), Style: layout.Style{Attr: layout.AttrBold}})
		row++
	}

	listRows := rows - row
	if listRows < 0 {
		listRows = 0
	}
	v.lastRows = listRows
	if len(v.filtered) == 0 {
		if listRows > 0 {
			msg := "No scratch files — Ctrl+N to create one"
			if v.field.Len() > 0 {
				msg = "No matching scratch files — Ctrl+N to create one with this name"
			}
			w.Println(row, layout.Segment{Text: msg, Style: dim})
		}
		return
	}

	if v.cursor < v.scrollTop {
		v.scrollTop = v.cursor
	}
	if v.cursor >= v.scrollTop+listRows {
		v.scrollTop = v.cursor - listRows + 1
	}
	if v.scrollTop < 0 {
		v.scrollTop = 0
	}
	for i := 0; i < listRows; i++ {
		idx := v.scrollTop + i
		if idx >= len(v.filtered) {
			break
		}
		e := v.filtered[idx]
		seg := layout.Segment{Text: fmt.Sprintf("%-*s %s", nameColumn, e.Name, age(v.now().Sub(e.ModTime)))}
		if idx == v.cursor {
			seg.Style.Attr |= layout.AttrReverse
		}
		w.Println(row+i, seg)
	}
}

// age renders d as a short "how long ago" label.
func age(d time.Duration) string {
	switch {
	case d < time.Minute:
		return "just now"
	case d < time.Hour:
		return fmt.Sprintf("%dm ago", int(d/time.Minute))
	case d < 24*time.Hour:
		return fmt.Sprintf("%dh ago", int(d/time.Hour))
	default:
		return fmt.Sprintf("%dd ago", int(d/(24*time.Hour)))
	}
}

// ScrollState implements layout.Scrollable.
func (v *View) ScrollState() layout.ScrollState {
	return layout.ScrollState{Top: v.scrollTop, Viewport: v.lastRows, Total: len(v.filtered)}
}

// ScrollTo implements layout.ScrollTarget, the same clamp as
// actionpopup.View.ScrollTo.
func (v *View) ScrollTo(top int) {
	maxTop := len(v.filtered) - v.lastRows
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

// CursorPosition implements layout.CursorProvider: the caret sits in the
// input row, which is always focused.
func (v *View) CursorPosition() (int, int, bool) {
	return len(v.label()) + v.field.Caret(), 0, true
}

// HandleKey always reports the key consumed: a modal should never leak
// input through to whatever is behind it.
func (v *View) HandleKey(k layout.Key) bool {
	if k.EventType == layout.EventRelease {
		return true
	}
	v.errText = ""

	// Anything other than "y" cancels, the same guard filetree's own
	// delete prompt uses.
	if v.confirming != nil {
		e := *v.confirming
		v.confirming = nil
		if k.Named == "" && k.Mods == 0 && (k.Text == "y" || k.Text == "Y") && v.OnDelete != nil {
			v.OnDelete(e.Path)
		}
		return true
	}

	action := v.keymap[k.String()]
	if v.mode == modeCreate {
		switch action {
		case "close":
			if v.OnClose != nil {
				v.OnClose()
			}
		case "open":
			if v.OnCreate != nil {
				v.OnCreate(v.field.String())
			}
		default:
			v.field.HandleKey(k)
		}
		return true
	}

	switch action {
	case "close":
		if v.OnClose != nil {
			v.OnClose()
		}
	case "move_down":
		if v.cursor < len(v.filtered)-1 {
			v.cursor++
		}
	case "move_up":
		if v.cursor > 0 {
			v.cursor--
		}
	case "open":
		if len(v.filtered) > 0 {
			if v.OnOpen != nil {
				v.OnOpen(v.filtered[v.cursor].Path)
			}
		} else if v.field.Len() > 0 && v.OnCreate != nil {
			// Nothing matches what was typed: Enter creates it, so the
			// list doubles as a "open or create" box.
			v.OnCreate(v.field.String())
		}
	case "new":
		v.ShowCreate(strings.TrimSpace(v.field.String()))
	case "delete":
		if len(v.filtered) > 0 {
			e := v.filtered[v.cursor]
			v.confirming = &e
		}
	default:
		if v.field.HandleKey(k) {
			v.refilter()
		}
	}
	return true
}
