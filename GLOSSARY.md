# Glossary

The domain language of nib. Use these words in code, comments, commits and reviews.

**Workspace**: every editor pane open in one nib session, plus the operations that span them: splitting, closing, moving tabs, opening files, and applying renames, deletes, diagnostics, git gutters and on-disk changes to every pane showing a file. Lives in `internal/workspace`. _Avoid_: pane manager, editor registry.

**Pane** (editor pane): one `editor.View` in the layout, with its own tab bar. A Workspace has at least one. The file tree and status bar are leaves of the layout, not panes.

**Active pane**: the pane that last genuinely had focus. Opening a file from the file tree or finder lands here, even while focus is on the tree.

**Target pane**: the pane a focus-driven action acts on: the focused pane, or the active pane when focus is elsewhere (e.g. the file tree).

**Tab**: one open file inside a pane, with its own cursor, scroll and selection. The same file can have a tab in several panes.

**Buffer**: a file's in-memory content, edit state and undo history. Shared by every tab on that file, across panes (see `editor.BufferStore`), so edits in one pane show in the other.

**Detached tab**: a tab whose file was deleted on disk while it had unsaved changes. It stays open; `:w` recreates the file.

**Save conflict**: a save that found its file changed on disk since nib last synced with it. Resolved by the user: keep mine, or reload from disk.

**Single-file mode**: nib launched on one or more files (`nib a.json b.csv`) instead of a folder. No file tree, no Session, no trust prompt, no repo-wide git status; only the files' own folders are watched, and closing the last tab quits. The files' first folder is still the root for LSP and git.

**Session**: the panes' layout and open tabs for a trusted project folder, saved in its `.nib` folder on exit and restored on the next launch.

**Action**: a named command a key can be bound to, such as `save` or `move_down`. Declared once, with its default keys and a description, in its scope (see `config.Action`). Help, the action popup and the generated config file are all built from these declarations. _Avoid_: command (except for `:` commands), binding (that's the key-to-action pair).

**Scope**: one keymap and the actions it can bind, such as `global`, `editor` or `filetree`. A config line names it: `keybind = editor:x = delete_char_forward`. The full list is `scopes` in `cmd/nib/main.go`.
