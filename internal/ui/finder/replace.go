package finder

import (
	"fmt"
	"path/filepath"
	"time"
	"unicode"

	"github.com/bricejulia/kiwi/internal/config"
	"github.com/bricejulia/kiwi/internal/layout"
	"github.com/bricejulia/kiwi/internal/textwidth"
	"github.com/bricejulia/kiwi/internal/ui/editor"
)

// ReplaceDefaultKeybinds are the replace-in-path overlay's built-in
// keybindings, overridable via the user config's "replace" scope (see
// internal/config). Deliberately narrow, like finder's own DefaultKeybinds:
// these only ever apply once focus is on the results list (see
// ReplaceView.HandleKey) — while typing into the Find/Replace fields,
// every character stays typeable the same way filetree's in-pane prompt
// keeps every character typeable, and Esc/Tab are structural to the
// pane (close / switch field) rather than remappable actions, so neither
// is listed here.
var ReplaceDefaultKeybinds = config.Defaults{
	{Trigger: "Down", Action: "move_down"},
	{Trigger: "Up", Action: "move_up"},
	{Trigger: "Space", Action: "toggle"},
	{Trigger: "Enter", Action: "replace_current"},
	{Trigger: "a", Action: "replace_all"},
}

// replaceHeaderRows is how many rows sit above the scrollable results
// list: the Find field, the Replace field, and a status/hint line.
const replaceHeaderRows = 3

// inputFocus is which part of the pane a keystroke currently goes to.
type inputFocus int

const (
	focusFind inputFocus = iota
	focusReplace
	focusResults
)

// textField is a single-line, always-typeable text input — the same shape
// filetree's in-pane prompt uses (a []rune buffer plus a caret index,
// prompt.go), factored out here since ReplaceView needs two independent
// instances (Find: and Replace:) rather than filetree's one.
type textField struct {
	buf   []rune
	caret int
}

func (f *textField) String() string { return string(f.buf) }

// textBeforeCaret is what CursorPosition measures the terminal caret's
// column from, mirroring filetree.View.CursorPosition's own caretCol
// computation.
func (f *textField) textBeforeCaret() string { return string(f.buf[:f.caret]) }

// handleKey edits the field in place, reporting whether it consumed k.
// Named keys it doesn't itself handle (Tab, Enter, Esc, arrows-that-aren't-
// Left/Right, paging) are left unconsumed so the caller can act on them —
// exactly how filetree's prompt reserves Esc for itself while still typing
// every other character.
func (f *textField) handleKey(k layout.Key) bool {
	switch k.Named {
	case layout.KeyBackspace:
		if f.caret > 0 {
			f.buf = append(f.buf[:f.caret-1], f.buf[f.caret:]...)
			f.caret--
		}
		return true
	case layout.KeyLeft:
		if f.caret > 0 {
			f.caret--
		}
		return true
	case layout.KeyRight:
		if f.caret < len(f.buf) {
			f.caret++
		}
		return true
	case layout.KeyHome:
		f.caret = 0
		return true
	case layout.KeyEnd:
		f.caret = len(f.buf)
		return true
	}
	// Any other named key (Tab, Enter, Esc, Up/Down, paging) is left to the
	// caller. Space is the exception: App's translateKey promotes it to a
	// Named value while leaving Text intact, so without this a space would
	// never make it into typed text.
	if k.Named != "" && k.Named != layout.KeySpace {
		return false
	}
	if k.Text == "" || k.Mods&(layout.ModCtrl|layout.ModAlt|layout.ModSuper) != 0 {
		return false
	}
	for _, r := range k.Text {
		if !unicode.IsPrint(r) {
			continue
		}
		f.buf = append(f.buf, 0)
		copy(f.buf[f.caret+1:], f.buf[f.caret:])
		f.buf[f.caret] = r
		f.caret++
	}
	return true
}

// replaceRow is one row of ReplaceView's flattened, occurrence-level
// results list — either a file header (one per matching file, its own
// checkbox toggling every occurrence beneath it) or a single occurrence
// (one per case-insensitive hit of the query on a matched line, per
// editor.FindOccurrences — a line with the query twice gets two
// independently-checkable rows, not one).
type replaceRow struct {
	isFile  bool
	path    string
	line    int    // occurrence rows only, 1-based
	ordinal int    // occurrence rows only, 0-based, per editor.FindOccurrences
	text    string // occurrence rows only, the real (untrimmed) line text
	checked bool
}

