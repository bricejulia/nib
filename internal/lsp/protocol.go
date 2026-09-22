// Package lsp is a minimal Language Server Protocol client: it spawns
// language server subprocesses and speaks LSP's JSON-RPC dialect to them,
// so the editor can offer real semantic features (diagnostics, go to
// definition) instead of only the syntax-level approximations tree-sitter
// can provide.
//
// The client itself is entirely language-agnostic — the only per-language
// knowledge anywhere is the server registry in manager.go, which is plain
// data (language name -> command to run). Adding a language means adding a
// map entry, never new code.
//
// Only the small slice of the protocol nib actually uses is implemented
// here (see protocol.go's types), hand-written rather than pulled from a
// full protocol library: the alternative (go.lsp.dev/protocol) drags in a
// logging framework and a fast-JSON library, and would force this module's
// Go version up several releases, all to save a few hundred lines of very
// stable struct definitions.
package lsp

import (
	"encoding/json"
	"net/url"
	"path/filepath"
	"strings"
)

// Protocol method names used by this client.
const (
	methodInitialize         = "initialize"
	methodInitialized        = "initialized"
	methodShutdown           = "shutdown"
	methodExit               = "exit"
	methodDidOpen            = "textDocument/didOpen"
	methodDidChange          = "textDocument/didChange"
	methodDidClose           = "textDocument/didClose"
	methodDefinition         = "textDocument/definition"
	methodCompletion         = "textDocument/completion"
	methodHover              = "textDocument/hover"
	methodSignatureHelp      = "textDocument/signatureHelp"
	methodFormatting         = "textDocument/formatting"
	methodPublishDiagnostics = "textDocument/publishDiagnostics"
	methodReferences         = "textDocument/references"
	methodRename             = "textDocument/rename"
	methodCodeAction         = "textDocument/codeAction"
)

// Position is a zero-based line/character pair. Note that LSP's
// "character" is an offset in UTF-16 code units by default, not runes or
// bytes; nib sends and receives rune offsets, which agree with UTF-16 for
// everything in the Basic Multilingual Plane (i.e. everything except
// emoji and a few rare scripts). That's an accepted simplification: a
// cursor sitting after an astral-plane character on the same line could
// address a slightly wrong column.
type Position struct {
	Line      int `json:"line"`
	Character int `json:"character"`
}

// Range is a half-open span between two Positions.
type Range struct {
	Start Position `json:"start"`
	End   Position `json:"end"`
}

// Location is a Range within a specific document.
type Location struct {
	URI   string `json:"uri"`
	Range Range  `json:"range"`
}

// Path is Location's URI as a filesystem path, or "" if it isn't a usable
// file:// URI — so callers can open the file without knowing about URIs.
func (l Location) Path() string { return uriToPath(l.URI) }

// TextDocumentIdentifier names a document by URI.
type TextDocumentIdentifier struct {
	URI string `json:"uri"`
}

// VersionedTextDocumentIdentifier names a document plus the revision the
// change applies to; servers use the version to order edits.
type VersionedTextDocumentIdentifier struct {
	URI     string `json:"uri"`
	Version int    `json:"version"`
}

// TextDocumentItem is a document's full contents, sent on didOpen.
type TextDocumentItem struct {
	URI        string `json:"uri"`
	LanguageID string `json:"languageId"`
	Version    int    `json:"version"`
	Text       string `json:"text"`
}

// DidOpenTextDocumentParams tells the server a document is now open in the
// editor, and that the editor's copy (not the file on disk) is
// authoritative from here on.
type DidOpenTextDocumentParams struct {
	TextDocument TextDocumentItem `json:"textDocument"`
}

// TextDocumentContentChangeEvent describes one edit. With no Range set it
// means "the whole document is now Text", which is the only form this
// client sends (full-document sync — see Client's capabilities).
type TextDocumentContentChangeEvent struct {
	Text string `json:"text"`
}

// DidChangeTextDocumentParams delivers edits for an open document.
type DidChangeTextDocumentParams struct {
	TextDocument   VersionedTextDocumentIdentifier  `json:"textDocument"`
	ContentChanges []TextDocumentContentChangeEvent `json:"contentChanges"`
}

// DidCloseTextDocumentParams tells the server the editor no longer has the
// document open, so the on-disk contents are authoritative again.
type DidCloseTextDocumentParams struct {
	TextDocument TextDocumentIdentifier `json:"textDocument"`
}

// TextDocumentPositionParams is the request shape shared by every
// "what's at this cursor position?" request — definition, hover,
// completion, references.
type TextDocumentPositionParams struct {
	TextDocument TextDocumentIdentifier `json:"textDocument"`
	Position     Position               `json:"position"`
}

