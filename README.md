# kiwi

A terminal code editor in Go. Modal editing in the vim tradition, real
syntax awareness from tree-sitter, and real semantic features from language
servers — in a codebase small enough to read in an afternoon.

```
┌─ Files ──────────────────┐┌─ Editor ─────────────────────────────────────┐
│   ▶ .git                 ││ main.go * |[helper.go]                       │
│ M main.go                ││   1 package main                             │
│   helper.go              ││   2                                          │
│   go.mod                 ││   3 func Helper() int {                      │
│                          ││E  4     return "42"                          │
│                          ││     E cannot use "42" (untyped string        │
│                          ││       constant) as int value [compiler]      │
│                          ││   6 }                                        │
└──────────────────────────┘└──────────────────────────────────────────────┘
 Tab Switch pane · Ctrl+P Finder · ? Help    Ln 4, Col 5   go ●   main   kiwi
```

## What it does

- **Modal editing** — Normal/Insert/Command modes, `hjkl` navigation, `i`/`a`/`o`,
  `x`/`X`, `u` + `Ctrl+r` undo/redo with vim's own granularity (one Insert
  session = one undo step).
- **Syntax highlighting** via tree-sitter, across ~186 languages.
- **Syntax error detection** for every trusted grammar, with no language
  server needed — tree-sitter is error-tolerant, so it points at where a
  parse actually went wrong.