// ReplaceSearchResult is a content search's result delivered back through
// the host application's Post — the same async pattern finder.SearchResult
// already uses (see View.refilterContent), as a distinct type since this
// pane feeds an occurrence-expansion step finder.View has no use for.
type ReplaceSearchResult struct {
	gen     int
	matches []contentMatch
}

// ReplaceView is the "Find & Replace in Path" overlay: a literal,
// project-wide search (reusing searchContent/contentMatch unchanged) whose
// results are expanded to one row per occurrence and individually
// checkable, with a second field for what to replace them with.
type ReplaceView struct {
	root string

	find    textField
	replace textField
	focus   inputFocus

	matches []contentMatch // raw searchContent results, one per matched line
	rows    []replaceRow   // flattened: file headers + occurrence rows

	cursor       int
	scrollTop    int
	hScroll      int
	lastListRows int

	searching     bool
	searchGen     int
	debounceTimer *time.Timer

	resultShown bool
	result      editor.Result

	// OnClose is called whenever the modal should be dismissed: Esc, or
	// after a caller-driven ShowResult is itself dismissed with Esc.
	OnClose func()
	// OnReplaceAll is called with the search/replacement strings and every
	// CHECKED occurrence (or, from "replace_current", just the one under
	// the cursor) — the host application (cmd/kiwi/main.go) is what
	// actually knows how to reach open editor panes, so it owns calling
	// editor.Apply and reporting back via ShowResult.
	OnReplaceAll func(search, replacement string, occurrences []editor.Occurrence)
	// Post, if set, delivers a completed search's ReplaceSearchResult back
	// through the host application's event loop, exactly like finder.View's
	// own Post. If nil (e.g. in unit tests), searches run synchronously.
	Post func(ev interface{})

	keymap map[string]string
}

// NewReplaceView creates a replace-in-path view rooted at absPath. Call
// Open each time it's about to be shown.
func NewReplaceView(absPath string) *ReplaceView {
	return &ReplaceView{root: absPath, keymap: ReplaceDefaultKeybinds.Resolve(nil)}
}

// SetKeymap merges the user config's "replace" scope overrides on top of
// ReplaceDefaultKeybinds, replacing the pane's active keymap.
func (v *ReplaceView) SetKeymap(overrides map[string]string) {
	v.keymap = ReplaceDefaultKeybinds.Resolve(overrides)
}

func (v *ReplaceView) Title() string { return "Find & Replace in Path" }

// Open resets the view to a blank query, ready to be shown.
func (v *ReplaceView) Open() {
	v.cancelPendingSearch()
	v.find = textField{}
	v.replace = textField{}
	v.focus = focusFind
	v.matches = nil
	v.rows = nil
	v.cursor = 0
	v.scrollTop = 0
	v.hScroll = 0
	v.resultShown = false
	v.result = editor.Result{}
}

// ShowResult transitions the view to its summary phase — called by the
// host application once its OnReplaceAll callback has actually run
// editor.Apply, so the view can report what happened without needing to
// know anything about editor panes itself.
func (v *ReplaceView) ShowResult(res editor.Result) {
	v.result = res
	v.resultShown = true
}

func (v *ReplaceView) cancelPendingSearch() {
	if v.debounceTimer != nil {
		v.debounceTimer.Stop()
		v.debounceTimer = nil
	}
	v.searchGen++
	v.searching = false
}

// refilter re-runs the content search for the current Find text, following
// the exact debounce/Post/searchGen pattern finder.View.refilterContent
// uses — see that function's doc comment for the full rationale.
func (v *ReplaceView) refilter() {
	v.cancelPendingSearch()
	v.matches = nil
	v.rows = nil
	v.cursor = 0
	v.scrollTop = 0

	query := v.find.String()
	if len(query) < minContentQueryLen {
		return
	}

	gen := v.searchGen
	root := v.root
	if v.Post == nil {
		matches, _ := searchContent(root, query)
		v.matches = matches
		v.rebuildRows()
		return
	}

	v.searching = true
	v.debounceTimer = time.AfterFunc(contentSearchDebounce, func() {
		matches, _ := searchContent(root, query)
		v.Post(ReplaceSearchResult{gen: gen, matches: matches})
	})
}