// ReferenceContext controls whether the reference set includes the
// symbol's own declaration. nib always sends false: "find references"
// means "find usages", not "find every mention including the
// declaration itself".
type ReferenceContext struct {
	IncludeDeclaration bool `json:"includeDeclaration"`
}

// ReferenceParams is textDocument/references' request shape:
// TextDocumentPositionParams plus the declaration-inclusion flag.
type ReferenceParams struct {
	TextDocument TextDocumentIdentifier `json:"textDocument"`
	Position     Position               `json:"position"`
	Context      ReferenceContext       `json:"context"`
}

// CompletionItem is one suggestion from the server. Label is what to show;
// InsertText, when set, is what to actually insert (they differ for things
// like a function whose label is "Foo(bar int)" but which inserts "Foo").
// SortText, when set, is what the server wants results ordered by — it
// encodes the server's own relevance ranking, which is far better than
// alphabetical (a type's own members should outrank random matches).
type CompletionItem struct {
	Label      string `json:"label"`
	InsertText string `json:"insertText,omitempty"`
	SortText   string `json:"sortText,omitempty"`
	Detail     string `json:"detail,omitempty"`
}

// Text is what should be inserted for this item — InsertText if the server
// supplied one, otherwise the label.
func (c CompletionItem) Text() string {
	if c.InsertText != "" {
		return c.InsertText
	}
	return c.Label
}

// Order is what this item should be sorted by: the server's own SortText
// when present, else the label.
func (c CompletionItem) Order() string {
	if c.SortText != "" {
		return c.SortText
	}
	return c.Label
}

// CompletionList is the object form of a completion response. Servers may
// also reply with a bare array of items or null, so responses are decoded
// leniently — see completionItems.
type CompletionList struct {
	IsIncomplete bool             `json:"isIncomplete"`
	Items        []CompletionItem `json:"items"`
}

// Hover is a server's answer to "what is this?" at a position — usually a
// type signature or doc comment. Contents is left as json.RawMessage
// because its wire shape varies by spec version: a bare string, a
// {kind,value} MarkupContent object, the older {language,value}
// MarkedString object, or an array of any of those — decodeDocumentation
// normalizes all of them to plain text. Range (the span hovered) is
// omitted: nib doesn't highlight it.
type Hover struct {
	Contents json.RawMessage `json:"contents"`
}

// SignatureHelp is a server's answer to "what parameters does this call
// take?" at a position.
type SignatureHelp struct {
	Signatures      []SignatureInformation `json:"signatures"`
	ActiveSignature int                    `json:"activeSignature"`
	ActiveParameter int                    `json:"activeParameter"`
}

// SignatureInformation describes one overload of the call.
type SignatureInformation struct {
	Label         string                 `json:"label"`
	Documentation json.RawMessage        `json:"documentation,omitempty"`
	Parameters    []ParameterInformation `json:"parameters,omitempty"`
}

// DocText normalizes Documentation the same way decodeDocumentation
// normalizes Hover.Contents — see there for the shapes handled.
func (s SignatureInformation) DocText() string { return decodeDocumentation(s.Documentation) }

// ParameterInformation is one parameter of a SignatureInformation. Only
// Label is kept: the alternate [start,end] offset-pair form some servers
// use to slice it out of the signature's own Label isn't handled, since
// every server nib targets (gopls, typescript-language-server,
// intelephense) sends a plain string label.
type ParameterInformation struct {
	Label string `json:"label"`
}

// decodeDocumentation normalizes the documentation/contents shapes LSP
// allows (MarkupContent's {kind,value}, the deprecated MarkedString's bare
// string or {language,value} object, or an array of either) into flat
// text. Null or empty input yields "".
func decodeDocumentation(raw json.RawMessage) string {
	if len(raw) == 0 || string(raw) == "null" {
		return ""
	}

	var s string
	if err := json.Unmarshal(raw, &s); err == nil {
		return s
	}

	var markup struct {
		Value string `json:"value"`
	}
	if err := json.Unmarshal(raw, &markup); err == nil && markup.Value != "" {
		return markup.Value
	}

	var list []json.RawMessage
	if err := json.Unmarshal(raw, &list); err == nil {
		var parts []string
		for _, item := range list {
			if text := decodeDocumentation(item); text != "" {
				parts = append(parts, text)
			}
		}
		return strings.Join(parts, "\n\n")
	}

	return ""
}

