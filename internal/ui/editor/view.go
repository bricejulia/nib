package editor

import (
	"fmt"
	"path/filepath"

	"github.com/bricejulia/kiwi/internal/layout"
	"github.com/bricejulia/kiwi/internal/textwidth"
)

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
}

// View is the read-only editor pane: zero or more open tabs, each an
// independent Buffer with its own scroll/cursor position. There is no
// insert mode yet — that's a later step — but the pane does show the
// terminal's real cursor (see CursorPosition) at the current position.
type View struct {
	tabs     []*tab
	active   int // index into tabs; meaningless when len(tabs) == 0
	tabWidth int

	lastWidth, lastHeight int
}

// NewView creates an empty editor pane with no tabs open; call Open to
// load a file into it.
func NewView() *View {
	return &View{tabWidth: 4}
}

func (v *View) Title() string { return "Editor" }

func (v *View) activeTab() *tab {
	if v.active < 0 || v.active >= len(v.tabs) {
		return nil
	}
	return v.tabs[v.active]
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

	buf, err := Load(path)
	v.tabs = append(v.tabs, &tab{path: path, buf: buf, err: err})
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
// new last tab, if the closed tab was leftmost).
func (v *View) CloseTab() {
	if len(v.tabs) == 0 {
		return
	}
	v.tabs = append(v.tabs[:v.active], v.tabs[v.active+1:]...)
	if v.active >= len(v.tabs) {
		v.active = len(v.tabs) - 1
	}
}

// CloseAllTabs closes all tabs.
func (v *View) CloseAllTabs() {
	if len(v.tabs) == 0 {
		return
	}
	v.tabs = []*tab{}
	v.active = 0
}

// StatusText is the "Ln N, Col N" text meant for a status bar (see
// internal/ui/statusbar). Col is 1-based over rune positions in the
// current line, not raw terminal display columns.
func (v *View) StatusText() string {
	t := v.activeTab()
	if t == nil || t.buf == nil {
		return ""
	}
	return fmt.Sprintf("Ln %d, Col %d", t.cursorLn+1, t.cursorCol+1)
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
		row := 0
		w.Println(row, layout.Segment{Text: msg})
		return
	}

	w.Println(0, layout.Segment{Text: tabBarText(v.tabs, v.active, cols)})

	t := v.activeTab()
	bodyRows := rows - 1
	if bodyRows < 0 {
		bodyRows = 0
	}
	// body content is drawn one row down, into a window offset past the
	// tab bar; layout.Window has no sub-window primitive of its own (that
	// lives one level down, on the real vaxis.Window), so the offset is
	// applied directly to the row index passed to Println instead.
	renderBody(w, t, v.tabWidth, cols, bodyRows, 1)
}

func tabBarText(tabs []*tab, active, cols int) string {
	line := ""
	for i, t := range tabs {
		name := "[No Name]"
		if t.path != "" {
			name = filepath.Base(t.path)
		}
		if i == active {
			line += "[" + name + "]"
		} else {
			line += " " + name + " "
		}
		if i < len(tabs)-1 {
			line += "|"
		}
	}
	if cols > 0 && len(line) > cols {
		line = line[:cols]
	}
	return line
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
		expanded := textwidth.ExpandTabs(t.buf.Lines[ln], tabWidth)
		visible := textwidth.SliceSegmentsByDisplayColumn(highlightLine(expanded), t.leftCol, contentWidth)

		gutterSeg := layout.Segment{
			Text:  fmt.Sprintf("%*d ", gutterWidth-1, ln+1),
			Style: layout.Style{Attr: layout.AttrDim},
		}
		w.Println(rowOffset+i, append([]layout.Segment{gutterSeg}, visible...)...)
	}
}

// gutterWidthFor is the line-number column's width (digits + 1 trailing
// space), derived from the buffer's line count.
func gutterWidthFor(t *tab) int {
	if t.buf == nil {
		return 1
	}
	return len(fmt.Sprintf("%d", len(t.buf.Lines))) + 1
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

func (v *View) HandleKey(k layout.Key) bool {
	if k.EventType == layout.EventRelease {
		return false
	}

	switch {
	case k.Text == "]":
		v.NextTab()
		return true
	case k.Text == "[":
		v.PrevTab()
		return true
	case k.Text == "x":
		v.CloseTab()
		return true
	case k.Text == "X":
		v.CloseAllTabs()
		return true
	}

	t := v.activeTab()
	if t == nil {
		return false
	}

	switch {
	case k.Named == layout.KeyDown || k.Text == "j":
		t.cursorLn++
	case k.Named == layout.KeyUp || k.Text == "k":
		t.cursorLn--
	case k.Named == layout.KeyLeft || k.Text == "h":
		t.cursorCol--
	case k.Named == layout.KeyRight || k.Text == "l":
		t.cursorCol++
	case k.Named == layout.KeyPageDown:
		t.cursorLn += v.pageSize()
	case k.Named == layout.KeyPageUp:
		t.cursorLn -= v.pageSize()
	case k.Named == layout.KeyHome:
		t.cursorCol = 0
	case k.Named == layout.KeyEnd:
		t.cursorCol = len(currentLineRunes(t, t.cursorLn, v.tabWidth))
	case k.Text == "g":
		t.cursorLn = 0
	case k.Text == "G":
		t.cursorLn = len(t.buf.Lines) - 1
	default:
		return false
	}

	v.clamp(t)
	return true
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