- **Language server support** (LSP) — diagnostics, cross-file go-to-definition,
  and member completion (`obj.` → the real fields of `obj`'s type).
- **Split panes** that share buffers: the same file open twice is *one*
  document, so edits, dirty state, and undo history are shared.
- **Fuzzy file finder** and project-wide content search (`git grep`).
- **Git integration** — file status in the tree, per-line diff markers in the
  gutter, branch and dirty summary in the status bar.
- **In-file search** — `/` with `n`/`N`, all matches highlighted.
- **Everything rebindable** through a plain-text config file.

## Getting started

```sh
make build          # or: go build -o kiwi cmd/kiwi/main.go
./kiwi [directory]  # defaults to the current directory
```

Requires Go 1.23+. For LSP features, install the relevant language server —
`gopls` for Go, `intelephense` or `phpactor` for PHP, and any other via one
config line (see [Language servers](#language-servers)). kiwi finds it on
`PATH` and degrades gracefully to its tree-sitter features if it isn't there.

## Architecture

The dependency direction is the main thing to understand:

```
cmd/kiwi          wiring: builds the pane tree, owns the callbacks between panes
     │
     ├── internal/ui          the ONLY package that imports vaxis (the terminal)
     │        └── app.go      event loop, focus, overlays, key translation
     │
     ├── internal/ui/editor   the editor pane: buffers, modes, undo, completion
     ├── internal/ui/filetree, finder, help, debug, statusbar, gitstyle
     │
     ├── internal/layout      terminal-INDEPENDENT: View, Key, Window, splits
     ├── internal/textwidth   display-column math (tabs, wide runes, slicing)
     ├── internal/lsp         language server client (JSON-RPC over stdio)
     ├── internal/config      the user's keybinding file
     ├── internal/vcs         git status, per-line diffs, fsnotify watching
     └── internal/debuglog    in-process ring buffer, viewable with Ctrl+D
```

Three rules hold the whole thing together:

**1. Only `internal/ui` knows about the terminal.** Every pane is a
`layout.View` — `Render(Window)`, `HandleKey(Key)`, `Title()` — and talks in
kiwi's own `Key`/`Segment`/`Style` types. `internal/ui/app.go` is the single
seam that translates vaxis events in and vaxis draw calls out. Panes are
therefore testable against a fake `Window` with no terminal at all, which is
how essentially all of the UI is tested.

**2. Everything happens on one goroutine.** Background work — the filesystem
watcher, `git grep`, every LSP request — runs on its own goroutine but is
forbidden from touching editor state. Results are marshalled back onto the
event loop through `App.Post`, and only then applied. This is why locks are
confined to exactly two packages — `lsp.Manager` (server threads write to it)
and `debuglog` (anything may log from anywhere) — and no UI state needs any.

**3. Panes don't know about each other.** A pane exposes a callback field —
`OnOpen`, `OnFindReferences`, `OnAllTabsClosed` — and `cmd/kiwi/main.go` wires
them together. The editor doesn't import the finder to show search results; it
calls `OnFindReferences` and main.go decides that means "open the finder".

### Buffers vs. windows

Following vim rather than most editors: a **buffer** is the document, a
**tab/pane** is a view into it. `BufferStore` hands out one reference-counted
`*Buffer` per path, so opening `main.go` in two split panes gives both the same
buffer. Undo history lives on the buffer (undo in one pane reverts an edit made
in the other); cursor position and scroll live on the tab.

This mattered: before it existed, two panes on one file each had a private copy,
and saving from the stale one silently discarded the other's work.

## Design decisions

Each of these was a fork in the road, so the reasoning is worth recording.

**Tree-sitter *and* LSP, not either/or.** They answer different questions.
Tree-sitter is instant, needs no subprocess, and covers ~186 languages — good
for highlighting, syntax errors, and structural navigation. An LSP knows
*types*, which is the only way to do correct go-to-definition across files or
completion after a `.`. So features prefer LSP when a server is running and
fall back to tree-sitter otherwise; the same keys work either way, they just
get smarter. Notably, "go to parent" (walk up the syntax tree) isn't an LSP
concept at all, so it stays tree-sitter-only.

**A hand-written LSP client.** `go.lsp.dev/protocol` would have supplied the
protocol types, but it pulls in a logging framework and a fast-JSON library, and
forces the module's Go version up several releases. The wire format is
`Content-Length` framing over JSON-RPC and the spec is clear, so `internal/lsp`
uses `sourcegraph/jsonrpc2` (zero transitive dependencies) for transport and
hand-writes the ~10 message types actually used.

**Syntax diagnostics are allowlisted per language.** Diagnostics need a higher
confidence bar than highlighting: a wrong grammar guess makes highlighting
bland, but makes diagnostics *invent errors*. `.txt` resolves to the `vimdoc`
grammar, which rejects ordinary prose as one giant parse error. Two heuristics
were measured and rejected — a genuinely broken PHP file was 95% error-covered
while a genuinely broken Go file was 30%, so "mostly errors ⇒ wrong grammar"
would have been actively wrong. So there's a curated list of grammars whose
extensions are unambiguous. Omitting a language fails safe.

**Syntax errors refresh between edits, not during them.** Half-typed code is
almost always unparseable, so refreshing per keystroke would flag an error under
your cursor constantly. Markers update on open, on Normal-mode edits, and when
an Insert session ends.

**Full re-parse on every edit.** Highlighting re-parses the whole buffer per
keystroke rather than using tree-sitter's incremental API. Simple and correct;
the incremental path is the natural optimisation once something needs to hold a
persistent tree anyway.

**No rope.** `Buffer` is a `[]string` of lines. Swapping in a rope later
replaces `Buffer` and nothing else — the cursor, scroll, and tab-width logic
don't depend on the representation.

**`Dirty` is derived, never stored.** It compares the buffer against the last
saved content. An earlier version stored a flag inside undo snapshots, which
went stale the moment a save happened mid-history: edit → save → undo → save →
redo left a file reading as "saved" while differing from disk.

**Display columns, not bytes, everywhere.** `internal/textwidth` owns tab
expansion and rune-width-aware slicing so a double-width CJK glyph is never cut
in half. Anywhere byte arithmetic sneaks in, a multi-byte glyph eventually
renders as `�`.

## Keybindings

Press `?` in kiwi for this list at runtime. Every binding is rebindable
(see [Configuration](#configuration)).

### Global

| Key | Action |
| --- | --- |
| `Ctrl+C` | Quit |
| `Tab` / `Shift+Tab` | Focus next / previous pane |
| `Ctrl+P` | File finder (also: double-tap `Shift`) |
| `Ctrl+D` | Debug log |
| `?` | Help |
| `Ctrl+O` | Open the config file in `$EDITOR` |
| `Ctrl+W` / `Ctrl+E` | Split the focused editor pane right / down |
| `Ctrl+X` | Close the focused editor pane |

### Editor — moving

| Key | Action |
| --- | --- |
| `h` `j` `k` `l`, arrows | Move cursor (arrows also work in Insert mode) |
| `PageUp` / `PageDown` | Move by a page |
| `Home` / `End`, `0` / `$` | Start / end of line |
| `g` / `G` | First / last line |
| `:<N>` | Go to line N |
| `]` / `[` | Next / previous tab |

### Editor — editing

| Key | Action |
| --- | --- |
| `i` | Insert mode |
| `a` | Insert after the cursor |
| `o` | Open a line below and insert |
| `Esc` | Back to Normal mode |
| `x` / `X` | Delete the character under / before the cursor |
| `dd` / `yy` | Delete (cut) / yank (copy) this line |
| `p` | Put (paste) the yanked line after this one |
| `Enter`, `Backspace`, `Tab` | Newline, delete back, insert a tab (Insert mode) |
| `u` / `Ctrl+R` | Undo / redo |
| `Ctrl+S` | Save |

### Editor — code intelligence

| Key | Action |
| --- | --- |
| `Ctrl+]` | Go to definition (LSP when available, else same-file) |
| `Ctrl+B` | Jump back |
| `Ctrl+G` | Go to parent node in the syntax tree |
| `Ctrl+F` | Find references (opens the finder) |
| `Ctrl+Space` | Autocomplete (LSP members, else buffer words) |
| `K` | Show error/warning details for this line |

### Editor — git

| Key | Action |
| --- | --- |
| `B` | Blame: who last changed this line, and why |
| `H` | Show the diff hunk this line belongs to |
| `D` | Show this file's full diff against `HEAD` (scrollable; `Esc` closes) |

### Editor — search and ex-commands

| Key | Action |
| --- | --- |
| `/` | Search in this file; `Enter` jumps, `Esc` cancels |
| `n` / `N` | Next / previous match |
| `:w` | Save |
| `:q` / `:q!` | Close tab (refuses if unsaved; `!` discards) |
| `:qa` / `:qa!` | Close all tabs |
| `:wq` | Save then close |

### File tree

| Key | Action |
| --- | --- |
| `j` `k`, arrows | Move cursor |
| `Enter`, `l`, `→` | Open file / expand directory |
| `h`, `←` | Collapse directory |
| `Shift+←` / `Shift+→` | Peek at a truncated name |

### Finder

`Tab` switches between filename and content search · `Enter` opens ·
`↑`/`↓` selects · `←`/`→` peeks at a long line · `Esc` closes

Both modes show each file's git status marker in the leftmost column, colored
the same way the file tree colors it.

### Diff

`j`/`k` and arrows scroll · `PageUp`/`PageDown` by a page · `Home`/`End` jump
to the ends · `←`/`→` peeks at a long line · `Esc` closes

## Configuration

`Ctrl+O` opens (and creates, on first use) a flat config file — in the spirit
of Ghostty's, not TOML or YAML. Every default is listed there, commented out:

```
keybind = <scope>:<trigger> = <action>
```

There are two directives, `keybind` and `lsp` (below), sharing that
three-field shape.

Scopes are `global` (the default, so the prefix is optional), `editor`,
`filetree`, `finder`, `debug`, and `help`. Triggers are spellings like
`ctrl+p`, `shift+left`, `ctrl+space`, or a bare `x`; capitalisation of
modifiers and named keys doesn't matter. Actions are the names in each
package's `DefaultKeybinds`.

```
# Reachable on an AZERTY keyboard, where Ctrl+] needs AltGr:
keybind = editor:ctrl+t = go_to_definition
keybind = editor:ctrl+y = jump_back
```

A malformed line is skipped rather than fatal, so a typo can't stop kiwi from
starting. Changes take effect on the next launch.

### Language servers

Register a server for any language with an `lsp` line — no rebuild needed:

```
lsp = <language> = <command args...>
```

The language name is the one kiwi's grammar detection reports, which is shown
in the status bar. These merge over the built-in defaults (`go` → `gopls`,
`php` → `intelephense --stdio`), so a config line wins:

```
lsp = php    = phpactor language-server     # prefer the open-source PHP server
lsp = python = pyright-langserver --stdio
lsp = rust   = rust-analyzer
```

The command must be on your `PATH` and speak LSP over stdin/stdout. A language
with no entry, or whose binary is missing, simply falls back to kiwi's
tree-sitter features — the reason is logged (`Ctrl+D`), and the status bar tells
you which case you're in:

| Status bar | Meaning |
| --- | --- |
| `go ●` | Server running |
| `go ○` | Server configured, but not running — check `Ctrl+D` |
| `go` | No server configured for this language |

Installing the PHP servers: `npm i -g intelephense`, or see
[Phpactor's install docs](https://phpactor.readthedocs.io/en/master/usage/standalone.html).

## Development

```sh
make build     # compile
make test      # go test ./...
make vet       # go vet ./...
make lint      # go vet + golangci-lint
```

Tests avoid mocking the world: panes render into a fake `Window` and assert on
the segments produced, and the `internal/lsp` and `internal/vcs` tests spawn
*real* `gopls` and `git` (skipping themselves when those aren't installed),
because the wire format and CLI output are exactly what a mock would get wrong.

## Status

kiwi is a work in progress and a learning project. Known gaps, deliberately:
no rope (large-file editing will suffer), one language server configured out of
the box, full re-parse per keystroke, no auto-restart of a crashed server, and
`Ctrl+X` closes a pane without checking for unsaved changes (unlike `:q`).