// FormattingOptions is the minimal per-request formatting preference LSP
// requires the client to send. InsertSpaces is always false: nib's own
// convention is real tab characters (see "insert_tab"'s handling in
// internal/ui/editor), so a server that reindents with spaces is told not
// to.
type FormattingOptions struct {
	TabSize      int  `json:"tabSize"`
	InsertSpaces bool `json:"insertSpaces"`
}

// DocumentFormattingParams requests a whole-document reformat.
type DocumentFormattingParams struct {
	TextDocument TextDocumentIdentifier `json:"textDocument"`
	Options      FormattingOptions      `json:"options"`
}

// TextEdit is one span-replacement a server wants applied to a document —
// formatting's response shape.
type TextEdit struct {
	Range   Range  `json:"range"`
	NewText string `json:"newText"`
}

// WorkspaceEdit is a set of per-file edits a server wants applied, keyed
// by file:// URI — the response shape for rename, and reused by
// CodeAction's Edit field. This is the type nib's own code (Manager,
// editor.ApplyWorkspaceEdit) works with; the wire decoding in
// workspaceEdit (client.go) normalizes the spec's alternate
// DocumentChanges shape into this same Changes map, so nothing
// downstream needs to know the response came in that form. gopls
// actually sends DocumentChanges for rename (verified against a real
// gopls, not assumed) rather than the plain Changes map, which is why
// both are handled despite this type only exposing one.
type WorkspaceEdit struct {
	Changes map[string][]TextEdit `json:"changes"`
}

// TextDocumentEdit is one file's edits within the spec's DocumentChanges
// WorkspaceEdit form: like Changes' map value, but naming its file via a
// VersionedTextDocumentIdentifier instead of a bare URI key, for servers
// that want optimistic-concurrency checks on the version. nib ignores
// the version (it never round-trips a WorkspaceEdit back to the server),
// so decoding just flattens this into the same Changes shape — see
// workspaceEdit in client.go. DocumentChanges can also contain resource
// operations (CreateFile/RenameFile/DeleteFile, distinguished on the
// wire by a "kind" field instead of "edits") which nib doesn't apply;
// an entry like that simply decodes with an empty Edits and contributes
// nothing, the same "not something nib can act on" treatment
// CodeAction's bare Command gets.
type TextDocumentEdit struct {
	TextDocument VersionedTextDocumentIdentifier `json:"textDocument"`
	Edits        []TextEdit                      `json:"edits"`
}

// UnmarshalJSON normalizes WorkspaceEdit's two wire forms — a plain
// URI -> TextEdit[] Changes map, or the DocumentChanges list of
// TextDocumentEdit — into Changes, the only shape nib's own code deals
// with. This has to live on WorkspaceEdit's own UnmarshalJSON, not a
// helper only the top-level rename response goes through, because a
// WorkspaceEdit also decodes nested inside CodeAction.Edit — and a
// server (gopls, for both) can send DocumentChanges there too.
func (w *WorkspaceEdit) UnmarshalJSON(data []byte) error {
	var wire struct {
		Changes         map[string][]TextEdit `json:"changes"`
		DocumentChanges []TextDocumentEdit    `json:"documentChanges"`
	}
	if err := json.Unmarshal(data, &wire); err != nil {
		return err
	}
	if len(wire.Changes) > 0 {
		w.Changes = wire.Changes
		return nil
	}
	if len(wire.DocumentChanges) == 0 {
		w.Changes = nil
		return nil
	}
	changes := make(map[string][]TextEdit, len(wire.DocumentChanges))
	for _, dc := range wire.DocumentChanges {
		if dc.TextDocument.URI == "" || len(dc.Edits) == 0 {
			continue // a resource operation (create/rename/delete), not an edit
		}
		changes[dc.TextDocument.URI] = append(changes[dc.TextDocument.URI], dc.Edits...)
	}
	w.Changes = changes
	return nil
}

// RenameParams is textDocument/rename's request shape.
type RenameParams struct {
	TextDocument TextDocumentIdentifier `json:"textDocument"`
	Position     Position               `json:"position"`
	NewName      string                 `json:"newName"`
}

// DiagnosticSeverity ranks a diagnostic; lower numbers are more severe,
// matching the wire protocol's own numbering.
type DiagnosticSeverity int

const (
	SeverityError       DiagnosticSeverity = 1
	SeverityWarning     DiagnosticSeverity = 2
	SeverityInformation DiagnosticSeverity = 3
	SeverityHint        DiagnosticSeverity = 4
)

// Diagnostic is one problem the server found in a document.
type Diagnostic struct {
	Range    Range              `json:"range"`
	Severity DiagnosticSeverity `json:"severity,omitempty"`
	Source   string             `json:"source,omitempty"`
	Message  string             `json:"message"`
}

