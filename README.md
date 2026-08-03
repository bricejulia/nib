# nib

[![CI](https://github.com/bricejulia/nib/actions/workflows/ci.yml/badge.svg)](https://github.com/bricejulia/nib/actions/workflows/ci.yml)
[![License: MIT](https://img.shields.io/badge/License-MIT-yellow.svg)](https://opensource.org/licenses/MIT)

![nib banner](https://github.com/bricejulia/nib/blob/main/assets/nib.png?raw=true)

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
 Tab Switch pane · Ctrl+P Finder · ? Help    Ln 4, Col 5   go ●   main   nib
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
- **Find & Replace in Path** — literal, project-wide search with a
  per-occurrence checklist; replace one match or every checked one, in open
  buffers or on disk.
- **Git integration** — file status in the tree, per-line diff markers in the
  gutter, branch and dirty summary in the status bar.
- **In-file search** — `/` with `n`/`N`, all matches highlighted.
- **Mouse text selection** — click, drag, double-click a word, triple-click a
  line; finishing a selection copies it to the system clipboard automatically.
- **Everything rebindable** through a plain-text config file.

## Install

```bash
curl -fsSL https://raw.githubusercontent.com/bricejulia/nib/main/install.sh | sh
```

Or with Homebrew:

```bash
brew install bricejulia/tap/nib
```

> **macOS:** if Gatekeeper blocks the binary on first run, remove the quarantine attribute:
> `xattr -d com.apple.quarantine $(which nib)`, or allow it manually in
> System Settings → Privacy & Security.

Or with Go:

```bash
go install github.com/bricejulia/nib/cmd/nib@latest
```

You can also download the latest binaries from the [release page](https://github.com/bricejulia/nib/releases). If you use this method, don't forget to check for updates regularly!

## Getting started

```sh
make build          # or: go build -o nib cmd/nib/main.go
./nib [directory]  # defaults to the current directory
```

Requires Go 1.24+. For LSP features, install the relevant language server —
`gopls` for Go, `intelephense` or `phpactor` for PHP,
`typescript-language-server` for TypeScript/JavaScript/JSX/TSX, and any other
via one config line (see [Language servers](#language-servers)). nib finds
it on `PATH` and degrades gracefully to its tree-sitter features if it isn't
there.

## Architecture

The dependency direction is the main thing to understand:

```
cmd/nib          wiring: builds the pane tree, owns the callbacks between panes
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
nib's own `Key`/`Segment`/`Style` types. `internal/ui/app.go` is the single
seam that translates vaxis events in and vaxis draw calls out. Panes are
therefore testable against a fake `Window` with no terminal at all, which is
how essentially all of the UI is tested.

**2. State only changes on one goroutine.** Background work — the filesystem
watcher, `git grep`, syntax highlighting, every LSP request — each runs on its
own goroutine but is forbidden from touching editor state directly. Results
are marshalled back onto the event loop through `App.Post`, and only applied
there. This is why locks are confined to exactly three packages — `lsp.Manager`
(server threads write to it), the editor's `Highlighter` (its worker and the UI
goroutine share a pending-jobs queue), and `debuglog` (anything may log from
anywhere) — and no other UI state needs any.

**3. Panes don't know about each other.** A pane exposes a callback field —
`OnOpen`, `OnFindReferences`, `OnAllTabsClosed` — and `cmd/nib/main.go` wires
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

Press `?` in nib for this list at runtime. Every binding is rebindable
(see [Configuration](#configuration)).

### Global

| Key | Action |
| --- | --- |
| `Ctrl+C` | Quit (asks first if there are unsaved changes) |
| `Tab` / `Shift+Tab` | Focus next / previous pane |
| `Ctrl+P` | File finder (also: double-tap `Shift`) |
| `Ctrl+F` | Find references: search file contents, pre-filled with the word under the cursor in the focused (or last-focused) editor pane |
| `Ctrl+Shift+R` | Find & replace in path |
| `Ctrl+D` | Debug log |
| `?` | Help |
| `Ctrl+O` | Open the config file in `$EDITOR` |
| `Ctrl+W` / `Ctrl+E` | Split the focused (or last-focused) editor pane right / down |
| `Ctrl+X` | Close the focused (or last-focused) editor pane |

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
| `p` | Put (paste) after this one — a line, or a fragment if the last copy was a selection |
| `Enter`, `Backspace`, `Tab` | Newline, delete back, insert a tab (Insert mode) |
| `u` / `Ctrl+R` | Undo / redo |
| `Ctrl+S` | Save |

### Editor — mouse

| Gesture | Action |
| --- | --- |
| Click | Place the cursor |
| Drag | Select text and copy it — drag past the pane's edge to scroll |
| Double-click | Select the word and copy it |
| Triple-click | Select the line (including its break) and copy it |
| `Shift`+click | Extend the selection — copy it with `y` |
| `y` | Copy the selection to the clipboard **and** the yank register |
| `Esc` | Dismiss the selection, as does any cursor movement |
| Wheel | Scroll whichever pane the pointer is over, focused or not |

**Finishing a selection copies it**, the way selecting in a terminal does — no
keypress needed. That auto-copy writes the system clipboard only, deliberately
leaving the yank register alone, so a stray drag can't destroy a line you just
took with `yy` and were about to `p`. Pressing `y` is how you ask for both.
(`Shift`+click is excluded so that building a selection up in steps doesn't
rewrite the clipboard at each one.)

Copying prefers a native helper — `pbcopy`, `wl-copy`, `xclip`, `xsel`, or
`clip` — and falls back to the OSC 52 escape sequence when nib is running over
ssh or no helper is installed. Two mechanisms because neither works everywhere:
OSC 52 is the only one that can reach the clipboard of the machine you're
actually sitting at, but it is widely blocked (tmux ignores it from
applications unless `set -g set-clipboard on`, and some terminals disable it
outright since it lets a remote program write your clipboard), while a helper
is reliable but sets the clipboard of whatever machine nib runs on. `Ctrl+D`
shows which one was chosen, and warns if a copy failed.

Note that nib asks the terminal for mouse reporting, which means the
terminal's *own* click-drag selection doesn't apply inside nib. Most
terminals bypass this while a modifier is held (`Option` on macOS).

### Editor — code intelligence

| Key | Action |
| --- | --- |
| `Ctrl+]` | Go to definition (LSP when available, else same-file) |
| `Ctrl+B` | Jump back |
| `Ctrl+G` | Go to parent node in the syntax tree |
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
| `a` | New file or directory |
| `r` | Rename / move |
| `d` | Delete |

`a` and `r` open a prompt on the pane's bottom row. What you type is a path
relative to the project root — `a` prefills the selected folder, `r` prefills
the selected entry — so editing the last segment renames and editing an
earlier one moves. End a name with `/` to create a directory; missing parent
directories are created for you. Nothing is ever overwritten: a name that
already exists is refused, with the reason shown on the prompt row.

`d` asks `(y/N)` for a file or an empty directory. A directory that still has
entries in it reports how many and requires typing `yes` — the removal is
recursive and permanent. Deleting a file that's open in an editor pane closes
its tab, unless it has unsaved changes, in which case the tab stays open
marked `-- DELETED --` so `:w` can write the file back.

### Finder

`Tab` switches between filename and content search · `Enter` opens ·
`↑`/`↓` selects · `←`/`→` peeks at a long line · `Esc` closes

Both modes show each file's git status marker in the leftmost column, colored
the same way the file tree colors it.

### Diff

`j`/`k` and arrows scroll · `PageUp`/`PageDown` by a page · `Home`/`End` jump
to the ends · `←`/`→` peeks at a long line · `Esc` closes

### Find & Replace

| Key | Action |
| --- | --- |
| `Tab` | Switch between Find, Replace, and the results list |
| `Up` / `Down` | Move selection (results list) |
| `Space` | Toggle an occurrence, or a whole file's occurrences |
| `Enter` | Replace just the occurrence under the cursor |
| `a` | Replace every checked occurrence |
| `Esc` | Close |

A literal (non-regex), case-insensitive search across the project — the same
`git grep` finder's own content search uses — grouped by file, with one row
per occurrence rather than per line: a line matching twice gets two
independently-checkable rows. Everything found starts checked; toggling a
file's row toggles every occurrence under it, and vice versa. Replacing a
file that's open in an editor pane goes through its buffer (one undo entry
for the whole file, gutter and language-server diagnostics update the same
as any other edit), left unsaved like any other edit; a file with no open
pane is rewritten on disk directly, preserving its permissions.

`Ctrl+Shift+R` needs a terminal that reports the Shift modifier on Ctrl
combos (the kitty keyboard protocol) to be distinguished from plain `Ctrl+R`
(redo, while an editor pane is focused) — remap it via `Ctrl+O` to any free
`Ctrl+`letter combo if it doesn't fire on your terminal.

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

A malformed line is skipped rather than fatal, so a typo can't stop nib from
starting. Changes take effect on the next launch.

### Language servers

Register a server for any language with an `lsp` line — no rebuild needed:

```
lsp = <language> = <command args...>
```

The language name is the one nib's grammar detection reports, which is shown
in the status bar. These merge over the built-in defaults (`go` → `gopls`,
`php` → `intelephense --stdio`, `javascript`/`typescript`/`tsx` →
`typescript-language-server --stdio`), so a config line wins:

```
lsp = php    = phpactor language-server     # prefer the open-source PHP server
lsp = python = pyright-langserver --stdio
lsp = rust   = rust-analyzer
```

`.jsx` files are detected as the `javascript` language (there's no separate
`jsx` grammar), so `lsp = javascript = ...` covers them too.

The command must be on your `PATH` and speak LSP over stdin/stdout. A language
with no entry, or whose binary is missing, simply falls back to nib's
tree-sitter features — the reason is logged (`Ctrl+D`), and the status bar tells
you which case you're in:

| Status bar | Meaning |
| --- | --- |
| `go ●` | Server running |
| `go ○` | Server configured, but not running — check `Ctrl+D` |
| `go` | No server configured for this language |

Installing the PHP servers: `npm i -g intelephense`, or see
[Phpactor's install docs](https://phpactor.readthedocs.io/en/master/usage/standalone.html).
Installing the TypeScript/JavaScript server:
`npm i -g typescript-language-server typescript`.

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

nib is a work in progress and a learning project. Known gaps, deliberately:
no rope (large-file editing will suffer), one language server configured out of
the box, full re-parse per keystroke, and no auto-restart of a crashed server.