package lsp

import (
	"context"
	"encoding/json"
	"fmt"
	"io"
	"os"
	"os/exec"
	"time"

	"github.com/sourcegraph/jsonrpc2"
)

// Timeouts for the two request/response calls this client makes. Both are
// bounded so a wedged or pathologically slow server can never hang the
// caller (and, for definition, the goroutine the UI is waiting on)
// indefinitely. initializeTimeout is the more generous of the two: a cold
// server may need to index a large project before replying.
const (
	initializeTimeout = 30 * time.Second
	requestTimeout    = 5 * time.Second
	shutdownTimeout   = 2 * time.Second
)

// stdio adapts a subprocess's separate stdin and stdout pipes into the
// single io.ReadWriteCloser jsonrpc2 expects. Closing it closes both
// halves; the subprocess itself is reaped separately (see Client.Close).
type stdio struct {
	in  io.WriteCloser
	out io.ReadCloser
}

func (s stdio) Read(p []byte) (int, error)  { return s.out.Read(p) }
func (s stdio) Write(p []byte) (int, error) { return s.in.Write(p) }
func (s stdio) Close() error {
	err := s.in.Close()
	if outErr := s.out.Close(); err == nil {
		err = outErr
	}
	return err
}

// Client is one running language server: the subprocess plus the JSON-RPC
// connection to it. A Client is created already-initialized (see
// newClient) — if the handshake fails, no Client exists at all, so any
// Client a caller holds is usable.
//
// Safe for concurrent use: jsonrpc2.Conn is, and the only other mutable
// state (onDiagnostics) is set once before any request is issued.
type Client struct {
	cmd  *exec.Cmd
	conn *jsonrpc2.Conn

	// onDiagnostics is called from the JSON-RPC read goroutine whenever
	// the server publishes diagnostics. Set once by newClient's caller
	// before any document is opened; must be safe to call from a
	// non-UI goroutine (the Manager's implementation marshals onto the UI
	// goroutine via Post).
	onDiagnostics func(PublishDiagnosticsParams)
}

// newClient spawns command in rootDir, performs the LSP initialize
// handshake, and returns a ready Client. On any failure the subprocess is
// cleaned up and an error is returned — there is no such thing as a
// half-initialized Client.
func newClient(rootDir string, command []string, onDiagnostics func(PublishDiagnosticsParams)) (*Client, error) {
	if len(command) == 0 {
		return nil, fmt.Errorf("empty server command")
	}

	cmd := exec.Command(command[0], command[1:]...)
	cmd.Dir = rootDir
	// Server stderr is discarded for now: it's useful for diagnosing a
	// misbehaving server, but plumbing it into debuglog is deferred (see
	// the plan's deferred list) and leaving it unset would inherit kiwi's
	// own stderr and scribble over the rendered UI.
	cmd.Stderr = nil

	stdin, err := cmd.StdinPipe()
	if err != nil {
		return nil, err
	}
	stdout, err := cmd.StdoutPipe()
	if err != nil {
		_ = stdin.Close()
		return nil, err
	}
	if err := cmd.Start(); err != nil {
		_ = stdin.Close()
		_ = stdout.Close()
		return nil, err
	}

	c := &Client{cmd: cmd, onDiagnostics: onDiagnostics}
	stream := jsonrpc2.NewBufferedStream(stdio{in: stdin, out: stdout}, jsonrpc2.VSCodeObjectCodec{})
	// AsyncHandler so a slow diagnostic callback can't stall the read
	// loop and thereby block responses to in-flight requests.
	c.conn = jsonrpc2.NewConn(context.Background(), stream, jsonrpc2.AsyncHandler(c))

	if err := c.initialize(rootDir); err != nil {
		_ = c.Close()
		return nil, err
	}
	return c, nil
}

// Handle implements jsonrpc2.Handler, receiving server-initiated requests
// and notifications. Only publishDiagnostics is acted on; everything else
// is ignored, which is protocol-legal for notifications and for the
// server-to-client requests kiwi never opts into.
func (c *Client) Handle(_ context.Context, _ *jsonrpc2.Conn, req *jsonrpc2.Request) {
	if req.Method != methodPublishDiagnostics || req.Params == nil || c.onDiagnostics == nil {
		return
	}
	var params PublishDiagnosticsParams
	if err := json.Unmarshal(*req.Params, &params); err != nil {
		return
	}
	c.onDiagnostics(params)
}

// initialize performs the opening handshake: the initialize request,
// then the initialized notification that tells the server the client is
// ready to receive messages.
func (c *Client) initialize(rootDir string) error {
	ctx, cancel := context.WithTimeout(context.Background(), initializeTimeout)
	defer cancel()

	params := InitializeParams{
		ProcessID: os.Getpid(),
		RootURI:   pathToURI(rootDir),
		Capabilities: ClientCapabilities{
			TextDocument: TextDocumentClientCapabilities{
				Definition: DefinitionCapabilities{LinkSupport: false},
			},
		},
	}
	var result InitializeResult
	if err := c.conn.Call(ctx, methodInitialize, params, &result); err != nil {
		return fmt.Errorf("initialize: %w", err)
	}
	return c.conn.Notify(ctx, methodInitialized, struct{}{})
}

