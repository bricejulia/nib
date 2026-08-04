package editor

import (
	"fmt"
	"path/filepath"
	"strconv"
	"strings"

	"github.com/bricejulia/nib/internal/config"
	"github.com/bricejulia/nib/internal/debuglog"
	"github.com/bricejulia/nib/internal/layout"
	"github.com/bricejulia/nib/internal/lsp"
	"github.com/bricejulia/nib/internal/textwidth"
	"github.com/bricejulia/nib/internal/ui/gitstyle"
	"github.com/bricejulia/nib/internal/vcs/gitblame"
	"github.com/bricejulia/nib/internal/vcs/gitstatus"
)

// DefaultKeybinds are the editor pane's built-in keybindings, overridable
// via the user config's "editor" scope (see internal/config).
var DefaultKeybinds = config.Defaults{
	{Trigger: "]", Action: "next_tab"},
	{Trigger: "[", Action: "prev_tab"},
	{Trigger: "x", Action: "delete_char_forward"},
	{Trigger: "X", Action: "delete_char_backward"},
	// "d", "y" and "c" are vim's operators: delete, yank, change. Each arms
	// on the first press (see pendingOp) and either combines with the next
	// motion/text-object ("dw", "ciw"), or, on an immediate second press of
	// the SAME operator, runs linewise over the current line(s) ("dd",
	// "yy", "cc" — see tryCompleteOperator/applyLinewiseOperator). Bound as
	// one trigger each rather than as a literal two-key sequence because a
	// trigger is a single key by construction — see config.Normalize.
	{Trigger: "d", Action: "delete_line"},
	{Trigger: "y", Action: "yank_line"},
	{Trigger: "c", Action: "change_line"},
	{Trigger: "p", Action: "put_after"},
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
	{Trigger: "0", Action: "line_start"},
	{Trigger: "End", Action: "line_end"},
	{Trigger: "$", Action: "line_end"},
	{Trigger: "g", Action: "first_line"},
	{Trigger: "G", Action: "last_line"},
	// Vim's small-word motions: the start of the next/previous word-or-
	// punctuation run ("w"/"b"), or the end of the next one ("e") — see
	// motion.go's classifyRune/wordForwardOnce/wordBackwardOnce/
	// wordEndOnce. Combine with an operator ("dw", "d$", ...) via
	// operatorMotions/tryCompleteOperator.
	{Trigger: "w", Action: "word_forward"},
	{Trigger: "e", Action: "word_end"},
	{Trigger: "b", Action: "word_backward"},
	{Trigger: "i", Action: "insert_mode"},
	{Trigger: "a", Action: "append_mode"},
	{Trigger: "o", Action: "append_new_line_mode"},
	{Trigger: "Esc", Action: "normal_mode"},
	{Trigger: "Enter", Action: "insert_newline"},
	{Trigger: "Backspace", Action: "insert_backspace"},
	{Trigger: "Tab", Action: "insert_tab"},
	{Trigger: "Ctrl+s", Action: "save"},
	{Trigger: "u", Action: "undo"},
	// Bare "r", not Ctrl+r: frees Ctrl+r up as the GLOBAL "open_replace"
	// binding (see cmd/nib/main.go), which needs it to be unclaimed here —
	// layout.Dispatch tries this pane's own keymap before ever falling
	// back to the global one, so as long as Ctrl+r meant redo here, the
	// global binding could never fire while an editor pane had focus.
	{Trigger: "r", Action: "redo"},
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
	// Not bound here: Ctrl+f ("find references") is a GLOBAL binding (see
	// cmd/nib/main.go's globalDefaultKeybinds) so it works regardless of
	// which pane has focus — the global handler asks this pane for
	// WordUnderCursor when it happens to be the focused one, rather than
	// this pane owning the trigger itself.
	{Trigger: "Ctrl+Space", Action: "trigger_autocomplete"},
	// Not Ctrl+Shift+Space (the more VSCode-familiar chord for signature
	// help): Ctrl+Shift+<key> is indistinguishable from plain Ctrl+<key> on
	// any terminal (or multiplexer, e.g. tmux with extended-keys off) that
	// isn't reporting the kitty keyboard protocol's full modifier state —
	// degrading silently to Ctrl+Space here would fire autocomplete instead,
	// in the exact scope/mode signature help needs.
	// Reachable from both Normal and Insert mode — see handleInsertKey.
	{Trigger: "Ctrl+a", Action: "trigger_signature_help"},
	// "K" is vim's own "look up what's under the cursor", which is exactly
	// this gesture — and being a letter, it works on any keyboard layout.
	{Trigger: "K", Action: "show_diagnostics"},
	// "I" ("info") is hover's own binding, kept separate from "K" rather
	// than merged into it — they're different LSP requests with different
	// popup content decisions, so a dedicated key keeps both independently
	// testable.
	{Trigger: "I", Action: "show_hover"},
	// The three git gestures, all shifted letters for the same
	// works-on-any-layout reason as "K"/"I", and all free of vim's own
	// meanings for those keys (nib binds no operator-pending "d", and
	// implements neither H/M/L screen motions nor "B").
	{Trigger: "B", Action: "show_blame"},
	{Trigger: "D", Action: "show_file_diff"},
	{Trigger: "H", Action: "show_line_diff"},
	// "F" ("format") reformats the whole document via the language server.
	{Trigger: "F", Action: "format_document"},
	{Trigger: "/", Action: "search_mode"},
	{Trigger: "n", Action: "search_next"},
	{Trigger: "N", Action: "search_prev"},
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
	// modeSearch is the "/" prompt — structurally the same as modeCommand
	// (type, Enter to commit, Esc to cancel) but it drives an in-file search
	// rather than an ex-command; see search.go.
	modeSearch
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
// (see onBufferEdited) — simple and correct, if not the most memory-frugal
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
	// is mid-session — cmd/nib/main.go's focus-change wiring (see
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

	// selAnchor is the fixed end of the current selection — the end that
	// stays put while dragging. The MOVING end is cursorLn/cursorCol itself,
	// deliberately: reusing the cursor as one end of the selection is what
	// makes auto-scroll-while-dragging fall out for free, since renderBody
	// already derives topLine/leftCol from the cursor (and it means every
	// existing clamp keeps applying to the selection too, with no second
	// copy of that logic).
	//
	// Only meaningful while hasSel is true. Per-tab rather than on View,
	// like cursorLn/cursorCol and for the same reason: switching tabs should
	// restore what was selected, and a selection means nothing in another
	// file. Per-tab rather than on Buffer for the mirror-image reason
	// insertSnapshot is — it's view state, not document state, so two split
	// panes on one buffer select independently.
	selAnchor position
	hasSel    bool

	// diagnostics is this tab's language-server problems, keyed by the
	// 0-based line each one starts on — set by ApplyDiagnostics, exactly
	// as lineStatus is set by ApplyLineStatus (the View never talks to a
	// server itself; results flow in from cmd/nib/main.go's event loop).
	// The full Diagnostic is kept, not just its severity: the gutter only
	// needs severity today, but the message is what a near-future "show
	// the problem under the cursor" step needs.
	diagnostics map[int][]lsp.Diagnostic

	// detached is set when this tab's file was deleted from disk while the
	// buffer still had unsaved changes, so the tab was kept rather than
	// closed (see CloseTabsUnder). Purely a display/reporting flag — the
	// buffer works normally, ":w" recreates the file and clears it (see
	// saveActive), ":q!" throws it away.
	detached bool
}

