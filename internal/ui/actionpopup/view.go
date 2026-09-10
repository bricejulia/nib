// Package actionpopup is a "Find Action" popup meant to be shown as a
// modal overlay (see ui.App.ShowOverlay/CloseOverlay): type to filter a
// list of named commands (drawn from both the global scope and the
// focused editor pane's), Up/Down to move the selection, Enter to run it,
// Esc to close without running anything. Unlike internal/ui/help (a
// read-only reference), selecting a row here actually dispatches the
// command — searching and executing, JetBrains' own "Find Action" popup.
package actionpopup

import (
	"fmt"
	"strings"

	"github.com/bricejulia/nib/internal/config"
	"github.com/bricejulia/nib/internal/layout"
	"github.com/bricejulia/nib/internal/ui/textfield"
)

// DefaultKeybinds are the action popup's built-in keybindings, overridable
// via the user config's "actionpopup" scope (see internal/config).
// Deliberately narrow, the same trade finder.DefaultKeybinds/
// help.DefaultKeybinds make for their own search query: any trigger not
// listed here falls through to being typed into the query box instead.
var DefaultKeybinds = config.Defaults{
	{Trigger: "Esc", Action: "close"},
	{Trigger: "Down", Action: "move_down"},
	{Trigger: "Up", Action: "move_up"},
	{Trigger: "Enter", Action: "execute"},
}

// queryLabel prefixes the always-typeable query box pinned above the
// scrollable result list — row 0, the same reserved-header-row convention
// help's and finder's own prompt rows use.
const queryLabel = "Run: "

// descColumn is how wide the description column is padded to before a
// row's resolved keybind hint (if any), so hints line up in a ragged
// right-hand column the same way help.buildLines pads its Key column.
const descColumn = 46

// View is the action popup's content: a query box plus a selectable,
// live-filtered list of commands.
type View struct {
	query textfield.TextField

	filtered []entry
	// keybind maps "scope:id" -> the currently resolved trigger for that
	// command, so each row can show its live shortcut. Rebuilt by Open on
	// every show (not just once, unlike help.View's static content) since
	// it can go stale after a live config reload.
	keybind map[string]string

	cursor              int
	scrollTop, lastRows int

	// OnClose is called when Esc is pressed, dismissing the overlay.
	OnClose func()
	// OnExecute is called with the selected row's id when Enter is
	// pressed on a non-empty list. This package only knows how to search
	// and select — actually dispatching the id (to cmd/nib/main.go's
	// global actions map, or to the focused editor.View.ExecuteAction) is
	// the caller's job, mirroring how internal/ui/help keeps display
	// separate from the real keymaps it describes.
	OnExecute func(id string)

	keymap map[string]string
}

// New creates an action popup with no keybind hints yet — call Open before
// showing it so hints and the result list start fresh.
func New() *View {
	v := &View{keymap: DefaultKeybinds.Resolve(nil)}
	v.refilter()
	return v
}

// SetKeymap merges the user config's "actionpopup" scope overrides on top
// of DefaultKeybinds, replacing the pane's active keymap.
func (v *View) SetKeymap(overrides map[string]string) {
	v.keymap = DefaultKeybinds.Resolve(overrides)
}

func (v *View) Title() string { return "Actions" }

// Open resets the query and selection to a blank slate, and rebuilds the
// id->trigger hint lookup from the currently resolved global and editor
// keymaps (first trigger found for a given action wins) — called every
// time the popup is shown, like finder.View.Open, rather than only once at
// construction like help.View: the shortcuts shown next to each command
// can change after a live config reload.
func (v *View) Open(global, editor map[string]string) {
	v.keybind = make(map[string]string, len(global)+len(editor))
	for trigger, action := range global {
		if _, ok := v.keybind["global:"+action]; !ok {
			v.keybind["global:"+action] = trigger
		}
	}
	for trigger, action := range editor {
		if _, ok := v.keybind["editor:"+action]; !ok {
			v.keybind["editor:"+action] = trigger
		}
	}
	v.query = textfield.TextField{}
	v.cursor = 0
	v.refilter()
}

