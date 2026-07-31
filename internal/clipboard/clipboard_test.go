package clipboard

import (
	"errors"
	"os"
	"path/filepath"
	"runtime"
	"strings"
	"testing"
)

// withStubs points the package's three indirections at fakes for the
// duration of a test, so mechanism resolution can be asserted for platforms
// and environments other than the one running the tests.
func withStubs(t *testing.T, osName string, env map[string]string, present ...string) {
	t.Helper()
	origLook, origGOOS, origEnv := lookPath, goos, getenv
	t.Cleanup(func() { lookPath, goos, getenv = origLook, origGOOS, origEnv })

	goos = osName
	getenv = func(k string) string { return env[k] }
	lookPath = func(name string) (string, error) {
		for _, p := range present {
			if p == name {
				return "/usr/bin/" + name, nil
			}
		}
		return "", errors.New("not found")
	}
}

func TestMechanismOnMacOSPrefersPbcopy(t *testing.T) {
	withStubs(t, "darwin", nil, "pbcopy")
	if got := New(nil).Mechanism(); got != "pbcopy" {
		t.Errorf("got %q, want pbcopy", got)
	}
}

func TestMechanismFallsBackToOSC52WhenNoHelperExists(t *testing.T) {
	withStubs(t, "darwin", nil)
	if got := New(nil).Mechanism(); got != OSC52 {
		t.Errorf("got %q, want %q", got, OSC52)
	}
}

func TestMechanismOverSSHIsAlwaysOSC52(t *testing.T) {
	// The clipboard worth writing is the one at the far end of the
	// connection, and only OSC 52 reaches it — a local pbcopy would
	// silently set the wrong machine's clipboard.
	for _, key := range []string{"SSH_CONNECTION", "SSH_TTY"} {
		t.Run(key, func(t *testing.T) {
			withStubs(t, "darwin", map[string]string{key: "something"}, "pbcopy")
			if got := New(nil).Mechanism(); got != OSC52 {
				t.Errorf("got %q, want %q even though pbcopy is present", got, OSC52)
			}
		})
	}
}

func TestMechanismOnWaylandPrefersWlCopyOverXclip(t *testing.T) {
	// xclip may be installed and would talk to an XWayland clipboard the
	// compositor's native clients never see.
	withStubs(t, "linux", map[string]string{"WAYLAND_DISPLAY": "wayland-0"}, "wl-copy", "xclip")
	if got := New(nil).Mechanism(); got != "wl-copy" {
		t.Errorf("got %q, want wl-copy", got)
	}
}

func TestMechanismOnX11PrefersXclipThenXsel(t *testing.T) {
	withStubs(t, "linux", nil, "xclip", "xsel")
	if got := New(nil).Mechanism(); got != "xclip" {
		t.Errorf("got %q, want xclip", got)
	}
	withStubs(t, "linux", nil, "xsel")
	if got := New(nil).Mechanism(); got != "xsel" {
		t.Errorf("got %q, want xsel", got)
	}
}

func TestMechanismOnLinuxFindsWlCopyWithoutWaylandDisplay(t *testing.T) {
	withStubs(t, "linux", nil, "wl-copy")
	if got := New(nil).Mechanism(); got != "wl-copy" {
		t.Errorf("got %q, want wl-copy", got)
	}
}

func TestMechanismOnWindowsUsesClip(t *testing.T) {
	withStubs(t, "windows", nil, "clip")
	if got := New(nil).Mechanism(); got != "clip" {
		t.Errorf("got %q, want clip", got)
	}
}

func TestXclipGetsTheClipboardSelectionArgument(t *testing.T) {
	// Without it xclip writes the PRIMARY selection, which Cmd/Ctrl+V does
	// not read — the copy would appear to do nothing.
	withStubs(t, "linux", nil, "xclip")
	w := New(nil)
	if len(w.native) != 3 || w.native[1] != "-selection" || w.native[2] != "clipboard" {
		t.Errorf("native = %q, want xclip -selection clipboard", w.native)
	}
}

