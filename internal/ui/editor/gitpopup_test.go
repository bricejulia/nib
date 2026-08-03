package editor

import (
	"errors"
	"strings"
	"testing"
	"time"

	"github.com/bricejulia/nib/internal/layout"
	"github.com/bricejulia/nib/internal/vcs/gitblame"
	"github.com/bricejulia/nib/internal/vcs/gitstatus"
)

func popupText(lines []popupLine) string {
	texts := make([]string, len(lines))
	for i, l := range lines {
		texts[i] = l.Text
	}
	return strings.Join(texts, "\n")
}

func TestBlamePopupLinesShowsCommitAuthorAndSummary(t *testing.T) {
	info := gitblame.Info{
		Commit:  "7d639fd",
		Author:  "Ada Lovelace",
		Time:    time.Now().Add(-3 * 24 * time.Hour),
		Summary: "feat: add lsp server support",
	}

	got := popupText(blamePopupLines(info, false, 80))
	for _, want := range []string{"7d639fd", "Ada Lovelace", "3 days ago", "feat: add lsp server support"} {
		if !strings.Contains(got, want) {
			t.Errorf("expected the popup to mention %q, got:\n%s", want, got)
		}
	}
}

func TestBlamePopupLinesUncommittedSaysSoAndHidesGitsPlaceholder(t *testing.T) {
	// git reports a working-tree-only line with this placeholder author,
	// which is git's plumbing showing through and shouldn't reach the user.
	info := gitblame.Info{Author: "Not Committed Yet", Uncommitted: true}

	got := popupText(blamePopupLines(info, true, 80))
	if !strings.Contains(strings.ToLower(got), "not committed yet") {
		t.Errorf("expected the popup to say the line isn't committed, got %q", got)
	}
	// git's placeholder author must not be echoed verbatim — the popup says
	// it in nib's own words instead (note the lowercase "committed").
	if strings.Contains(got, info.Author) {
		t.Errorf("git's placeholder author %q leaked into the popup: %q", info.Author, got)
	}
	// Nor is the dirty-buffer caveat useful here: "uncommitted" already
	// covers it, so it must not be piled on top.
	if strings.Contains(got, "unsaved") {
		t.Errorf("expected no redundant unsaved-changes warning, got %q", got)
	}
}

func TestBlamePopupLinesWarnsWhenTheBufferIsDirty(t *testing.T) {
	info := gitblame.Info{Commit: "abc1234", Author: "A", Summary: "s", Time: time.Now()}

	clean := popupText(blamePopupLines(info, false, 80))
	if strings.Contains(clean, "unsaved") {
		t.Errorf("clean buffer should carry no warning, got:\n%s", clean)
	}
	dirty := popupText(blamePopupLines(info, true, 80))
	if !strings.Contains(dirty, "unsaved") {
		t.Errorf("dirty buffer should warn about the offset, got:\n%s", dirty)
	}
}

func TestBlamePopupLinesNoRoomRendersNothing(t *testing.T) {
	info := gitblame.Info{Commit: "abc1234", Summary: "s"}
	if got := blamePopupLines(info, false, 0); got != nil {
		t.Errorf("expected no rows for zero width, got %+v", got)
	}
}

func TestRelativeTime(t *testing.T) {
	now := time.Date(2026, 7, 30, 12, 0, 0, 0, time.UTC)
	cases := []struct {
		ago  time.Duration
		want string
	}{
		{ago: 10 * time.Second, want: "just now"},
		{ago: time.Minute, want: "1 minute ago"},
		{ago: 5 * time.Minute, want: "5 minutes ago"},
		{ago: time.Hour, want: "1 hour ago"},
		{ago: 3 * time.Hour, want: "3 hours ago"},
		{ago: 24 * time.Hour, want: "1 day ago"},
		{ago: 3 * 24 * time.Hour, want: "3 days ago"},
		{ago: 40 * 24 * time.Hour, want: "1 month ago"},
		{ago: 200 * 24 * time.Hour, want: "6 months ago"},
		{ago: 400 * 24 * time.Hour, want: "1 year ago"},
		{ago: 3 * 365 * 24 * time.Hour, want: "3 years ago"},
		// Clock skew or a rewritten date: never render a negative age.
		{ago: -time.Hour, want: "just now"},
	}
	for _, c := range cases {
		if got := relativeTime(now.Add(-c.ago), now); got != c.want {
			t.Errorf("relativeTime(-%v) = %q, want %q", c.ago, got, c.want)
		}
	}
}