// PublishDiagnosticsParams is the server-initiated notification carrying
// the complete current diagnostic set for one document — it always
// replaces any previous set for that URI rather than adding to it, so an
// empty Diagnostics slice means "this file is now clean".
type PublishDiagnosticsParams struct {
	URI         string       `json:"uri"`
	Diagnostics []Diagnostic `json:"diagnostics"`
}

// CodeActionContext tells the server what's wrong at the requested
// range, so it can offer targeted fixes rather than only generic
// refactors.
type CodeActionContext struct {
	Diagnostics []Diagnostic `json:"diagnostics"`
}

// CodeActionParams is textDocument/codeAction's request shape: a range
// (nib always sends a zero-width range at the cursor) plus context.
type CodeActionParams struct {
	TextDocument TextDocumentIdentifier `json:"textDocument"`
	Range        Range                  `json:"range"`
	Context      CodeActionContext      `json:"context"`
}

// CodeAction is one server-suggested fix or refactor. Command is left
// unhandled (the spec allows a response entry to be either a Command or
// a CodeAction; a bare Command decodes here with an empty Title and a
// nil Edit) — nib has no generic "execute an arbitrary server command"
// plumbing, only "apply this WorkspaceEdit", so a server that only
// offers command-based actions simply produces nothing nib can act on.
// See codeActions, which filters those out.
type CodeAction struct {
	Title string         `json:"title"`
	Kind  string         `json:"kind,omitempty"`
	Edit  *WorkspaceEdit `json:"edit,omitempty"`
}

// InitializeParams is the opening handshake. Capabilities is deliberately
// sparse: nib advertises only what it actually implements, and servers
// are required to degrade gracefully for everything omitted.
type InitializeParams struct {
	ProcessID    int                `json:"processId"`
	RootURI      string             `json:"rootUri"`
	Capabilities ClientCapabilities `json:"capabilities"`
}

// ClientCapabilities declares what this client supports.
type ClientCapabilities struct {
	TextDocument TextDocumentClientCapabilities `json:"textDocument"`
}

// TextDocumentClientCapabilities is the per-document-feature slice of
// ClientCapabilities.
type TextDocumentClientCapabilities struct {
	Synchronization    SynchronizationCapabilities    `json:"synchronization"`
	Definition         DefinitionCapabilities         `json:"definition"`
	PublishDiagnostics PublishDiagnosticsCapabilities `json:"publishDiagnostics"`
}

// SynchronizationCapabilities declares which document-sync notifications
// the client sends. WillSave/DidSave are omitted (false) — nib doesn't
// send them.
type SynchronizationCapabilities struct {
	DynamicRegistration bool `json:"dynamicRegistration"`
}

// DefinitionCapabilities declares go-to-definition support. LinkSupport
// is false, so servers reply with plain Locations rather than
// LocationLinks — one less response shape to handle.
type DefinitionCapabilities struct {
	LinkSupport bool `json:"linkSupport"`
}

// PublishDiagnosticsCapabilities declares how much diagnostic detail the
// client can render. Everything here is off: nib shows a gutter marker
// keyed on severity, with no related-information or tag handling.
type PublishDiagnosticsCapabilities struct {
	RelatedInformation bool `json:"relatedInformation"`
}

// InitializeResult is the server's handshake reply. Its capabilities are
// mostly ignored — nib asks for definitions and reads diagnostics
// regardless, and a server that doesn't support one simply answers empty.
type InitializeResult struct {
	Capabilities map[string]any `json:"capabilities"`
}

// pathToURI converts an absolute filesystem path to a file:// URI, the
// only way LSP identifies documents. Uses net/url so path segments needing
// escapes (spaces, "#", non-ASCII) are handled properly rather than by
// hand-rolled string surgery.
func pathToURI(path string) string {
	u := url.URL{Scheme: "file", Path: filepath.ToSlash(path)}
	return u.String()
}

// uriToPath is pathToURI's inverse, for turning a server's response
// locations back into paths nib can open. A URI that isn't a parseable
// file:// URI yields "" — callers treat that as "no usable location"
// rather than guessing.
func uriToPath(uri string) string {
	u, err := url.Parse(uri)
	if err != nil || u.Scheme != "file" {
		return ""
	}
	// url.Parse puts a Windows-style "/C:/x" in Path; on Unix this is a
	// no-op, and it keeps the function honest if nib is ever built for
	// Windows.
	p := u.Path
	if len(p) > 2 && p[0] == '/' && p[2] == ':' {
		p = p[1:]
	}
	return filepath.FromSlash(strings.TrimSuffix(p, "/"))
}