func TestCopyEmptyStringIsANoOp(t *testing.T) {
	// Clearing the clipboard because a click selected nothing would be
	// actively hostile.
	withStubs(t, "darwin", nil)
	called := false
	w := New(func(string) { called = true })
	if err := w.Copy(""); err != nil {
		t.Errorf("unexpected error: %v", err)
	}
	if called {
		t.Error("an empty copy should not reach the terminal")
	}
}

func TestCopyUsesTheOSC52FuncWhenThereIsNoHelper(t *testing.T) {
	withStubs(t, "darwin", nil)
	var got string
	w := New(func(s string) { got = s })
	if err := w.Copy("hello"); err != nil {
		t.Fatalf("unexpected error: %v", err)
	}
	if got != "hello" {
		t.Errorf("osc52 got %q, want %q", got, "hello")
	}
}

func TestCopyWithNoMechanismAtAllReportsAnError(t *testing.T) {
	// Better a logged warning than a silent no-op — silence is what made the
	// original bug take three rounds to find.
	withStubs(t, "darwin", nil)
	if err := New(nil).Copy("hello"); err == nil {
		t.Error("expected an error when neither a helper nor OSC 52 is available")
	}
}

// TestCopyPipesTextToTheHelpersStdin runs a real subprocess — a tiny shell
// script standing in for pbcopy that writes whatever it is given to a file —
// so the stdin plumbing is verified end to end rather than by inspecting
// fields. This is the property that matters: text must never be passed as an
// argument, where shell-special characters and length limits would break it.
func TestCopyPipesTextToTheHelpersStdin(t *testing.T) {
	if runtime.GOOS == "windows" {
		t.Skip("needs a POSIX shell")
	}
	dir := t.TempDir()
	out := filepath.Join(dir, "captured")
	script := filepath.Join(dir, "fakecopy")
	if err := os.WriteFile(script, []byte("#!/bin/sh\ncat > \""+out+"\"\n"), 0o755); err != nil {
		t.Fatal(err)
	}

	w := &Writer{native: []string{script}}

	cases := map[string]string{
		"plain":          "hello world",
		"shell specials": `$(rm -rf /) && echo "pwned" | tee 'x' > $HOME; ${VAR}`,
		"newlines":       "one\ntwo\nthree",
		"wide runes":     "世界 — émoji 🥝",
		"long":           strings.Repeat("abcdefghij", 5000),
	}
	for name, text := range cases {
		t.Run(name, func(t *testing.T) {
			if err := w.Copy(text); err != nil {
				t.Fatalf("Copy: %v", err)
			}
			got, err := os.ReadFile(out)
			if err != nil {
				t.Fatal(err)
			}
			if string(got) != text {
				t.Errorf("helper received %q, want %q", truncate(string(got)), truncate(text))
			}
		})
	}
}

func TestCopyReportsAFailingHelper(t *testing.T) {
	if runtime.GOOS == "windows" {
		t.Skip("needs a POSIX shell")
	}
	dir := t.TempDir()
	script := filepath.Join(dir, "failcopy")
	if err := os.WriteFile(script, []byte("#!/bin/sh\necho boom >&2\nexit 3\n"), 0o755); err != nil {
		t.Fatal(err)
	}

	w := &Writer{native: []string{script}}
	err := w.Copy("hello")
	if err == nil {
		t.Fatal("expected an error from a failing helper")
	}
	// The helper's own stderr is included: "exit status 3" alone would tell a
	// user nothing about what went wrong.
	if !strings.Contains(err.Error(), "boom") {
		t.Errorf("error %q should include the helper's stderr", err)
	}
}

func TestMechanismReportsTheBareCommandNameNotItsPath(t *testing.T) {
	// It's a label for a human reading the debug pane.
	w := &Writer{native: []string{filepath.Join("/usr", "local", "bin", "pbcopy")}}
	if got := w.Mechanism(); got != "pbcopy" {
		t.Errorf("got %q, want pbcopy", got)
	}
}

func truncate(s string) string {
	if len(s) <= 60 {
		return s
	}
	return s[:60] + "..."
}
