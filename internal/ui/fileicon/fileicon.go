// Package fileicon maps a file or directory to the Nerd Font glyph (and
// color) drawn before its name when "icons = true" is set — shared so the
// file tree and, later, other file lists render a type identically.
//
// Every glyph is a Basic Multilingual Plane Private Use Area code point
// (U+E000–U+F8FF: the Seti, Devicons and Font Awesome sets), never the
// supplementary-plane Material set (nf-md-*, U+F0000+). go-runewidth
// counts BMP PUA runes as one column, which is what the tree's column
// arithmetic assumes, and terminals agree on that range more reliably.
// Glyphs are written as escapes so the table stays readable without a
// patched font.
package fileicon

import (
	"path/filepath"
	"strings"

	"github.com/bricejulia/nib/internal/layout"
	"github.com/bricejulia/nib/internal/theme"
)

// Icon is one glyph and its color. A zero Color means "no color of its
// own": Foreground falls back to the theme's FiletreeIcon role.
type Icon struct {
	Glyph string
	Color layout.Color
}

// Foreground is the color to draw the glyph in, resolving the fallback
// at call time so a theme reload takes effect on the next render.
func (i Icon) Foreground() layout.Color {
	if i.Color == layout.ColorDefault {
		return theme.Get(theme.FiletreeIcon)
	}
	return i.Color
}

var (
	folderClosed = Icon{Glyph: ""} // fa-folder
	folderOpen   = Icon{Glyph: ""} // fa-folder_open
	genericFile  = Icon{Glyph: ""} // fa-file_o
)

// ForDir is the folder glyph for a directory, open when expanded.
func ForDir(expanded bool) Icon {
	if expanded {
		return folderOpen
	}
	return folderClosed
}

// BrokenLink is the glyph for a symlink whose target is missing, in the
// same color the tree gives the rest of a broken link's row.
func BrokenLink() Icon {
	return Icon{Glyph: "", Color: theme.Get(theme.FiletreeSymlinkBroken)} // fa-chain_broken
}

// ForFile looks name (a base name, not a path) up by exact file name
// first, so go.mod or package.json beat their extension, then by
// extension case-insensitively, then falls back to a generic file glyph.
func ForFile(name string) Icon {
	if icon, ok := byName[name]; ok {
		return icon
	}
	if ext := strings.TrimPrefix(filepath.Ext(name), "."); ext != "" {
		if icon, ok := byExt[strings.ToLower(ext)]; ok {
			return icon
		}
	}
	return genericFile
}

var (
	goIcon      = Icon{Glyph: "", Color: layout.ColorCyan}        // seti-go
	jsIcon      = Icon{Glyph: "", Color: layout.ColorYellow}      // dev-javascript
	tsIcon      = Icon{Glyph: "", Color: layout.ColorBlue}        // seti-typescript
	reactIcon   = Icon{Glyph: "", Color: layout.ColorCyan}        // dev-react
	jsonIcon    = Icon{Glyph: "", Color: layout.ColorYellow}      // seti-json
	configIcon  = Icon{Glyph: "", Color: layout.ColorMagenta}     // seti-config
	markdown    = Icon{Glyph: "", Color: layout.ColorBrightBlue}  // seti-markdown
	textIcon    = Icon{Glyph: "", Color: layout.ColorWhite}       // fa-file_text
	cIcon       = Icon{Glyph: "", Color: layout.ColorBlue}        // seti-c
	cppIcon     = Icon{Glyph: "", Color: layout.ColorBlue}        // seti-cpp
	shellIcon   = Icon{Glyph: "", Color: layout.ColorGreen}       // dev-terminal
	gitIcon     = Icon{Glyph: "", Color: layout.ColorBrightRed}   // dev-git
	dockerIcon  = Icon{Glyph: "", Color: layout.ColorBlue}        // dev-docker
	lockIcon    = Icon{Glyph: "", Color: layout.ColorBrightBlack} // fa-lock
	imageIcon   = Icon{Glyph: "", Color: layout.ColorMagenta}     // fa-file_image_o
	archiveIcon = Icon{Glyph: "", Color: layout.ColorYellow}      // fa-file_archive_o
	videoIcon   = Icon{Glyph: "", Color: layout.ColorMagenta}     // fa-file_video_o
	audioIcon   = Icon{Glyph: "", Color: layout.ColorCyan}        // fa-file_audio_o
	codeIcon    = Icon{Glyph: "", Color: layout.ColorYellow}      // fa-code
)

