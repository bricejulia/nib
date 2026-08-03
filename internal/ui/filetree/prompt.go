package filetree

import (
	"fmt"
	"path/filepath"
	"unicode"

	"github.com/bricejulia/nib/internal/debuglog"
	"github.com/bricejulia/nib/internal/layout"
	"github.com/bricejulia/nib/internal/textwidth"
)

// promptMode is which file operation, if any, is currently asking the user
// for input on the pane's bottom row. promptNone means the tree is in its
// normal browsing state and the whole pane is tree rows.
type promptMode int

const (
	promptNone promptMode = iota
	// promptCreate takes a project-root-relative path; a trailing "/"
	// creates a directory instead of a file.
	promptCreate
	// promptRename takes a project-root-relative path too, prefilled with
	// the selected entry's own — editing the last segment renames it,
	// editing an earlier one moves it. Both are one os.Rename.
	promptRename
	// promptConfirm is a single-keypress y/N guard, used for deleting a
	// file or an empty directory.
	promptConfirm
	// promptConfirmYes requires typing "yes" in full, used for deleting a
	// directory that still has entries in it: that's a recursive,
	// permanent removal, and a mistyped "y" shouldn't be enough.
	promptConfirmYes
)

// promptCaretStyle keeps the prompt row visually distinct from the tree rows
// above it; promptErrStyle marks a refusal, which is shown inline because
// everything else in nib reports errors only to the debug log (Ctrl+D).
var (
	promptStyle    = layout.Style{Attr: layout.AttrBold}
	promptErrStyle = layout.Style{Foreground: layout.ColorRed}
)

// label returns the prompt's leading text, which is also what the caret
// column is measured from.
func (v *View) promptLabel() string {
	switch v.prompt {
	case promptCreate:
		return "new: "
	case promptRename:
		return "rename: "
	case promptConfirm:
		return fmt.Sprintf("delete %s? (y/N) ", filepath.Base(v.promptTarget))
	case promptConfirmYes:
		return fmt.Sprintf("delete %s (%d entries)? type \"yes\": ",
			filepath.Base(v.promptTarget), v.promptCount)
	default:
		return ""
	}
}

// beginCreate opens the create prompt, prefilled with the target
// directory's root-relative path and a trailing "/". What's typed is always
// interpreted relative to the PROJECT ROOT, exactly like the rename prompt:
// one validation path serves both, and the prefill is editable, so the
// create can be retargeted anywhere in the project without moving the
// cursor first.
func (v *View) beginCreate() {
	dir := v.createTargetDir()
	prefill := ""
	if rel, ok := relPath(v.root.Path, dir); ok {
		prefill = rel + "/"
	}
	v.openPrompt(promptCreate, prefill)
}

// createTargetDir picks which directory a new entry lands in: the selected
// directory (whether or not it's expanded — you shouldn't have to open a
// folder to put something in it), the selected file's own directory, or the
// project root when nothing is selected.
func (v *View) createTargetDir() string {
	if v.cursor < 0 || v.cursor >= len(v.rows) {
		return v.root.Path
	}
	n := v.rows[v.cursor].Node
	if n.IsDir {
		return n.Path
	}
	// filepath.Dir, not n.Parent: Parent is only populated by EnsureLoaded,
	// and is nil in a tree built as a bare struct literal (as some tests
	// do). The path is always right.
	return filepath.Dir(n.Path)
}

// beginRename opens the rename/move prompt on the selected entry, prefilled
// with its root-relative path and the caret at the end.
func (v *View) beginRename() {
	if v.cursor < 0 || v.cursor >= len(v.rows) {
		return
	}
	n := v.rows[v.cursor].Node
	rel, ok := relPath(v.root.Path, n.Path)
	if !ok {
		return // the project root itself has no row, so this is unreachable
	}
	v.promptTarget = n.Path
	v.openPrompt(promptRename, rel)
}

// beginDelete opens the delete confirmation for the selected entry: a
// single y/N for a file or an empty directory, and the stricter type-"yes"
// form for a directory that would be removed recursively.
func (v *View) beginDelete() {
	if v.cursor < 0 || v.cursor >= len(v.rows) {
		return
	}
	n := v.rows[v.cursor].Node
	v.promptTarget = n.Path
	if n.IsDir {
		if count := dirEntryCount(n.Path); count > 0 {
			v.promptCount = count
			v.openPrompt(promptConfirmYes, "")
			return
		}
	}
	v.openPrompt(promptConfirm, "")
}