func TestHunkPopupLinesColorsBothSides(t *testing.T) {
	h := gitstatus.Hunk{
		NewStart: 4,
		NewLines: 1,
		OldLines: 1,
		Removed:  []string{"old line"},
		Added:    []string{"new line"},
	}

	lines := hunkPopupLines(h, 4, 80)
	if len(lines) != 3 {
		t.Fatalf("expected a header plus one line each side, got %+v", lines)
	}
	if lines[0].Style.Foreground != layout.ColorCyan {
		t.Errorf("header should be cyan, got %+v", lines[0].Style)
	}
	if lines[1].Text != "-old line" || lines[1].Style.Foreground != layout.ColorRed {
		t.Errorf("removed row = %q %+v", lines[1].Text, lines[1].Style)
	}
	if lines[2].Text != "+new line" || lines[2].Style.Foreground != layout.ColorGreen {
		t.Errorf("added row = %q %+v", lines[2].Text, lines[2].Style)
	}
}

func TestHunkPopupLinesSummarizesOneSidedHunks(t *testing.T) {
	added := hunkPopupLines(gitstatus.Hunk{Added: []string{"a", "b"}}, 4, 80)
	if !strings.Contains(added[0].Text, "2 lines added") {
		t.Errorf("header = %q, want it to say 2 lines added", added[0].Text)
	}
	removed := hunkPopupLines(gitstatus.Hunk{Removed: []string{"a"}}, 4, 80)
	if !strings.Contains(removed[0].Text, "1 line removed") {
		t.Errorf("header = %q, want it to say 1 line removed", removed[0].Text)
	}
	both := hunkPopupLines(gitstatus.Hunk{Removed: []string{"a"}, Added: []string{"b", "c"}}, 4, 80)
	if !strings.Contains(both[0].Text, "1 line → 2 lines") {
		t.Errorf("header = %q, want it to say 1 line → 2 lines", both[0].Text)
	}
}

func TestHunkPopupLinesCapsEachSide(t *testing.T) {
	many := make([]string, maxHunkPopupSide+7)
	for i := range many {
		many[i] = "x"
	}

	lines := hunkPopupLines(gitstatus.Hunk{Removed: many, Added: many}, 4, 80)
	// header + (side cap + "… N more") twice
	if want := 1 + 2*(maxHunkPopupSide+1); len(lines) != want {
		t.Fatalf("got %d rows, want %d:\n%s", len(lines), want, popupText(lines))
	}
	if !strings.Contains(popupText(lines), "7 more") {
		t.Errorf("expected the omitted count to be reported, got:\n%s", popupText(lines))
	}
}

func TestHunkPopupLinesExpandsTabsAndClampsWidth(t *testing.T) {
	h := gitstatus.Hunk{Added: []string{"\tindented"}}

	lines := hunkPopupLines(h, 4, 80)
	if got := lines[1].Text; got != "+    indented" {
		t.Errorf("row = %q, want the tab expanded to 4 spaces", got)
	}

	// A row wider than the popup is truncated, never overflowed — the popup's
	// padding is computed from display width (see renderStyledPopup).
	narrow := hunkPopupLines(gitstatus.Hunk{Added: []string{"0123456789"}}, 4, 5)
	if got := narrow[1].Text; len([]rune(got)) > 5 {
		t.Errorf("row %q exceeds the 5-column budget", got)
	}
}

// The View must not talk to git itself; with no callbacks wired the git
// tooltips are simply absent, the way a pane with no language server has no
// LSP features.
func TestGitTooltipsAreNoOpsWithoutCallbacks(t *testing.T) {
	v := NewView()
	v.tabs = []*tab{{path: "/x/f.go", buf: &Buffer{Lines: []string{"a"}}}}

	v.HandleKey(layout.Key{Text: "B"})
	v.HandleKey(layout.Key{Text: "H"})
	v.HandleKey(layout.Key{Text: "D"})
	if v.gitPopup != nil {
		t.Errorf("expected no popup without callbacks, got %+v", v.gitPopup)
	}
}

func TestShowBlameUsesOneBasedLines(t *testing.T) {
	v := NewView()
	v.lastWidth = 80
	v.tabs = []*tab{{path: "/x/f.go", buf: &Buffer{Lines: []string{"a", "b", "c"}}, cursorLn: 2}}

	var gotPath string
	var gotLine int
	v.BlameFunc = func(path string, line int) (gitblame.Info, error) {
		gotPath, gotLine = path, line
		return gitblame.Info{Commit: "abc1234", Author: "A", Summary: "s"}, nil
	}

	v.HandleKey(layout.Key{Text: "B"})
	if gotPath != "/x/f.go" {
		t.Errorf("path = %q", gotPath)
	}
	if gotLine != 3 {
		t.Errorf("line = %d, want 3 (1-based for cursor index 2)", gotLine)
	}
	if !strings.Contains(popupText(v.gitPopup), "abc1234") {
		t.Errorf("expected the blame popup to be showing, got %+v", v.gitPopup)
	}
}

