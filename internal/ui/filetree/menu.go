package filetree

import (
	"strings"

	"github.com/bricejulia/nib/internal/layout"
	"github.com/bricejulia/nib/internal/textwidth"
)

// menuAction is one item in the tree's right-click context menu.
type menuAction int

const (
	menuCopyPath menuAction = iota
	menuCopyRelPath
	menuRename
	menuDelete
)

func (a menuAction) label() string {
	switch a {
	case menuCopyPath:
		return "Copy Path"
	case menuCopyRelPath:
		return "Copy Relative Path"
	case menuRename:
		return "Rename / Move"
	case menuDelete:
		return "Delete"
	default:
		return ""
	}
}

// icon is the glyph shown before the label. Plain single-width Unicode
// like the tree's own ▶/▼/→, not Nerd Font or emoji: those need a patched
// font or render double-width depending on the terminal, which would break
// the box's column arithmetic.
func (a menuAction) icon() string {
	switch a {
	case menuCopyPath:
		return "⎘"
	case menuCopyRelPath:
		return "⤷"
	case menuRename:
		return "✎"
	case menuDelete:
		return "✕"
	default:
		return " "
	}
}

// text is the item as drawn: icon, then label.
func (a menuAction) text() string { return a.icon() + " " + a.label() }

// menuState is the right-click context menu, open for one entry. Same
// shape as the editor's tab-bar menu (see editor.tabMenuState): keyboard-
// navigable with Up/Down/Enter/Esc, mouse-clickable on its own rows.
//
// The target is kept as an absolute PATH, never a *Node, for the same
// reason promptTarget is: a watcher-driven Refresh can rebuild the Nodes
// while the menu is open.
type menuState struct {
	target   string
	items    []menuAction
	selected int

	// anchorRow/anchorCol are where the right-click landed, in pane
	// coordinates; row/col/width/height are where the LAST Render actually
	// drew the box, kept for hit-testing the next mouse event against.
	anchorRow, anchorCol    int
	row, col, width, height int
}

// openMenu selects tree row idx — so the row the menu acts on is visibly
// the selected one, and so beginRename/beginDelete, which act on the
// cursor, target it — and opens the context menu anchored at (col, row).
func (v *View) openMenu(idx, col, row int) {
	v.cancelPrompt()
	v.notice = ""
	if v.cursor != idx {
		v.cursor = idx
		v.hScroll = 0
	}
	items := []menuAction{menuCopyPath, menuCopyRelPath}
	// ModeChanges is navigate-and-open only, not a file-op target — see
	// beginRename/beginDelete, which refuse it anyway.
	if v.mode == ModeFiles {
		items = append(items, menuRename, menuDelete)
	}
	v.menu = &menuState{
		target:    v.rows[idx].Node.Path,
		items:     items,
		anchorRow: row,
		anchorCol: col,
	}
}

// closeMenu dismisses the menu without acting.
func (v *View) closeMenu() { v.menu = nil }

// activateMenuItem runs the selected item, then closes the menu.
func (v *View) activateMenuItem() {
	menu := v.menu
	v.menu = nil
	if menu == nil || menu.selected < 0 || menu.selected >= len(menu.items) {
		return
	}
	switch menu.items[menu.selected] {
	case menuCopyPath:
		v.copyToClipboard(menu.target)
	case menuCopyRelPath:
		if rel, ok := relPath(v.root.Path, menu.target); ok {
			v.copyToClipboard(rel)
		}
	case menuRename:
		if v.selectPath(menu.target) {
			v.beginRename()
		}
	case menuDelete:
		if v.selectPath(menu.target) {
			v.beginDelete()
		}
	}
}

// selectPath puts the cursor back on target's row before a prompt opens on
// it — the rows may have been rebuilt since the menu opened. Reports false
// if target no longer has a row (deleted from outside nib meanwhile).
func (v *View) selectPath(target string) bool {
	v.ensureFresh()
	for i, r := range v.rows {
		if r.Node.Path == target {
			v.cursor = i
			return true
		}
	}
	return false
}

// copyToClipboard hands s to CopyFunc and confirms it in the status bar.
func (v *View) copyToClipboard(s string) {
	if v.CopyFunc == nil {
		return
	}
	v.CopyFunc(s)
	v.notice = "copied " + s
}

