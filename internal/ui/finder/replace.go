package finder

import (
	"fmt"
	"path/filepath"
	"time"

	"github.com/bricejulia/nib/internal/config"
	"github.com/bricejulia/nib/internal/layout"
	"github.com/bricejulia/nib/internal/textwidth"
	"github.com/bricejulia/nib/internal/ui/editor"
	"github.com/bricejulia/nib/internal/ui/textfield"
)

// ReplaceScope is find & replace mode's actions and their default keys,
// overridable via the user config's "replace" scope (see internal/config).
// All but "focus_scope" only apply once focus is on the results list (the
// "browse" state, see ReplaceView.HandleKey): while typing into the
// Find/Replace/Scope fields every character stays typeable, and Esc/Enter/
// Tab are structural there (leave the field / next field) rather than
// remappable actions.
var ReplaceScope = config.Scope{Name: "replace", Title: "Find & Replace", Actions: config.Actions{
	{ID: "move_down", Keys: []string{"Down", "j"}, Desc: "Move selection down (results list)"},
	{ID: "move_up", Keys: []string{"Up", "k"}, Desc: "Move selection up (results list)"},
	{ID: "edit_query", Keys: []string{"/", "i"}, Desc: "Back to the Find field (results list)"},
	{ID: "toggle", Keys: []string{"Space"}, Desc: "Toggle the occurrence, or a whole file's occurrences"},
	{ID: "replace_current", Keys: []string{"Enter"}, Desc: "Replace just this occurrence (results list)"},
	{ID: "replace_all", Keys: []string{"a"}, Desc: "Replace every checked occurrence"},
	// See finder.Scope' own "focus_scope" binding for why Ctrl+k
	// rather than Ctrl+g or Ctrl+l.
	{ID: "focus_scope", Keys: []string{"Ctrl+k"}, Desc: "Edit the folder scope (Enter returns to Find)"},
}}

// replaceHeaderRows is how many rows sit above the scrollable results
// list: the Find field, the Replace field, the scope field, and a
// status/hint line.
const replaceHeaderRows = 4

// inputFocus is which part of the pane a keystroke currently goes to.
// focusReplaceScope is deliberately NOT part of cycleFocus's
// Find/Replace/Results rotation (Tab/Enter never land on it) — it's
// reached only via the "focus_scope" action, the same way finder.View's
// own scope field (a distinct type, viewFocus, hence the different name)
// is reached via a dedicated toggle rather than folded into its
// Tab-driven mode switching.
type inputFocus int

const (
	focusFind inputFocus = iota
	focusReplace
	focusResults
	focusReplaceScope
)

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

	find       textfield.TextField
	replace    textfield.TextField
	scopeField textfield.TextField // "" = whole project; see SetScope, currentScope
	focus      inputFocus

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
	// the cursor) — the host application (cmd/nib/main.go) is what
	// actually knows how to reach open editor panes, so it owns calling
	// editor.Apply and reporting back via ShowResult.
	OnReplaceAll func(search, replacement string, occurrences []editor.Occurrence)
	// Post, if set, delivers a completed search's ReplaceSearchResult back
	// through the host application's event loop, exactly like finder.View's
	// own Post. If nil (e.g. in unit tests), searches run synchronously.
	Post func(ev interface{})

	keymap map[string]string

	// docked mirrors finder.View's own (see View.SetDocked): keys nothing
	// here used are reported unconsumed instead of swallowed.
	docked bool
}

// NewReplaceView creates a replace-in-path view rooted at absPath. Call
// Open each time it's about to be shown.
func NewReplaceView(absPath string) *ReplaceView {
	return &ReplaceView{root: absPath, keymap: ReplaceScope.Actions.Resolve(nil)}
}

// SetKeymap merges the user config's "replace" scope overrides on top of
// ReplaceScope, replacing the pane's active keymap.
func (v *ReplaceView) SetKeymap(overrides map[string]string) {
	v.keymap = ReplaceScope.Actions.Resolve(overrides)
}

func (v *ReplaceView) Title() string {
	if scope := v.currentScope(); scope != "" {
		return "Find & Replace in Path — " + scope
	}
	return "Find & Replace in Path"
}

// SetScope sets the scope field's starting text — called from
// finder.View (setMode, openReplace) right before Open, so a scope set
// in file/content mode, or pre-filled via OpenScoped/OpenReplaceScoped,
// carries forward into replace mode as this view's own, independently
// editable, starting value. A no-op call with "" (the default) leaves
// replace mode unscoped, exactly like today.
func (v *ReplaceView) SetScope(scope string) {
	v.scopeField = textfield.New(scope)
}

// currentScope is the scope field's text, normalized — "" means "whole
// project". Read fresh on every search rather than cached, since the
// field is editable for the life of the popup.
func (v *ReplaceView) currentScope() string {
	return normalizeScope(v.scopeField.String())
}

// SetQuery pre-fills the Find field with q, caret at the end, and starts
// the search for it — called right after Open, e.g. with the word under
// the editor's cursor (see View.OpenReplaceWithQueryScoped).
func (v *ReplaceView) SetQuery(q string) {
	v.find = textfield.New(q)
	v.refilter()
}

