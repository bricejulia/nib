package lsp

import (
	"sync"

	"github.com/bricejulia/nib/internal/debuglog"
)

// DefaultServers maps a language name (as reported by
// gotreesitter/grammars' language detection, which the editor already uses
// to pick a highlighter — see editor.languageFor) to the command that
// starts its language server.
//
// This map is the ONLY per-language knowledge in this package, and it's
// data rather than code: supporting another language is one more entry,
// never a new code path. Users add or override entries with "lsp" lines in
// nib's config file (see internal/config), which merge over these — so
// this list only needs to cover sensible defaults, not every language.
//
// An entry whose binary isn't installed costs nothing: the failure is
// logged once and that language falls back to nib's tree-sitter features.
//
// PHP defaults to Intelephense as the most widely used option; its free
// tier covers everything nib asks for (diagnostics, definitions,
// completion — the paid features are refactorings nib doesn't do).
// Phpactor is a fully open-source alternative, one config line away.
//
// TypeScript, JavaScript, and TSX share one server, typescript-language-server,
// the standard community TS/JS server. There's no separate entry for JSX:
// grammar detection resolves .jsx to the "javascript" language same as .js,
// so the "javascript" entry already covers it.
var DefaultServers = map[string][]string{
	"go":         {"gopls"},
	"php":        {"intelephense", "--stdio"},
	"javascript": {"typescript-language-server", "--stdio"},
	"typescript": {"typescript-language-server", "--stdio"},
	"tsx":        {"typescript-language-server", "--stdio"},
}

// DiagnosticsEvent is posted to the UI goroutine when a server publishes
// diagnostics for a file. Path is a filesystem path (not a URI) so the
// receiving code can match it against open tabs directly.
type DiagnosticsEvent struct {
	Path        string
	Diagnostics []Diagnostic
}

// AsyncResult carries the outcome of a request/response call back to the
// UI goroutine. Apply is a closure built by whoever made the request, so
// no per-feature routing is needed where these are received — the event
// loop just calls Apply. Every future request-based feature (completion,
// references, hover) reuses this same envelope.
type AsyncResult struct {
	Apply func()
}

// ServerStatus describes what language-server support exists for a
// language, for display in the UI (see editor.View.LanguageStatus).
// Distinguishing "none configured" from "configured but not running"
// matters: the first means nib was never set up for that language, while
// the second usually means the server binary is missing or failed to
// start — very different things for someone wondering why they're not
// getting diagnostics.
type ServerStatus int

const (
	// StatusNone means no server is configured for this language.
	StatusNone ServerStatus = iota
	// StatusNotRunning means a server is configured but isn't running:
	// either no file of that language has been opened yet, or starting it
	// failed (which is logged — see clientFor).
	StatusNotRunning
	// StatusRunning means a server is up and answering.
	StatusRunning
)

// serverClient is the slice of *Client the Manager depends on, so the
// Manager's own logic (refcounting, versioning, dispatch) can be tested
// against a fake without spawning a real subprocess.
type serverClient interface {
	didOpen(path, languageID, text string, version int) error
	didChange(path, text string, version int) error
	didClose(path string) error
	definition(path string, line, character int) (Location, bool, error)
	completion(path string, line, character int) ([]CompletionItem, error)
	hover(path string, line, character int) (string, bool, error)
	signatureHelp(path string, line, character int) (SignatureHelp, bool, error)
	formatting(path string, tabWidth int, insertSpaces bool) ([]TextEdit, error)
	Close() error
}

// openFile tracks one document registered with a server: how many editor
// tabs currently have it open (so a file open in two split panes is
// announced to the server exactly once), and the document version, which
// LSP requires to increase on every change.
type openFile struct {
	count    int
	version  int
	language string
}

// Manager owns one language server per language, spawned on demand, plus
// the bookkeeping of which documents are open and at what version.
//
// Unlike most state in nib, this IS touched from more than one goroutine:
// diagnostics arrive on the JSON-RPC read goroutine and definition
// requests run on their own goroutines, so the maps are mutex-guarded.
// Results always cross back onto the UI goroutine through Post before
// touching any View/Buffer state.
type Manager struct {
	root    string
	servers map[string][]string

	// Post marshals a value onto the UI event loop (wired to ui.App.Post
	// by cmd/nib/main.go — the same field shape finder.View.Post uses).
	// Anything arriving from a server goroutine must go through this
	// rather than touching editor state directly. A nil Post silently
	// drops async results, which is what makes the Manager safe to use in
	// tests with no event loop running.
	Post func(ev interface{})

	mu sync.Mutex
	// clients holds only servers that started successfully. tried records
	// every language a start was ATTEMPTED for, so a missing binary is
	// reported once and never retried — the same "remember the failure"
	// approach editor.highlighterCache uses for broken grammars. Keeping
	// them separate (rather than a nil entry in clients meaning "failed")
	// is what lets Status tell a failed start apart from a language that
	// was never configured in the first place.
	clients map[string]serverClient
	tried   map[string]bool
	open    map[string]*openFile
}

