// Package finder is a fuzzy file finder meant to be shown as a modal
// overlay (see ui.App.ShowOverlay/CloseOverlay). It has three modes,
// switched with Tab: find a file by name (fuzzy match), find a line by
// its content (via `git grep`), or search-and-replace across the project
// (see ReplaceView). Up/Down moves the selection, Enter opens it, Esc
// closes without opening anything.
package finder

import (
	"fmt"
	"path/filepath"
	"sort"
	"time"

	"github.com/bricejulia/nib/internal/config"
	"github.com/bricejulia/nib/internal/layout"
	"github.com/bricejulia/nib/internal/textwidth"
	"github.com/bricejulia/nib/internal/ui/gitstyle"
	"github.com/bricejulia/nib/internal/vcs/gitstatus"
)

// DefaultKeybinds are the finder's built-in keybindings, overridable via
// the user config's "finder" scope (see internal/config). Deliberately
// narrow: any trigger not listed here (and not otherwise Ctrl/Alt/Super-
// modified) falls through to being typed into the search query instead,
// so overriding a plain letter key here means it stops being typeable —
// see HandleKey.
var DefaultKeybinds = config.Defaults{
	{Trigger: "Esc", Action: "close"},
	{Trigger: "Tab", Action: "switch_mode"},
	{Trigger: "Enter", Action: "open_selection"},
	{Trigger: "Down", Action: "move_down"},
	{Trigger: "Up", Action: "move_up"},
}

type mode int

const (
	modeFiles mode = iota
	modeContent
	modeReplace
)

type scoredItem struct {
	path  string
	score int
}

// minContentQueryLen is how many characters must be typed before a
// content search runs — grepping the whole project for 0-1 characters
// would be slow and returns an unusably large, unranked result set.
const minContentQueryLen = 2

// contentSearchDebounce is how long to wait after the last keystroke
// before actually running a content search — var, not const, so tests can
// shrink it. `git grep` can take a real amount of time on a large
// project, so searching on every single keystroke would spawn a pile of
// redundant, overlapping searches while typing.
var contentSearchDebounce = 150 * time.Millisecond

// SearchResult is a content search's result, delivered back into the
// host application's event loop via Post (see ui.App.Post) rather than
// blocking the UI thread — pass it to ApplyContentResult, e.g. from
// ui.App.SetCustomEventHandler.
type SearchResult struct {
	gen     int
	matches []contentMatch
}

// View is the finder's content. It implements layout.View (so it can be
// shown via ui.App.ShowOverlay like any pane) and layout.CursorProvider
// (so the terminal's real cursor sits at the end of the typed query).
type View struct {
	root string
	mode mode

	// replace is the search-and-replace mode's entire state and behavior,
	// delegated to wholesale (Render, HandleKey, Title, CursorPosition,
	// ScrollState/ScrollTo) rather than folded into View's own fields and
	// switches — see toggleMode, Render, and HandleKey. It's also reachable
	// directly via OpenReplace/Replace, for a global shortcut that jumps
	// straight into this mode (Ctrl+r, see cmd/nib/main.go) the same way
	// OpenWithQuery jumps straight into content-search mode.
	replace *ReplaceView

	items          []string // file mode: candidate paths, from listFiles
	fileMatches    []scoredItem
	contentMatches []contentMatch // content mode: git grep hits

	status    map[string]gitstatus.Status // keyed by repo-relative path
	query     textField
	cursor    int
	scrollTop int

	// lastListRows is the last Render's visible result-row count (listRows
	// there is otherwise local to Render), kept for ScrollState/ScrollTo —
	// the same "last Render's row count" field every other pane already
	// stores under some name (lastRows, lastHeight).
	lastListRows int

	searching     bool
	searchGen     int // bumped on every state change that invalidates in-flight searches
	debounceTimer *time.Timer

	// OnSelect is called when Enter is pressed on a match: absPath is the
	// chosen file, line is 1-based for a content-mode match or 0 for a
	// file-mode match (meaning "no specific line").
	OnSelect func(absPath string, line int)
	// OnClose is called whenever the modal should be dismissed: Esc, or
	// right after OnSelect fires for a selection.
	OnClose func()
	// Post, if set, delivers a completed content search's SearchResult
	// back through the host application's event loop (see ui.App.Post)
	// instead of running `git grep` synchronously on the UI thread — on a
	// large project that can take real time, and this View has no
	// goroutine of its own to do it off to the side without this. If nil
	// (e.g. in unit tests), searches run synchronously instead.
	Post func(ev interface{})

	keymap map[string]string
}

