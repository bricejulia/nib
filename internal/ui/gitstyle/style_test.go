package gitstyle

import (
	"testing"

	"github.com/bricejulia/kiwi/internal/layout"
	"github.com/bricejulia/kiwi/internal/vcs/gitstatus"
)

func TestMarkerCoversEveryStatus(t *testing.T) {
	cases := map[gitstatus.Status]string{
		gitstatus.Unmodified: " ",
		gitstatus.Modified:   "M",
		gitstatus.Added:      "A",
		gitstatus.Deleted:    "D",
		gitstatus.Renamed:    "R",
		gitstatus.Untracked:  "?",
		gitstatus.Conflicted: "!",
	}
	for status, want := range cases {
		if got := Marker(status); got != want {
			t.Errorf("Marker(%v) = %q, want %q", status, got, want)
		}
	}
}

func TestStyleIsDistinctPerNotableStatus(t *testing.T) {
	statuses := []gitstatus.Status{
		gitstatus.Modified, gitstatus.Added, gitstatus.Deleted,
		gitstatus.Renamed, gitstatus.Untracked, gitstatus.Conflicted,
	}
	seen := map[layout.Style]gitstatus.Status{}
	for _, s := range statuses {
		style := Style(s)
		if style == (layout.Style{}) {
			t.Errorf("Style(%v) should not be the zero-value default style", s)
		}
		if prev, ok := seen[style]; ok {
			t.Errorf("Style(%v) collides with Style(%v): both are %+v", s, prev, style)
		}
		seen[style] = s
	}
}

func TestStyleUnmodifiedIsDefault(t *testing.T) {
	if got := Style(gitstatus.Unmodified); got != (layout.Style{}) {
		t.Errorf("Style(Unmodified) = %+v, want the zero-value default style", got)
	}
}