// handleMenuKey handles a key while the menu is open. Always consumes it:
// Up/Down move, Enter acts, and anything else (Esc included) just closes
// the menu — a stray key must not also trigger a tree binding like "d".
func (v *View) handleMenuKey(k layout.Key) bool {
	if k.EventType == layout.EventRelease {
		return true
	}
	menu := v.menu
	if k.Named == layout.KeyEnter {
		v.activateMenuItem()
		return true
	}
	switch v.keymap[k.String()] {
	case "move_up":
		menu.selected = (menu.selected - 1 + len(menu.items)) % len(menu.items)
	case "move_down":
		menu.selected = (menu.selected + 1) % len(menu.items)
	default:
		v.closeMenu()
	}
	return true
}

// rowAt returns the item index under (col, row), or ok=false if that's
// outside the box the last Render drew.
func (menu *menuState) rowAt(col, row int) (int, bool) {
	if menu.height <= 0 || menu.width <= 0 ||
		row < menu.row || row >= menu.row+menu.height ||
		col < menu.col || col >= menu.col+menu.width {
		return 0, false
	}
	return row - menu.row, true
}

// handleMenuMouse handles every mouse event while the menu is open:
// hovering highlights an item, a left press on one activates it, and any
// other press closes the menu. Always consumes the event.
func (v *View) handleMenuMouse(m layout.Mouse) bool {
	menu := v.menu
	switch m.EventType {
	case layout.EventMotion:
		if i, ok := menu.rowAt(m.Col, m.Row); ok {
			menu.selected = i
		}
	case layout.EventPress:
		if m.Button == layout.MouseWheelUp || m.Button == layout.MouseWheelDown {
			return true
		}
		if i, ok := menu.rowAt(m.Col, m.Row); ok && m.Button == layout.MouseLeft {
			menu.selected = i
			v.activateMenuItem()
			return true
		}
		v.closeMenu()
	default:
		// Release etc.: swallowed so it doesn't leak through.
	}
	return true
}

// renderMenu draws the menu over the tree rows: below the clicked row when
// it fits, above it otherwise, clipped to the pane as a last resort. Each
// row it covers is redrawn with the tree text kept on either side of the
// box, so the menu reads as floating over the tree rather than blanking
// whole lines.
func (v *View) renderMenu(w layout.Window, cols, rows int) {
	menu := v.menu
	n := len(menu.items)
	width := 0
	for _, it := range menu.items {
		width = max(width, textwidth.DisplayWidth(it.text()))
	}
	width = min(width+2, cols) // one column of padding either side

	start := menu.anchorRow + 1
	switch {
	case rows-start >= n:
	case menu.anchorRow >= n:
		start = menu.anchorRow - n
	default:
		start = max(rows-n, 0)
	}
	n = min(n, rows-start)
	col := max(min(menu.anchorCol, cols-width), 0)
	menu.row, menu.col, menu.width, menu.height = start, col, width, n
	if n <= 0 || width <= 0 {
		return
	}

	for i := 0; i < n; i++ {
		screenRow := start + i
		var under []layout.Segment
		if idx := v.scrollTop + screenRow; idx < len(v.rows) {
			segs := rowSegments(v.rows[idx], idx == v.cursor, v.showIcons)
			under = textwidth.SliceSegmentsByDisplayColumn(segs, v.hScroll, cols)
		}
		left := padSegmentsTo(textwidth.SliceSegmentsByDisplayColumn(under, 0, col), col)
		right := textwidth.SliceSegmentsByDisplayColumn(under, col+width, cols-col-width)

		item := " " + menu.items[i].text()
		item = padTo(textwidth.SliceByDisplayColumn(item, 0, width), width)
		style := menuStyle
		if i == menu.selected {
			style.Attr |= layout.AttrReverse
		}
		line := append(left, layout.Segment{Text: item, Style: style})
		w.Println(screenRow, append(line, right...)...)
	}
}

// menuStyle sets the box apart from the tree rows it floats over; the
// selected item is reversed on top of it.
var menuStyle = layout.Style{Attr: layout.AttrBold}

// padSegmentsTo right-pads segs with unstyled spaces to exactly width
// display columns (segs is assumed to be no wider already).
func padSegmentsTo(segs []layout.Segment, width int) []layout.Segment {
	if pad := width - segmentsWidth(segs); pad > 0 {
		return append(segs, layout.Segment{Text: strings.Repeat(" ", pad)})
	}
	return segs
}

// padTo right-pads s with spaces to exactly width display columns (s is
// assumed to be no wider already).
func padTo(s string, width int) string {
	if pad := width - textwidth.DisplayWidth(s); pad > 0 {
		return s + strings.Repeat(" ", pad)
	}
	return s
}
