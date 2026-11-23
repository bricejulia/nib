package merge

import (
	"slices"
	"strings"
	"testing"
)

func lines(s string) []string {
	if s == "" {
		return nil
	}
	return strings.Split(s, "\n")
}

func TestParseTwoWay(t *testing.T) {
	src := lines("a\n<<<<<<< HEAD\nmine\n=======\ntheirs\n>>>>>>> abc123 (fix it)\nz")
	got := Parse(src)
	if len(got) != 1 {
		t.Fatalf("got %d regions, want 1", len(got))
	}
	r := got[0]
	if r.Start != 1 || r.Mid != 3 || r.End != 5 || r.Base != -1 {
		t.Errorf("positions = %+v", r)
	}
	if r.HasBase() {
		t.Error("HasBase on a two-way region")
	}
	if r.LeftLabel != "HEAD" || r.RightLabel != "abc123 (fix it)" {
		t.Errorf("labels = %q / %q", r.LeftLabel, r.RightLabel)
	}
	if !slices.Equal(r.Left, []string{"mine"}) || !slices.Equal(r.Right, []string{"theirs"}) {
		t.Errorf("sides = %q / %q", r.Left, r.Right)
	}
}

func TestParseDiff3(t *testing.T) {
	src := lines("<<<<<<< HEAD\nL\n||||||| base\nB1\nB2\n=======\nR\n>>>>>>> topic")
	got := Parse(src)
	if len(got) != 1 {
		t.Fatalf("got %d regions", len(got))
	}
	r := got[0]
	if !r.HasBase() || r.Base != 2 || r.BaseLabel != "base" {
		t.Errorf("base = %d %q", r.Base, r.BaseLabel)
	}
	if !slices.Equal(r.BaseLines, []string{"B1", "B2"}) {
		t.Errorf("BaseLines = %q", r.BaseLines)
	}
	for ln, want := range []Side{SideLeft, SideLeft, SideBase, SideBase, SideBase, SideRight, SideRight, SideRight} {
		if got := r.SideOf(ln); got != want {
			t.Errorf("SideOf(%d) = %d, want %d", ln, got, want)
		}
	}
}

func TestParseMarkerSizeAndEmptySides(t *testing.T) {
	src := lines("<<<<<<<<< ours\n=========\n>>>>>>>>> theirs\n<<<<<<< a\n========\n>>>>>>> b")
	got := Parse(src)
	if len(got) != 1 {
		t.Fatalf("got %d regions, want 1 (second has a mismatched =)", len(got))
	}
	if got[0].Left != nil || got[0].Right != nil {
		t.Errorf("empty sides = %q / %q", got[0].Left, got[0].Right)
	}
}

func TestParseIgnoresUnclosedAndLookalikes(t *testing.T) {
	for _, src := range []string{
		"<<<<<<< HEAD\nx\n=======\ny",           // never closed
		"<<<<<<<HEAD\nx\n=======\ny\n>>>>>>> b", // no space after marker
		"<<<<<< HEAD\nx\n=======\ny\n>>>>>> b",  // too short
	} {
		if got := Parse(lines(src)); len(got) != 0 {
			t.Errorf("Parse(%q) = %d regions, want 0", src, len(got))
		}
	}
}

func TestParseRestartsOnNestedOpen(t *testing.T) {
	src := lines("<<<<<<< a\nx\n<<<<<<< b\ny\n=======\nz\n>>>>>>> c")
	got := Parse(src)
	if len(got) != 1 || got[0].Start != 2 {
		t.Fatalf("got %+v, want one region starting at line 2", got)
	}
}

func TestTakes(t *testing.T) {
	r := Region{Left: []string{"l"}, Right: []string{"r"}}
	if !slices.Equal(r.TakeBoth(), []string{"l", "r"}) {
		t.Errorf("TakeBoth = %q", r.TakeBoth())
	}
	if r.TakeNeither() != nil {
		t.Error("TakeNeither not empty")
	}
	both := r.TakeBoth()
	both[0] = "changed"
	if r.Left[0] != "l" {
		t.Error("TakeBoth aliases Left")
	}
}

func TestAutoResolve(t *testing.T) {
	cases := []struct {
		name              string
		base, left, right string
		want              string
		ok                bool
	}{
		{"left unchanged", "a", "a", "b", "b", true},
		{"right unchanged", "a", "b", "a", "b", true},
		{"same change", "a", "b", "b", "b", true},
		{"different lines", "a\nb\nc", "A\nb\nc", "a\nb\nC", "A\nb\nC", true},
		{"adjacent lines", "a\nb", "A\nb", "a\nB", "A\nB", true},
		{"different words same line", "foo(a, b)", "foo(x, b)", "foo(a, y)", "foo(x, y)", true},
		{"one deletes, other edits elsewhere", "a\nb\nc\nd", "a\nc\nd", "a\nb\nc\nD", "a\nc\nD", true},
		{"same word both sides", "foo(a)", "foo(x)", "foo(y)", "", false},
		{"both delete everything", "a", "", "", "", true},
	}
	for _, c := range cases {
		t.Run(c.name, func(t *testing.T) {
			r := Region{Base: 0, BaseLines: lines(c.base), Left: lines(c.left), Right: lines(c.right)}
			got, ok := r.AutoResolve()
			if ok != c.ok {
				t.Fatalf("ok = %v, want %v (got %q)", ok, c.ok, got)
			}
			if ok && strings.Join(got, "\n") != c.want {
				t.Errorf("got %q, want %q", strings.Join(got, "\n"), c.want)
			}
		})
	}
}

func TestAutoResolveNeedsBase(t *testing.T) {
	r := Region{Base: -1, Left: []string{"a"}, Right: []string{"a"}}
	if _, ok := r.AutoResolve(); ok {
		t.Error("resolved a region without a base")
	}
}

func TestMerge3(t *testing.T) {
	got, ok := Merge3(lines("a\nb\nc\nd"), lines("a\nX\nc\nd"), lines("a\nb\nc\nd\ne"))
	if !ok || strings.Join(got, ",") != "a,X,c,d,e" {
		t.Errorf("Merge3 = %q, %v", got, ok)
	}
	if _, ok := Merge3(lines("a"), lines("b"), lines("c")); ok {
		t.Error("conflicting change merged")
	}
}

func TestMatchBaseTooBigIsConflict(t *testing.T) {
	big := make([]string, 3000)
	other := make([]string, 3000)
	for i := range big {
		big[i] = "b" + strings.Repeat("x", i%7)
		other[i] = "o" + strings.Repeat("y", i%5)
	}
	if _, ok := Merge3(big, other, big[:10]); ok {
		t.Error("expected a conflict when the table is too large to compute")
	}
}
