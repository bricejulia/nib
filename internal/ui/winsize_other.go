//go:build !(darwin || linux || freebsd || netbsd || openbsd)

package ui

import "go.rockorager.dev/vaxis"

// sizeWatcher is a no-op where SIGWINCH/TIOCGWINSZ don't exist; vaxis's own
// resize handling is used as-is.
type sizeWatcher struct{}

func newSizeWatcher() *sizeWatcher                { return nil }
func (w *sizeWatcher) size() (vaxis.Resize, bool) { return vaxis.Resize{}, false }
func (w *sizeWatcher) watch(fn func())            {}
func (w *sizeWatcher) stop()                      {}
