// Package refview is a "Find References" results popup meant to be shown
// as a modal overlay (see ui.App.ShowOverlay/CloseOverlay): a fixed,
// already-resolved list of lsp.Locations, Up/Down to move the selection,
// Enter to jump to it, Esc to close.
//
// Unlike internal/ui/finder, there's no query to filter by: by the time
// this is shown, the language server has already done the searching. So
// this is deliberately its own small package, modeled closely on
// internal/ui/actionpopup's list mechanics (scroll/select/render) minus
// the query box, rather than an extension of finder.View — finder's
// machinery (fuzzy scoring, the query textfield, the git-grep subprocess)
// has nothing to do with rendering an already-resolved list.
package refview

import (
	"bufio"
	"fmt"
	"os"
	"path/filepath"
	"strings"

	"github.com/bricejulia/nib/internal/config"
	"github.com/bricejulia/nib/internal/layout"
	"github.com/bricejulia/nib/internal/lsp"
)

// DefaultKeybinds are this popup's built-in keybindings, overridable via
// the user config's "references" scope (see internal/config).
var DefaultKeybinds = config.Defaults{
	{Trigger: "Esc", Action: "close"},
	{Trigger: "Down", Action: "move_down"},
	{Trigger: "Up", Action: "move_up"},
	{Trigger: "Enter", Action: "select"},
}

// View is the references popup's content: a fixed header row plus a
// selectable, scrollable list of locations.
type View struct {
	root string
	refs []lsp.Location

	cursor              int
	scrollTop, lastRows int

	// OnSelect is called with the chosen location when Enter is pressed on
	// a non-empty list. Jumping there (opening the file, moving focus) is
	// the caller's job — this package only knows how to list and select,
	// the same split actionpopup.View makes between searching/selecting
	// and actually dispatching.
	OnSelect func(loc lsp.Location)
	// OnClose is called when Esc is pressed, dismissing the overlay.
	OnClose func()

	keymap map[string]string
}

// New creates an empty references popup; call Open before showing it.
func New(root string) *View {
	return &View{root: root, keymap: DefaultKeybinds.Resolve(nil)}
}

// SetKeymap merges the user config's "references" scope overrides on top
// of DefaultKeybinds, replacing the popup's active keymap.
func (v *View) SetKeymap(overrides map[string]string) {
	v.keymap = DefaultKeybinds.Resolve(overrides)
}

func (v *View) Title() string { return "References" }

// Open resets the list and selection to refs, resolving each location's
// path relative to root for display and reading its line's own text so
// results are scannable without opening each one.
func (v *View) Open(refs []lsp.Location) {
	v.refs = refs
	v.cursor = 0
	v.scrollTop = 0
}

// headerText summarizes the result count as the popup's fixed row 0, the
// same reserved-header-row convention actionpopup's query row occupies.
func (v *View) headerText() string {
	if len(v.refs) == 1 {
		return "1 reference"
	}
	return fmt.Sprintf("%d references", len(v.refs))
}

// rowText renders one location as "relpath:line: <line text>" — the
// containing line's own text, trimmed, so a result is useful to scan
// without jumping into it. A location whose file can't be read (since
// deleted, permissions) falls back to just "relpath:line".
func (v *View) rowText(loc lsp.Location) string {
	path := loc.Path()
	rel := path
	if path != "" {
		if r, err := filepath.Rel(v.root, path); err == nil {
			rel = r
		}
	}
	line := loc.Range.Start.Line + 1
	text := strings.TrimSpace(readLine(path, loc.Range.Start.Line))
	if text == "" {
		return fmt.Sprintf("%s:%d", rel, line)
	}
	return fmt.Sprintf("%s:%d: %s", rel, line, text)
}

// readLine returns line n (0-based) of path, or "" if the file can't be
// read or has fewer than n+1 lines. A references popup showing a symbol
// used across many files can't afford to load and cache every one of
// them, so this reads and discards the file fresh per row — acceptable
// because Open only runs once per keypress, not per render.
func readLine(path string, n int) string {
	if path == "" {
		return ""
	}
	f, err := os.Open(path)
	if err != nil {
		return ""
	}
	defer func() { _ = f.Close() }()

	scanner := bufio.NewScanner(f)
	for i := 0; scanner.Scan(); i++ {
		if i == n {
			return scanner.Text()
		}
	}
	_ = scanner.Err() // best-effort read; a scan error just yields "" like a missing line
	return ""
}

func (v *View) Render(w layout.Window) {
	_, rows := w.Size()
	w.Clear()

	w.Println(0, layout.Segment{Text: v.headerText(), Style: layout.Style{Attr: layout.AttrDim}})

	listRows := rows - 1
	if listRows < 0 {
		listRows = 0
	}
	v.lastRows = listRows

	if len(v.refs) == 0 {
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
		if idx >= len(v.refs) {
			break
		}
		seg := layout.Segment{Text: v.rowText(v.refs[idx])}
		if idx == v.cursor {
			seg.Style.Attr |= layout.AttrReverse
		}
		w.Println(1+i, seg)
	}
}

// ScrollState implements layout.Scrollable.
func (v *View) ScrollState() layout.ScrollState {
	return layout.ScrollState{Top: v.scrollTop, Viewport: v.lastRows, Total: len(v.refs)}
}

// ScrollTo implements layout.ScrollTarget, same clamped-assignment shape
// as actionpopup.View's.
func (v *View) ScrollTo(top int) {
	maxTop := len(v.refs) - v.lastRows
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
		if v.cursor < len(v.refs)-1 {
			v.cursor++
		}
	case "move_up":
		if v.cursor > 0 {
			v.cursor--
		}
	case "select":
		if len(v.refs) > 0 && v.OnSelect != nil {
			v.OnSelect(v.refs[v.cursor])
		}
	}
	return true
}
