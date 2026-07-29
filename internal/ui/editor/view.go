package editor

import (
	"fmt"
	"path/filepath"
	"strconv"
	"strings"

	"github.com/bricejulia/kiwi/internal/config"
	"github.com/bricejulia/kiwi/internal/debuglog"
	"github.com/bricejulia/kiwi/internal/layout"
	"github.com/bricejulia/kiwi/internal/textwidth"
	"github.com/bricejulia/kiwi/internal/ui/gitstyle"
	"github.com/bricejulia/kiwi/internal/vcs/gitstatus"
)

// DefaultKeybinds are the editor pane's built-in keybindings, overridable
// via the user config's "editor" scope (see internal/config).
var DefaultKeybinds = config.Defaults{
	{Trigger: "]", Action: "next_tab"},
	{Trigger: "[", Action: "prev_tab"},
	{Trigger: "x", Action: "delete_char_forward"},
	{Trigger: "X", Action: "delete_char_backward"},
	{Trigger: "Down", Action: "move_down"},
	{Trigger: "j", Action: "move_down"},
	{Trigger: "Up", Action: "move_up"},
	{Trigger: "k", Action: "move_up"},
	{Trigger: "Left", Action: "move_left"},
	{Trigger: "h", Action: "move_left"},
	{Trigger: "Right", Action: "move_right"},
	{Trigger: "l", Action: "move_right"},
	{Trigger: "PageDown", Action: "page_down"},
	{Trigger: "PageUp", Action: "page_up"},
	{Trigger: "Home", Action: "line_start"},
	{Trigger: "End", Action: "line_end"},
	{Trigger: "g", Action: "first_line"},
	{Trigger: "G", Action: "last_line"},
	{Trigger: "i", Action: "insert_mode"},
	{Trigger: "a", Action: "append_mode"},
	{Trigger: "o", Action: "append_new_line_mode"},
	{Trigger: "Esc", Action: "normal_mode"},
	{Trigger: "Enter", Action: "insert_newline"},
	{Trigger: "Backspace", Action: "insert_backspace"},
	{Trigger: "Tab", Action: "insert_tab"},
	{Trigger: "Ctrl+s", Action: "save"},
	{Trigger: "u", Action: "undo"},
	{Trigger: "Ctrl+r", Action: "redo"},
	{Trigger: ":", Action: "command_mode"},
	{Trigger: "Ctrl+g", Action: "go_to_parent"},
	{Trigger: "Ctrl+]", Action: "go_to_definition"},
	// Not Ctrl+[ (a more obvious visual pairing with Ctrl+]): on legacy
	// terminal protocols Ctrl+[ sends the exact same byte as Esc, so it's
	// indistinguishable from a bare Esc keypress and would never fire —
	// the same class of terminal ambiguity the double-shift detector
	// elsewhere already has to work around. Ctrl+b ("back") has no such
	// collision.
	{Trigger: "Ctrl+b", Action: "jump_back"},
	{Trigger: "Ctrl+f", Action: "find_references"},
	{Trigger: "Ctrl+Space", Action: "trigger_autocomplete"},
}

// editMode is the editor pane's modal-editing state: Normal (the pane's
// original, navigation-only behavior), Insert (typed text goes into the
// buffer), or Command (a minimal, digit-only ":<line>" prompt — see
// handleCommandKey). Mirrors vim's Normal/Insert/command-line split,
// matching the hjkl/g/G navigation scheme DefaultKeybinds already uses.
type editMode int

const (
	modeNormal editMode = iota
	modeInsert
	modeCommand
)

// maxUndoEntries bounds each tab's undo stack, the same way
// debuglog.maxEntries bounds its ring buffer: oldest entry dropped once
// full, so a long editing session's undo history can't grow unbounded.
// Redo's stack is naturally bounded by this too, since undo/redo only ever
// move one entry between the two stacks.
const maxUndoEntries = 100

// undoEntry is one snapshot of a tab's editable state: the whole-buffer
// contents an Insert session started from (or the state just before an
// undo/redo), plus enough cursor state to restore it exactly. Taking a
// whole-buffer snapshot once per Insert session (not per keystroke) is
// cheap relative to the per-keystroke re-highlight this pane already does
// (see reHighlight) — simple and correct, if not the most memory-frugal
// possible approach. Deliberately does not capture Dirty — see
// Buffer.Restore for why that has to be recomputed against Buffer.saved
// instead of carried through a snapshot.
type undoEntry struct {
	lines     []string
	cursorLn  int
	cursorCol int
}

// tab holds one open file's buffer plus its own scroll/cursor state, so
// switching tabs restores exactly where you left off. path is recorded
// separately from buf.Path so the tab bar can still show which file failed
// to load when buf is nil.
//
// cursorCol is a rune index into the CURRENT line after tab expansion
// (see currentLineRunes) — not a display column. leftCol is the
// horizontal scroll offset, in display columns; unlike cursorCol it is
// never set directly by a key handler, only derived in Render (see
// renderBody) to keep the cursor's display column in view, exactly the
// way topLine is derived from cursorLn. This is what naturally bounds how
// far the pane can scroll horizontally: since cursorCol itself is clamped
// to the line's length, there is nothing "past the end" to scroll to.
type tab struct {
	path      string
	buf       *Buffer
	err       error
	topLine   int
	leftCol   int
	cursorLn  int
	cursorCol int

	// insertSnapshot is the pending undo entry for the Insert session
	// currently in progress in THIS pane (nil outside of Insert mode),
	// taken by enterInsertMode and either committed (onto buf.undoStack)
	// or discarded by exitInsertMode. Deliberately per-tab rather than on
	// Buffer, unlike the committed undoStack/redoStack: it's the
	// not-yet-committed half of an edit, scoped to whichever single pane
	// is mid-session — cmd/kiwi/main.go's focus-change wiring (see
	// View.ExitEditingModes) guarantees at most one pane is ever
	// mid-session on a given buffer at a time, which is what makes this
	// split safe.
	insertSnapshot *undoEntry

	// lineStatus is this tab's per-line git diff gutter markers (see
	// gitstatus.FileHunks), keyed by 0-based index into buf.Lines. Set by
	// ApplyLineStatus — the View itself never shells out to git, matching
	// how file-level status flows in from the caller (see
	// filetree.View.ApplyStatus) rather than being computed here. nil
	// (the zero value, before the first ApplyLineStatus call, or whenever
	// path has no git repo) means "draw no markers", same as an empty map.
	lineStatus map[int]gitstatus.LineStatus

	// jumpStack holds cursor positions saved by goToParent/goToDefinition
	// (see navigate.go), popped by jumpBack (Ctrl+b) — per-pane navigation
	// history, not buffer content, so it stays on tab like cursorLn/
	// cursorCol rather than moving to Buffer alongside undo/redo.
	jumpStack []jumpLocation
}