// View is the editor pane: zero or more open tabs, each with its own
// scroll/cursor position and modal (Normal/Insert) editing state — see
// editMode. A tab's Buffer is not necessarily private to it: opening the
// same path from more than one View sharing a BufferStore (see
// SetBufferStore, e.g. split panes in cmd/nib/main.go) gives both tabs
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

	// count is the numeric prefix accumulated digit-by-digit before an
	// operator or a motion — e.g. the "3" of "3dd" or "3j" — with 0 meaning
	// "none typed yet", per vim's own "no count means 1" convention (see
	// resolvedCount in motion.go). Reset to 0 the instant it's consumed:
	// arming an operator, completing one, or running a bare motion.
	count int

	// pendingOp is the operator armed and waiting for its second press (the
	// doubled "dd"/"yy"/"cc" form), a motion to combine with ("dw", "d$"),
	// or a text-object prefix ("diw") — the zero value means no operator is
	// armed. Deliberately holds the ACTION, not the key, the same way the
	// single-slot mechanism this replaced did: what makes "dd" fire is the
	// same action arriving twice in a row, so a user who rebinds
	// delete_line to some other key gets the doubling on that key instead,
	// for free. See pendingOperator and tryCompleteOperator in motion.go.
	pendingOp pendingOperator

	// OnAllTabsClosed, if set, is called whenever CloseTab/CloseAllTabs
	// (directly, or via the ":q"/":qa" family — see closeActiveTab/
	// closeAllTabsCmd) leaves this pane with zero open tabs. Set by
	// cmd/nib/main.go to refocus the file tree — same plain-callback
	// pattern as finder.View.OnClose/debug.View.OnClose.
	OnAllTabsClosed func()

	// completion holds the in-progress autocomplete popup (Ctrl+Space),
	// nil when none is showing — see completion.go.
	completion *completionState

	// showDiagnostics is true while the diagnostic details popup ("K") is
	// up. Transient by design: the very next keypress dismisses it, so it
	// reads like a tooltip rather than another mode to get stuck in.
	showDiagnostics bool

	// gitPopup holds the rows of the git blame ("B") or current-line diff
	// ("H") tooltip, nil when neither is up. Unlike showDiagnostics — whose
	// content is re-derived from t.diagnostics on every Render — these are
	// captured once, when the key is pressed: they come from a git query
	// made at that moment (see BlameFunc/HunkFunc), and re-running git on
	// every frame to redraw the same tooltip would put a subprocess in the
	// render path. Dismissed by the next keypress, exactly like
	// showDiagnostics.
	gitPopup []popupLine

	// hoverText holds the language server's answer to "what is this?" at
	// the cursor ("I"), "" when nothing is showing — see hover.go. Same
	// tooltip lifetime as showDiagnostics: dismissed by the very next
	// keypress, not a mode to get stuck in.
	hoverText string

	// signatureHelp holds the in-progress signature-help popup (Ctrl+a),
	// nil when none is showing — see signaturehelp.go. Same tooltip
	// lifetime as hoverText/showDiagnostics.
	signatureHelp *lsp.SignatureHelp

	// BlameFunc, if set, resolves who last changed path's 1-based line —
	// enabling the "B" blame tooltip. Left nil (the default), "B" does
	// nothing, the same way a language-server-less pane simply has no LSP
	// features. Provided as a callback rather than called directly because
	// this View never talks to git itself; see ApplyLineStatus.
	BlameFunc func(path string, line int) (gitblame.Info, error)

	// HunkFunc, if set, resolves the diff hunk covering path's 0-based line
	// index — enabling the "H" current-line diff tooltip. ok is false when
	// that line is unchanged. Same callback rationale as BlameFunc.
	HunkFunc func(path string, line int) (hunk gitstatus.Hunk, ok bool, err error)

	// OnShowFileDiff, if set, is called with the active tab's path when "D"
	// fires — set by cmd/nib/main.go to open the diff overlay (see
	// internal/ui/diffview). A whole-file diff is a scrollable document
	// rather than a tooltip, so it belongs in an overlay the app owns, not
	// in a popup this pane draws. Same plain-callback pattern as
	// OnAllTabsClosed.
	OnShowFileDiff func(path string)

	// In-file search state (see search.go). searchBuf is what's typed at the
	// "/" prompt; searchPattern is the last committed pattern, which n/N
	// repeat and which stays highlighted. searchOrigin* remembers where the
	// prompt was opened, so Esc can put the cursor back.
	searchBuf                       string
	searchPattern                   string
	searchMatches                   []searchMatch
	searchOriginLn, searchOriginCol int

	// jumpStack holds positions saved by goToParent/goToDefinition (see
	// navigate.go), popped by jumpBack (Ctrl+b). Per-pane, and each entry
	// records a path as well as a position, because a go-to-definition can
	// land in a different file — see pushJump.
	jumpStack []jumpLocation

	// store resolves Open's path to a *Buffer — see BufferStore. Defaults
	// to a private store (below), so a View nobody explicitly shares one
	// with behaves exactly as if buffers were never shared at all.
	store *BufferStore

	// register is the "dd"/"yy"/"p" clipboard — see Register and
	// SetRegister. Defaults to a private one, on the same rationale as
	// store above.
	register *Register

	// CopyFunc, when set, puts text on the system clipboard — wired to
	// App.CopyToClipboard in cmd/nib/main.go. A func field rather than an
	// import for the same reason BlameFunc and HunkFunc are: this package
	// stays free of the terminal and of the OS, and is testable without
	// either. nil (the default) means a copy still fills the yank register,
	// so "p" works and only the crossing-out-of-nib half is missing.
	CopyFunc func(string)

	// dragging is true between a left-button press in the text area and its
	// release — the window during which pointer motion extends the
	// selection. Pane-wide rather than per-tab because it describes the
	// mouse, not a document: a drag cannot survive a tab switch.
	dragging bool

	// dragMoved records whether the pointer actually moved while dragging,
	// which is what tells a drag apart from the press/release pair a plain
	// double- or triple-click also produces. Only a drag that moved copies on
	// release; without this, every double-click would copy twice (once in
	// mousePress, once on the release right behind it) and spawn two clipboard
	// helper processes for one gesture. See HandleMouse.
	dragMoved bool

	// highlights, when non-nil, computes tree-sitter highlighting off the
	// UI goroutine — see Highlighter and submitHighlight. Every pane should
	// share ONE (like store and register), since results land on the shared
	// Buffer.
	//
	// nil means highlight inline instead, exactly as this package did
	// before the worker existed. That keeps a bare NewView() — every test
	// in this package, and any embedder that never wires an event loop —
	// synchronous and deterministic: the highlight is there the moment the
	// edit returns, with no goroutine to wait on.
	highlights *Highlighter

	// lsp, when non-nil, provides real semantic features (diagnostics, go
	// to definition) via language servers — see internal/lsp. Optional by
	// design: nil (the default) means every LSP-backed feature falls back
	// to its tree-sitter/local equivalent, which is also what happens for
	// a file whose language has no server configured.
	//
	// Held as an interface (satisfied by *lsp.Manager) rather than the
	// concrete type purely so this package's tests can substitute a fake
	// instead of spawning real language server subprocesses.
	lsp languageServer

	// welcomeVersion/welcomeFolder/welcomeBranch are the static context shown
	// centered in this pane when it has no tabs open — see SetWelcomeInfo and
	// renderWelcome. welcomeBranch is a func rather than a plain string
	// because the current branch can change after this pane was already
	// showing the empty state (e.g. a checkout via an external tool while
	// nib is running) — reading it live each Render, the same closure-over-a-
	// var pattern cmd/nib/main.go's statusBarView.TextFunc already uses for
	// gitBranch, keeps this pane from needing its own git plumbing or a
	// setter called on every refresh.
	welcomeVersion  string
	welcomeFolder   string
	welcomeBranch   func() string
	welcomeKeybinds []WelcomeKeybind
}

