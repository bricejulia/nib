package theme

import (
	"strings"

	"github.com/bricejulia/nib/internal/layout"
)

// colorNames maps a config-file color token to one of the 16 standard ANSI
// colors. Bright variants accept both a plain and a hyphenated spelling
// ("brightred"/"bright-red"), matching config.Normalize's tolerance for
// multiple spellings of the same named key; "gray"/"grey" alias
// bright-black, the closest ANSI color to an actual gray.
var colorNames = map[string]layout.Color{
	"default": layout.ColorDefault,

	"black":   layout.ColorBlack,
	"red":     layout.ColorRed,
	"green":   layout.ColorGreen,
	"yellow":  layout.ColorYellow,
	"blue":    layout.ColorBlue,
	"magenta": layout.ColorMagenta,
	"cyan":    layout.ColorCyan,
	"white":   layout.ColorWhite,

	"brightblack": layout.ColorBrightBlack, "bright-black": layout.ColorBrightBlack,
	"gray": layout.ColorBrightBlack, "grey": layout.ColorBrightBlack,
	"brightred": layout.ColorBrightRed, "bright-red": layout.ColorBrightRed,
	"brightgreen": layout.ColorBrightGreen, "bright-green": layout.ColorBrightGreen,
	"brightyellow": layout.ColorBrightYellow, "bright-yellow": layout.ColorBrightYellow,
	"brightblue": layout.ColorBrightBlue, "bright-blue": layout.ColorBrightBlue,
	"brightmagenta": layout.ColorBrightMagenta, "bright-magenta": layout.ColorBrightMagenta,
	"brightcyan": layout.ColorBrightCyan, "bright-cyan": layout.ColorBrightCyan,
	"brightwhite": layout.ColorBrightWhite, "bright-white": layout.ColorBrightWhite,
}

// ParseColorName parses a config-file color token, case-insensitively,
// into one of the 16 standard ANSI colors. ok is false for anything
// unrecognized, which callers (config.Parse) treat as a malformed line to
// silently skip, not an error.
func ParseColorName(s string) (layout.Color, bool) {
	c, ok := colorNames[strings.ToLower(strings.TrimSpace(s))]
	return c, ok
}
