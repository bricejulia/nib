// Package finder is a fuzzy file finder meant to be shown as a modal
// overlay (see ui.App.ShowOverlay/CloseOverlay). It has two modes,
// switched with Tab: find a file by name (fuzzy match), or find a line by
// its content (via `git grep`). Up/Down moves the selection, Enter opens
// it, Esc closes without opening anything.
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
	{Trigger: "Right", Action: "peek_right"},
	{Trigger: "Left", Action: "peek_left"},
	{Trigger: "Backspace", Action: "backspace"},
}

// hScrollStep is how many display columns Left/Right shift the view of a
// result line that's wider than the modal — Left/Right are otherwise
// unused in the finder (unlike the file tree, where they expand/collapse),
// so they're free for "peek right/left to see the rest of this long
// path/line" instead.
const hScrollStep = 10

type mode int

const (
	modeFiles mode = iota
	modeContent
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

	items          []string // file mode: candidate paths, from listFiles
	fileMatches    []scoredItem
	contentMatches []contentMatch // content mode: git grep hits

	status    map[string]gitstatus.Status // keyed by repo-relative path
	query     []rune
	cursor    int
	scrollTop int
	hScroll   int // display columns; how far the selected row is peeked right

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
	return &View{root: absPath, keymap: DefaultKeybinds.Resolve(nil)}
}

// SetKeymap merges the user config's "finder" scope overrides on top of
// DefaultKeybinds, replacing the pane's active keymap.
func (v *View) SetKeymap(overrides map[string]string) {
	v.keymap = DefaultKeybinds.Resolve(overrides)
}

func (v *View) Title() string {
	if v.mode == modeContent {
		return "Find in Files"
	}
	return "Find File"
}

// Open (re)indexes the project's files, resets the query/cursor, and
// always starts back in file-name mode. A fresh index on every open is
// simpler than caching-with-invalidation and fast enough in practice (a
// single `git ls-files` call) — see listFiles.
func (v *View) Open() {
	v.cancelPendingSearch()
	v.items = listFiles(v.root)
	v.mode = modeFiles
	v.query = v.query[:0]
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
	v.query = []rune(query)
	v.refilter()
}

// ApplyStatus attaches git statuses (repo-relative path -> Status, the
// same direct per-file map the file tree rolls up) so file-mode matches
// show a status marker and color. It's fine to call this before Open,
// after Open, or whenever the caller's own git-status refresh fires —
// Render always reads the current map.
func (v *View) ApplyStatus(direct map[string]gitstatus.Status) {
	v.status = direct
}

func (v *View) toggleMode() {
	v.cancelPendingSearch()
	if v.mode == modeFiles {
		v.mode = modeContent
	} else {
		v.mode = modeFiles
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
	v.hScroll = 0 // a changed result set means "start peeking from the left again"
	switch v.mode {
	case modeContent:
		v.refilterContent()
	default:
		v.refilterFiles()
	}
}

func (v *View) refilterFiles() {
	v.fileMatches = v.fileMatches[:0]
	query := string(v.query)
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

	if len(v.query) < minContentQueryLen {
		v.clampCursor()
		return
	}

	gen := v.searchGen
	query := string(v.query)
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
// native cursor right after the typed query on the prompt row.
func (v *View) CursorPosition() (int, int, bool) {
	return len(v.promptPrefix()) + len(v.query), 0, true
}

func (v *View) Render(w layout.Window) {
	cols, rows := w.Size()
	w.Clear()

	hint := "(Tab: search content, ←→: see full line)"
	if v.mode == modeContent {
		hint = "(Tab: search files, ←→: see full line)"
	}
	w.Println(0, layout.Segment{Text: v.promptPrefix() + string(v.query) + "  " + hint})

	listRows := rows - 1
	if listRows < 0 {
		listRows = 0
	}
	v.lastListRows = listRows
	if listRows == 0 {
		return
	}

	if v.mode == modeContent && len(v.query) < minContentQueryLen {
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
		case len(v.query) > 0:
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

	// hScroll is a manual "peek right" offset (via Left/Right), clamped
	// each frame against the SELECTED row's actual width — so you can
	// never scroll past the end of the very line you're looking at, the
	// same policy the editor pane uses for its own horizontal scroll.
	if v.cursor >= 0 && v.cursor < v.resultCount() {
		selectedText := rowSegmentsText(v.rowSegments(v.cursor))
		v.hScroll = textwidth.ClampScroll(v.hScroll, textwidth.DisplayWidth(selectedText), cols)
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
		segs = textwidth.SliceSegmentsByDisplayColumn(segs, v.hScroll, cols)
		w.Println(1+i, segs...)
	}
}

// HandleKey always reports the key consumed: a modal should never leak
// input through to whatever is behind it, matched or not.
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
	case "peek_right":
		v.hScroll += hScrollStep // clamped against the selected row's width in Render
		return true
	case "peek_left":
		v.hScroll -= hScrollStep
		if v.hScroll < 0 {
			v.hScroll = 0
		}
		return true
	case "backspace":
		if len(v.query) > 0 {
			v.query = v.query[:len(v.query)-1]
			v.refilter()
		}
		return true
	}

	// No bound action for this exact key: fall through to typing it into
	// the query, as long as it's plain text with no Ctrl/Alt/Super held —
	// this is what lets a "finder" scope override stick to Ctrl/Alt/Super
	// combos (or named keys) without ever swallowing normal typing.
	if k.Text != "" && k.Mods&(layout.ModCtrl|layout.ModAlt|layout.ModSuper) == 0 {
		v.query = append(v.query, []rune(k.Text)...)
		v.refilter()
	}
	return true
}

// ScrollState implements layout.Scrollable.
func (v *View) ScrollState() layout.ScrollState {
	return layout.ScrollState{Top: v.scrollTop, Viewport: v.lastListRows, Total: v.resultCount()}
}

// ScrollTo implements layout.ScrollTarget. Like the editor and file tree,
// scrollTop is re-derived from the cursor on every Render ("if v.cursor <
// v.scrollTop ...", "if v.cursor >= v.scrollTop+listRows ..."), so a scroll
// that leaves the cursor outside the new viewport has to move the cursor
// too, or the next Render would silently undo it.
func (v *View) ScrollTo(top int) {
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
	v.hScroll = 0 // peeking is per-row: start from the left on the new selection, same as moveCursor
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
	v.hScroll = 0 // peeking is per-row: start from the left on the new selection
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