func (v *View) openPrompt(mode promptMode, prefill string) {
	v.prompt = mode
	v.promptBuf = []rune(prefill)
	v.promptCaret = len(v.promptBuf)
	v.promptErr = ""
	v.promptScroll = 0
}

// CancelPrompt abandons any prompt in progress, leaving the filesystem
// untouched. Exported for cmd/nib's focus-change wiring: a pane left
// mid-prompt when focus moves away (e.g. a mouse click elsewhere, which
// never routes a key through this pane's own HandleKey) would otherwise
// still be swallowing every key the next time the tree got focus back —
// the same hazard editor.View.ExitEditingModes exists for.
func (v *View) CancelPrompt() { v.cancelPrompt() }

func (v *View) cancelPrompt() {
	v.prompt = promptNone
	v.promptBuf = nil
	v.promptCaret = 0
	v.promptErr = ""
	v.promptTarget = ""
	v.promptCount = 0
	v.promptScroll = 0
}

// handlePromptKey routes a keystroke while a prompt is open.
//
// Two rules make this work, and both are load-bearing. First, it ALWAYS
// reports the key consumed: an unconsumed key bubbles to the global keymap
// (see layout.Dispatch), so typing "?" would open the help overlay and
// "Tab" would move focus away mid-filename. Ctrl+c is swallowed too, so Esc
// is the only way out of a prompt — the same trade finder.View.HandleKey
// already makes as a modal.
//
// Second, it never consults v.keymap. The editing keys below are therefore
// not configurable, and that's the point: it's what keeps every printable
// character — including the letters bound to create/rename/delete in normal
// mode — typeable in a filename.
func (v *View) handlePromptKey(k layout.Key) bool {
	if k.EventType == layout.EventRelease {
		return true
	}

	// The single-keypress confirmation has no buffer to edit: "y" deletes,
	// literally anything else (including Enter, which is the capital N in
	// "(y/N)") cancels.
	if v.prompt == promptConfirm {
		if k.Named == "" && (k.Text == "y" || k.Text == "Y") {
			v.commitDelete(false)
		} else {
			v.cancelPrompt()
		}
		return true
	}

	switch k.Named {
	case layout.KeyEsc:
		v.cancelPrompt()
		return true
	case layout.KeyEnter:
		v.commitPrompt()
		return true
	case layout.KeyBackspace:
		v.promptErr = ""
		if v.promptCaret > 0 {
			v.promptBuf = append(v.promptBuf[:v.promptCaret-1], v.promptBuf[v.promptCaret:]...)
			v.promptCaret--
		}
		return true
	case layout.KeyLeft:
		if v.promptCaret > 0 {
			v.promptCaret--
		}
		return true
	case layout.KeyRight:
		if v.promptCaret < len(v.promptBuf) {
			v.promptCaret++
		}
		return true
	case layout.KeyHome:
		v.promptCaret = 0
		return true
	case layout.KeyEnd:
		v.promptCaret = len(v.promptBuf)
		return true
	}

	// Any other special key (Tab, the paging keys, a bare Shift press) is
	// swallowed without being typed. Space is the exception: App's
	// translateKey promotes the space bar to Named "Space" while leaving
	// Text " " intact, so without letting it through here a space in a
	// filename would be silently dropped.
	if k.Named != "" && k.Named != layout.KeySpace {
		return true
	}
	if k.Text != "" && k.Mods&(layout.ModCtrl|layout.ModAlt|layout.ModSuper) == 0 {
		v.promptErr = ""
		for _, r := range k.Text {
			if !unicode.IsPrint(r) {
				continue
			}
			v.promptBuf = append(v.promptBuf, 0)
			copy(v.promptBuf[v.promptCaret+1:], v.promptBuf[v.promptCaret:])
			v.promptBuf[v.promptCaret] = r
			v.promptCaret++
		}
	}
	return true
}

// commitPrompt acts on Enter.
func (v *View) commitPrompt() {
	switch v.prompt {
	case promptNone, promptConfirm:
		// promptNone: nothing to commit. promptConfirm is a single-keypress
		// y/N handled entirely in handlePromptKey before Enter ever reaches
		// here (see the early return there), so this case is never actually
		// hit — kept only so this switch stays exhaustive.
	case promptCreate:
		v.commitCreate()
	case promptRename:
		v.commitRename()
	case promptConfirmYes:
		// Anything other than the full word cancels: this is the guard
		// against a recursive delete, so a near-miss must not go through.
		if string(v.promptBuf) != "yes" {
			v.cancelPrompt()
			return
		}
		v.commitDelete(true)
	}
}