// byName matches an exact base name, case-sensitively: these are
// conventional names, and "makefile" vs "Makefile" is the project's call.
var byName = map[string]Icon{
	"go.mod":         {Glyph: "", Color: layout.ColorMagenta}, // seti-go, tinted as config
	"go.work":        {Glyph: "", Color: layout.ColorMagenta},
	"go.sum":         lockIcon,
	"package.json":   {Glyph: "", Color: layout.ColorRed}, // dev-npm
	"Makefile":       {Glyph: "", Color: layout.ColorRed}, // dev-gnu
	"GNUmakefile":    {Glyph: "", Color: layout.ColorRed},
	"Dockerfile":     dockerIcon,
	"LICENSE":        {Glyph: "", Color: layout.ColorYellow}, // seti-license
	"LICENSE.md":     {Glyph: "", Color: layout.ColorYellow},
	"README.md":      {Glyph: "", Color: layout.ColorBrightWhite}, // fa-info_circle
	".gitignore":     gitIcon,
	".gitattributes": gitIcon,
	".gitmodules":    gitIcon,
	".editorconfig":  configIcon,
	".env":           {Glyph: "", Color: layout.ColorYellow}, // fa-key
}

// byExt matches a lower-cased extension without its dot.
var byExt = map[string]Icon{
	"go": goIcon,

	"js":  jsIcon,
	"mjs": jsIcon,
	"cjs": jsIcon,
	"ts":  tsIcon,
	"jsx": reactIcon,
	"tsx": reactIcon,

	"py":    {Glyph: "", Color: layout.ColorYellow},    // seti-python
	"rs":    {Glyph: "", Color: layout.ColorRed},       // dev-rust
	"rb":    {Glyph: "", Color: layout.ColorRed},       // dev-ruby
	"php":   {Glyph: "", Color: layout.ColorMagenta},   // seti-php
	"lua":   {Glyph: "", Color: layout.ColorBlue},      // seti-lua
	"java":  {Glyph: "", Color: layout.ColorRed},       // dev-java
	"kt":    {Glyph: "", Color: layout.ColorMagenta},   // seti-kotlin
	"swift": {Glyph: "", Color: layout.ColorBrightRed}, // dev-swift
	"vim":   {Glyph: "", Color: layout.ColorGreen},     // seti-vim
	"sql":   {Glyph: "", Color: layout.ColorWhite},     // dev-database
	"c":     cIcon,
	"h":     cIcon,
	"cpp":   cppIcon,
	"cc":    cppIcon,
	"hpp":   cppIcon,

	"html": {Glyph: "", Color: layout.ColorBrightRed}, // seti-html
	"htm":  {Glyph: "", Color: layout.ColorBrightRed},
	"css":  {Glyph: "", Color: layout.ColorBlue},    // dev-css3
	"scss": {Glyph: "", Color: layout.ColorMagenta}, // seti-sass
	"xml":  codeIcon,

	"sh":   shellIcon,
	"bash": shellIcon,
	"zsh":  shellIcon,
	"fish": shellIcon,

	"json": jsonIcon,
	"yaml": configIcon,
	"yml":  configIcon,
	"toml": configIcon,
	"ini":  configIcon,
	"conf": configIcon,
	"lock": lockIcon,

	"md":       markdown,
	"markdown": markdown,
	"txt":      textIcon,
	"log":      textIcon,
	"pdf":      {Glyph: "", Color: layout.ColorRed},   // fa-file_pdf_o
	"csv":      {Glyph: "", Color: layout.ColorGreen}, // fa-file_excel_o

	"png":  imageIcon,
	"jpg":  imageIcon,
	"jpeg": imageIcon,
	"gif":  imageIcon,
	"svg":  imageIcon,
	"webp": imageIcon,
	"ico":  imageIcon,

	"zip": archiveIcon,
	"tar": archiveIcon,
	"gz":  archiveIcon,
	"tgz": archiveIcon,
	"xz":  archiveIcon,
	"7z":  archiveIcon,

	"mp4":  videoIcon,
	"mov":  videoIcon,
	"webm": videoIcon,
	"mp3":  audioIcon,
	"wav":  audioIcon,
	"flac": audioIcon,
}
