// Package help is a read-only pane listing every keybinding in the app
// plus the current version — meant to be shown as a modal overlay (see
// ui.App.ShowOverlay/CloseOverlay), the same pattern internal/ui/debug
// uses for the debug log.
package help

import (
	"fmt"
	"strings"

	"github.com/bricejulia/nib/internal/config"
	"github.com/bricejulia/nib/internal/layout"
	"github.com/bricejulia/nib/internal/ui/textfield"
)

// DefaultKeybinds are the help overlay's built-in keybindings,
// overridable via the user config's "help" scope (see internal/config).
// Deliberately narrow, the same trade finder.DefaultKeybinds makes for its
// own search query: any trigger not listed here falls through to being
// typed into the search box instead (see HandleKey) — that's what makes
// this list searchable at all.
var DefaultKeybinds = config.Defaults{
	{Trigger: "Esc", Action: "close"},
	{Trigger: "Down", Action: "scroll_down"},
	{Trigger: "Up", Action: "scroll_up"},
	{Trigger: "PageDown", Action: "page_down"},
	{Trigger: "PageUp", Action: "page_up"},
	{Trigger: "Home", Action: "home"},
	{Trigger: "End", Action: "end"},
}

// searchLabel prefixes the always-typeable search box pinned above the
// scrollable list — row 0, the same reserved-header-row convention the
// editor's tab bar and finder's own prompt row use.
const searchLabel = "Search: "

// View renders the keybinding reference in sections, filtered live by a
// typed query, with a scrollable viewport for terminals too small to show
// it all at once.
type View struct {
	version string
	query   textfield.TextField

	lines    [][]layout.Segment // sections filtered to the current query
	topLine  int
	lastRows int // last Render's visible content-row count (excludes the search bar), for PageUp/PageDown sizing

	// OnClose is called when Esc is pressed, dismissing the overlay.
	OnClose func()

	keymap map[string]string
}

// New creates a help view showing version alongside every keybinding.
func New(version string) *View {
	v := &View{version: version, keymap: DefaultKeybinds.Resolve(nil)}
	v.refilter()
	return v
}

// SetKeymap merges the user config's "help" scope overrides on top of
// DefaultKeybinds, replacing the pane's active keymap.
func (v *View) SetKeymap(overrides map[string]string) {
	v.keymap = DefaultKeybinds.Resolve(overrides)
}

func (v *View) Title() string { return "Help" }

// refilter rebuilds v.lines from the current query and scrolls back to the
// top — run synchronously on every keystroke, since this is an in-memory
// static list rather than the async, debounced git-grep search
// finder.View.refilterContent runs for its own query.
func (v *View) refilter() {
	v.lines = buildLines(v.version, v.query.String())
	v.topLine = 0
}

// buildLines flattens the version header and every section into one styled
// segment slice per displayed row. With a blank query every section shows
// in full, unfiltered — the original, always-shown content. With a query,
// each binding is kept only if its key or description contains it
// (case-insensitive); a section with no surviving bindings is dropped
// entirely (heading included), and a query matching nothing at all gets an
// explicit "no matching keybindings" line rather than silently rendering
// empty.
func buildLines(version, query string) [][]layout.Segment {
	lines := [][]layout.Segment{
		{{Text: "nib " + version}},
	}

	q := strings.ToLower(strings.TrimSpace(query))
	anyMatch := false
	for _, sec := range sections {
		var rows [][]layout.Segment
		for _, b := range sec.Bindings {
			if q != "" && !strings.Contains(strings.ToLower(b.Key+" "+b.Desc), q) {
				continue
			}
			text := fmt.Sprintf("  %-24s %s", b.Key, b.Desc)
			rows = append(rows, []layout.Segment{{Text: text}})
		}
		if len(rows) == 0 {
			continue
		}
		anyMatch = true
		lines = append(lines, nil) // blank separator
		lines = append(lines, []layout.Segment{{Text: sec.Title, Style: layout.Style{Attr: layout.AttrBold}}})
		lines = append(lines, rows...)
	}

	if q != "" && !anyMatch {
		lines = append(lines, nil, []layout.Segment{
			{Text: "no matching keybindings", Style: layout.Style{Attr: layout.AttrDim}},
		})
	}
	return lines
}

func (v *View) Render(w layout.Window) {
	_, rows := w.Size()
	w.Clear()

	w.Println(0, layout.Segment{Text: searchLabel + v.query.String()})

	listRows := rows - 1
	if listRows < 0 {
		listRows = 0
	}
	v.lastRows = listRows

	maxTop := len(v.lines) - listRows
	if maxTop < 0 {
		maxTop = 0
	}
	if v.topLine > maxTop {
		v.topLine = maxTop
	}
	if v.topLine < 0 {
		v.topLine = 0
	}

	for i := 0; i < listRows; i++ {
		idx := v.topLine + i
		if idx >= len(v.lines) {
			break
		}
		w.Println(1+i, v.lines[idx]...)
	}
}

// ScrollState implements layout.Scrollable.
func (v *View) ScrollState() layout.ScrollState {
	return layout.ScrollState{Top: v.topLine, Viewport: v.lastRows, Total: len(v.lines)}
}

// ScrollTo implements layout.ScrollTarget — a direct, no-followup
// assignment, exactly like diffview's: v.topLine is only ever clamped in
// Render, never re-derived from anything else.
func (v *View) ScrollTo(top int) {
	maxTop := len(v.lines) - v.lastRows
	if maxTop < 0 {
		maxTop = 0
	}
	if top < 0 {
		top = 0
	}
	if top > maxTop {
		top = maxTop
	}
	v.topLine = top
}

// CursorPosition implements layout.CursorProvider: the terminal's native
// cursor sits at the end of the typed query on the search bar (row 0),
// always visible — the search box is always focused, the same way
// finder.View's own query field always is.
func (v *View) CursorPosition() (int, int, bool) {
	return len(searchLabel) + v.query.Caret(), 0, true
}

// HandleKey always reports the key consumed: a modal should never leak
// input through to whatever is behind it.
func (v *View) HandleKey(k layout.Key) bool {
	if k.EventType == layout.EventRelease {
		return true
	}

	page := v.lastRows
	if page <= 0 {
		page = 1
	}

	switch v.keymap[k.String()] {
	case "close":
		// Reset the search so the next open starts fresh, the same way
		// finder.View.Open blanks its own query on every show — this view
		// has no such Open hook (see New's doc comment: its content is
		// otherwise static for the process lifetime), so closing is the
		// only natural place to do it.
		v.query = textfield.TextField{}
		v.refilter()
		if v.OnClose != nil {
			v.OnClose()
		}
	case "scroll_down":
		v.topLine++
	case "scroll_up":
		v.topLine--
	case "page_down":
		v.topLine += page
	case "page_up":
		v.topLine -= page
	case "home":
		v.topLine = 0
	case "end":
		v.topLine = len(v.lines)
	default:
		// Everything else — printable text, Backspace, Left/Right — is
		// unclaimed by DefaultKeybinds, so it edits the search box directly
		// via textfield.TextField.HandleKey: the same "reserve only the
		// named actions this pane needs, let everything else type" trade
		// finder.View.HandleKey makes for its own query. Home/End stay
		// reserved for jumping the list (above) rather than the caret, so
		// there's no new conflict with the keymap this pane already had.
		if v.query.HandleKey(k) {
			v.refilter()
		}
	}
	if v.topLine < 0 {
		v.topLine = 0
	}
	return true
}
