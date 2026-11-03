//go:build darwin || linux || freebsd || netbsd || openbsd

package ui

import (
	"os"
	"os/signal"
	"syscall"

	"go.rockorager.dev/vaxis"
	"golang.org/x/sys/unix"
)

// sizeWatcher re-reads the tty's size via ioctl. It backs up vaxis's own
// resize detection: when the terminal supports in-band resize reports (DEC
// mode 2048), vaxis stops listening for SIGWINCH entirely and trusts the
// terminal to send a report on every size change. Some terminals don't —
// notably after macOS wakes from sleep and re-lays out external displays —
// leaving the app drawn at the stale size until the user resizes by hand.
type sizeWatcher struct {
	tty  *os.File
	sigs chan os.Signal
}

func newSizeWatcher() *sizeWatcher {
	tty, err := os.Open("/dev/tty")
	if err != nil {
		return nil
	}
	return &sizeWatcher{tty: tty, sigs: make(chan os.Signal, 1)}
}

// size returns the tty's current dimensions.
func (w *sizeWatcher) size() (vaxis.Resize, bool) {
	if w == nil {
		return vaxis.Resize{}, false
	}
	ws, err := unix.IoctlGetWinsize(int(w.tty.Fd()), unix.TIOCGWINSZ)
	if err != nil || ws.Col == 0 || ws.Row == 0 {
		return vaxis.Resize{}, false
	}
	return vaxis.Resize{
		Cols:   int(ws.Col),
		Rows:   int(ws.Row),
		XPixel: int(ws.Xpixel),
		YPixel: int(ws.Ypixel),
	}, true
}

// watch calls fn on every SIGWINCH until stop is called.
func (w *sizeWatcher) watch(fn func()) {
	if w == nil {
		return
	}
	signal.Notify(w.sigs, syscall.SIGWINCH)
	go func() {
		for range w.sigs {
			fn()
		}
	}()
}

func (w *sizeWatcher) stop() {
	if w == nil {
		return
	}
	signal.Stop(w.sigs)
	close(w.sigs)
	_ = w.tty.Close()
}