// View is the editor pane: zero or more open tabs, each with its own
// scroll/cursor position and modal (Normal/Insert) editing state — see
// editMode. A tab's Buffer is not necessarily private to it: opening the
// same path from more than one View sharing a BufferStore (see
// SetBufferStore, e.g. split panes in cmd/kiwi/main.go) gives both tabs
// the SAME Buffer, so edits/dirty state/undo are shared exactly like
// vim's buffers-vs-windows model. The pane shows the terminal's real
// cursor (see CursorPosition) at the current position.
type View struct {
	tabs     []*tab
	active   int // index into tabs; meaningless when len(tabs) == 0
	tabWidth int

	lastWidth, lastHeight int

	keymap map[string]string

	// mode is Normal unless the active tab is being typed into, or a
	// ":<line>" prompt is open; see editMode and HandleKey.
	mode editMode

	// commandBuf holds the characters typed so far in Command mode (see
	// handleCommandKey) — a single command line shared by the pane, like
	// vim's, not per-tab.
	commandBuf string

	// OnAllTabsClosed, if set, is called whenever CloseTab/CloseAllTabs
	// (directly, or via the ":q"/":qa" family — see closeActiveTab/
	// closeAllTabsCmd) leaves this pane with zero open tabs. Set by
	// cmd/kiwi/main.go to refocus the file tree — same plain-callback
	// pattern as finder.View.OnClose/debug.View.OnClose.
	OnAllTabsClosed func()

	// OnFindReferences, if set, is called with the identifier under the
	// cursor when "find references" (Ctrl+f) fires — set by
	// cmd/kiwi/main.go to open the finder overlay pre-seeded with that
	// query (see finder.View.OpenWithQuery). Same plain-callback pattern
	// as OnAllTabsClosed.
	OnFindReferences func(word string)

	// completion holds the in-progress autocomplete popup (Ctrl+Space),
	// nil when none is showing — see completion.go.
	completion *completionState

	// store resolves Open's path to a *Buffer — see BufferStore. Defaults
	// to a private store (below), so a View nobody explicitly shares one
	// with behaves exactly as if buffers were never shared at all.
	store *BufferStore
}

// NewView creates an empty editor pane with no tabs open; call Open to
// load a file into it.
func NewView() *View {
	return &View{tabWidth: 4, keymap: DefaultKeybinds.Resolve(nil), store: NewBufferStore()}
}

// SetBufferStore replaces this pane's BufferStore, so it shares loaded
// buffers with every other View given the same store (e.g. split panes)
// instead of loading its own private copies. Call before opening any
// tabs — an already-open tab's Buffer came from whichever store was
// active when it was opened, and doesn't retroactively move.
func (v *View) SetBufferStore(s *BufferStore) {
	v.store = s
}

// SetKeymap merges the user config's "editor" scope overrides on top of
// DefaultKeybinds, replacing the pane's active keymap.
func (v *View) SetKeymap(overrides map[string]string) {
	v.keymap = DefaultKeybinds.Resolve(overrides)
}

func (v *View) Title() string { return "Editor" }

// ActivePath returns the active tab's file path, or "" if no tabs are
// open. Used when splitting a pane, so the new pane starts on the same
// file rather than empty.
func (v *View) ActivePath() string {
	t := v.activeTab()
	if t == nil {
		return ""
	}
	return t.path
}

func (v *View) activeTab() *tab {
	if v.active < 0 || v.active >= len(v.tabs) {
		return nil
	}
	return v.tabs[v.active]
}

// OpenPaths returns the paths of every open tab, for the caller
// (cmd/kiwi/main.go) to compute per-file git line status against — the
// View has no git/repo knowledge of its own; see ApplyLineStatus.
func (v *View) OpenPaths() []string {
	paths := make([]string, len(v.tabs))
	for i, t := range v.tabs {
		paths[i] = t.path
	}
	return paths
}

// ApplyLineStatus sets the git-diff gutter markers (see gitstatus.
// FileHunks) for the open tab whose path matches path, redrawn on the
// next Render. A no-op if path isn't currently open — a tab can close
// between the caller listing OpenPaths and computing its status.
func (v *View) ApplyLineStatus(path string, lines map[int]gitstatus.LineStatus) {
	for _, t := range v.tabs {
		if t.path == path {
			t.lineStatus = lines
			return
		}
	}
}

// Open loads path into a tab. If path is already open, its existing tab is
// simply activated (matching typical editor behavior — opening a file
// that's already open switches to it rather than duplicating it) and its
// scroll/cursor position is left untouched. Otherwise a new tab is
// appended and activated. A load error is shown in the tab rather than
// propagated, since there is no error-reporting channel between panes in
// Step 0.
func (v *View) Open(path string) {
	for i, t := range v.tabs {
		if t.path == path {
			v.active = i
			return
		}
	}

	buf, err := v.store.Open(path)
	t := &tab{path: path, buf: buf, err: err}
	if buf != nil && buf.highlighted == nil {
		buf.highlighted = highlightBuffer(buf) // already cached if another pane opened this path first
	}
	v.tabs = append(v.tabs, t)
	v.active = len(v.tabs) - 1
}

