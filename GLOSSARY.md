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

**Session**: the panes' layout and open tabs for a trusted project folder, saved in its `.nib` folder on exit and restored on the next launch.