func (v *View) commitCreate() {
	abs, isDir, err := resolveInRoot(v.root.Path, string(v.promptBuf))
	if err == nil {
		err = createEntry(abs, isDir)
	}
	if err != nil {
		v.failPrompt("create", err)
		return
	}
	v.cancelPrompt()
	v.syncAfter(abs, filepath.Dir(abs))
	v.notifyMutated()
}

func (v *View) commitRename() {
	src := v.promptTarget
	dst, _, err := resolveInRoot(v.root.Path, string(v.promptBuf))
	if err == nil {
		err = movePath(src, dst)
	}
	if err != nil {
		v.failPrompt("rename", err)
		return
	}
	v.cancelPrompt()
	if src == dst {
		return // nothing changed on disk; leave the tree and the callers alone
	}

	// Callers first, so anything holding the old path (open editor buffers,
	// their language-server registrations) is retargeted before the next
	// frame can render against the new tree.
	if v.OnPathMoved != nil {
		v.OnPathMoved(src, dst)
	}
	if v.moveNodeInTree(src, dst) {
		v.dirty = true
		v.revealPath(dst)
	} else {
		v.syncAfter(dst, filepath.Dir(src), filepath.Dir(dst))
	}
	v.notifyMutated()
}

func (v *View) commitDelete(recursive bool) {
	target := v.promptTarget
	if err := deletePath(target, recursive); err != nil {
		// Unlike the text prompts, there's no typed input worth preserving
		// here, so a failed delete closes the prompt rather than staying
		// open with an inline message.
		debuglog.Warn("filetree: delete %s: %v", target, err)
		v.cancelPrompt()
		return
	}
	v.cancelPrompt()

	if v.OnPathDeleted != nil {
		v.OnPathDeleted(target)
	}
	// No path to select: syncAfter falls back to ensureFresh, whose
	// existing clamp keeps the cursor's row index and pulls it back into
	// range — so deleting a middle row lands on the following sibling and
	// deleting the last row lands on the new last row.
	v.syncAfter("", filepath.Dir(target))
	v.notifyMutated()
}

// failPrompt keeps the prompt open with the refusal shown inline at the end
// of the prompt row. Closing it would throw away the path the user just
// typed over a fixable typo, and inline is the only user-visible error
// surface in the app — everything else goes to the debug log behind Ctrl+D,
// which this also writes to, with the full context.
func (v *View) failPrompt(op string, err error) {
	v.promptErr = err.Error()
	debuglog.Warn("filetree: %s %q: %v", op, string(v.promptBuf), err)
}

func (v *View) notifyMutated() {
	if v.OnMutated != nil {
		v.OnMutated()
	}
}

// renderPrompt draws the prompt on row, updating promptScroll so the caret
// stays visible when the typed path is wider than the pane.
func (v *View) renderPrompt(w layout.Window, row, cols int) {
	label := v.promptLabel()
	caretCol := textwidth.DisplayWidth(label + string(v.promptBuf[:v.promptCaret]))

	if caretCol-v.promptScroll >= cols {
		v.promptScroll = caretCol - cols + 1
	}
	if caretCol < v.promptScroll {
		v.promptScroll = caretCol
	}
	if v.promptScroll < 0 {
		v.promptScroll = 0
	}

	segs := []layout.Segment{{Text: label + string(v.promptBuf), Style: promptStyle}}
	if v.promptErr != "" {
		// After the buffer, never before it: a message in front would shift
		// the caret column out from under the cursor.
		segs = append(segs, layout.Segment{Text: "  " + v.promptErr, Style: promptErrStyle})
	}
	w.Println(row, textwidth.SliceSegmentsByDisplayColumn(segs, v.promptScroll, cols)...)
}

// CursorPosition implements layout.CursorProvider: while a prompt is open
// the terminal's real cursor sits in the typed text on the pane's bottom
// row, and the rest of the time there is no cursor at all — the tree shows
// its selection with a reversed row instead.
//
// Safe to compute from the last Render's height and scroll offset because
// ui.App renders a pane immediately before asking it for its cursor.
func (v *View) CursorPosition() (int, int, bool) {
	if v.prompt == promptNone || v.lastHeight <= 0 {
		return 0, 0, false
	}
	col := textwidth.DisplayWidth(v.promptLabel()+string(v.promptBuf[:v.promptCaret])) - v.promptScroll
	return max(col, 0), v.lastHeight - 1, true
}