// ApplyReplaceSearchResult applies a completed search's result — call this
// from the host application's custom-event handler when it receives a
// finder.ReplaceSearchResult. A result superseded by newer input (further
// typing, or the view being closed and reopened) is silently discarded.
func (v *ReplaceView) ApplyReplaceSearchResult(r ReplaceSearchResult) {
	if r.gen != v.searchGen {
		return
	}
	v.matches = r.matches
	v.searching = false
	v.rebuildRows()
}

// rebuildRows expands v.matches (one entry per matched LINE) into the
// occurrence-level, file-grouped list ReplaceView actually displays and
// acts on: git grep reports a line once regardless of how many times the
// query appears on it, so each line is re-scanned client-side (via
// editor.FindOccurrences, the same scan rewriteLine uses at apply time, so
// the two can never disagree about a line's occurrence count) into one row
// per occurrence. A file header is only ever emitted once it actually has
// at least one occurrence row — a match whose line no longer contains the
// query by the time this runs (a narrow Unicode case-folding difference
// between git's and Go's case-insensitivity, in practice) simply produces
// no rows, rather than a header with nothing under it.
func (v *ReplaceView) rebuildRows() {
	query := v.find.String()

	type group struct {
		path string
		rows []replaceRow
	}
	var groups []group
	for _, m := range v.matches {
		offsets := editor.FindOccurrences(m.text, query)
		if len(offsets) == 0 {
			continue
		}
		if len(groups) == 0 || groups[len(groups)-1].path != m.path {
			groups = append(groups, group{path: m.path})
		}
		g := &groups[len(groups)-1]
		for i := range offsets {
			g.rows = append(g.rows, replaceRow{path: m.path, line: m.line, ordinal: i, text: m.text, checked: true})
		}
	}

	rows := make([]replaceRow, 0, len(v.matches)*2)
	for _, g := range groups {
		rows = append(rows, replaceRow{isFile: true, path: g.path, checked: true})
		rows = append(rows, g.rows...)
	}
	v.rows = rows
	v.clampCursor()
}

func (v *ReplaceView) clampCursor() {
	if v.cursor >= len(v.rows) {
		v.cursor = len(v.rows) - 1
	}
	if v.cursor < 0 {
		v.cursor = 0
	}
}

func (v *ReplaceView) moveCursor(delta int) {
	v.cursor += delta
	v.clampCursor()
	v.hScroll = 0
}

// toggleCurrent flips the checked state of the row under the cursor. A
// file row flips every occurrence beneath it to match; an occurrence row
// flips just itself, and its file row's own state becomes "checked only if
// every occurrence under it now is" — binary, no indeterminate/half-checked
// glyph, matching the codebase's existing strictly-binary status glyphs
// (gitstyle.Marker, the editor's filled/hollow LSP-status dot).
func (v *ReplaceView) toggleCurrent() {
	if v.cursor < 0 || v.cursor >= len(v.rows) {
		return
	}
	row := &v.rows[v.cursor]
	if row.isFile {
		row.checked = !row.checked
		for i := v.cursor + 1; i < len(v.rows) && !v.rows[i].isFile; i++ {
			v.rows[i].checked = row.checked
		}
		return
	}
	row.checked = !row.checked
	v.syncFileChecked(row.path)
}

// syncFileChecked recomputes path's file-header row's checked state from
// its occurrence rows, after one of them was toggled individually.
func (v *ReplaceView) syncFileChecked(path string) {
	for i := range v.rows {
		if !v.rows[i].isFile || v.rows[i].path != path {
			continue
		}
		allChecked := true
		for j := i + 1; j < len(v.rows) && !v.rows[j].isFile; j++ {
			if !v.rows[j].checked {
				allChecked = false
				break
			}
		}
		v.rows[i].checked = allChecked
		return
	}
}

// replaceCurrent fires OnReplaceAll for just the occurrence under the
// cursor, immediately — the "Replace" half of JetBrains' Replace/Replace
// All split. A no-op on a file header row (toggle it and use replace_all
// instead) or with no rows at all.
func (v *ReplaceView) replaceCurrent() {
	if v.cursor < 0 || v.cursor >= len(v.rows) {
		return
	}
	row := v.rows[v.cursor]
	if row.isFile {
		return
	}
	v.fireReplace([]editor.Occurrence{v.occurrenceFor(row)})
}