// New creates a finder rooted at absPath. Call Open each time it's about
// to be shown.
func New(absPath string) *View {
	return &View{root: absPath, keymap: DefaultKeybinds.Resolve(nil), replace: NewReplaceView(absPath)}
}

// Replace returns the replace-mode sub-view, so the host application can
// wire up its callbacks (OnReplaceAll, Post, OnClose) and keymap overrides
// once, up front — see cmd/nib/main.go.
func (v *View) Replace() *ReplaceView {
	return v.replace
}

// SetKeymap merges the user config's "finder" scope overrides on top of
// DefaultKeybinds, replacing the pane's active keymap.
func (v *View) SetKeymap(overrides map[string]string) {
	v.keymap = DefaultKeybinds.Resolve(overrides)
}

func (v *View) Title() string {
	switch v.mode {
	case modeContent:
		return "Find in Files"
	case modeReplace:
		return v.replace.Title()
	default:
		return "Find File"
	}
}

// Open (re)indexes the project's files, resets the query/cursor, and
// always starts back in file-name mode. A fresh index on every open is
// simpler than caching-with-invalidation and fast enough in practice (a
// single `git ls-files` call) — see listFiles.
func (v *View) Open() {
	v.cancelPendingSearch()
	v.items = listFiles(v.root)
	v.mode = modeFiles
	v.query = textField{}
	v.cursor = 0
	v.scrollTop = 0
	v.refilter()
}

// OpenWithQuery opens the finder already in content-search mode with
// query pre-filled and searched — used by the global "find references"
// action (Ctrl+F, see cmd/nib/main.go's openFindReferences and
// editor.View.WordUnderCursor) so the user doesn't have to retype the
// identifier under their cursor.
func (v *View) OpenWithQuery(query string) {
	v.Open()
	v.mode = modeContent
	runes := []rune(query)
	v.query = textField{buf: runes, caret: len(runes)}
	v.refilter()
}

// OpenReplace opens the finder already in search-and-replace mode — used
// by the global "find & replace in path" action (Ctrl+R, see cmd/nib/main.go),
// the same direct-to-mode shortcut OpenWithQuery gives content-search mode.
func (v *View) OpenReplace() {
	v.Open()
	v.mode = modeReplace
	v.replace.Open()
}

// ApplyStatus attaches git statuses (repo-relative path -> Status, the
// same direct per-file map the file tree rolls up) so file-mode matches
// show a status marker and color. It's fine to call this before Open,
// after Open, or whenever the caller's own git-status refresh fires —
// Render always reads the current map.
func (v *View) ApplyStatus(direct map[string]gitstatus.Status) {
	v.status = direct
}

// toggleMode cycles Files -> Content -> Replace -> Files. Replace mode
// keeps its own independent state (query, replacement, results — see
// ReplaceView), so entering it always starts fresh via replace.Open(),
// the same way switching into it via OpenReplace does.
func (v *View) toggleMode() {
	v.cancelPendingSearch()
	switch v.mode {
	case modeFiles:
		v.mode = modeContent
	case modeContent:
		v.mode = modeReplace
	default:
		v.mode = modeFiles
	}
	if v.mode == modeReplace {
		v.replace.Open()
		return
	}
	v.cursor = 0
	v.scrollTop = 0
	v.refilter()
}

// cancelPendingSearch stops any pending debounce timer and bumps
// searchGen, so a search already in flight (its git-grep goroutine
// already running) delivers its result to a generation nothing will
// recognize as current anymore — see ApplyContentResult.
func (v *View) cancelPendingSearch() {
	if v.debounceTimer != nil {
		v.debounceTimer.Stop()
		v.debounceTimer = nil
	}
	v.searchGen++
	v.searching = false
}

func (v *View) refilter() {
	switch v.mode {
	case modeContent:
		v.refilterContent()
	default:
		v.refilterFiles()
	}
}

func (v *View) refilterFiles() {
	v.fileMatches = v.fileMatches[:0]
	query := v.query.String()
	for _, it := range v.items {
		score, ok := fuzzyMatch(query, it)
		if !ok {
			continue
		}
		v.fileMatches = append(v.fileMatches, scoredItem{path: it, score: score})
	}
	sort.SliceStable(v.fileMatches, func(i, j int) bool {
		return v.fileMatches[i].score > v.fileMatches[j].score
	})
	v.clampCursor()
}