// OpenAtLine is Open, followed by moving the cursor to line (1-based),
// clamped to the buffer's bounds — e.g. for jumping to a content-search
// match. Unlike Open, this always moves the cursor, even if path was
// already open in another tab.
func (v *View) OpenAtLine(path string, line int) {
	v.Open(path)
	if line <= 0 {
		return
	}
	t := v.activeTab()
	if t == nil || t.buf == nil {
		return
	}
	t.cursorLn = line - 1
	t.cursorCol = 0
	v.clamp(t)
}

// NextTab activates the next open tab, wrapping around.
func (v *View) NextTab() {
	if len(v.tabs) == 0 {
		return
	}
	v.active = (v.active + 1) % len(v.tabs)
}

// PrevTab activates the previous open tab, wrapping around.
func (v *View) PrevTab() {
	if len(v.tabs) == 0 {
		return
	}
	v.active = (v.active - 1 + len(v.tabs)) % len(v.tabs)
}

// CloseTab closes the active tab, activating the tab to its left (or the
// new last tab, if the closed tab was leftmost). Fires OnAllTabsClosed if
// this was the last one.
func (v *View) CloseTab() {
	if len(v.tabs) == 0 {
		return
	}
	closed := v.tabs[v.active]
	v.tabs = append(v.tabs[:v.active], v.tabs[v.active+1:]...)
	if v.active >= len(v.tabs) {
		v.active = len(v.tabs) - 1
	}
	v.releaseTab(closed)
	v.notifyIfEmpty()
}

// CloseAllTabs closes all tabs. Fires OnAllTabsClosed.
func (v *View) CloseAllTabs() {
	if len(v.tabs) == 0 {
		return
	}
	closed := v.tabs
	v.tabs = []*tab{}
	v.active = 0
	for _, t := range closed {
		v.releaseTab(t)
	}
	v.notifyIfEmpty()
}

// releaseTab releases t's Buffer back to the store (if it successfully
// loaded one — a failed load never registered a reference to release),
// decrementing the shared reference count; see BufferStore.Release.
func (v *View) releaseTab(t *tab) {
	if t.buf != nil {
		v.store.Release(t.path)
	}
}

// notifyIfEmpty calls OnAllTabsClosed, if set, when the pane has just been
// left with zero open tabs.
func (v *View) notifyIfEmpty() {
	if len(v.tabs) == 0 && v.OnAllTabsClosed != nil {
		v.OnAllTabsClosed()
	}
}

// StatusText is the "Ln N, Col N" text meant for a status bar (see
// internal/ui/statusbar), prefixed with an "-- INSERT --" indicator while
// the pane is in Insert mode, or replaced by the in-progress ":<line>"
// prompt while in Command mode. Col is 1-based over rune positions in the
// current line, not raw terminal display columns.
func (v *View) StatusText() string {
	t := v.activeTab()
	if t == nil || t.buf == nil {
		return ""
	}
	if v.mode == modeCommand {
		return ":" + v.commandBuf
	}
	prefix := ""
	if v.mode == modeInsert {
		prefix = "-- INSERT -- "
	}
	return fmt.Sprintf("%sLn %d, Col %d", prefix, t.cursorLn+1, t.cursorCol+1)
}

// CursorPosition implements layout.CursorProvider: it reports where, in
// this View's own Window coordinates, the terminal's native cursor should
// be shown. It is only meaningful right after Render has run (Render is
// what keeps topLine/leftCol following the cursor), which is exactly the
// order App renders in.
func (v *View) CursorPosition() (int, int, bool) {
	t := v.activeTab()
	if t == nil || t.buf == nil {
		return 0, 0, false
	}
	col := gutterWidthFor(t) + cursorDisplayColumn(t, v.tabWidth) - t.leftCol
	row := 1 + (t.cursorLn - t.topLine) // +1: row 0 is the tab bar
	return col, row, true
}

func (v *View) Render(w layout.Window) {
	cols, rows := w.Size()
	v.lastWidth, v.lastHeight = cols, rows
	w.Clear()

	if len(v.tabs) == 0 {
		msg := "No file open — select a file in the tree and press Enter"
		w.Println(0, layout.Segment{Text: msg, Style: layout.Style{Attr: layout.AttrDim}})
		return
	}

	w.Println(0, tabBarSegments(v.tabs, v.active, cols)...)

	t := v.activeTab()
	// Defensive: a sibling pane sharing this tab's Buffer (see
	// BufferStore) could have shrunk it since this pane's own last
	// keypress — the only other time cursorLn/cursorCol are normally
	// clamped. Without this, a now-out-of-range cursorLn makes the body
	// loop below break on its very first row, silently rendering an empty
	// pane until this pane's own next keystroke.
	if t != nil {
		v.clamp(t)
	}
	bodyRows := rows - 1
	if bodyRows < 0 {
		bodyRows = 0
	}
	// body content is drawn one row down, into a window offset past the
	// tab bar; layout.Window has no sub-window primitive of its own (that
	// lives one level down, on the real vaxis.Window), so the offset is
	// applied directly to the row index passed to Println instead.
	renderBody(w, t, v.tabWidth, cols, bodyRows, 1)

	if v.completion != nil {
		if col, row, ok := v.CursorPosition(); ok {
			v.renderCompletionPopup(w, cols, rows, col, row)
		}
	}
}