// replaceAll fires OnReplaceAll for every currently checked occurrence.
func (v *ReplaceView) replaceAll() {
	var occs []editor.Occurrence
	for _, row := range v.rows {
		if row.isFile || !row.checked {
			continue
		}
		occs = append(occs, v.occurrenceFor(row))
	}
	v.fireReplace(occs)
}

func (v *ReplaceView) occurrenceFor(row replaceRow) editor.Occurrence {
	return editor.Occurrence{AbsPath: filepath.Join(v.root, row.path), Line: row.line, Ordinal: row.ordinal}
}

func (v *ReplaceView) fireReplace(occs []editor.Occurrence) {
	if v.OnReplaceAll == nil || len(occs) == 0 {
		return
	}
	v.OnReplaceAll(v.find.String(), v.replace.String(), occs)
}

// HandleKey always reports the key consumed: a modal should never leak
// input through to whatever is behind it.
//
// Esc and Tab are structural to this pane (close / switch field) rather
// than remappable actions — the same trade filetree's in-pane prompt makes
// for Esc — so they're intercepted before v.keymap is ever consulted, and
// aren't listed in ReplaceDefaultKeybinds. Everything else routes to
// whichever field has focus (typing, never through the keymap — see
// textField.handleKey) or, once focus is on the results list, through
// v.keymap.
func (v *ReplaceView) HandleKey(k layout.Key) bool {
	if k.EventType == layout.EventRelease {
		return true
	}

	if v.resultShown {
		if k.Named == layout.KeyEsc && v.OnClose != nil {
			v.OnClose()
		}
		return true
	}

	switch k.Named {
	case layout.KeyEsc:
		if v.OnClose != nil {
			v.OnClose()
		}
		return true
	case layout.KeyTab:
		v.cycleFocus()
		return true
	}

	if v.focus != focusResults {
		field := &v.find
		if v.focus == focusReplace {
			field = &v.replace
		}
		if field.handleKey(k) && v.focus == focusFind {
			v.refilter()
		}
		return true
	}

	switch v.keymap[k.String()] {
	case "toggle":
		v.toggleCurrent()
	case "replace_current":
		v.replaceCurrent()
	case "replace_all":
		v.replaceAll()
	case "move_down":
		v.moveCursor(1)
	case "move_up":
		v.moveCursor(-1)
	}
	return true
}

func (v *ReplaceView) cycleFocus() {
	switch v.focus {
	case focusFind:
		v.focus = focusReplace
	case focusReplace:
		v.focus = focusResults
	default:
		v.focus = focusFind
	}
}

// CursorPosition implements layout.CursorProvider: the terminal's native
// cursor sits in whichever text field has focus, and is hidden entirely
// once focus is on the results list (the selected row's reverse video is
// the FocusResults indicator, the same convention the file tree and finder
// itself use) or while showing a replace summary.
func (v *ReplaceView) CursorPosition() (int, int, bool) {
	if v.resultShown {
		return 0, 0, false
	}
	switch v.focus {
	case focusFind:
		return textwidth.DisplayWidth(findLabel + v.find.textBeforeCaret()), 0, true
	case focusReplace:
		return textwidth.DisplayWidth(replaceLabel + v.replace.textBeforeCaret()), 1, true
	default:
		return 0, 0, false
	}
}

const (
	findLabel    = "Find:    "
	replaceLabel = "Replace: "
)

