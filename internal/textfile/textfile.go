// Package textfile owns charset and line-ending detection/conversion at
// the boundary between a file's on-disk bytes and nib's in-memory,
// always-UTF-8-with-LF representation — the same split
// internal/textwidth makes for display-width math. Nothing outside a
// Buffer's Load/Save should need to know a file's charset or EOL style;
// everywhere else, text is plain UTF-8 split on "\n".
package textfile

import (
	"encoding/binary"
	"fmt"
	"strings"
	"unicode/utf16"
)

// Charset identifies how a file's bytes are encoded. The zero value, UTF8,
// is deliberately today's default (no BOM) — every Buffer{} literal built
// without setting Charset (there are many, across tests) keeps behaving
// exactly as before.
type Charset string

const (
	UTF8    Charset = ""
	UTF8BOM Charset = "utf-8-bom"
	UTF16LE Charset = "utf-16le"
	UTF16BE Charset = "utf-16be"
)

// EOL identifies a file's line-ending style. The zero value, LF, is
// deliberately today's default (a bare "\n"), for the same reason as
// Charset's zero value above.
type EOL string

const (
	LF   EOL = ""
	CRLF EOL = "crlf"
	CR   EOL = "cr"
)

var (
	utf8BOM    = []byte{0xEF, 0xBB, 0xBF}
	utf16leBOM = []byte{0xFF, 0xFE}
	utf16beBOM = []byte{0xFE, 0xFF}
)

// Decode converts data to a UTF-8 string, detecting its charset from a
// byte-order mark. With no recognized BOM, data is assumed to already be
// UTF-8 — today's behavior, unchanged: an invalid byte sequence is not an
// error, it just decodes to the Unicode replacement character wherever it
// occurs (Go's string() conversion already does this for free).
func Decode(data []byte) (text string, charset Charset, err error) {
	switch {
	case hasPrefix(data, utf8BOM):
		return string(data[len(utf8BOM):]), UTF8BOM, nil
	case hasPrefix(data, utf16leBOM):
		text, err := decodeUTF16(data[len(utf16leBOM):], binary.LittleEndian)
		return text, UTF16LE, err
	case hasPrefix(data, utf16beBOM):
		text, err := decodeUTF16(data[len(utf16beBOM):], binary.BigEndian)
		return text, UTF16BE, err
	default:
		return string(data), UTF8, nil
	}
}

// Encode is Decode's inverse: re-adds a BOM and/or re-encodes to UTF-16 as
// charset requires. UTF8 (the zero value) is a plain byte-slice cast,
// identical to today's Save.
func Encode(text string, charset Charset) ([]byte, error) {
	switch charset {
	case UTF8:
		return []byte(text), nil
	case UTF8BOM:
		return append(append([]byte(nil), utf8BOM...), []byte(text)...), nil
	case UTF16LE:
		return encodeUTF16(text, binary.LittleEndian, utf16leBOM), nil
	case UTF16BE:
		return encodeUTF16(text, binary.BigEndian, utf16beBOM), nil
	default:
		return nil, fmt.Errorf("textfile: unsupported charset %q", charset)
	}
}

// SplitLines detects text's line-ending style (the first "\r\n", bare
// "\r", or "\n" wins; no terminator at all defaults to LF) and splits it
// into lines with every line-ending sequence stripped — including a lone
// trailing one, so an all-empty text is a single empty line. Mixed line
// endings within one text are all treated as line breaks; only the
// detected (first) style is reported back, to use on a subsequent
// JoinLines.
//
// For an LF-only text (the common case) this produces byte-for-byte the
// same lines as the old TrimSuffix+Split it replaces.
func SplitLines(text string) (lines []string, eol EOL) {
	eol = detectEOL(text)
	normalized := strings.NewReplacer("\r\n", "\n", "\r", "\n").Replace(text)
	normalized = strings.TrimSuffix(normalized, "\n")
	if normalized == "" {
		return []string{""}, eol
	}
	return strings.Split(normalized, "\n"), eol
}

// JoinLines is SplitLines' inverse for a given EOL style.
func JoinLines(lines []string, eol EOL) string {
	return strings.Join(lines, eolString(eol))
}

func detectEOL(text string) EOL {
	for i := 0; i < len(text); i++ {
		switch text[i] {
		case '\r':
			if i+1 < len(text) && text[i+1] == '\n' {
				return CRLF
			}
			return CR
		case '\n':
			return LF
		}
	}
	return LF
}

func eolString(eol EOL) string {
	switch eol {
	case CRLF:
		return "\r\n"
	case CR:
		return "\r"
	default:
		return "\n"
	}
}

func hasPrefix(data, prefix []byte) bool {
	if len(data) < len(prefix) {
		return false
	}
	for i, b := range prefix {
		if data[i] != b {
			return false
		}
	}
	return true
}

func decodeUTF16(data []byte, order binary.ByteOrder) (string, error) {
	if len(data)%2 != 0 {
		return "", fmt.Errorf("textfile: truncated utf-16 (odd byte count %d)", len(data))
	}
	units := make([]uint16, len(data)/2)
	for i := range units {
		units[i] = order.Uint16(data[i*2:])
	}
	return string(utf16.Decode(units)), nil
}

func encodeUTF16(text string, order binary.ByteOrder, bom []byte) []byte {
	units := utf16.Encode([]rune(text))
	out := make([]byte, len(bom)+len(units)*2)
	copy(out, bom)
	for i, u := range units {
		order.PutUint16(out[len(bom)+i*2:], u)
	}
	return out
}
