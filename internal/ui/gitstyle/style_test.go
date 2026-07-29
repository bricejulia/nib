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

func TestLineMarkerCoversEveryStatus(t *testing.T) {
	cases := map[gitstatus.LineStatus]string{
		gitstatus.LineUnchanged:     " ",
		gitstatus.LineAdded:         "+",
		gitstatus.LineModified:      "~",
		gitstatus.LineDeletedBefore: "_",
	}
	for status, want := range cases {
		if got := LineMarker(status); got != want {
			t.Errorf("LineMarker(%v) = %q, want %q", status, got, want)
		}
	}
}

func TestLineStyleIsDistinctPerNotableStatus(t *testing.T) {
	statuses := []gitstatus.LineStatus{
		gitstatus.LineAdded, gitstatus.LineModified, gitstatus.LineDeletedBefore,
	}
	seen := map[layout.Style]gitstatus.LineStatus{}
	for _, s := range statuses {
		style := LineStyle(s)
		if style == (layout.Style{}) {
			t.Errorf("LineStyle(%v) should not be the zero-value default style", s)
		}
		if prev, ok := seen[style]; ok {
			t.Errorf("LineStyle(%v) collides with LineStyle(%v): both are %+v", s, prev, style)
		}
		seen[style] = s
	}
}

func TestLineStyleUnchangedIsDefault(t *testing.T) {
	if got := LineStyle(gitstatus.LineUnchanged); got != (layout.Style{}) {
		t.Errorf("LineStyle(LineUnchanged) = %+v, want the zero-value default style", got)
	}
}