// tabBarSegments builds the tab bar as styled segments — the active tab is
// reverse-video highlighted (the same "selected" convention the file tree
// and finder use), not just bracket-punctuated — then truncated to cols
// via the same wide-rune-safe helper used for the editor body, rather than
// raw byte slicing.
func tabBarSegments(tabs []*tab, active, cols int) []layout.Segment {
	names := tabDisplayNames(tabs)
	var segs []layout.Segment
	for i := range tabs {
		name := names[i]
		text := " " + name + " "
		style := layout.Style{}
		if i == active {
			text = "[" + name + "]"
			style.Attr |= layout.AttrReverse
		}
		segs = append(segs, layout.Segment{Text: text, Style: style})
		if i < len(tabs)-1 {
			segs = append(segs, layout.Segment{Text: "|"})
		}
	}
	return textwidth.SliceSegmentsByDisplayColumn(segs, 0, cols)
}

// tabDisplayNames returns, per tab, the name shown in the tab bar: just
// the bare filename, unless another open tab shares that same filename
// — in which case enough of each clashing tab's parent path is
// prefixed to tell them apart (e.g. "editor/view.go" vs
// "finder/view.go", or "a/x/foo.go" vs "b/x/foo.go" if one parent
// folder's name isn't enough either).
func tabDisplayNames(tabs []*tab) []string {
	names := make([]string, len(tabs))
	groups := map[string][]int{} // bare filename -> indices of tabs with that name
	for i, t := range tabs {
		if t.path == "" {
			names[i] = "[No Name]"
			continue
		}
		name := filepath.Base(t.path)
		names[i] = name
		groups[name] = append(groups[name], i)
	}

	for _, idxs := range groups {
		if len(idxs) < 2 {
			continue
		}
		paths := make([]string, len(idxs))
		for j, i := range idxs {
			paths[j] = tabs[i].path
		}
		disambiguated := disambiguatePaths(paths)
		for j, i := range idxs {
			names[i] = disambiguated[j]
		}
	}

	for i, t := range tabs {
		if t.buf != nil && t.buf.Dirty {
			names[i] += " *" // unsaved-edits marker, appended after disambiguation
		}
	}
	return names
}

// disambiguatePaths returns, for each path, just enough trailing
// path segments (filename plus however many parent directories are
// needed) to make every result distinct. Since distinct tabs always
// have distinct absolute paths (see activeTab/Open, which reuse a tab
// rather than opening the same path twice), growing the segment count
// far enough is always guaranteed to terminate in unique names.
func disambiguatePaths(paths []string) []string {
	segLists := make([][]string, len(paths))
	for i, p := range paths {
		segLists[i] = strings.Split(filepath.ToSlash(p), "/")
	}

	for n := 2; ; n++ {
		result := make([]string, len(paths))
		counts := map[string]int{}
		fullyExpanded := true
		for i, segs := range segLists {
			take := n
			if take >= len(segs) {
				take = len(segs)
			} else {
				fullyExpanded = false
			}
			result[i] = strings.Join(segs[len(segs)-take:], "/")
			counts[result[i]]++
		}

		unique := true
		for _, c := range counts {
			if c > 1 {
				unique = false
				break
			}
		}
		if unique || fullyExpanded {
			return result
		}
	}
}

func renderBody(w layout.Window, t *tab, tabWidth, cols, rows, rowOffset int) {
	if t == nil {
		return
	}
	if t.buf == nil {
		if t.err != nil {
			w.Println(rowOffset, layout.Segment{Text: "error: " + t.err.Error()})
		}
		return
	}

	gutterWidth := gutterWidthFor(t)
	contentWidth := cols - gutterWidth
	if contentWidth < 0 {
		contentWidth = 0
	}

	if t.cursorLn < t.topLine {
		t.topLine = t.cursorLn
	}
	if t.cursorLn >= t.topLine+rows {
		t.topLine = t.cursorLn - rows + 1
	}
	if t.topLine < 0 {
		t.topLine = 0
	}

	cursorCol := cursorDisplayColumn(t, tabWidth)
	if cursorCol < t.leftCol {
		t.leftCol = cursorCol
	}
	if cursorCol >= t.leftCol+contentWidth {
		t.leftCol = cursorCol - contentWidth + 1
	}
	if t.leftCol < 0 {
		t.leftCol = 0
	}

	for i := 0; i < rows; i++ {
		ln := t.topLine + i
		if ln >= len(t.buf.Lines) {
			break
		}
		var raw []layout.Segment
		if ln < len(t.buf.highlighted) && t.buf.highlighted[ln] != nil {
			raw = t.buf.highlighted[ln] // real tree-sitter output, raw (not tab-expanded)
		} else {
			raw = highlightLine(t.buf.Lines[ln]) // heuristic fallback, also raw
		}
		expandedSegs := textwidth.ExpandTabsSegments(raw, tabWidth)
		visible := textwidth.SliceSegmentsByDisplayColumn(expandedSegs, t.leftCol, contentWidth)

		diffSeg := layout.Segment{
			Text:  gitstyle.LineMarker(t.lineStatus[ln]),
			Style: gitstyle.LineStyle(t.lineStatus[ln]),
		}
		gutterSeg := layout.Segment{
			Text:  fmt.Sprintf("%*d ", gutterWidth-2, ln+1),
			Style: layout.Style{Attr: layout.AttrDim},
		}
		w.Println(rowOffset+i, append([]layout.Segment{diffSeg, gutterSeg}, visible...)...)
	}
}

// gutterWidthFor is the line-number column's width: a leading git-diff
// marker column (see ApplyLineStatus), the line-number digits, and 1
// trailing space, derived from the buffer's line count.
func gutterWidthFor(t *tab) int {
	if t.buf == nil {
		return 2
	}
	return len(fmt.Sprintf("%d", len(t.buf.Lines))) + 2
}

