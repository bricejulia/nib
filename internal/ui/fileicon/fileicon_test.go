package fileicon

import (
	"testing"

	"github.com/mattn/go-runewidth"

	"github.com/bricejulia/nib/internal/layout"
	"github.com/bricejulia/nib/internal/theme"
)

func TestForFileNameBeatsExtension(t *testing.T) {
	if got, ext := ForFile("package.json"), ForFile("x.json"); got == ext {
		t.Errorf("package.json got the plain .json icon %+v", got)
	}
	if got, ext := ForFile("go.mod"), ForFile("main.go"); got == ext {
		t.Errorf("go.mod got the plain .go icon %+v", got)
	}
}

func TestForFileExtensionIsCaseInsensitive(t *testing.T) {
	if got, want := ForFile("MAIN.GO"), ForFile("main.go"); got != want {
		t.Errorf("ForFile(MAIN.GO) = %+v, want %+v", got, want)
	}
	if got, want := ForFile("NOTES.Md"), ForFile("notes.md"); got != want {
		t.Errorf("ForFile(NOTES.Md) = %+v, want %+v", got, want)
	}
}

func TestForFileFallsBackToGeneric(t *testing.T) {
	for _, name := range []string{"unknown.zzz", "noext", ".hidden", "trailing."} {
		if got := ForFile(name); got != genericFile {
			t.Errorf("ForFile(%q) = %+v, want the generic icon", name, got)
		}
	}
}

func TestForFileMatchesDotfileByName(t *testing.T) {
	if got := ForFile(".gitignore"); got != gitIcon {
		t.Errorf("ForFile(.gitignore) = %+v, want the git icon", got)
	}
}

func TestForDirOpenAndClosedDiffer(t *testing.T) {
	if ForDir(true) == ForDir(false) {
		t.Error("expanded and collapsed folders share a glyph")
	}
}

func TestForegroundFallsBackToThemeRole(t *testing.T) {
	if got, want := genericFile.Foreground(), theme.Get(theme.FiletreeIcon); got != want {
		t.Errorf("generic Foreground() = %v, want the filetree_icon role %v", got, want)
	}
	if got := goIcon.Foreground(); got != layout.ColorCyan {
		t.Errorf("go Foreground() = %v, want its own color", got)
	}
}

// TestEveryGlyphIsSingleWidthBMPPUA guards the column arithmetic the
// package doc promises: one rune, U+E000–U+F8FF, one display column.
func TestEveryGlyphIsSingleWidthBMPPUA(t *testing.T) {
	all := []Icon{folderClosed, folderOpen, genericFile, BrokenLink()}
	for _, icon := range byName {
		all = append(all, icon)
	}
	for _, icon := range byExt {
		all = append(all, icon)
	}
	for _, icon := range all {
		runes := []rune(icon.Glyph)
		if len(runes) != 1 {
			t.Errorf("glyph %q is %d runes, want 1", icon.Glyph, len(runes))
			continue
		}
		r := runes[0]
		if r < 0xE000 || r > 0xF8FF {
			t.Errorf("glyph U+%04X is outside the BMP Private Use Area", r)
		}
		if w := runewidth.RuneWidth(r); w != 1 {
			t.Errorf("glyph U+%04X has width %d, want 1", r, w)
		}
	}
}