// Open resets the view to a blank query, ready to be shown.
func (v *ReplaceView) Open() {
	v.cancelPendingSearch()
	v.find = textfield.TextField{}
	v.replace = textfield.TextField{}
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
	scope := v.currentScope()
	if v.Post == nil {
		matches, _ := searchContent(root, scope, query, false)
		v.matches = matches
		v.rebuildRows()
		return
	}

	v.searching = true
	v.debounceTimer = time.AfterFunc(contentSearchDebounce, func() {
		matches, _ := searchContent(root, scope, query, false)
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

// HandleKey routes a key by focus. The text fields (Find, Replace, scope)
// are this mode's "typing" state: every printable key is typed, Enter
// advances to the next field, and Esc drops to the results list — the
// "browse" state, where keys go through v.keymap (Space, Enter, a, j/k, and
// "/"/"i" back to the Find field) and Esc closes. finder.View checks its own
// next_view/prev_view keys first while Browsing, so "[" and "]" cycle views
// from the results list but stay typeable in the fields.
//
// Tab cycles fields as an overlay; docked it is left to the global keymap
// (next pane), the same as every other key nothing here used. As an
// overlay every key is reported consumed — a modal never leaks input.
func (v *ReplaceView) HandleKey(k layout.Key) bool {
	if k.EventType == layout.EventRelease {
		return true
	}

	if v.resultShown {
		if k.Named == layout.KeyEsc {
			if v.OnClose != nil {
				v.OnClose()
			}
			return true
		}
		return !v.docked
	}

	switch k.Named {
	case layout.KeyEsc:
		if v.focus == focusResults {
			if v.OnClose != nil {
				v.OnClose()
			}
		} else {
			v.focus = focusResults
		}
		return true
	case layout.KeyTab:
		if v.docked {
			return false
		}
		v.cycleFocus()
		return true
	}

	if v.keymap[k.String()] == "focus_scope" {
		if v.focus == focusReplaceScope {
			v.focus = focusFind
		} else {
			v.focus = focusReplaceScope
		}
		return true
	}

	if v.focus == focusReplaceScope {
		if k.Named == layout.KeyEnter {
			v.focus = focusFind
			return true
		}
		if v.scopeField.HandleKey(k) {
			v.refilter()
			return true
		}
		return !v.docked
	}

	if v.focus != focusResults {
		// Enter advances Find -> Replace -> Results, the same as Tab does
		// as an overlay.
		if k.Named == layout.KeyEnter {
			v.cycleFocus()
			return true
		}
		field := &v.find
		if v.focus == focusReplace {
			field = &v.replace
		}
		if !field.HandleKey(k) {
			return !v.docked
		}
		if v.focus == focusFind {
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
	case "edit_query":
		v.focus = focusFind
	default:
		return !v.docked
	}
	return true
}

// Browsing reports whether focus is on the results list (or the
// post-replace summary) rather than a text field — the state in which
// finder.View's next_view/prev_view keys apply. See HandleKey.
func (v *ReplaceView) Browsing() bool {
	return v.resultShown || v.focus == focusResults
}

// EditFind moves focus back to the Find field, keeping its text.
func (v *ReplaceView) EditFind() {
	v.focus = focusFind
}

// Browse moves focus to the results list without touching anything else —
// used when the panel cycles into this mode with "[" / "]".
func (v *ReplaceView) Browse() {
	v.focus = focusResults
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
		return textwidth.DisplayWidth(findLabel + v.find.TextBeforeCaret()), 0, true
	case focusReplace:
		return textwidth.DisplayWidth(replaceLabel + v.replace.TextBeforeCaret()), 1, true
	case focusReplaceScope:
		return textwidth.DisplayWidth(scopeLabel + v.scopeField.TextBeforeCaret()), 2, true
	default:
		return 0, 0, false
	}
}

const (
	findLabel    = "Find:    "
	replaceLabel = "Replace: "
)

// scopeRowSegments mirrors finder.View's own scopeRowSegments (see
// view.go) — duplicated rather than shared since the two panes' focus
// enums differ, but kept in lockstep with it.
func (v *ReplaceView) scopeRowSegments() []layout.Segment {
	text := v.scopeField.String()
	focused := v.focus == focusReplaceScope
	if text == "" && !focused {
		return []layout.Segment{{
			Text:  "(scope: whole project — Ctrl+K to set)",
			Style: layout.Style{Attr: layout.AttrDim},
		}}
	}
	style := layout.Style{}
	if focused {
		style.Attr = layout.AttrBold
	}
	return []layout.Segment{{Text: scopeLabel + text, Style: style}}
}

func (v *ReplaceView) Render(w layout.Window) {
	cols, rows := w.Size()
	w.Clear()

	w.Println(0, layout.Segment{Text: findLabel + v.find.String(), Style: fieldStyle(v.focus == focusFind)})
	w.Println(1, layout.Segment{Text: replaceLabel + v.replace.String(), Style: fieldStyle(v.focus == focusReplace)})
	w.Println(2, v.scopeRowSegments()...)

	if v.resultShown {
		v.renderResult(w, replaceHeaderRows-1)
		return
	}
	w.Println(replaceHeaderRows-1, layout.Segment{Text: v.statusLine(), Style: layout.Style{Attr: layout.AttrDim}})

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
		return fmt.Sprintf("%d occurrence(s) in %d file(s) — Enter: next field, Esc: results · Space: toggle, Enter: replace one, a: replace all",
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