// currentLineRunes returns the expanded (tabs-to-spaces) runes of t's
// current line (ln), or nil if ln is out of range.
func currentLineRunes(t *tab, ln, tabWidth int) []rune {
	if t.buf == nil || ln < 0 || ln >= len(t.buf.Lines) {
		return nil
	}
	return []rune(textwidth.ExpandTabs(t.buf.Lines[ln], tabWidth))
}

// cursorDisplayColumn converts t.cursorCol (a rune index) to a display
// column on t's current line, accounting for double-width runes.
func cursorDisplayColumn(t *tab, tabWidth int) int {
	runes := currentLineRunes(t, t.cursorLn, tabWidth)
	col := t.cursorCol
	if col > len(runes) {
		col = len(runes)
	}
	if col < 0 {
		col = 0
	}
	return textwidth.DisplayWidth(string(runes[:col]))
}

// expandedColForRawIndex converts a raw rune index into line (an index
// into the buffer's un-expanded storage) to the corresponding tab-expanded
// rune index — cursorCol's own units — by measuring how many runes
// ExpandTabs produces for the raw prefix up to idx. This is how an edit's
// raw-index result (see rawIndexForExpandedCol) gets translated back into
// cursorCol.
func expandedColForRawIndex(line string, idx, tabWidth int) int {
	runes := []rune(line)
	if idx > len(runes) {
		idx = len(runes)
	}
	if idx < 0 {
		idx = 0
	}
	return len([]rune(textwidth.ExpandTabs(string(runes[:idx]), tabWidth)))
}

// rawIndexForExpandedCol is expandedColForRawIndex's inverse: given col (a
// tab-expanded rune index, i.e. cursorCol), it returns the corresponding
// raw rune index into line, for splicing an edit into Buffer's un-expanded
// storage. It walks raw runes tracking the same running column ExpandTabs
// does; a column landing inside a tab's expansion (there is no single raw
// rune "at" a mid-tab column) snaps to just past that tab — edits treat a
// tab as one atomic character, never splitting it. Known edge case: this
// tracks column by rune count rather than go-runewidth display width, so a
// line mixing wide (CJK) runes before a tab could compute a slightly-off
// split point — an accepted simplification, not meant to be pixel-perfect.
func rawIndexForExpandedCol(line string, col, tabWidth int) int {
	if tabWidth <= 0 {
		tabWidth = 8
	}
	runes := []rune(line)
	expanded := 0
	for i, r := range runes {
		if r == '\t' {
			span := tabWidth - (expanded % tabWidth)
			if col < expanded+span {
				return i + 1 // snap past the tab, not into it
			}
			expanded += span
			continue
		}
		if col <= expanded {
			return i
		}
		expanded++
	}
	return len(runes)
}

func (v *View) HandleKey(k layout.Key) bool {
	if k.EventType == layout.EventRelease {
		return false
	}

	switch v.mode {
	case modeInsert:
		return v.handleInsertKey(k)
	case modeCommand:
		return v.handleCommandKey(k)
	}

	action, ok := v.keymap[k.String()]
	if !ok {
		return false
	}

	switch action {
	case "next_tab":
		v.NextTab()
		return true
	case "prev_tab":
		v.PrevTab()
		return true
	case "insert_mode":
		v.enterInsertMode()
		return true
	case "append_mode":
		// vim's "a": insert AFTER the character under the cursor, rather
		// than before it — same one-past-the-end clamping normal cursor
		// movement already allows (see TestViewCursorColClampsAtLineLength).
		if v.enterInsertMode() {
			t := v.activeTab()
			t.cursorCol++
			v.clamp(t)
		}
		return true
	case "append_new_line_mode":
		// vim's "o": open a blank line below the cursor and enter Insert
		// mode on it, as a single undo unit covering the opened line plus
		// anything typed before the next Esc — enterInsertMode captures
		// that pre-edit snapshot, exactly like "i"/"a" do.
		if v.enterInsertMode() {
			v.openLineBelow()
		}
		return true
	case "command_mode":
		if v.activeTab() != nil {
			v.mode = modeCommand
		}
		return true
	case "save":
		v.saveActive()
		return true
	}

	t := v.activeTab()
	if t == nil {
		return false
	}

	switch action {
	case "undo":
		v.undo(t)
	case "redo":
		v.redo(t)
	case "delete_char_forward":
		v.deleteCharForward(t)
	case "delete_char_backward":
		v.deleteCharBackward(t)
	case "go_to_parent":
		v.goToParent(t)
	case "go_to_definition":
		v.goToDefinition(t)
	case "jump_back":
		v.jumpBack(t)
	case "find_references":
		v.findReferences(t)
	default:
		if !v.applyMovement(t, action) {
			return false
		}
	}

	v.clamp(t)
	return true
}

// applyMovement mutates t's cursor for a Normal-mode movement action,
// shared with handleInsertKey (arrow keys move the cursor even while
// inserting — see there). Returns false if action isn't a movement
// action, so callers can tell "not a movement" apart from "movement
// handled, cursor happens not to have changed".
func (v *View) applyMovement(t *tab, action string) bool {
	switch action {
	case "move_down":
		t.cursorLn++
	case "move_up":
		t.cursorLn--
	case "move_left":
		t.cursorCol--
	case "move_right":
		t.cursorCol++
	case "page_down":
		t.cursorLn += v.pageSize()
	case "page_up":
		t.cursorLn -= v.pageSize()
	case "line_start":
		t.cursorCol = 0
	case "line_end":
		t.cursorCol = len(currentLineRunes(t, t.cursorLn, v.tabWidth))
	case "first_line":
		t.cursorLn = 0
	case "last_line":
		t.cursorLn = len(t.buf.Lines) - 1
	default:
		return false
	}
	return true
}

