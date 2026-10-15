package opener

import (
	"errors"
	"slices"
	"testing"
)

// withStubs points the package's indirections at fakes for the duration of
// a test, recording what start would have launched.
func withStubs(t *testing.T, osName string, env map[string]string, present ...string) *[]string {
	t.Helper()
	origLook, origGOOS, origEnv, origStart := lookPath, goos, getenv, start
	t.Cleanup(func() { lookPath, goos, getenv, start = origLook, origGOOS, origEnv, origStart })

	goos = osName
	getenv = func(k string) string { return env[k] }
	lookPath = func(name string) (string, error) {
		if slices.Contains(present, name) {
			return "/usr/bin/" + name, nil
		}
		return "", errors.New("not found")
	}
	var launched []string
	start = func(argv []string) error {
		launched = argv
		return nil
	}
	return &launched
}

func TestOpenPerPlatform(t *testing.T) {
	cases := []struct {
		goos, helper string
		want         []string
	}{
		{"darwin", "open", []string{"/usr/bin/open", "/p/index.html"}},
		{"linux", "xdg-open", []string{"/usr/bin/xdg-open", "/p/index.html"}},
		{"freebsd", "xdg-open", []string{"/usr/bin/xdg-open", "/p/index.html"}},
		{"windows", "rundll32", []string{"/usr/bin/rundll32", "url.dll,FileProtocolHandler", "/p/index.html"}},
	}
	for _, c := range cases {
		launched := withStubs(t, c.goos, nil, c.helper)
		if err := Open("/p/index.html"); err != nil {
			t.Fatalf("%s: unexpected error: %v", c.goos, err)
		}
		if !slices.Equal(*launched, c.want) {
			t.Errorf("%s: launched %q, want %q", c.goos, *launched, c.want)
		}
	}
}

func TestOpenRefusesOverSSH(t *testing.T) {
	for _, key := range []string{"SSH_CONNECTION", "SSH_TTY"} {
		launched := withStubs(t, "darwin", map[string]string{key: "x"}, "open")
		if err := Open("/p/index.html"); !errors.Is(err, ErrRemote) {
			t.Errorf("%s set: got %v, want ErrRemote", key, err)
		}
		if *launched != nil {
			t.Errorf("%s set: launched %q, want nothing", key, *launched)
		}
	}
}

func TestOpenReportsMissingHelper(t *testing.T) {
	launched := withStubs(t, "linux", nil)
	if err := Open("/p/index.html"); err == nil {
		t.Error("expected an error with no xdg-open on PATH")
	}
	if *launched != nil {
		t.Errorf("launched %q, want nothing", *launched)
	}
}

func TestOpenReportsUnsupportedPlatform(t *testing.T) {
	withStubs(t, "plan9", nil, "open")
	if err := Open("/p/index.html"); err == nil {
		t.Error("expected an error on an unsupported platform")
	}
}
