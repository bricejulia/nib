package editor

import "github.com/bricejulia/nib/internal/layout"

// tabMenuAction is one item in the tab bar's right-click context menu.
type tabMenuAction int

const (
	tabMenuClose tabMenuAction = iota
	tabMenuCloseOthers
	tabMenuCloseAll
	tabMenuSplitRight
	tabMenuSplitDown
	tabMenuMoveRight
	tabMenuMoveDown
	tabMenuMoveToNextPane
)

func (a tabMenuAction) label() string {
	switch a {
	case tabMenuClose:
		return "Close"
	case tabMenuCloseOthers:
		return "Close Others"
	case tabMenuCloseAll:
		return "Close All"
	case tabMenuSplitRight:
		return "Split Right"
	case tabMenuSplitDown:
		return "Split Down"
	case tabMenuMoveRight:
		return "Move Right"
	case tabMenuMoveDown:
		return "Move Down"
	case tabMenuMoveToNextPane:
		return "Move to next pane"
	default:
		return ""
	}
}

// popupRect is where a popup actually ended up drawing (see popupBounds),
// kept around so a later mouse event can hit-test against it — the same
// "compute at render time, consumed by the next input event" shape App's
// own leaf-rect cache (a.rects) uses for pane hit-testing.
type popupRect struct {
	row, height, col, width int
	offset                  int // index into items of the row at row — see popupBounds's offset return; 0 for any menu that fits fully in view, as tabMenuState's fixed 4-item list always does today
}

// tabMenuState is the tab bar's right-click context menu, open for one
// specific tab (target) regardless of which is active — see the design
// notes on Q3: right-clicking a background tab acts on it without
// switching to it. Rendered as an in-pane popup exactly like the
// autocomplete list (see completionState): keyboard-navigable via the same
// Up/Down/Enter/Esc convention (see handleTabMenuKey) and additionally
// mouse-clickable on its own rows (see handleTabMenuMouse).
type tabMenuState struct {
	target    int // index of the tab this menu was opened for
	items     []tabMenuAction
	selected  int
	anchorCol int
	rect      popupRect // where the LAST Render actually drew it
}

// openTabMenu shows the right-click context menu for tabs[i], anchored at
// anchorCol on the tab bar (row 0) — see handleTabBarPress. Clears the
// other (read-only, "next key dismisses") tooltip popups so a stale one
// can't linger underneath/instead of it before any key is next pressed.
func (v *View) openTabMenu(i, anchorCol int) {
	v.showDiagnostics = false
	v.hoverText = ""
	v.signatureHelp = nil
	v.gitPopup = nil
	items := []tabMenuAction{tabMenuClose, tabMenuCloseOthers, tabMenuCloseAll, tabMenuSplitRight, tabMenuSplitDown}
	if v.CanMoveRight != nil && v.CanMoveRight() {
		items = append(items, tabMenuMoveRight)
	}
	if v.CanMoveDown != nil && v.CanMoveDown() {
		items = append(items, tabMenuMoveDown)
	}
	items = append(items, tabMenuMoveToNextPane)
	v.tabMenu = &tabMenuState{
		target:    i,
		items:     items,
		anchorCol: anchorCol,
	}
}

// activateTabMenuItem runs the currently selected menu item against the
// tab it was opened for, then closes the menu.
func (v *View) activateTabMenuItem() {
	menu := v.tabMenu
	v.tabMenu = nil
	if menu == nil || menu.selected < 0 || menu.selected >= len(menu.items) {
		return
	}
	switch menu.items[menu.selected] {
	case tabMenuClose:
		v.requestCloseTab(menu.target)
	case tabMenuCloseOthers:
		v.requestCloseOtherTabs(menu.target)
	case tabMenuCloseAll:
		v.requestCloseAllTabs()
	case tabMenuSplitRight:
		if v.OnSplitAndMoveRight != nil {
			v.OnSplitAndMoveRight(menu.target)
		}
	case tabMenuSplitDown:
		if v.OnSplitAndMoveDown != nil {
			v.OnSplitAndMoveDown(menu.target)
		}
	case tabMenuMoveRight:
		if v.OnMoveRight != nil {
			v.OnMoveRight(menu.target)
		}
	case tabMenuMoveDown:
		if v.OnMoveDown != nil {
			v.OnMoveDown(menu.target)
		}
	case tabMenuMoveToNextPane:
		if v.OnMoveTabToNextPane != nil {
			v.OnMoveTabToNextPane(menu.target)
		}
	}
}