// NewManager returns a Manager rooted at absRoot (the project directory
// servers are told to index), using DefaultServers.
func NewManager(absRoot string) *Manager {
	return &Manager{
		root:    absRoot,
		servers: DefaultServers,
		clients: map[string]serverClient{},
		tried:   map[string]bool{},
		open:    map[string]*openFile{},
	}
}

// SetServers replaces the language->command registry, for tests and (in a
// follow-up) config-file overrides.
func (m *Manager) SetServers(servers map[string][]string) {
	m.mu.Lock()
	defer m.mu.Unlock()
	m.servers = servers
}

// clientFor returns the running server for language, spawning it on first
// use. Returns nil if the language has no configured server, or if
// spawning/handshaking failed (once, remembered). Callers must hold m.mu.
func (m *Manager) clientFor(language string) serverClient {
	if language == "" {
		return nil
	}
	if c, ok := m.clients[language]; ok {
		return c
	}
	if m.tried[language] {
		return nil // a remembered failure: don't retry every file open
	}
	m.tried[language] = true

	command, ok := m.servers[language]
	if !ok {
		return nil // no server configured for this language
	}

	c, err := newClient(m.root, command, m.handleDiagnostics)
	if err != nil {
		// Same graceful degradation as a missing fsnotify watcher: log it
		// once and carry on. The editor falls back to its tree-sitter
		// features for this language.
		debuglog.Warn("lsp: %s server (%s) unavailable: %v", language, command[0], err)
		return nil
	}
	debuglog.Info("lsp: started %s server (%s)", language, command[0])
	m.clients[language] = c
	return c
}

// handleDiagnostics receives a server's publishDiagnostics notification on
// the JSON-RPC read goroutine and forwards it to the UI goroutine.
func (m *Manager) handleDiagnostics(params PublishDiagnosticsParams) {
	path := uriToPath(params.URI)
	if path == "" || m.Post == nil {
		return
	}
	m.Post(DiagnosticsEvent{Path: path, Diagnostics: params.Diagnostics})
}

// Status reports what language-server support exists for language, for the
// UI to display (see editor.View.LanguageStatus). Like Ready, it never
// spawns anything — a status check must not have the side effect of
// launching a subprocess.
func (m *Manager) Status(language string) ServerStatus {
	if language == "" {
		return StatusNone
	}
	m.mu.Lock()
	defer m.mu.Unlock()

	if m.clients[language] != nil {
		return StatusRunning
	}
	if _, configured := m.servers[language]; configured {
		return StatusNotRunning
	}
	return StatusNone
}

// Ready reports whether language has a running server — i.e. whether
// LSP-backed features can be attempted for it, as opposed to falling back
// to nib's own tree-sitter equivalents.
func (m *Manager) Ready(language string) bool {
	return m.Status(language) == StatusRunning
}

// Open registers path (contents text, in the given language) with the
// appropriate server, spawning it if this is the first file of that
// language. Reference-counted: opening the same path from a second editor
// pane bumps the count without re-announcing it to the server.
func (m *Manager) Open(path, language, text string) {
	m.mu.Lock()
	defer m.mu.Unlock()

	if f, ok := m.open[path]; ok {
		f.count++
		return
	}
	c := m.clientFor(language)
	if c == nil {
		return
	}

	f := &openFile{count: 1, version: 1, language: language}
	m.open[path] = f
	if err := c.didOpen(path, language, text, f.version); err != nil {
		debuglog.Warn("lsp: didOpen %s: %v", path, err)
	}
}

// Change tells the server path's contents are now text. A no-op for a
// path that was never opened (e.g. a language with no server).
func (m *Manager) Change(path, text string) {
	m.mu.Lock()
	defer m.mu.Unlock()

	f, ok := m.open[path]
	if !ok {
		return
	}
	c := m.clients[f.language]
	if c == nil {
		return
	}
	f.version++
	if err := c.didChange(path, text, f.version); err != nil {
		debuglog.Warn("lsp: didChange %s: %v", path, err)
	}
}

// Close drops one reference to path, telling the server the document is
// closed once no editor tab has it open anymore.
func (m *Manager) Close(path string) {
	m.mu.Lock()
	defer m.mu.Unlock()

	f, ok := m.open[path]
	if !ok {
		return
	}
	f.count--
	if f.count > 0 {
		return
	}
	delete(m.open, path)

	if c := m.clients[f.language]; c != nil {
		if err := c.didClose(path); err != nil {
			debuglog.Warn("lsp: didClose %s: %v", path, err)
		}
	}
}