// handleInsertKey handles a key while the pane is in Insert mode. Only
// normal_mode/save/insert_newline/insert_backspace are read from the
// keymap — deliberately not the full Normal-mode action set, so hjkl/g/G/
// ]/[/x/X etc. stay literal insertable text instead of re-triggering their
// Normal-mode actions. Anything else with printable text is inserted at
// the cursor, mirroring internal/ui/finder/view.go's query-typing
// fallback (same Ctrl/Alt/Super guard).
func (v *View) handleInsertKey(k layout.Key) bool {
	// The autocomplete popup (Ctrl+Space) gets first look at every key
	// while it's open — Up/Down/Enter/Tab/Esc are fully its own; anything
	// else (Backspace, printable text) falls through to the normal
	// handling below, which re-filters the popup afterward.
	if v.completion != nil && v.handleCompletionKey(k) {
		return true
	}

	switch v.keymap[k.String()] {
	case "normal_mode":
		v.exitInsertMode()
		return true
	case "save":
		v.saveActive()
		return true
	case "insert_newline":
		// Never reached while the popup is open — handleCompletionKey
		// above already intercepts Enter as "accept" in that case.
		v.insertNewline()
		return true
	case "insert_backspace":
		v.deleteBackward()
		if v.completion != nil {
			v.refilterCompletion()
		}
		return true
	case "insert_tab":
		// Tab arrives as a Named key with no Text (same as Enter/Esc/
		// Backspace), so the printable-text fallback below never sees
		// it — it needs its own action, same as those.
		v.insertText("\t")
		return true
	case "trigger_autocomplete":
		v.triggerAutocomplete()
		return true
	}

	// Arrow keys move the cursor even while inserting, like most editors.
	// hjkl (and any other letter bound to the same move_* actions) must
	// NOT — they arrive as Text with Named == "", whereas arrow keys are
	// always Named, so restricting to exactly these four action names
	// (not the full applyMovement set: Home/End/PageUp/PageDown/g/G stay
	// Normal-mode only) plus this Named check is what tells them apart.
	if k.Named != "" {
		switch v.keymap[k.String()] {
		case "move_up", "move_down", "move_left", "move_right":
			if t := v.activeTab(); t != nil {
				v.applyMovement(t, v.keymap[k.String()])
				v.clamp(t)
			}
			v.completion = nil // moving the cursor invalidates any open popup's context
			return true
		}
	}

	if k.Text != "" && k.Mods&(layout.ModCtrl|layout.ModAlt|layout.ModSuper) == 0 {
		v.insertText(k.Text)
		if v.completion != nil {
			v.refilterCompletion()
		}
	}
	return true
}

// handleCommandKey handles a key while the pane is in Command mode — a
// minimal ":<command>" prompt (not a general ex-command line): characters
// accumulate in v.commandBuf, Enter commits (see commitCommand), Esc
// cancels, Backspace edits what's typed so far.
func (v *View) handleCommandKey(k layout.Key) bool {
	switch v.keymap[k.String()] {
	case "normal_mode":
		v.mode = modeNormal
		v.commandBuf = ""
		return true
	case "insert_newline": // Enter
		v.commitCommand()
		return true
	case "insert_backspace": // Backspace
		if n := len(v.commandBuf); n > 0 {
			v.commandBuf = v.commandBuf[:n-1]
		}
		return true
	}

	if len(k.Text) == 1 && k.Mods&(layout.ModCtrl|layout.ModAlt|layout.ModSuper) == 0 {
		v.commandBuf += k.Text
	}
	return true
}

// commitCommand parses v.commandBuf and executes it, then always closes
// the prompt back to Normal mode. A purely numeric command jumps the
// active tab's cursor to that 1-based line (see goToLine); otherwise it's
// matched against a small fixed set of vim ex-commands — "q"/"q!" close
// the active tab, "qa"/"qa!" close all tabs, "w" saves, "wq" saves then
// closes. Anything else (including an empty buffer) just closes the
// prompt without effect — no error UI, matching the "simple first pass"
// precedent set by Save's error handling.
func (v *View) commitCommand() {
	cmd := v.commandBuf
	v.commandBuf = ""
	v.mode = modeNormal

	if n, err := strconv.Atoi(cmd); err == nil {
		v.goToLine(n)
		return
	}

	switch cmd {
	case "q":
		v.closeActiveTab(false)
	case "q!":
		v.closeActiveTab(true)
	case "qa":
		v.closeAllTabsCmd(false)
	case "qa!":
		v.closeAllTabsCmd(true)
	case "w":
		v.saveActive()
	case "wq":
		v.saveActive()
		v.closeActiveTab(false) // if the save failed, Dirty is still true and this correctly still refuses
	default:
		debuglog.Warn("editor: unknown command %q", cmd)
	}
}

// goToLine moves the active tab's cursor to line (1-based), clamped to
// the buffer (via the same v.clamp every other cursor move uses). line
// <= 0 is a no-op.
func (v *View) goToLine(line int) {
	if line <= 0 {
		return
	}
	t := v.activeTab()
	if t == nil || t.buf == nil {
		return
	}
	t.cursorLn = line - 1
	t.cursorCol = 0
	v.clamp(t)
}

// closeActiveTab implements vim's ":q"/":q!": closes the active tab,
// refusing (vim: "no write since last change") if it has unsaved changes
// unless force is true.
func (v *View) closeActiveTab(force bool) {
	t := v.activeTab()
	if t == nil {
		return
	}
	if !force && t.buf != nil && t.buf.Dirty {
		debuglog.Warn("q: %s has unsaved changes (use q! to discard)", t.path)
		return
	}
	v.CloseTab()
}