// WelcomeKeybind is one entry in the empty-pane welcome screen's key
// reference — see SetWelcomeInfo.
type WelcomeKeybind struct {
	Key  string
	Desc string
}

// languageServer is the slice of lsp.Manager the editor pane actually uses
// — see View.lsp for why it's an interface.
type languageServer interface {
	Ready(language string) bool
	Status(language string) lsp.ServerStatus
	Open(path, language, text string)
	Change(path, text string)
	Close(path string)
	Definition(path, language string, line, character int, apply func(loc lsp.Location, ok bool)) bool
	Completion(path, language string, line, character int, apply func(items []lsp.CompletionItem, ok bool)) bool
	Hover(path, language string, line, character int, apply func(text string, ok bool)) bool
	SignatureHelp(path, language string, line, character int, apply func(sh lsp.SignatureHelp, ok bool)) bool
	Formatting(path, language string, tabWidth int, apply func(edits []lsp.TextEdit, ok bool)) bool
}

// NewView creates an empty editor pane with no tabs open; call Open to
// load a file into it.
func NewView() *View {
	return &View{tabWidth: 4, keymap: DefaultKeybinds.Resolve(nil), store: NewBufferStore(), register: NewRegister()}
}

// SetBufferStore replaces this pane's BufferStore, so it shares loaded
// buffers with every other View given the same store (e.g. split panes)
// instead of loading its own private copies. Call before opening any
// tabs — an already-open tab's Buffer came from whichever store was
// active when it was opened, and doesn't retroactively move.
func (v *View) SetBufferStore(s *BufferStore) {
	v.store = s
}

// SetRegister replaces this pane's yank/delete register, so "dd"/"yy" in one
// pane and "p" in another share one clipboard — vim's own registers-are-
// global behavior. Every pane should be given the SAME register (see
// cmd/nib/main.go); a nil argument is ignored rather than leaving the pane
// with no register to put from.
func (v *View) SetRegister(r *Register) {
	if r == nil {
		return
	}
	v.register = r
}

// SetHighlighter gives this pane a background highlight worker, so
// keystrokes stop paying for a tree-sitter re-parse. Every pane should
// share ONE (see cmd/nib/main.go): highlights are stored on the shared
// Buffer, and one worker is also what keeps the tree-sitter parsers it
// uses single-goroutine (see highlighterCache).
//
// A nil argument leaves the pane highlighting inline — see View.highlights.
func (v *View) SetHighlighter(h *Highlighter) {
	v.highlights = h
}

// submitHighlight refreshes buf's highlighting after its content or path
// changed: handed to the worker when there is one, computed inline when
// there isn't. immediate skips the worker's debounce, for the one-off
// refreshes that aren't part of a typing burst (open, rename).
//
// The inline path is also why the worker never highlights on the UI
// goroutine and vice versa: a Highlighter and its parsers are safe for one
// goroutine at a time, so a View either has a worker doing all of its
// parsing or does all of it itself.
func (v *View) submitHighlight(buf *Buffer, immediate bool) {
	if buf == nil {
		return
	}
	if v.highlights == nil {
		buf.highlighted = highlightBuffer(buf)
		return
	}
	if immediate {
		v.highlights.SubmitNow(buf)
		return
	}
	v.highlights.Submit(buf)
}

// SetLSPManager gives this pane a language-server manager, enabling
// LSP-backed features for languages it has a server for. Every pane should
// share ONE manager (see cmd/nib/main.go), so a file open in two split
// panes is announced to the server once and its diagnostics reach both.
// Call before opening any tabs — an already-open tab was never registered
// with the server, so it won't retroactively be.
func (v *View) SetLSPManager(m *lsp.Manager) {
	if m == nil {
		v.lsp = nil // avoid a non-nil interface wrapping a nil pointer
		return
	}
	v.lsp = m
}

// SetKeymap merges the user config's "editor" scope overrides on top of
// DefaultKeybinds, replacing the pane's active keymap.
func (v *View) SetKeymap(overrides map[string]string) {
	v.keymap = DefaultKeybinds.Resolve(overrides)
}