// Definition asks language's server where the symbol at (line, character)
// in path is defined, delivering the answer to apply on the UI goroutine
// via Post. Returns false if no request could be made at all (no server
// for that language), so callers can immediately fall back to a local
// implementation rather than waiting for a response that will never come.
//
// The request itself runs on its own goroutine because it blocks; apply is
// called from the event loop, so it may safely touch editor state.
func (m *Manager) Definition(path, language string, line, character int, apply func(loc Location, ok bool)) bool {
	m.mu.Lock()
	c := m.clients[language]
	m.mu.Unlock()
	if c == nil {
		return false
	}

	go func() {
		loc, found, err := c.definition(path, line, character)
		if err != nil {
			debuglog.Warn("lsp: definition %s: %v", path, err)
			found = false
		}
		if m.Post == nil {
			return
		}
		m.Post(AsyncResult{Apply: func() { apply(loc, found) }})
	}()
	return true
}

// Completion asks language's server what could be inserted at (line,
// character), delivering the answer to apply on the UI goroutine via Post.
// Returns false if no request could be made (no server for that language),
// so callers can fall back to a local candidate source immediately.
//
// Same async shape as Definition — see there for why the request runs on
// its own goroutine.
func (m *Manager) Completion(path, language string, line, character int, apply func(items []CompletionItem, ok bool)) bool {
	m.mu.Lock()
	c := m.clients[language]
	m.mu.Unlock()
	if c == nil {
		return false
	}

	go func() {
		items, err := c.completion(path, line, character)
		if err != nil {
			debuglog.Warn("lsp: completion %s: %v", path, err)
			items = nil
		}
		if m.Post == nil {
			return
		}
		m.Post(AsyncResult{Apply: func() { apply(items, err == nil) }})
	}()
	return true
}

// Hover asks language's server for documentation/type info at (line,
// character) in path, delivering the answer to apply on the UI goroutine
// via Post. Returns false if no request could be made at all (no server
// for that language).
//
// Same async shape as Definition/Completion — see Definition for why the
// request runs on its own goroutine.
func (m *Manager) Hover(path, language string, line, character int, apply func(text string, ok bool)) bool {
	m.mu.Lock()
	c := m.clients[language]
	m.mu.Unlock()
	if c == nil {
		return false
	}

	go func() {
		text, found, err := c.hover(path, line, character)
		if err != nil {
			debuglog.Warn("lsp: hover %s: %v", path, err)
			found = false
		}
		if m.Post == nil {
			return
		}
		m.Post(AsyncResult{Apply: func() { apply(text, found) }})
	}()
	return true
}

// SignatureHelp asks language's server what parameters the call enclosing
// (line, character) in path takes, delivering the answer to apply on the UI
// goroutine via Post. Returns false if no request could be made at all (no
// server for that language).
//
// Same async shape as Definition/Completion.
func (m *Manager) SignatureHelp(path, language string, line, character int, apply func(sh SignatureHelp, ok bool)) bool {
	m.mu.Lock()
	c := m.clients[language]
	m.mu.Unlock()
	if c == nil {
		return false
	}

	go func() {
		sh, found, err := c.signatureHelp(path, line, character)
		if err != nil {
			debuglog.Warn("lsp: signatureHelp %s: %v", path, err)
			found = false
		}
		if m.Post == nil {
			return
		}
		m.Post(AsyncResult{Apply: func() { apply(sh, found) }})
	}()
	return true
}

// Formatting asks language's server to reformat path's entire document,
// delivering the edits to apply on the UI goroutine via Post. Returns false
// if no request could be made at all (no server for that language).
//
// Same async shape as Definition/Completion.
func (m *Manager) Formatting(path, language string, tabWidth int, insertSpaces bool, apply func(edits []TextEdit, ok bool)) bool {
	m.mu.Lock()
	c := m.clients[language]
	m.mu.Unlock()
	if c == nil {
		return false
	}

	go func() {
		edits, err := c.formatting(path, tabWidth, insertSpaces)
		if err != nil {
			debuglog.Warn("lsp: formatting %s: %v", path, err)
			edits = nil
		}
		if m.Post == nil {
			return
		}
		m.Post(AsyncResult{Apply: func() { apply(edits, err == nil && len(edits) > 0) }})
	}()
	return true
}

// Shutdown stops every running server and forgets all state. Called from
// cmd/nib/main.go on exit, so quitting nib never leaves orphaned
// language server processes behind.
func (m *Manager) Shutdown() {
	m.mu.Lock()
	clients := m.clients
	m.clients = map[string]serverClient{}
	m.tried = map[string]bool{}
	m.open = map[string]*openFile{}
	m.mu.Unlock()

	for lang, c := range clients {
		if c == nil {
			continue
		}
		if err := c.Close(); err != nil {
			debuglog.Warn("lsp: shutting down %s server: %v", lang, err)
		}
	}
}
