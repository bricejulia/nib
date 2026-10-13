package workspace

import (
	"os"

	"github.com/bricejulia/nib/internal/debuglog"
	"github.com/bricejulia/nib/internal/layout"
	"github.com/bricejulia/nib/internal/session"
	"github.com/bricejulia/nib/internal/ui/editor"
)

// Restore replaces the panes with sess's saved layout, reopening each
// pane's tabs at their saved cursor and scroll positions. Files that no
// longer exist are skipped, and a pane left with none is dropped. The
// first surviving pane reuses the initial, still-empty one rather than
// orphaning it, so Restore is meant for startup. Returns the leaf to focus
// — the saved focused pane if it survived — or ok=false, with nothing
// changed, if no pane did. Panes past the first MaxPanes are dropped, tabs
// and all, so a session saved before the cap (or by a runaway split) comes
// back usable.
func (w *Workspace) Restore(sess *session.Session) (layout.LeafID, bool) {
	built, dropped := 0, 0
	root, focused := session.Build(sess, func(sp session.Pane) *layout.LeafNode {
		states, active := existingTabs(w.opts.Root, sp)
		if len(states) == 0 {
			return nil
		}
		if built >= MaxPanes {
			dropped++
			return nil
		}
		p := w.active
		if built > 0 {
			p = w.newPane()
			w.add(p)
		}
		built++
		p.view.RestoreTabs(states, active)
		for _, st := range states {
			w.refreshLineStatus(st.Path)
		}
		return p.leaf
	})
	if dropped > 0 {
		debuglog.Warn("restore session: dropped %d pane(s) beyond the max of %d", dropped, MaxPanes)
	}
	if root == nil {
		return 0, false
	}
	w.root.Children[0].Node = root
	w.active = w.panes[focused.ID]
	return focused.ID, true
}

// Capture snapshots the panes' layout and open tabs, with paths relative
// to Root. Nil if no pane has anything open.
func (w *Workspace) Capture() *session.Session {
	return session.Capture(w.root, func(l *layout.LeafNode) (*session.Pane, bool) {
		p, ok := w.panes[l.ID]
		if !ok {
			return nil, false
		}
		return capturePane(w.opts.Root, p.view)
	}, w.active.leaf.ID)
}

// existingTabs converts a saved pane's tabs into editor.TabStates with
// absolute paths, skipping files that no longer exist (deleted or renamed
// since the session was saved) and remapping the active index onto what's
// left — falling back to the first tab if the active one was skipped.
func existingTabs(root string, sp session.Pane) ([]editor.TabState, int) {
	var states []editor.TabState
	active := 0
	for i, t := range sp.Tabs {
		path := session.AbsPath(root, t.Path)
		if info, err := os.Stat(path); err != nil || info.IsDir() {
			continue
		}
		if i == sp.Active {
			active = len(states)
		}
		states = append(states, editor.TabState{Path: path, Line: t.Line, Col: t.Col, TopLine: t.Top})
	}
	return states, active
}

// capturePane is existingTabs' inverse, for saving: ok is false for a pane
// with nothing open, which session.Capture then leaves out.
func capturePane(root string, v *editor.View) (*session.Pane, bool) {
	states, active := v.TabStates()
	if len(states) == 0 {
		return nil, false
	}
	sp := &session.Pane{Active: active, Tabs: make([]session.Tab, len(states))}
	for i, st := range states {
		sp.Tabs[i] = session.Tab{Path: session.RelPath(root, st.Path), Line: st.Line, Col: st.Col, Top: st.TopLine}
	}
	return sp, true
}