// refilterContent starts a content search for the current query. It never
// runs `git grep` inline on the caller's goroutine (the UI thread) when
// Post is available: on a large project a single search can take real
// time, and doing that synchronously from HandleKey would freeze
// rendering and input for as long as it takes. Instead:
//  1. Cancel whatever search was pending (debounce).
//  2. Wait contentSearchDebounce for typing to pause, so a fast typist
//     doesn't spawn one git-grep process per keystroke.
//  3. Run the search on its own goroutine and deliver the result back via
//     Post — see ApplyContentResult, which discards it if the user has
//     since changed the query, mode, or closed/reopened the finder.
func (v *View) refilterContent() {
	v.cancelPendingSearch() // stops any prior timer; bumps searchGen for us too
	v.contentMatches = nil

	if len(v.query.buf) < minContentQueryLen {
		v.clampCursor()
		return
	}

	gen := v.searchGen
	query := v.query.String()
	root := v.root

	if v.Post == nil {
		// No async plumbing wired up (e.g. unit tests): search inline.
		v.contentMatches, _ = searchContent(root, query)
		v.clampCursor()
		return
	}

	v.searching = true
	v.debounceTimer = time.AfterFunc(contentSearchDebounce, func() {
		// Runs on its own goroutine, after the debounce delay. Only
		// touches local copies (root, query) and the Post callback —
		// never View fields directly, since those belong to the UI
		// goroutine.
		matches, _ := searchContent(root, query)
		v.Post(SearchResult{gen: gen, matches: matches})
	})
}

// ApplyContentResult applies a content search's result — call this from
// the host application's custom-event handler when it receives a
// finder.SearchResult (see ui.App.SetCustomEventHandler). Results from a
// search superseded by newer input (further typing, a mode switch, or the
// finder being closed and reopened) are silently discarded.
func (v *View) ApplyContentResult(r SearchResult) {
	if r.gen != v.searchGen {
		return
	}
	v.contentMatches = r.matches
	v.searching = false
	v.clampCursor()
}

func (v *View) clampCursor() {
	if n := v.resultCount(); v.cursor >= n {
		v.cursor = n - 1
	}
	if v.cursor < 0 {
		v.cursor = 0
	}
}

func (v *View) resultCount() int {
	if v.mode == modeContent {
		return len(v.contentMatches)
	}
	return len(v.fileMatches)
}

// promptPrefix distinguishes the two modes at a glance: "> " for
// find-by-name (matching typical fuzzy finders), "/ " for find-by-content
// (matching the vi/less "search" prompt).
func (v *View) promptPrefix() string {
	if v.mode == modeContent {
		return "/ "
	}
	return "> "
}

// CursorPosition implements layout.CursorProvider, placing the terminal's
// native cursor right after the typed query on the prompt row — or, in
// replace mode, wherever ReplaceView.CursorPosition puts it.
func (v *View) CursorPosition() (int, int, bool) {
	if v.mode == modeReplace {
		return v.replace.CursorPosition()
	}
	return len(v.promptPrefix()) + v.query.caret, 0, true
}