func (v *ReplaceView) Render(w layout.Window) {
	cols, rows := w.Size()
	w.Clear()

	w.Println(0, layout.Segment{Text: findLabel + v.find.String(), Style: fieldStyle(v.focus == focusFind)})
	w.Println(1, layout.Segment{Text: replaceLabel + v.replace.String(), Style: fieldStyle(v.focus == focusReplace)})

	if v.resultShown {
		v.renderResult(w, 2)
		return
	}
	w.Println(2, layout.Segment{Text: v.statusLine(), Style: layout.Style{Attr: layout.AttrDim}})

	listRows := rows - replaceHeaderRows
	if listRows < 0 {
		listRows = 0
	}
	v.lastListRows = listRows
	if listRows == 0 || len(v.rows) == 0 {
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

	if v.cursor >= 0 && v.cursor < len(v.rows) {
		selectedText := rowSegmentsText(v.rowSegments(v.cursor))
		v.hScroll = textwidth.ClampScroll(v.hScroll, textwidth.DisplayWidth(selectedText), cols)
	}

	for i := 0; i < listRows; i++ {
		idx := v.scrollTop + i
		if idx >= len(v.rows) {
			break
		}
		segs := v.rowSegments(idx)
		if idx == v.cursor && v.focus == focusResults {
			for j := range segs {
				segs[j].Style.Attr |= layout.AttrReverse
			}
		}
		segs = textwidth.SliceSegmentsByDisplayColumn(segs, v.hScroll, cols)
		w.Println(replaceHeaderRows+i, segs...)
	}
}

func fieldStyle(focused bool) layout.Style {
	if focused {
		return layout.Style{Attr: layout.AttrBold}
	}
	return layout.Style{}
}

// checkMarker mirrors the editor's own filled/hollow LSP-status dot
// convention (●/○) rather than a bracket checkbox, since that pairing is
// already this codebase's established "on/off" glyph.
func checkMarker(checked bool) string {
	if checked {
		return "●"
	}
	return "○"
}

func (v *ReplaceView) rowSegments(idx int) []layout.Segment {
	row := v.rows[idx]
	if row.isFile {
		return []layout.Segment{{Text: checkMarker(row.checked) + " " + row.path, Style: layout.Style{Attr: layout.AttrBold}}}
	}
	prefix := fmt.Sprintf("  %s %d: ", checkMarker(row.checked), row.line)
	return []layout.Segment{
		{Text: prefix, Style: layout.Style{Attr: layout.AttrDim}},
		{Text: row.text},
	}
}

func (v *ReplaceView) counts() (files, occurrences int) {
	for _, r := range v.rows {
		if r.isFile {
			files++
		} else {
			occurrences++
		}
	}
	return
}

func (v *ReplaceView) statusLine() string {
	switch {
	case len(v.find.String()) < minContentQueryLen:
		return fmt.Sprintf("type at least %d characters to search", minContentQueryLen)
	case v.searching:
		return "searching…"
	case len(v.rows) == 0:
		if len(v.find.String()) > 0 {
			return "no matches"
		}
		return ""
	default:
		files, occs := v.counts()
		return fmt.Sprintf("%d occurrence(s) in %d file(s) — Tab: switch field, Space: toggle, Enter: replace one, a: replace all",
			occs, files)
	}
}

func (v *ReplaceView) renderResult(w layout.Window, row int) {
	lines := []string{fmt.Sprintf("Replaced %d occurrence(s).", v.result.Replaced)}
	if len(v.result.Skipped) > 0 {
		lines = append(lines, fmt.Sprintf("Skipped %d (no longer found at replace time).", len(v.result.Skipped)))
	}
	if len(v.result.Failed) > 0 {
		lines = append(lines, fmt.Sprintf("Failed to write %d file(s):", len(v.result.Failed)))
		for path, err := range v.result.Failed {
			lines = append(lines, fmt.Sprintf("  %s: %v", path, err))
		}
	}
	lines = append(lines, "(Esc to close)")
	for i, l := range lines {
		w.Println(row+i, layout.Segment{Text: l})
	}
}

// ScrollState implements layout.Scrollable. No bar while showing the
// replace summary or with no results — there's nothing worth scrolling.
func (v *ReplaceView) ScrollState() layout.ScrollState {
	if v.resultShown || len(v.rows) == 0 {
		return layout.ScrollState{}
	}
	return layout.ScrollState{Top: v.scrollTop, Viewport: v.lastListRows, Total: len(v.rows), RowOffset: replaceHeaderRows}
}

// ScrollTo implements layout.ScrollTarget. Like finder.View's own ScrollTo,
// scrollTop is re-derived from the cursor on every Render, so a scroll
// that leaves the cursor outside the new viewport has to move the cursor
// too, or the next Render would silently undo it.
func (v *ReplaceView) ScrollTo(top int) {
	viewport := v.lastListRows
	total := len(v.rows)
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
	v.clampCursor()
	v.hScroll = 0
}