// renderTabMenu draws the menu and records where it actually landed (see
// popupRect) for handleTabMenuMouse to hit-test the next click against.
func (v *View) renderTabMenu(w layout.Window, cols, rows int) {
	menu := v.tabMenu
	lines := make([]popupLine, len(menu.items))
	for i, it := range menu.items {
		lines[i] = popupLine{Text: it.label()}
	}
	startRow, n, offset, width := popupBounds(cols, rows, menu.anchorCol, 0, lines, menu.selected)
	menu.rect = popupRect{row: startRow, height: n, col: menu.anchorCol, width: width, offset: offset}
	if n <= 0 || width <= 0 {
		return
	}
	renderStyledPopup(w, cols, rows, menu.anchorCol, 0, lines, menu.selected)
}

// handleTabMenuKey handles a key while the menu is open, called before
// HandleKey's normal Normal/Insert/Command-mode dispatch — the same "modal
// input owner" position handleCompletionKey occupies for the autocomplete
// popup. Returns true if it fully handled the key; false means "not
// recognized," and the caller (HandleKey) both dismisses the menu AND
// continues dispatching the same key normally — the same "next key
// dismisses" convention the read-only tooltips already follow.
func (v *View) handleTabMenuKey(k layout.Key) bool {
	menu := v.tabMenu
	switch v.keymap[k.String()] {
	case "normal_mode": // Esc
		v.tabMenu = nil
		return true
	case "insert_newline": // Enter activates the selected item
		v.activateTabMenuItem()
		return true
	case "move_up":
		menu.selected--
		if menu.selected < 0 {
			menu.selected = len(menu.items) - 1
		}
		return true
	case "move_down":
		menu.selected = (menu.selected + 1) % len(menu.items)
		return true
	}
	return false
}

// tabMenuRowAt returns the item index under (col, row), or ok=false if
// that's outside the menu's last-rendered rect — shared by hover-highlight
// (EventMotion) and click-to-activate (EventPress) so the two agree on
// exactly which row is which.
func (menu *tabMenuState) rowAt(col, row int) (index int, ok bool) {
	r := menu.rect
	if r.height <= 0 || r.width <= 0 ||
		row < r.row || row >= r.row+r.height ||
		col < r.col || col >= r.col+r.width {
		return 0, false
	}
	return r.offset + (row - r.row), true
}

// handleTabMenuMouse handles every mouse event while the menu is open:
// hovering a row highlights it (mouse motion is delivered continuously
// regardless of button state — see mousePress's own comment on that — so
// this costs nothing extra to wire up, unlike the tab bar's own hover,
// which was deliberately skipped as a separate capability); a left press
// on a row activates it; a left press anywhere else, or any other button,
// dismisses the menu (see the design notes on Q4 — outside-click
// dismisses, same as Esc). Always returns true: the menu is modal to this
// pane's mouse input until dismissed or acted on.
func (v *View) handleTabMenuMouse(m layout.Mouse) bool {
	menu := v.tabMenu
	switch m.EventType {
	case layout.EventMotion:
		if i, ok := menu.rowAt(m.Col, m.Row); ok {
			menu.selected = i
		}
		return true
	case layout.EventPress:
		if m.Button == layout.MouseLeft {
			if i, ok := menu.rowAt(m.Col, m.Row); ok {
				menu.selected = i
				v.activateTabMenuItem()
				return true
			}
		}
		v.tabMenu = nil
		return true
	default:
		return true // release, etc.: swallowed, doesn't leak through
	}
}