func (v *View) Render(w layout.Window) {
	if v.mode == modeReplace {
		v.replace.Render(w)
		return
	}

	cols, rows := w.Size()
	w.Clear()

	hint := "(Tab: search content)"
	if v.mode == modeContent {
		hint = "(Tab: find & replace)"
	}
	w.Println(0, layout.Segment{Text: v.promptPrefix() + v.query.String() + "  " + hint})

	listRows := rows - 1
	if listRows < 0 {
		listRows = 0
	}
	v.lastListRows = listRows
	if listRows == 0 {
		return
	}

	if v.mode == modeContent && len(v.query.buf) < minContentQueryLen {
		w.Println(1, layout.Segment{
			Text:  fmt.Sprintf("type at least %d characters to search file contents", minContentQueryLen),
			Style: layout.Style{Attr: layout.AttrDim},
		})
		return
	}
	if v.resultCount() == 0 {
		switch {
		case v.searching:
			w.Println(1, layout.Segment{Text: "searching…", Style: layout.Style{Attr: layout.AttrDim}})
		case len(v.query.buf) > 0:
			w.Println(1, layout.Segment{Text: "no matches", Style: layout.Style{Attr: layout.AttrDim}})
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
		if idx >= v.resultCount() {
			break
		}
		segs := v.rowSegments(idx)
		if idx == v.cursor {
			for j := range segs {
				segs[j].Style.Attr |= layout.AttrReverse
			}
		}
		segs = textwidth.SliceSegmentsByDisplayColumn(segs, 0, cols)
		w.Println(1+i, segs...)
	}
}

// HandleKey always reports the key consumed: a modal should never leak
// input through to whatever is behind it, matched or not.
//
// Close and switch-mode are handled up front, before any mode-specific
// dispatch, so Esc/Tab always close/cycle regardless of mode — including
// out of replace mode, where everything else is delegated wholesale to
// v.replace.HandleKey below rather than threaded through the switches
// that follow (those are files/content-mode-specific: move_down and
// friends act on v.cursor/v.fileMatches/v.contentMatches, none of which
// replace mode uses).
func (v *View) HandleKey(k layout.Key) bool {
	if k.EventType == layout.EventRelease {
		return true
	}

	switch v.keymap[k.String()] {
	case "close":
		if v.OnClose != nil {
			v.OnClose()
		}
		return true
	case "switch_mode":
		v.toggleMode()
		return true
	}

	if v.mode == modeReplace {
		return v.replace.HandleKey(k)
	}

	switch v.keymap[k.String()] {
	case "open_selection":
		v.selectCurrent()
		if v.OnClose != nil {
			v.OnClose()
		}
		return true
	case "move_down":
		v.moveCursor(1)
		return true
	case "move_up":
		v.moveCursor(-1)
		return true
	}

	// Left/Right/Home/End/Backspace and typed text all edit the query's
	// caret directly, checking k.Named first and never consulting
	// v.keymap — the same pattern textField.handleKey already implements
	// for ReplaceView's own Find/Replace fields (replace.go), and
	// handlePromptKey's for the file tree's inline prompt. Only
	// Esc/Tab/Enter/Up/Down are ever remappable actions in this mode (see
	// DefaultKeybinds); every other key stays typeable.
	if v.query.handleKey(k) {
		v.refilter()
	}
	return true
}

// ScrollState implements layout.Scrollable.
func (v *View) ScrollState() layout.ScrollState {
	if v.mode == modeReplace {
		return v.replace.ScrollState()
	}
	return layout.ScrollState{Top: v.scrollTop, Viewport: v.lastListRows, Total: v.resultCount()}
}

// ScrollTo implements layout.ScrollTarget. Like the editor and file tree,
// scrollTop is re-derived from the cursor on every Render ("if v.cursor <
// v.scrollTop ...", "if v.cursor >= v.scrollTop+listRows ..."), so a scroll
// that leaves the cursor outside the new viewport has to move the cursor
// too, or the next Render would silently undo it.
func (v *View) ScrollTo(top int) {
	if v.mode == modeReplace {
		v.replace.ScrollTo(top)
		return
	}
	viewport := v.lastListRows
	total := v.resultCount()
	maxTop := total - viewport
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

	if viewport > 0 {
		if v.cursor < top {
			v.cursor = top
		} else if v.cursor >= top+viewport {
			v.cursor = top + viewport - 1
		}
	}
	if v.cursor < 0 {
		v.cursor = 0
	}
	if v.cursor >= total {
		v.cursor = total - 1
	}
}

func (v *View) selectCurrent() {
	if v.OnSelect == nil || v.cursor < 0 || v.cursor >= v.resultCount() {
		return
	}
	if v.mode == modeContent {
		m := v.contentMatches[v.cursor]
		v.OnSelect(filepath.Join(v.root, m.path), m.line)
		return
	}
	v.OnSelect(filepath.Join(v.root, v.fileMatches[v.cursor].path), 0)
}

func (v *View) moveCursor(delta int) {
	v.cursor += delta
	if v.cursor < 0 {
		v.cursor = 0
	}
	if n := v.resultCount(); v.cursor >= n {
		v.cursor = n - 1
	}
}

// rowSegments builds the full (unclipped) styled segments for result row
// idx — shared between Render's per-row loop and the hScroll clamp, which
// needs to know the SELECTED row's full width regardless of how it'll
// actually be clipped for display. Content-mode rows dim the "path:line: "
// prefix so the matched text itself stands out, matching the file-mode
// rows' git-status coloring convention of styling metadata separately from
// the path/text a user actually reads.
//
// Both modes lead with the same git-status marker column, so a row lines up
// with the other whichever mode you're in — and so "is this match in
// something I've already touched?" is answerable while searching content,
// not only while searching filenames.
func (v *View) rowSegments(idx int) []layout.Segment {
	if v.mode == modeContent {
		m := v.contentMatches[idx]
		status := v.status[m.path]
		prefix := fmt.Sprintf("%s:%d: ", m.path, m.line)
		return []layout.Segment{
			{Text: gitstyle.Marker(status) + " ", Style: gitstyle.Style(status)},
			{Text: prefix, Style: layout.Style{Attr: layout.AttrDim}},
			{Text: m.text},
		}
	}
	item := v.fileMatches[idx]
	status := v.status[item.path]
	return []layout.Segment{{Text: gitstyle.Marker(status) + " " + item.path, Style: gitstyle.Style(status)}}
}

// rowSegmentsText joins rowSegments' text with no styling, for width
// measurement (see the hScroll clamp in Render).
func rowSegmentsText(segs []layout.Segment) string {
	text := ""
	for _, s := range segs {
		text += s.Text
	}
	return text
}