// SetWelcomeInfo supplies the context shown centered in this pane whenever
// it has no tabs open (see renderWelcome): nibVersion and folder, fixed for
// the session, plus branchFunc, re-read on every Render so the branch shown
// stays current even while the pane is sitting empty (e.g. across a
// checkout made outside nib) — see welcomeBranch. keybinds is the reference
// list shown below the message; nil/empty omits it.
func (v *View) SetWelcomeInfo(nibVersion, folder string, branchFunc func() string, keybinds []WelcomeKeybind) {
	v.welcomeVersion = nibVersion
	v.welcomeFolder = folder
	v.welcomeBranch = branchFunc
	v.welcomeKeybinds = keybinds
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

// WordUnderCursor returns the identifier-like word touching the active
// tab's cursor, or "" if there's no active tab or the cursor isn't
// touching one — the query cmd/nib/main.go pre-fills the finder's content
// search with when the global "find references" binding (Ctrl+F) fires
// while this pane happens to be focused.
func (v *View) WordUnderCursor() string {
	t := v.activeTab()
	if t == nil {
		return ""
	}
	return wordUnderCursor(t, v.tabWidth)
}

// OpenPaths returns the paths of every open tab, for the caller
// (cmd/nib/main.go) to compute per-file git line status against — the
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

// ApplyDiagnostics sets the language-server diagnostic gutter markers for
// the open tab whose path matches path, redrawn on the next Render — the
// diagnostics counterpart to ApplyLineStatus, and likewise a no-op if
// path isn't open in this pane (each server notification is fanned out to
// every pane, most of which won't have that file open).
//
// diags always REPLACES whatever this tab had, including with nil: LSP's
// publishDiagnostics is a complete restatement per file, so an empty set
// means "this file is clean now" rather than "nothing new to report".
func (v *View) ApplyDiagnostics(path string, diags []lsp.Diagnostic) {
	for _, t := range v.tabs {
		if t.path == path {
			t.diagnostics = diagnosticsByLine(diags)
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
		// Already highlighted if another pane opened this path first.
		// Submitted immediately (no debounce): opening a file isn't part of
		// a typing burst, so there is nothing to coalesce with — the file
		// shows the heuristic colors for the one frame the parse takes.
		v.submitHighlight(buf, true)
	}
	v.tabs = append(v.tabs, t)
	v.active = len(v.tabs) - 1

	// Hand the buffer to the language server (spawning it on the first
	// file of its language) so it can start analyzing and publishing
	// diagnostics. Reference-counted inside the Manager, so the same file
	// open in two panes registers once — mirroring BufferStore above.
	if v.lsp != nil && buf != nil {
		if lang := languageFor(path); lang != "" {
			v.lsp.Open(path, lang, string(buf.Source))
		}
	}
	// Instant parse-error markers, without waiting for (or needing) a
	// language server — see refreshSyntaxDiagnostics.
	v.refreshSyntaxDiagnostics(t)
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

// Repath updates every tab whose file moved from oldPath to newPath —
// either that file itself, or, when oldPath is a directory, every open file
// underneath it, since one folder rename relocates them all at once.
//
// It fans out the consequences a stale tab.path would otherwise cause: the
// shared BufferStore is re-keyed (once, however many panes share the buffer
// — see BufferStore.Rekey) so Buffer.Path follows and ":w" stops recreating
// the file at its old location; the language server is told the old URI
// closed and the new one opened, which also re-picks the server when the new
// extension means a different language; and the jump stack's recorded paths
// are rewritten so Ctrl+b doesn't try to reopen a path that no longer
// exists. Returns how many tabs it touched.
//
// The language-server calls live here rather than in the caller for the same
// reason Open and releaseTab own theirs: this View is what holds the
// Manager, and a repath is exactly an Open/Close pair. Manager's per-tab
// reference counting makes the fan-out safe — two panes each doing
// Close-then-Open leaves both counts exactly where they started.
func (v *View) Repath(oldPath, newPath string) int {
	if oldPath == newPath {
		return 0
	}
	n := 0
	for _, t := range v.tabs {
		np, ok := movedPath(oldPath, newPath, t.path)
		if !ok {
			continue
		}

		// A tab already sitting on np means the move landed on a file this
		// pane has open. Two tabs with the same path would break Open's
		// de-dupe and tabDisplayNames' distinct-paths assumption, so the
		// stale one goes — unless it has unsaved work, in which case nothing
		// is destroyed and this tab is left as it was. The file tree refuses
		// a move onto an existing path, so this is defensive only.
		if other := v.tabForPath(np); other != nil && other != t {
			if other.buf != nil && other.buf.Dirty {
				debuglog.Warn("repath %s -> %s: %s is open with unsaved changes, tab left alone", t.path, np, np)
				continue
			}
			v.removeTabs(map[*tab]bool{other: true})
		}

		if t.buf != nil {
			// The store is the gate: if it can't represent the move, the tab
			// must keep the path it's filed under, or its Release would miss
			// and leak the entry.
			if !v.store.Rekey(t.buf, t.path, np) {
				debuglog.Error("repath %s -> %s: buffer store refused (destination busy)", t.path, np)
				continue
			}
			// Rekey dropped the buffer's highlight, since the new extension
			// can mean a new language entirely (see Buffer.Repath). Rebuild
			// it under the new path — immediately, as a rename is a one-off,
			// not part of a burst.
			v.submitHighlight(t.buf, true)
			if v.lsp != nil {
				v.lsp.Close(t.path)
				if lang := languageFor(np); lang != "" {
					// The buffer's current text, unsaved edits included: that
					// is what the server already believes is open.
					v.lsp.Open(np, lang, string(t.buf.Source))
				}
			}
			if languageFor(t.path) != languageFor(np) {
				t.diagnostics = nil // the old language's problems are meaningless now
			}
		}

		t.path = np
		t.detached = false
		for i := range v.jumpStack {
			if jp, ok := movedPath(oldPath, newPath, v.jumpStack[i].path); ok {
				v.jumpStack[i].path = jp
			}
		}
		v.refreshSyntaxDiagnostics(t)
		n++
	}
	return n
}

// CloseTabsUnder reacts to path having been deleted from disk: it closes
// every tab for path itself or, path being a directory, for any open file
// underneath it — releasing each one's shared Buffer and language-server
// registration exactly as ":q" would.
//
// A tab with unsaved changes is the exception: it stays open, flagged
// detached, and its path is returned. Deleting a file must never be a way to
// silently discard edits — this package refuses ":q" on a dirty buffer and
// makes you type ":q!" (see closeActiveTab) — and since the deletion has
// already happened by the time this runs, refusing outright isn't on the
// table. A detached tab is what vim does when a file vanishes underneath it:
// the buffer is still yours, ":w" recreates the file, ":q!" throws it away.
// Its git gutter is cleared, there being no file left to diff against, and
// its language-server registration is deliberately KEPT: an open document
// with unsaved client-side content is precisely what LSP's didOpen means.
func (v *View) CloseTabsUnder(path string) (detached []string) {
	doomed := map[*tab]bool{}
	for _, t := range v.tabs {
		if !pathUnder(path, t.path) {
			continue
		}
		if t.buf != nil && t.buf.Dirty {
			t.detached = true
			t.lineStatus = nil
			detached = append(detached, t.path)
			continue
		}
		doomed[t] = true
	}
	if len(doomed) > 0 {
		v.removeTabs(doomed)
	}
	// Entries pointing into the deleted subtree can no longer be jumped
	// back to, so drop them rather than reopening an error tab later.
	kept := v.jumpStack[:0]
	for _, j := range v.jumpStack {
		if !pathUnder(path, j.path) {
			kept = append(kept, j)
		}
	}
	v.jumpStack = kept
	return detached
}

// tabForPath returns the open tab for path, or nil.
func (v *View) tabForPath(path string) *tab {
	for _, t := range v.tabs {
		if t.path == path {
			return t
		}
	}
	return nil
}

// removeTabs drops every tab in doomed (by identity), keeping the active
// selection on the same tab when it survived and falling back to a clamped
// index when it didn't, releases each removed tab's Buffer and
// language-server registration, and fires OnAllTabsClosed if the pane is
// left empty.
//
// CloseTab's index arithmetic deliberately isn't reused: it removes exactly
// one tab at a known index, which doesn't generalize to removing a set.
func (v *View) removeTabs(doomed map[*tab]bool) {
	if len(doomed) == 0 || len(v.tabs) == 0 {
		return
	}
	wasActive := v.tabs[v.active]
	kept := make([]*tab, 0, len(v.tabs))
	removed := make([]*tab, 0, len(doomed))
	for _, t := range v.tabs {
		if doomed[t] {
			removed = append(removed, t)
			continue
		}
		kept = append(kept, t)
	}

	prevActive := v.active
	v.tabs = kept
	v.active = 0
	if !doomed[wasActive] {
		for i, t := range v.tabs {
			if t == wasActive {
				v.active = i
				break
			}
		}
	} else if prevActive >= len(v.tabs) {
		v.active = len(v.tabs) - 1
	} else {
		v.active = prevActive
	}
	if v.active < 0 {
		v.active = 0
	}

	for _, t := range removed {
		v.releaseTab(t)
	}
	v.notifyIfEmpty()
}

// releaseTab releases t's Buffer back to the store and its language-server
// registration (if it successfully loaded one — a failed load never
// registered either), decrementing both reference counts; see
// BufferStore.Release and lsp.Manager.Close.
func (v *View) releaseTab(t *tab) {
	if t.buf == nil {
		return
	}
	v.store.Release(t.path)
	if v.lsp != nil {
		v.lsp.Close(t.path)
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
	if v.mode == modeSearch {
		return "/" + v.searchBuf
	}
	prefix := ""
	if v.mode == modeInsert {
		prefix = "-- INSERT -- "
	}
	// The file this tab came from was deleted while the buffer still had
	// unsaved changes (see CloseTabsUnder). Said here because the in-pane
	// "error:" line only ever shows for a tab with NO buffer, so this status
	// prefix and the tab bar's own marker are the only places the user can
	// find out without opening the debug log.
	if t.detached {
		prefix += "-- DELETED -- "
	}
	return fmt.Sprintf("%sLn %d, Col %d", prefix, t.cursorLn+1, t.cursorCol+1)
}

// Glyphs for the language-server indicator in LanguageStatus. A filled dot
// reads as "on", a hollow one as "set up but not on", and no glyph at all
// as "nib has no server for this language" — see lsp.ServerStatus for why
// those last two are worth telling apart.
const (
	lspRunningGlyph    = "●"
	lspNotRunningGlyph = "○"
)

// LanguageStatus is the active tab's detected language plus a compact
// language-server indicator, for the status bar (see cmd/nib/main.go):
// "go ●" when a server is running, "go ○" when one is configured but not
// running, plain "go" when nib has no server for that language, and "" if
// no file is open or no grammar recognizes it.
func (v *View) LanguageStatus() string {
	t := v.activeTab()
	if t == nil || t.buf == nil {
		return ""
	}
	lang := languageFor(t.path)
	if lang == "" {
		return ""
	}
	if v.lsp == nil {
		return lang
	}
	switch v.lsp.Status(lang) {
	case lsp.StatusRunning:
		return lang + " " + lspRunningGlyph
	case lsp.StatusNotRunning:
		return lang + " " + lspNotRunningGlyph
	default:
		return lang
	}
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
		v.renderWelcome(w, cols, rows)
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
	renderBody(w, t, v.tabWidth, cols, bodyRows, 1, v.searchMatches)

	// Popups draw last so they sit on top of the file content. Only one can
	// be up at a time: completion belongs to Insert mode, the rest
	// (diagnostic, hover, signature-help, and git tooltips) to Normal mode.
	if v.completion != nil {
		if col, row, ok := v.CursorPosition(); ok {
			v.renderCompletionPopup(w, cols, rows, col, row)
		}
	} else if v.showDiagnostics && t != nil {
		if col, row, ok := v.CursorPosition(); ok {
			lines := diagnosticPopupLines(t.diagnostics[t.cursorLn], cols-col)
			renderPopup(w, cols, rows, col, row, lines, -1)
		}
	} else if v.hoverText != "" && t != nil {
		if col, row, ok := v.CursorPosition(); ok {
			lines := wrapText(v.hoverText, cols-col)
			if len(lines) > maxHoverPopupRows {
				lines = lines[:maxHoverPopupRows]
			}
			renderPopup(w, cols, rows, col, row, lines, -1)
		}
	} else if v.signatureHelp != nil && t != nil {
		if col, row, ok := v.CursorPosition(); ok {
			lines := signatureHelpLines(*v.signatureHelp, cols-col)
			renderPopup(w, cols, rows, col, row, lines, -1)
		}
	} else if v.gitPopup != nil {
		if col, row, ok := v.CursorPosition(); ok {
			renderStyledPopup(w, cols, rows, col, row, v.gitPopup, -1)
		}
	}
}

// renderWelcome draws this pane's empty-tab-list state: nib's name and
// version, the open folder and its git branch (if either SetWelcomeInfo
// supplied), the usual "no file open" prompt, and a short key-binding
// reference — all centered in the pane, since there is no document content
// or tab bar to anchor to instead.
func (v *View) renderWelcome(w layout.Window, cols, rows int) {
	bold := layout.Style{Attr: layout.AttrBold}
	dim := layout.Style{Attr: layout.AttrDim}

	var lines [][]layout.Segment

	header := "nib"
	if v.welcomeVersion != "" {
		header += " " + v.welcomeVersion
	}
	lines = append(lines, []layout.Segment{{Text: header, Style: bold}})

	if v.welcomeFolder != "" {
		loc := v.welcomeFolder
		if v.welcomeBranch != nil {
			if branch := v.welcomeBranch(); branch != "" {
				loc += " (" + branch + ")"
			}
		}
		lines = append(lines, []layout.Segment{{Text: loc, Style: dim}})
	}

	lines = append(lines,
		nil,
		[]layout.Segment{{Text: "No file open — select a file in the tree and press Enter", Style: dim}},
	)

	if len(v.welcomeKeybinds) > 0 {
		lines = append(lines, nil)
		for _, kb := range v.welcomeKeybinds {
			lines = append(lines, []layout.Segment{
				{Text: kb.Key, Style: bold},
				{Text: "  " + kb.Desc, Style: dim},
			})
		}
	}

	renderCenteredLines(w, cols, rows, lines)
}

// renderCenteredLines draws lines vertically centered within rows and each
// line horizontally centered within cols, by measuring its segments'
// combined display width (textwidth.DisplayWidth, so wide runes are
// accounted for) and padding on the left with plain spaces — w.Clear()
// (already called by Render before this runs) takes care of the right-hand
// side, so no trailing pad is needed. A nil entry in lines is a blank
// separator row.
func renderCenteredLines(w layout.Window, cols, rows int, lines [][]layout.Segment) {
	top := (rows - len(lines)) / 2
	if top < 0 {
		top = 0
	}
	for i, segs := range lines {
		row := top + i
		if row < 0 || row >= rows {
			continue
		}
		if len(segs) == 0 {
			continue
		}
		width := 0
		for _, s := range segs {
			width += textwidth.DisplayWidth(s.Text)
		}
		pad := (cols - width) / 2
		if pad > 0 {
			segs = append([]layout.Segment{{Text: strings.Repeat(" ", pad)}}, segs...)
		}
		w.Println(row, segs...)
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
		if t.detached {
			// Its file was deleted from under it (see CloseTabsUnder) — so
			// an inactive tab in that state is still visible, not only the
			// active one via StatusText.
			names[i] += " ✗"
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

func renderBody(w layout.Window, t *tab, tabWidth, cols, rows, rowOffset int, searchMatches []searchMatch) {
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

	// The selection is read off the tab rather than passed in, like the
	// cursor and scroll offsets this function already reads from it — and
	// unlike searchMatches, which belongs to the pane because the search
	// prompt does.
	selStart, selEnd, hasSel := t.selectionSpan()

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
		// Search matches are overlaid while the segments are still raw,
		// because that's the only stage where a rune index means the same
		// thing to the highlighter, the cursor, and findMatches alike.
		if ranges := matchRangesOnLine(searchMatches, ln); len(ranges) > 0 {
			raw = applyHighlightRanges(raw, ranges, searchHighlightStyle)
		}
		// The selection goes on after the search highlight, and at the same
		// raw stage, so a match inside a selection shows both.
		selToEOL := false
		if hasSel {
			var selRanges []runeRange
			selRanges, selToEOL = selectionRangesOnLine(selStart, selEnd, t.buf.Lines[ln], ln, tabWidth)
			if len(selRanges) > 0 {
				raw = applyHighlightRanges(raw, selRanges, selectionStyle)
			}
		}
		expandedSegs := textwidth.ExpandTabsSegments(raw, tabWidth)
		visible := textwidth.SliceSegmentsByDisplayColumn(expandedSegs, t.leftCol, contentWidth)
		if selToEOL {
			// A line whose selection runs past its last rune has the line
			// break selected too, so the highlight continues to the right
			// edge. This is also the only thing that renders a selected
			// BLANK line: an empty line produces no segments at all, and
			// applyHighlightRanges has nothing to split — so without this
			// pad, a selection spanning a blank line would appear to skip it.
			visible = padSelection(visible, contentWidth)
		}

		worst := worstSeverity(t.diagnostics[ln])
		diagSeg := layout.Segment{
			Text:  diagnosticMarker(worst),
			Style: diagnosticStyle(worst),
		}
		diffSeg := layout.Segment{
			Text:  gitstyle.LineMarker(t.lineStatus[ln]),
			Style: gitstyle.LineStyle(t.lineStatus[ln]),
		}
		gutterSeg := layout.Segment{
			Text:  fmt.Sprintf("%*d ", gutterWidth-3, ln+1),
			Style: layout.Style{Attr: layout.AttrDim},
		}
		w.Println(rowOffset+i, append([]layout.Segment{diagSeg, diffSeg, gutterSeg}, visible...)...)
	}
}

// padSelection extends already-clipped segments with selection-styled
// spaces out to width, so a selected line break reads as "the rest of this
// row is selected" rather than stopping ragged at the end of the text.
// A no-op once the row is already full.
func padSelection(segs []layout.Segment, width int) []layout.Segment {
	used := 0
	for _, s := range segs {
		used += textwidth.DisplayWidth(s.Text)
	}
	if used >= width {
		return segs
	}
	return append(segs, layout.Segment{
		Text:  strings.Repeat(" ", width-used),
		Style: selectionStyle,
	})
}

// gutterWidthFor is the width of everything left of a line's text: a
// diagnostic marker column (see ApplyDiagnostics), a git-diff marker
// column (see ApplyLineStatus), the line-number digits, and 1 trailing
// space — so 3 fixed columns plus however many digits the buffer's line
// count needs.
func gutterWidthFor(t *tab) int {
	if t.buf == nil {
		return 3
	}
	return len(fmt.Sprintf("%d", len(t.buf.Lines))) + 3
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

	// The diagnostic, hover, signature-help, and git popups are tooltips,
	// not modes: whatever you press next dismisses them and is then handled
	// normally.
	if v.showDiagnostics {
		v.showDiagnostics = false
	}
	v.gitPopup = nil
	v.hoverText = ""
	v.signatureHelp = nil

	switch v.mode {
	case modeInsert:
		return v.handleInsertKey(k)
	case modeCommand:
		return v.handleCommandKey(k)
	case modeSearch:
		return v.handleSearchKey(k)
	case modeNormal:
		// Falls through to the Normal-mode keymap below.
	}

	// A digit (other than a bare "0", vim's own line_start motion) always
	// accumulates into the count prefix ahead of everything else — the "3"
	// of "3dd" or "3j" is never itself an action, whether or not an
	// operator happens to be armed (so "d0" still reaches the operator
	// below as the line_start MOTION, not as a count).
	if n, isDigit := normalModeDigit(k); isDigit && (n != 0 || v.count > 0) {
		v.count = v.count*10 + n
		return true
	}

	action, ok := v.keymap[k.String()]

	// A key arriving while an operator is armed either completes it (the
	// doubled form, a motion, a text object) or aborts it — see
	// tryCompleteOperator. An abort falls through to dispatch this same key
	// fresh below, exactly as if no operator had been pending.
	if v.pendingOp.action != "" && v.tryCompleteOperator(action, ok) {
		return true
	}

	t := v.activeTab()
	if ok && isOperatorAction(action) {
		// With a selection up, "y" copies it on the FIRST press — vim's own
		// behaviour, where a visual-mode yank needs no doubling because the
		// range is already chosen. The selection is consumed along with it,
		// so a following "y" is an ordinary "yy" again. Delete/change have
		// no equivalent shortcut: nib has no keyboard-driven Visual mode to
		// have selected FOR them.
		if action == "yank_line" && t != nil && t.hasSel {
			v.copySelection(t)
			t.clearSelection()
			v.count = 0
			return true
		}
		v.pendingOp = pendingOperator{action: action, count: v.count}
		v.count = 0
		return true
	}

	count := resolvedCount(v.count)
	v.count = 0
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
	case "search_mode":
		v.enterSearchMode()
		return true
	case "save":
		v.saveActive()
		return true
	}

	if t == nil {
		return false
	}

	switch action {
	case "normal_mode":
		// Esc in Normal mode has no mode to leave, but it does dismiss a
		// selection. Deliberately only consumed when there IS one: an Esc
		// with nothing to dismiss has always bubbled to the global keymap,
		// and swallowing it here would quietly break that.
		if !t.hasSel {
			return false
		}
		t.clearSelection()
		return true
	case "undo":
		v.undo(t)
	case "redo":
		v.redo(t)
	case "delete_char_forward":
		v.deleteCharForward(t)
	case "delete_char_backward":
		v.deleteCharBackward(t)
	case "put_after":
		v.putAfter(t)
	case "go_to_parent":
		v.goToParent(t)
	case "go_to_definition":
		v.goToDefinition(t)
	case "jump_back":
		v.jumpBack()
	case "search_next":
		v.searchNext()
	case "search_prev":
		v.searchPrev()
	case "show_diagnostics":
		// Only worth a popup if the line actually has something to say.
		v.showDiagnostics = len(t.diagnostics[t.cursorLn]) > 0
	case "show_hover":
		v.triggerHover(t)
	case "trigger_signature_help":
		v.triggerSignatureHelp()
	case "format_document":
		v.triggerFormat()
	case "show_blame":
		v.showBlame(t)
	case "show_line_diff":
		v.showLineDiff(t)
	case "show_file_diff":
		if v.OnShowFileDiff != nil && t.path != "" {
			v.OnShowFileDiff(t.path)
		}
	default:
		if !v.applyMovement(t, action, count) {
			return false
		}
	}

	v.clamp(t)
	return true
}

// HandlePaste implements layout.Paster: it inserts s as a single operation
// instead of routing it through HandleKey one character at a time, which is
// what lets a multi-line paste actually produce multiple lines rather than
// gluing every pasted line onto the cursor's line (see internal/ui/app.go's
// bracketed-paste accumulation, which is what assembles s in the first
// place).
//
// Command and Search mode have no use for embedded newlines — both are
// single-line prompts — so a paste there just drops them rather than
// committing/searching partway through, the way a literal Enter keystroke
// would.
func (v *View) HandlePaste(s string) bool {
	if s == "" {
		return true
	}

	switch v.mode {
	case modeCommand:
		v.commandBuf += strings.ReplaceAll(s, "\n", "")
		return true
	case modeSearch:
		v.searchBuf += strings.ReplaceAll(s, "\n", "")
		v.refreshSearchHighlights()
		return true
	case modeNormal, modeInsert:
		// Falls through to pasting into the buffer below.
	}

	t := v.activeTab()
	if t == nil || t.buf == nil {
		return true
	}

	// Pasting in Normal mode inserts the same as pasting in Insert mode —
	// bracketed paste exists precisely so a terminal app can tell "this is
	// pasted text" apart from "this is typed commands" and treat it as
	// literal content either way. Wrapping it in enter/exitInsertMode gives
	// it its own undo entry, the same one Insert-mode typing gets.
	wasInsert := v.mode == modeInsert
	if !wasInsert && !v.enterInsertMode() {
		return true
	}
	v.completion = nil // a pasted block isn't a continuation of any open completion

	lines := strings.Split(s, "\n")
	for i, line := range lines {
		if line != "" {
			v.insertText(line)
		}
		if i < len(lines)-1 {
			v.insertNewline()
		}
	}

	if !wasInsert {
		v.exitInsertMode()
	}
	return true
}

// applyMovement mutates t's cursor for a Normal-mode movement action,
// shared with handleInsertKey (arrow keys move the cursor even while
// inserting — see there, always passing count 1). Returns false if action
// isn't a movement action, so callers can tell "not a movement" apart from
// "movement handled, cursor happens not to have changed".
//
// count repeats the motion vim's own number-of-times ("3j", "5l"); actions
// that jump to an absolute place (line_start/line_end/first_line/last_line)
// or already read v.pageSize() ignore it, since repeating an idempotent jump
// N times lands in the same place as doing it once.
func (v *View) applyMovement(t *tab, action string, count int) bool {
	switch action {
	case "move_down":
		t.cursorLn += count
	case "move_up":
		t.cursorLn -= count
	case "move_left":
		t.cursorCol -= count
	case "move_right":
		t.cursorCol += count
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
	case "word_forward", "word_backward", "word_end":
		raw := rawIndexForExpandedCol(t.buf.Lines[t.cursorLn], t.cursorCol, v.tabWidth)
		newLn, newRaw := motionDestination(t.buf, t.cursorLn, raw, action, count)
		t.cursorLn = newLn
		t.cursorCol = expandedColForRawIndex(t.buf.Lines[newLn], newRaw, v.tabWidth)
	default:
		return false
	}
	// Moving the cursor collapses any mouse selection, the way it does in
	// every editor: the cursor is the selection's own moving end (see
	// tab.selAnchor), so leaving it set would silently turn an arrow key into
	// a selection-extending gesture. Only on an actual movement — the
	// default branch above returns before this.
	t.clearSelection()
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
	case "trigger_signature_help":
		v.triggerSignatureHelp()
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
				v.applyMovement(t, v.keymap[k.String()], 1)
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
	case "x":
		v.saveActive()
		v.closeActiveTab(false)
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
	// The typing is done, so the code should be parseable again — now is
	// when syntax errors are worth showing (see onBufferEdited's note on
	// why not during the session).
	v.refreshSyntaxDiagnostics(t)
}

// ExitEditingModes returns the pane to Normal mode, discarding an
// in-progress Command or Search prompt and committing (or discarding, if
// nothing was typed) an in-progress Insert session — exactly what Esc
// would do in any of the three. Meant to be called when focus moves away
// from this pane
// (see cmd/nib/main.go's focus-change wiring): a mouse click can switch
// focus without ever routing a key through the losing pane's HandleKey
// (unlike Tab-cycling, which Insert mode's own key-trap already blocks),
// so without this a pane could be left "stuck" mid-Insert-session
// indefinitely. That matters now that buffers can be shared across panes
// (see BufferStore) — two panes simultaneously mid-session on the same
// buffer would scramble whose pending snapshot ends up committed to its
// undo history; this guarantees at most one pane ever is.
func (v *View) ExitEditingModes() {
	// A half-typed "dd" (or "dw", "diw", ...) is discarded for the same
	// reason: the pane is losing focus, so its next key would otherwise
	// complete an operator armed arbitrarily long ago.
	v.pendingOp = pendingOperator{}
	v.count = 0
	// Likewise a drag interrupted by focus moving elsewhere: the release that
	// would have ended it is going to another pane, so nothing else would ever
	// clear this. The selection ITSELF is kept — it survives losing focus the
	// way the cursor position does, so clicking back into the pane and
	// pressing "y" still copies what's highlighted.
	v.dragging = false
	switch v.mode {
	case modeInsert:
		v.exitInsertMode()
	case modeCommand:
		v.mode = modeNormal
		v.commandBuf = ""
	case modeSearch:
		// Same as pressing Esc mid-search: restores the cursor to where
		// the prompt was opened and clears the in-progress highlights, so
		// a pane doesn't stay "stuck" showing a half-typed "/" query (and
		// its stale match highlights) after focus moves away from it —
		// the same hazard this function exists for in Insert/Command mode.
		v.cancelSearch()
	case modeNormal:
		// Nothing to exit.
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
	v.onBufferEdited(t)
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
	v.onBufferEdited(t)
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
	v.onBufferEdited(t)
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
	v.onBufferEdited(t)
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
	v.onBufferEdited(t)
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
	v.onBufferEdited(t)
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
	v.onBufferEdited(t)
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
	v.onBufferEdited(t)
	v.clamp(t)
}

// onBufferEdited is the single funnel every buffer mutation runs through
// once it's applied: it queues the buffer for re-highlighting and pushes
// the new contents to the language server, if any, so its diagnostics and
// definition answers reflect what's actually on screen.
//
// The highlight is queued rather than computed here — that one line used
// to be the whole cost of a keystroke (236ms on an 1800-line Go file). The
// edit itself has already nil'd the lines it touched, so they render with
// the heuristic until the worker answers. See submitHighlight.
func (v *View) onBufferEdited(t *tab) {
	if t.buf == nil {
		return
	}
	v.submitHighlight(t.buf, false)
	if v.lsp != nil {
		v.lsp.Change(t.path, string(t.buf.Source))
	}
	// Unlike the diagnostics refresh below, this runs on every edit
	// including mid-Insert-session: a stale match highlight sitting on top
	// of text that no longer reads as the pattern is wrong at every
	// keystroke, not just once the edit completes. Cheap enough to redo
	// every time — a linear substring scan, the same cost stepSearch
	// already pays on every n/N.
	if v.searchPattern != "" {
		v.refreshSearchMatchesForPattern()
	}
	// Deliberately NOT while typing: mid-edit code is almost always
	// momentarily unparseable, so refreshing here would flag an error under
	// the cursor on nearly every keystroke. Normal-mode edits (x, X, undo,
	// redo) are complete changes, so those do refresh; an Insert session
	// refreshes once when it ends (see exitInsertMode).
	if v.mode != modeInsert {
		v.refreshSyntaxDiagnostics(t)
	}
}

// refreshSyntaxDiagnostics recomputes t's parse-error markers from
// tree-sitter (see syntaxDiagnostics) — but only when no language server is
// running for the file, because a server reports syntax errors too (with
// better messages), and showing both would double up every marker.
//
// The two sources share tab.diagnostics rather than being merged at render
// time. That works because of this rule: whenever a server is live it owns
// the field outright, and when it isn't, tree-sitter does. It also gives a
// nice property on open — a cold server takes a second or two to start, so
// tree-sitter's instant markers show first and are then replaced by the
// server's richer ones once it warms up.
func (v *View) refreshSyntaxDiagnostics(t *tab) {
	if t.buf == nil {
		return
	}
	if v.lsp != nil && v.lsp.Ready(languageFor(t.path)) {
		return
	}
	t.diagnostics = diagnosticsByLine(syntaxDiagnostics(t.buf))
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
	if err := v.saveTab(t); err != nil {
		debuglog.Error("save %s: %v", t.buf.Path, err)
	}
}

// saveTab saves t's buffer to disk. The file exists again on success, so
// the tab is no longer detached (see CloseTabsUnder). Note a save can
// still fail with ENOENT when it was the tab's parent DIRECTORY that was
// deleted — Dirty stays true in that case, which is the honest outcome.
func (v *View) saveTab(t *tab) error {
	if err := t.buf.Save(); err != nil {
		return err
	}
	t.detached = false
	return nil
}

// DirtyPaths returns the paths of every tab open in this pane whose
// buffer has unsaved changes — used by cmd/nib/main.go to warn before
// quitting would silently discard them.
func (v *View) DirtyPaths() []string {
	var paths []string
	for _, t := range v.tabs {
		if t.buf != nil && t.buf.Dirty {
			paths = append(paths, t.path)
		}
	}
	return paths
}

// SaveDirtyTabs saves every tab open in this pane with unsaved changes,
// returning the error for each save that failed, keyed by path. A buffer
// shared with another pane (see BufferStore) that this pane's own dirty
// scan just saved is simply no longer Dirty by the time that other pane's
// SaveDirtyTabs runs, so it costs nothing to call this once per pane.
func (v *View) SaveDirtyTabs() map[string]error {
	var failed map[string]error
	for _, t := range v.tabs {
		if t.buf == nil || !t.buf.Dirty {
			continue
		}
		if err := v.saveTab(t); err != nil {
			if failed == nil {
				failed = map[string]error{}
			}
			failed[t.path] = err
		}
	}
	return failed
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