// didOpen tells the server the editor now owns path's contents.
func (c *Client) didOpen(path, languageID, text string, version int) error {
	return c.conn.Notify(context.Background(), methodDidOpen, DidOpenTextDocumentParams{
		TextDocument: TextDocumentItem{
			URI:        pathToURI(path),
			LanguageID: languageID,
			Version:    version,
			Text:       text,
		},
	})
}

// didChange sends path's complete new contents (full-document sync — see
// TextDocumentContentChangeEvent).
func (c *Client) didChange(path, text string, version int) error {
	return c.conn.Notify(context.Background(), methodDidChange, DidChangeTextDocumentParams{
		TextDocument: VersionedTextDocumentIdentifier{URI: pathToURI(path), Version: version},
		ContentChanges: []TextDocumentContentChangeEvent{
			{Text: text},
		},
	})
}

// didClose tells the server the on-disk contents are authoritative again.
func (c *Client) didClose(path string) error {
	return c.conn.Notify(context.Background(), methodDidClose, DidCloseTextDocumentParams{
		TextDocument: TextDocumentIdentifier{URI: pathToURI(path)},
	})
}

// definition asks where the symbol at (line, character) is defined.
// Blocks until the server answers or requestTimeout elapses, so callers
// must run it off the UI goroutine.
//
// The reply shape is famously inconsistent across servers — the spec
// permits a single Location, an array of them, or null — so this decodes
// into json.RawMessage first and tries both shapes. ok=false means "no
// definition found", which is a normal answer, not an error.
func (c *Client) definition(path string, line, character int) (Location, bool, error) {
	ctx, cancel := context.WithTimeout(context.Background(), requestTimeout)
	defer cancel()

	params := TextDocumentPositionParams{
		TextDocument: TextDocumentIdentifier{URI: pathToURI(path)},
		Position:     Position{Line: line, Character: character},
	}
	var raw json.RawMessage
	if err := c.conn.Call(ctx, methodDefinition, params, &raw); err != nil {
		return Location{}, false, err
	}
	return firstLocation(raw)
}

// completion asks what could be inserted at (line, character) — the
// request that makes member completion ("myObject." + Ctrl+Space) possible,
// since only the server knows what type myObject is and what it has.
// Blocks until the server answers or requestTimeout elapses, so callers
// must run it off the UI goroutine.
func (c *Client) completion(path string, line, character int) ([]CompletionItem, error) {
	ctx, cancel := context.WithTimeout(context.Background(), requestTimeout)
	defer cancel()

	params := TextDocumentPositionParams{
		TextDocument: TextDocumentIdentifier{URI: pathToURI(path)},
		Position:     Position{Line: line, Character: character},
	}
	var raw json.RawMessage
	if err := c.conn.Call(ctx, methodCompletion, params, &raw); err != nil {
		return nil, err
	}
	return completionItems(raw)
}

// completionItems decodes a completion response, which — like definition —
// has more than one spec-legal shape: a CompletionList object, a bare array
// of items, or null.
func completionItems(raw json.RawMessage) ([]CompletionItem, error) {
	if len(raw) == 0 || string(raw) == "null" {
		return nil, nil
	}

	var list CompletionList
	if err := json.Unmarshal(raw, &list); err == nil && list.Items != nil {
		return list.Items, nil
	}

	var items []CompletionItem
	if err := json.Unmarshal(raw, &items); err != nil {
		return nil, fmt.Errorf("completion: unrecognized response shape: %w", err)
	}
	return items, nil
}

// firstLocation extracts the first usable Location from a definition
// response, accepting either a bare Location object or an array of them
// (both spec-legal, and different servers pick differently).
func firstLocation(raw json.RawMessage) (Location, bool, error) {
	if len(raw) == 0 || string(raw) == "null" {
		return Location{}, false, nil
	}

	var list []Location
	if err := json.Unmarshal(raw, &list); err == nil {
		if len(list) == 0 {
			return Location{}, false, nil
		}
		return list[0], list[0].URI != "", nil
	}

	var single Location
	if err := json.Unmarshal(raw, &single); err != nil {
		return Location{}, false, fmt.Errorf("definition: unrecognized response shape: %w", err)
	}
	return single, single.URI != "", nil
}

// Close shuts the server down as politely as time allows: the spec's
// shutdown request then exit notification, falling back to killing the
// process if it doesn't oblige promptly. Always reaps the subprocess, so
// quitting kiwi never leaves an orphaned language server behind.
func (c *Client) Close() error {
	if c.conn != nil {
		ctx, cancel := context.WithTimeout(context.Background(), shutdownTimeout)
		if err := c.conn.Call(ctx, methodShutdown, nil, nil); err == nil {
			_ = c.conn.Notify(ctx, methodExit, nil)
		}
		cancel()
		_ = c.conn.Close()
	}
	if c.cmd == nil || c.cmd.Process == nil {
		return nil
	}

	// Give the server a moment to exit on its own after the exit
	// notification before killing it.
	done := make(chan error, 1)
	go func() { done <- c.cmd.Wait() }()
	select {
	case <-done:
		return nil
	case <-time.After(shutdownTimeout):
		_ = c.cmd.Process.Kill()
		<-done // reap it, so no zombie is left
		return nil
	}
}
