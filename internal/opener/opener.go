// Package opener hands a file to the operating system's default handler
// for it — an .html file to the browser, an image to the image viewer, a
// directory to the file manager — the way double-clicking it would.
//
// Like internal/clipboard, it's a small package that shells out (open,
// xdg-open, rundll32), kept separate so the per-platform choice is
// testable on whatever platform CI runs.
package opener

import (
	"errors"
	"fmt"
	"os"
	"os/exec"
	"runtime"
)

// ErrRemote is returned when nib is running over ssh: the default app
// would launch on the remote host, not on the machine at the keyboard.
var ErrRemote = errors.New("can't open files over ssh: the default app would run on the remote host")

// lookPath, goos and getenv are exec.LookPath, runtime.GOOS and os.Getenv,
// indirected so tests can describe a platform and environment without
// depending on the machine running them — same seams as internal/clipboard.
var (
	lookPath = exec.LookPath
	goos     = runtime.GOOS
	getenv   = os.Getenv
)

// start launches argv without waiting for it, indirected so tests can
// check the resolved command without spawning anything.
var start = func(argv []string) error {
	cmd := exec.Command(argv[0], argv[1:]...)
	if err := cmd.Start(); err != nil {
		return err
	}
	// Reaped in the background: xdg-open in particular can stay alive as
	// long as the app it launched, and the UI goroutine must never wait on
	// that.
	go func() { _ = cmd.Wait() }()
	return nil
}

// Open asks the OS to open path with its default application. It returns
// once the helper has been started, not when the app has opened — the
// helpers report nothing useful after that point anyway.
func Open(path string) error {
	argv, err := command(path)
	if err != nil {
		return err
	}
	if err := start(argv); err != nil {
		return fmt.Errorf("%s: %w", argv[0], err)
	}
	return nil
}

// command resolves the helper invocation for path on this platform. path
// is always its own argv element, never interpolated into a shell string,
// so no character in a filename can be misread.
func command(path string) ([]string, error) {
	// Same rule as clipboard.nativeCommand: launching a browser on the
	// remote host would succeed silently and show nothing to the user.
	if getenv("SSH_CONNECTION") != "" || getenv("SSH_TTY") != "" {
		return nil, ErrRemote
	}

	var helper string
	var args []string
	switch goos {
	case "darwin":
		helper = "open"
	case "linux", "freebsd", "openbsd", "netbsd":
		helper = "xdg-open"
	case "windows":
		// Not "cmd /c start": start treats a quoted first argument as the
		// window title and cmd re-parses metacharacters like & in the path.
		helper, args = "rundll32", []string{"url.dll,FileProtocolHandler"}
	default:
		return nil, fmt.Errorf("opening files isn't supported on %s", goos)
	}
	resolved, err := lookPath(helper)
	if err != nil {
		return nil, fmt.Errorf("%s not found: %w", helper, err)
	}
	return append(append([]string{resolved}, args...), path), nil
}
