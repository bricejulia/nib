package filetree

import (
	"path/filepath"
	"strings"
	"testing"

	"github.com/bricejulia/nib/internal/layout"
	"github.com/bricejulia/nib/internal/textwidth"
	"github.com/bricejulia/nib/internal/theme"
	"github.com/bricejulia/nib/internal/ui/fileicon"
	"github.com/bricejulia/nib/internal/ui/gitstyle"
	"github.com/bricejulia/nib/internal/vcs/gitstatus"
)

func joinSegments(segs []layout.Segment) string {
	var b strings.Builder
	for _, s := range segs {
		b.WriteString(s.Text)
	}
	return b.String()
}

// iconSegment is the glyph segment rowSegments emits when icons are on.
func iconSegment(t *testing.T, segs []layout.Segment) layout.Segment {
	t.Helper()
	if len(segs) != 4 {
		t.Fatalf("expected 4 segments with icons on, got %d: %+v", len(segs), segs)
	}
	return segs[1]
}

func TestRowSegmentsIconsOffMatchFormatRow(t *testing.T) {
	rows := []Row{
		{Node: &Node{Name: "main.go"}, Depth: 1},
		{Node: &Node{Name: "sub", IsDir: true, Expanded: true}},
		{Node: &Node{Name: "link", IsSymlink: true, LinkTarget: "x", LinkState: LinkOK}},
	}
	for _, r := range rows {
		for _, cursor := range []bool{false, true} {
			segs := rowSegments(r, cursor, false)
			want := layout.Segment{Text: formatRow(r, cursor), Style: styleForRow(r, cursor)}
			if len(segs) != 1 || segs[0] != want {
				t.Errorf("%s (cursor=%v): got %+v, want the single segment %+v", r.Node.Name, cursor, segs, want)
			}
		}
	}
}

func TestRowSegmentsIconsOnInsertGlyphBeforeName(t *testing.T) {
	file := Row{Node: &Node{Name: "main.go"}, Depth: 1}
	dir := Row{Node: &Node{Name: "sub", IsDir: true}}
	open := Row{Node: &Node{Name: "sub", IsDir: true, Expanded: true}}

	for _, tc := range []struct {
		row  Row
		icon fileicon.Icon
	}{
		{file, fileicon.ForFile("main.go")},
		{dir, fileicon.ForDir(false)},
		{open, fileicon.ForDir(true)},
	} {
		prefix, name := rowParts(tc.row, false)
		got := joinSegments(rowSegments(tc.row, false, true))
		if want := prefix + tc.icon.Glyph + " " + name; got != want {
			t.Errorf("row %q: got %q, want %q", tc.row.Node.Name, got, want)
		}
	}
}

// TestRowSegmentsIconsKeepChildrenAligned: a file one level down puts
// its icon in the same column as its parent folder's icon.
func TestRowSegmentsIconsKeepChildrenAligned(t *testing.T) {
	parent := Row{Node: &Node{Name: "sub", IsDir: true, Expanded: true}}
	child := Row{Node: &Node{Name: "a.go"}, Depth: 1}
	col := func(r Row) int {
		prefix, _ := rowParts(r, false)
		return textwidth.DisplayWidth(prefix)
	}
	if col(parent) != col(child) {
		t.Errorf("parent icon at column %d, child icon at %d", col(parent), col(child))
	}
}

func TestRowSegmentsIconColor(t *testing.T) {
	clean := Row{Node: &Node{Name: "main.go"}}
	if got, want := iconSegment(t, rowSegments(clean, false, true)).Style.Foreground, fileicon.ForFile("main.go").Foreground(); got != want {
		t.Errorf("clean file icon color = %v, want the table's %v", got, want)
	}

	modified := Row{Node: &Node{Name: "main.go", Status: gitstatus.Modified}}
	if got, want := iconSegment(t, rowSegments(modified, false, true)).Style, gitstyle.Style(gitstatus.Modified); got != want {
		t.Errorf("modified file icon style = %+v, want the git style %+v", got, want)
	}

	untracked := Row{Node: &Node{Name: "main.go", Status: gitstatus.Untracked}}
	if got := iconSegment(t, rowSegments(untracked, false, true)).Style; got != gitstyle.Style(gitstatus.Untracked) {
		t.Errorf("untracked file icon style = %+v, want the git style", got)
	}

	dir := Row{Node: &Node{Name: "sub", IsDir: true}}
	if got, want := iconSegment(t, rowSegments(dir, false, true)).Style.Foreground, theme.Get(theme.FiletreeIcon); got != want {
		t.Errorf("folder icon color = %v, want the filetree_icon role %v", got, want)
	}
}