// refilter rebuilds v.filtered from the current query and resets the
// selection to the top — run synchronously on every keystroke, exactly
// like help.View.refilter, since entries is a small in-memory static list.
func (v *View) refilter() {
	v.filtered = filterEntries(v.query.String())
	v.scrollTop = 0
	if v.cursor >= len(v.filtered) {
		v.cursor = len(v.filtered) - 1
	}
	if v.cursor < 0 {
		v.cursor = 0
	}
}

// filterEntries keeps only the entries whose description contains query
// (case-insensitive); a blank query keeps everything, in entries' declared
// order.
func filterEntries(query string) []entry {
	q := strings.ToLower(strings.TrimSpace(query))
	if q == "" {
		return entries
	}
	var out []entry
	for _, e := range entries {
		if strings.Contains(strings.ToLower(e.Desc), q) {
			out = append(out, e)
		}
	}
	return out
}

// rowText renders one entry as its description, padded out to descColumn
// and followed by its resolved keybind hint when it has one — the same
// "%-Ns %s" two-column convention help.buildLines uses, just with the
// key/description roles swapped since here the description is the primary
// searchable text.
func (v *View) rowText(e entry) string {
	hint := v.keybind[e.Scope+":"+e.ID]
	if hint == "" {
		return e.Desc
	}
	return fmt.Sprintf("%-*s %s", descColumn, e.Desc, hint)
}

func (v *View) Render(w layout.Window) {
	_, rows := w.Size()
	w.Clear()

	w.Println(0, layout.Segment{Text: queryLabel + v.query.String()})

	listRows := rows - 1
	if listRows < 0 {
		listRows = 0
	}
	v.lastRows = listRows

	if len(v.filtered) == 0 {
		if v.query.Len() > 0 && listRows > 0 {
			w.Println(1, layout.Segment{Text: "no matching commands", Style: layout.Style{Attr: layout.AttrDim}})
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
		seg := layout.Segment{Text: v.rowText(v.filtered[idx])}
		if idx == v.cursor {
			seg.Style.Attr |= layout.AttrReverse
		}
		w.Println(1+i, seg)
	}
}

// ScrollState implements layout.Scrollable.
func (v *View) ScrollState() layout.ScrollState {
	return layout.ScrollState{Top: v.scrollTop, Viewport: v.lastRows, Total: len(v.filtered)}
}

// ScrollTo implements layout.ScrollTarget, exactly like finder.View's own:
// a direct assignment, clamped to the valid range — Render's own clamp
// (keeping the selected row visible) still wins on the next frame if the
// cursor sits outside whatever range this picks.
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

// CursorPosition implements layout.CursorProvider: the terminal's native
// cursor sits at the end of the typed query on the query row (row 0),
// always visible — the query box is always focused, the same way
// finder.View's and help.View's own query fields always are.
func (v *View) CursorPosition() (int, int, bool) {
	return len(queryLabel) + v.query.Caret(), 0, true
}

// HandleKey always reports the key consumed: a modal should never leak
// input through to whatever is behind it.
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
		if v.cursor < len(v.filtered)-1 {
			v.cursor++
		}
	case "move_up":
		if v.cursor > 0 {
			v.cursor--
		}
	case "execute":
		if len(v.filtered) > 0 && v.OnExecute != nil {
			v.OnExecute(v.filtered[v.cursor].ID)
		}
	default:
		// Everything else — printable text, Backspace, Left/Right — is
		// unclaimed by DefaultKeybinds, so it edits the query box directly
		// via textfield.TextField.HandleKey, the same "reserve only the
		// named actions this pane needs, let everything else type" trade
		// help.View.HandleKey makes for its own query.
		if v.query.HandleKey(k) {
			v.refilter()
		}
	}
	return true
}
