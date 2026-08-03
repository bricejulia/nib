// Package clipboard writes text to the operating system clipboard, choosing
// between a native helper command (pbcopy and friends) and the OSC 52
// terminal escape sequence.
//
// Neither mechanism alone is sufficient, which is the whole reason this
// package exists rather than a one-line call somewhere:
//
// OSC 52 is the only thing that works over ssh — the terminal at the user's
// keyboard owns the clipboard, not the host nib runs on — but it is
// routinely blocked. tmux ignores it from applications unless its
// set-clipboard option is "on" (the default is "external", which does not),
// and some terminals disable it deliberately, since it lets a remote program
// write the local clipboard. It also has no reply, so a program can never
// tell whether it worked.
//
// A native helper is unconditionally reliable and reports errors, but sets
// the clipboard of the machine nib is running on — the wrong machine
// whenever that isn't the one at the keyboard.
//
// So: a native helper when nib is running locally, OSC 52 when it isn't or
// when no helper is installed. This mirrors how the rest of nib reaches
// outside itself (see internal/vcs/gitblame, internal/ui/finder's grep):
// a small package that shells out, with the terminal-specific half injected
// by internal/ui, the only package allowed to touch vaxis.
package clipboard

import (
	"fmt"
	"os"
	"os/exec"
	"runtime"
	"strings"
)

// OSC52 is the Mechanism name for the escape-sequence path.
const OSC52 = "osc52"

// Writer copies text to the system clipboard by whichever mechanism was
// resolved at construction. Safe to reuse for the lifetime of the process.
type Writer struct {
	// native is the helper command and its arguments, or nil when OSC 52 is
	// to be used instead.
	native []string
	// osc52 writes a string to the terminal as an OSC 52 sequence. Injected
	// rather than implemented here so this package doesn't import vaxis.
	// nil is tolerated: it just means the OSC 52 path is unavailable, which
	// is what a caller with no terminal (a test) gets.
	osc52 func(string)
}

// lookPath is exec.LookPath, indirected so tests can describe a PATH
// without depending on what happens to be installed on the machine running
// them.
var lookPath = exec.LookPath

// goos is runtime.GOOS, indirected for the same reason: the mechanism choice
// is per-platform logic worth testing on whatever platform CI runs.
var goos = runtime.GOOS

// getenv is os.Getenv, indirected so the ssh-detection rule is testable
// without mutating the real environment.
var getenv = os.Getenv

// New resolves the copy mechanism once and returns a Writer using it. osc52
// writes an OSC 52 sequence to the terminal (App.CopyToClipboard passes
// vaxis's ClipboardPush); it may be nil, in which case only a native helper
// can work.
//
// Resolution happens here, not per copy, so that copying is a single exec
// and never re-scans PATH.
func New(osc52 func(string)) *Writer {
	return &Writer{native: nativeCommand(), osc52: osc52}
}

// nativeCommand returns the helper command to pipe text into, or nil when
// OSC 52 should be used instead.
func nativeCommand() []string {
	// Over ssh, the clipboard worth writing is the one attached to the
	// terminal at the far end, and only OSC 52 can reach it. A local helper
	// would silently succeed while setting the wrong machine's clipboard,
	// which is worse than not trying.
	if getenv("SSH_CONNECTION") != "" || getenv("SSH_TTY") != "" {
		return nil
	}

	switch goos {
	case "darwin":
		if path, err := lookPath("pbcopy"); err == nil {
			return []string{path}
		}
	case "linux", "freebsd", "openbsd", "netbsd":
		// Wayland first when the session is Wayland: xclip/xsel may still be
		// installed and would talk to an XWayland clipboard that the
		// compositor's native clients never see.
		if getenv("WAYLAND_DISPLAY") != "" {
			if path, err := lookPath("wl-copy"); err == nil {
				return []string{path}
			}
		}
		if path, err := lookPath("xclip"); err == nil {
			return []string{path, "-selection", "clipboard"}
		}
		if path, err := lookPath("xsel"); err == nil {
			return []string{path, "--clipboard", "--input"}
		}
		// A Wayland session whose only helper is wl-copy but which didn't
		// set WAYLAND_DISPLAY still deserves a try before giving up.
		if path, err := lookPath("wl-copy"); err == nil {
			return []string{path}
		}
	case "windows":
		if path, err := lookPath("clip"); err == nil {
			return []string{path}
		}
	}
	return nil
}

// Mechanism names what Copy will use — a helper's command name, or OSC52.
// Reported to the debug log at startup so "the copy silently did nothing"
// is diagnosable from inside nib rather than by reading its source.
func (w *Writer) Mechanism() string {
	if len(w.native) == 0 {
		return OSC52
	}
	// The bare command name, not the resolved absolute path: this is a label
	// for a human reading the debug pane.
	parts := strings.Split(w.native[0], string(os.PathSeparator))
	return parts[len(parts)-1]
}

// Copy places text on the system clipboard.
//
// Copying the empty string is a no-op rather than an error: it would only
// ever come from an empty selection, and clearing the user's clipboard
// because they clicked without dragging would be actively hostile.
//
// A returned error means the native helper failed and the clipboard was NOT
// set. The OSC 52 path can only ever return nil — the sequence has no reply,
// so a terminal (or tmux) that discards it is indistinguishable from one
// that honoured it. That asymmetry is exactly why a helper is preferred
// where one exists.
func (w *Writer) Copy(text string) error {
	if text == "" {
		return nil
	}
	if len(w.native) == 0 {
		if w.osc52 == nil {
			return fmt.Errorf("no clipboard mechanism available")
		}
		w.osc52(text)
		return nil
	}

	cmd := exec.Command(w.native[0], w.native[1:]...)
	// Via stdin, never as an argument: an argument would be mangled by any
	// shell-special character in the selected text, break on embedded
	// newlines, and hit the platform's argv length limit on a large
	// selection.
	cmd.Stdin = strings.NewReader(text)
	if out, err := cmd.CombinedOutput(); err != nil {
		return fmt.Errorf("%s: %w: %s", w.Mechanism(), err, strings.TrimSpace(string(out)))
	}
	return nil
}