// closeAllTabsCmd implements vim's ":qa"/":qa!": closes every tab,
// refusing (naming which are unsaved) if any has unsaved changes unless
// force is true.
func (v *View) closeAllTabsCmd(force bool) {
	if !force {
		var dirty []string
		for _, t := range v.tabs {
			if t.buf != nil && t.buf.Dirty {
				dirty = append(dirty, t.path)
			}
		}
		if len(dirty) > 0 {
			debuglog.Warn("qa: %d unsaved file(s) (use qa! to discard): %s", len(dirty), strings.Join(dirty, ", "))
			return
		}
	}
	v.CloseAllTabs()
}

// enterInsertMode switches to Insert mode and snapshots the active tab's
// current state as the pending undo entry for this Insert session (see
// exitInsertMode, which commits or discards it). Returns false, leaving
// the mode unchanged, if there's no open buffer to edit.
func (v *View) enterInsertMode() bool {
	t := v.activeTab()
	if t == nil || t.buf == nil {
		return false
	}
	v.mode = modeInsert
	snap := snapshotTab(t)
	t.insertSnapshot = &snap
	return true
}

// exitInsertMode returns to Normal mode, committing the just-finished
// Insert session as a single undo entry if it actually changed the
// buffer's Lines — vim's own undo granularity (one entry per Insert
// session, not per keystroke). An Insert session opened and closed
// without typing anything (or one that ends up producing identical text)
// leaves no entry and never touches the redo stack.
func (v *View) exitInsertMode() {
	v.mode = modeNormal
	t := v.activeTab()
	if t == nil || t.insertSnapshot == nil {
		return
	}
	snap := *t.insertSnapshot
	t.insertSnapshot = nil
	v.pushUndoIfChanged(t, snap)
}

// ExitEditingModes returns the pane to Normal mode, discarding an
// in-progress Command prompt and committing (or discarding, if nothing
// was typed) an in-progress Insert session — exactly what Esc would do in
// either mode. Meant to be called when focus moves away from this pane
// (see cmd/kiwi/main.go's focus-change wiring): a mouse click can switch
// focus without ever routing a key through the losing pane's HandleKey
// (unlike Tab-cycling, which Insert mode's own key-trap already blocks),
// so without this a pane could be left "stuck" mid-Insert-session
// indefinitely. That matters now that buffers can be shared across panes
// (see BufferStore) — two panes simultaneously mid-session on the same
// buffer would scramble whose pending snapshot ends up committed to its
// undo history; this guarantees at most one pane ever is.
func (v *View) ExitEditingModes() {
	switch v.mode {
	case modeInsert:
		v.exitInsertMode()
	case modeCommand:
		v.mode = modeNormal
		v.commandBuf = ""
	}
}

// pushUndoIfChanged pushes before onto t.buf's undo stack (capped at
// maxUndoEntries, oldest dropped) and clears its redo stack, but only if
// t.buf.Lines actually differs from before's — an edit that ends up a
// no-op doesn't clutter undo history. Shared by exitInsertMode (one entry
// per Insert session) and any single-key Normal-mode edit like "x"/"X"
// (one entry per keypress, since those are already complete changes on
// their own, unlike an Insert session). Lives on Buffer, not tab — see
// Buffer.undoStack's doc comment — so this is undo history shared by
// every pane showing t.buf, not just this one.
func (v *View) pushUndoIfChanged(t *tab, before undoEntry) {
	if t.buf == nil || linesEqual(before.lines, t.buf.Lines) {
		return
	}
	if len(t.buf.undoStack) >= maxUndoEntries {
		t.buf.undoStack = t.buf.undoStack[1:]
	}
	t.buf.undoStack = append(t.buf.undoStack, before)
	t.buf.redoStack = nil
}

// undo reverts t's buffer to its state before the most recently completed
// Insert session (or the most recent prior undo/redo) — from ANY pane
// sharing t.buf, not just this one — pushing the current state onto the
// redo stack first so redo can reapply it. A no-op on an empty undo
// stack. Moves only THIS pane's cursor to the reverted entry's recorded
// position; a sibling pane also showing t.buf keeps its own cursor
// wherever it was (clamped defensively on its next Render if the content
// shrank out from under it).
func (v *View) undo(t *tab) {
	if len(t.buf.undoStack) == 0 {
		return
	}
	entry := t.buf.undoStack[len(t.buf.undoStack)-1]
	t.buf.undoStack = t.buf.undoStack[:len(t.buf.undoStack)-1]
	t.buf.redoStack = append(t.buf.redoStack, snapshotTab(t))
	applyUndoEntry(t, entry)
	v.reHighlight(t)
}

// redo re-applies the most recently undone change, pushing the current
// state onto the undo stack first so it can be undone again. A no-op on
// an empty redo stack.
func (v *View) redo(t *tab) {
	if len(t.buf.redoStack) == 0 {
		return
	}
	entry := t.buf.redoStack[len(t.buf.redoStack)-1]
	t.buf.redoStack = t.buf.redoStack[:len(t.buf.redoStack)-1]
	t.buf.undoStack = append(t.buf.undoStack, snapshotTab(t))
	applyUndoEntry(t, entry)
	v.reHighlight(t)
}

// snapshotTab captures t's current buffer contents (copied, so later
// mutation of t.buf.Lines can't alias the snapshot) and cursor state into
// an undoEntry.
func snapshotTab(t *tab) undoEntry {
	return undoEntry{
		lines:     append([]string(nil), t.buf.Lines...),
		cursorLn:  t.cursorLn,
		cursorCol: t.cursorCol,
	}
}

// applyUndoEntry restores t's buffer and cursor to a previously captured
// undoEntry.
func applyUndoEntry(t *tab, e undoEntry) {
	t.buf.Restore(e.lines)
	t.cursorLn = e.cursorLn
	t.cursorCol = e.cursorCol
}