func TestShowBlameErrorShowsNoPopup(t *testing.T) {
	v := NewView()
	v.lastWidth = 80
	v.tabs = []*tab{{path: "/x/f.go", buf: &Buffer{Lines: []string{"a"}}}}
	v.BlameFunc = func(string, int) (gitblame.Info, error) {
		return gitblame.Info{}, errors.New("not a git repository")
	}

	v.HandleKey(layout.Key{Text: "B"})
	if v.gitPopup != nil {
		t.Errorf("expected no popup when blame fails, got %+v", v.gitPopup)
	}
}

func TestShowLineDiffUsesZeroBasedLinesAndReportsCleanLines(t *testing.T) {
	v := NewView()
	v.lastWidth = 80
	v.tabs = []*tab{{path: "/x/f.go", buf: &Buffer{Lines: []string{"a", "b"}}, cursorLn: 1}}

	var gotLine int
	v.HunkFunc = func(_ string, line int) (gitstatus.Hunk, bool, error) {
		gotLine = line
		return gitstatus.Hunk{}, false, nil
	}

	v.HandleKey(layout.Key{Text: "H"})
	if gotLine != 1 {
		t.Errorf("line = %d, want 1 (0-based, matching the gutter's indexing)", gotLine)
	}
	if !strings.Contains(popupText(v.gitPopup), "no change") {
		t.Errorf("expected an explicit 'no change' popup, got %+v", v.gitPopup)
	}
}

func TestShowLineDiffUntrackedFileExplainsItself(t *testing.T) {
	v := NewView()
	v.lastWidth = 80
	v.tabs = []*tab{{path: "/x/new.go", buf: &Buffer{Lines: []string{"a"}}}}
	v.HunkFunc = func(string, int) (gitstatus.Hunk, bool, error) {
		return gitstatus.Hunk{}, false, gitstatus.ErrUntracked
	}

	v.HandleKey(layout.Key{Text: "H"})
	if !strings.Contains(popupText(v.gitPopup), "untracked") {
		t.Errorf("expected the popup to explain the file is untracked, got %+v", v.gitPopup)
	}
}

func TestShowLineDiffWarnsWhenTheBufferIsDirty(t *testing.T) {
	v := NewView()
	v.lastWidth = 80
	buf := &Buffer{Lines: []string{"a"}}
	buf.Dirty = true
	v.tabs = []*tab{{path: "/x/f.go", buf: buf}}
	v.HunkFunc = func(string, int) (gitstatus.Hunk, bool, error) {
		return gitstatus.Hunk{Removed: []string{"old"}, Added: []string{"new"}}, true, nil
	}

	v.HandleKey(layout.Key{Text: "H"})
	if !strings.Contains(popupText(v.gitPopup), "unsaved") {
		t.Errorf("expected the offset warning on a dirty buffer, got:\n%s", popupText(v.gitPopup))
	}
}

func TestShowFileDiffPassesTheActivePath(t *testing.T) {
	v := NewView()
	v.tabs = []*tab{{path: "/x/f.go", buf: &Buffer{Lines: []string{"a"}}}}

	var got string
	v.OnShowFileDiff = func(path string) { got = path }

	v.HandleKey(layout.Key{Text: "D"})
	if got != "/x/f.go" {
		t.Errorf("path = %q, want /x/f.go", got)
	}
}

// The git tooltips are tooltips, not modes: the next keypress clears them.
func TestGitPopupIsDismissedByTheNextKey(t *testing.T) {
	v := NewView()
	v.lastWidth = 80
	v.tabs = []*tab{{path: "/x/f.go", buf: &Buffer{Lines: []string{"a", "b"}}}}
	v.BlameFunc = func(string, int) (gitblame.Info, error) {
		return gitblame.Info{Commit: "abc1234", Summary: "s"}, nil
	}

	v.HandleKey(layout.Key{Text: "B"})
	if v.gitPopup == nil {
		t.Fatal("expected the popup to be showing")
	}
	v.HandleKey(layout.Key{Named: layout.KeyDown})
	if v.gitPopup != nil {
		t.Errorf("expected the next keypress to dismiss the popup, got %+v", v.gitPopup)
	}
}