func TestRowSegmentsCursorReversesEverySegment(t *testing.T) {
	r := Row{Node: &Node{Name: "sub", IsDir: true}}
	for i, s := range rowSegments(r, true, true) {
		if s.Style.Attr&layout.AttrReverse == 0 {
			t.Errorf("segment %d (%q) not reversed on the cursor row", i, s.Text)
		}
	}
}

func TestRowSegmentsBoldOnlyOnDirName(t *testing.T) {
	segs := rowSegments(Row{Node: &Node{Name: "sub", IsDir: true}}, false, true)
	for i, s := range segs {
		bold := s.Style.Attr&layout.AttrBold != 0
		if isName := i == len(segs)-1; bold != isName {
			t.Errorf("segment %d (%q) bold = %v, want %v", i, s.Text, bold, isName)
		}
	}
}

func TestRowSegmentsSymlinkIcons(t *testing.T) {
	blocked := Row{Node: &Node{Name: "out", IsSymlink: true, LinkState: LinkBlocked}}
	if icon := iconSegment(t, rowSegments(blocked, false, true)); icon.Style.Attr&layout.AttrDim == 0 {
		t.Errorf("blocked symlink icon not dimmed: %+v", icon.Style)
	}

	broken := Row{Node: &Node{Name: "gone.go", IsSymlink: true, LinkState: LinkBroken}}
	if icon := iconSegment(t, rowSegments(broken, false, true)); icon.Text != fileicon.BrokenLink().Glyph ||
		icon.Style.Foreground != theme.Get(theme.FiletreeSymlinkBroken) {
		t.Errorf("broken symlink icon = %+v, want the broken-link glyph in filetree_symlink_broken", icon)
	}

	toDir := Row{Node: &Node{Name: "d", IsDir: true, IsSymlink: true, LinkState: LinkOK}}
	segs := rowSegments(toDir, false, true)
	if icon := iconSegment(t, segs); icon.Text != fileicon.ForDir(false).Glyph {
		t.Errorf("dir symlink icon = %q, want the folder glyph", icon.Text)
	}
	if !strings.HasSuffix(segs[0].Text, "▶→") {
		t.Errorf("dir symlink prefix %q lost its → glyph", segs[0].Text)
	}

	toFile := Row{Node: &Node{Name: "alias", IsSymlink: true, LinkState: LinkOK, LinkReal: filepath.Join("x", "main.go")}}
	if icon := iconSegment(t, rowSegments(toFile, false, true)); icon.Text != fileicon.ForFile("main.go").Glyph {
		t.Errorf("file symlink icon = %q, want its target's type", icon.Text)
	}
}

func TestRenderWithIconsShowsGlyphs(t *testing.T) {
	v := New(fixtureRoot(t))
	w := newFakeWindow(40, 10)
	v.Render(w)
	before := strings.Join(w.lines, "\n")

	v.SetShowIcons(true)
	v.Render(w)
	joined := strings.Join(w.lines, "\n")
	for _, want := range []string{
		fileicon.ForDir(false).Glyph + " sub",
		fileicon.ForFile("a.txt").Glyph + " a.txt",
	} {
		if !strings.Contains(joined, want) {
			t.Errorf("rendered output missing %q:\n%s", want, joined)
		}
	}

	v.SetShowIcons(false)
	v.Render(w)
	if after := strings.Join(w.lines, "\n"); after != before {
		t.Errorf("icons off should render as before:\n%s\nvs\n%s", after, before)
	}
}

func TestRenderChangesModeWithIcons(t *testing.T) {
	v := New(fixtureRoot(t))
	v.SetShowIcons(true)
	v.ApplyChanges(map[string]gitstatus.Status{"a.txt": gitstatus.Modified})
	v.SetMode(ModeChanges)
	w := newFakeWindow(40, 10)
	v.Render(w)
	if want := fileicon.ForFile("a.txt").Glyph + " a.txt"; !strings.Contains(strings.Join(w.lines, "\n"), want) {
		t.Errorf("changes mode missing %q:\n%s", want, strings.Join(w.lines, "\n"))
	}
}