// insertText inserts s at the active tab's cursor, advances the cursor
// past it, and re-highlights. A no-op if no editable buffer is open.
func (v *View) insertText(s string) {
	t := v.activeTab()
	if t == nil || t.buf == nil {
		return
	}
	raw := rawIndexForExpandedCol(t.buf.Lines[t.cursorLn], t.cursorCol, v.tabWidth)
	newRaw := t.buf.InsertText(t.cursorLn, raw, s)
	t.cursorCol = expandedColForRawIndex(t.buf.Lines[t.cursorLn], newRaw, v.tabWidth)
	v.reHighlight(t)
	v.clamp(t)
}

// insertNewline splits the active tab's current line at the cursor — the
// Enter key's effect in Insert mode.
func (v *View) insertNewline() {
	t := v.activeTab()
	if t == nil || t.buf == nil {
		return
	}
	raw := rawIndexForExpandedCol(t.buf.Lines[t.cursorLn], t.cursorCol, v.tabWidth)
	t.buf.SplitLine(t.cursorLn, raw)
	t.cursorLn++
	t.cursorCol = 0
	v.reHighlight(t)
	v.clamp(t)
}

// deleteBackward deletes one character before the active tab's cursor,
// joining with the previous line if the cursor is at column 0 — the
// Backspace key's effect in Insert mode.
func (v *View) deleteBackward() {
	t := v.activeTab()
	if t == nil || t.buf == nil {
		return
	}
	raw := rawIndexForExpandedCol(t.buf.Lines[t.cursorLn], t.cursorCol, v.tabWidth)
	newLn, newRaw := t.buf.DeleteBackward(t.cursorLn, raw)
	t.cursorLn = newLn
	t.cursorCol = expandedColForRawIndex(t.buf.Lines[newLn], newRaw, v.tabWidth)
	v.reHighlight(t)
	v.clamp(t)
}

// deleteCharForward implements vim's "x": deletes the rune under the
// cursor (not the one before it, like Backspace) and stays in Normal
// mode. A no-op on an empty line or when the cursor is already past the
// last rune. Recorded as its own undo entry — unlike an Insert session, a
// single "x" press is already a complete change on its own.
func (v *View) deleteCharForward(t *tab) {
	if t.buf == nil {
		return
	}
	raw := rawIndexForExpandedCol(t.buf.Lines[t.cursorLn], t.cursorCol, v.tabWidth)
	if raw >= len([]rune(t.buf.Lines[t.cursorLn])) {
		return
	}
	before := snapshotTab(t)
	_, newRaw := t.buf.DeleteBackward(t.cursorLn, raw+1) // deletes exactly the rune at raw
	t.cursorCol = expandedColForRawIndex(t.buf.Lines[t.cursorLn], newRaw, v.tabWidth)
	v.pushUndoIfChanged(t, before)
	v.reHighlight(t)
}

// deleteCharBackward implements vim's "X": deletes the rune immediately
// before the cursor (joining with the previous line at column 0, just
// like Backspace in Insert mode) but stays in Normal mode and is recorded
// as its own undo entry rather than folded into an Insert session.
func (v *View) deleteCharBackward(t *tab) {
	if t.buf == nil {
		return
	}
	before := snapshotTab(t)
	raw := rawIndexForExpandedCol(t.buf.Lines[t.cursorLn], t.cursorCol, v.tabWidth)
	newLn, newRaw := t.buf.DeleteBackward(t.cursorLn, raw)
	t.cursorLn = newLn
	t.cursorCol = expandedColForRawIndex(t.buf.Lines[newLn], newRaw, v.tabWidth)
	v.pushUndoIfChanged(t, before)
	v.reHighlight(t)
}

// openLineBelow implements vim's "o": inserts a new blank line below the
// cursor's current line and moves the cursor onto it — called after
// enterInsertMode has already captured the pre-"o" snapshot, so the
// opened line plus anything typed into it undoes as one unit, exactly
// like "i"/"a".
func (v *View) openLineBelow() {
	t := v.activeTab()
	if t == nil || t.buf == nil {
		return
	}
	t.buf.SplitLine(t.cursorLn, len([]rune(t.buf.Lines[t.cursorLn])))
	t.cursorLn++
	t.cursorCol = 0
	v.reHighlight(t)
	v.clamp(t)
}

// reHighlight recomputes t.buf.highlighted after an edit — a full
// re-parse, the same cost as Open's initial call; see the highlighted
// field's doc comment for the incremental-parsing optimization this
// defers.
func (v *View) reHighlight(t *tab) {
	if t.buf != nil {
		t.buf.highlighted = highlightBuffer(t.buf)
	}
}

// saveActive writes the active tab's buffer back to disk. A failure is
// logged rather than shown in the pane — the buffer's Dirty flag simply
// stays true, so the tab's dirty marker keeps reflecting that the edit is
// still unsaved.
func (v *View) saveActive() {
	t := v.activeTab()
	if t == nil || t.buf == nil {
		return
	}
	if err := t.buf.Save(); err != nil {
		debuglog.Error("save %s: %v", t.buf.Path, err)
	}
}

func (v *View) pageSize() int {
	// One row is reserved for the tab bar.
	if v.lastHeight <= 1 {
		return 10
	}
	return v.lastHeight - 1
}

// clamp keeps cursorLn within the buffer and cursorCol within the
// (possibly just-changed) current line's length. There is no "sticky
// column" — moving through a short line and back to a long one does not
// remember the original column, an acceptable simplification for now.
func (v *View) clamp(t *tab) {
	if t.cursorLn < 0 {
		t.cursorLn = 0
	}
	if t.buf != nil && t.cursorLn >= len(t.buf.Lines) {
		t.cursorLn = len(t.buf.Lines) - 1
	}
	if t.cursorCol < 0 {
		t.cursorCol = 0
	}
	if max := len(currentLineRunes(t, t.cursorLn, v.tabWidth)); t.cursorCol > max {
		t.cursorCol = max
	}
}
