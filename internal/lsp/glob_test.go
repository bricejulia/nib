package lsp

import "testing"

func TestCompileGlob(t *testing.T) {
	for _, tc := range []struct {
		pattern, path string
		want          bool
	}{
		{"**/*.{go,mod,sum,work,s}", "main.go", true},
		{"**/*.{go,mod,sum,work,s}", "internal/lsp/glob.go", true},
		{"**/*.{go,mod,sum,work,s}", "go.mod", true},
		{"**/*.{go,mod,sum,work,s}", "sub/go.sum", true},
		{"**/*.{go,mod,sum,work,s}", "README.md", false},
		{"**/*.{go,mod,sum,work,s}", "main.gox", false},
		{"*.go", "main.go", true},
		{"*.go", "a/main.go", false},
		{"/root/**/*.go", "/root/a/b.go", true},
		{"/root/**/*.go", "/other/a/b.go", false},
		{"**/go.mod", "go.mod", true},
		{"file?.go", "file1.go", true},
		{"file[0-9].go", "file7.go", true},
		{"file[!0-9].go", "file7.go", false},
		{"**", "any/thing", true},
	} {
		re := compileGlob(tc.pattern)
		if re == nil {
			t.Errorf("compileGlob(%q) = nil", tc.pattern)
			continue
		}
		if got := re.MatchString(tc.path); got != tc.want {
			t.Errorf("glob %q on %q = %v, want %v", tc.pattern, tc.path, got, tc.want)
		}
	}
}

func TestCompileGlobRejectsMalformed(t *testing.T) {
	for _, p := range []string{"{a,b", "a}", "[abc", "{a,{b}}"} {
		if re := compileGlob(p); re != nil {
			t.Errorf("compileGlob(%q) = %v, want nil", p, re)
		}
	}
}
