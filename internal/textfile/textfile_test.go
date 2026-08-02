package textfile

import (
	"strings"
	"testing"
)

func TestDecodePlainUTF8HasNoBOM(t *testing.T) {
	text, charset, err := Decode([]byte("hello\nworld"))
	if err != nil {
		t.Fatal(err)
	}
	if charset != UTF8 {
		t.Errorf("charset = %q, want UTF8 (zero value)", charset)
	}
	if text != "hello\nworld" {
		t.Errorf("text = %q", text)
	}
}

func TestDecodeUTF8BOM(t *testing.T) {
	data := append(append([]byte(nil), utf8BOM...), []byte("hello")...)
	text, charset, err := Decode(data)
	if err != nil {
		t.Fatal(err)
	}
	if charset != UTF8BOM {
		t.Errorf("charset = %q, want UTF8BOM", charset)
	}
	if text != "hello" {
		t.Errorf("text = %q, want %q (BOM stripped)", text, "hello")
	}
}

func TestDecodeUTF16RoundTrips(t *testing.T) {
	for _, charset := range []Charset{UTF16LE, UTF16BE} {
		original := "héllo\nwörld €"
		data, err := Encode(original, charset)
		if err != nil {
			t.Fatalf("%s: Encode: %v", charset, err)
		}
		text, got, err := Decode(data)
		if err != nil {
			t.Fatalf("%s: Decode: %v", charset, err)
		}
		if got != charset {
			t.Errorf("detected charset = %q, want %q", got, charset)
		}
		if text != original {
			t.Errorf("%s: round-tripped text = %q, want %q", charset, text, original)
		}
	}
}

func TestDecodeUTF16TruncatedIsError(t *testing.T) {
	// A lone trailing byte after the BOM can't form a full UTF-16 code
	// unit.
	data := append(append([]byte(nil), utf16leBOM...), 0x41)
	if _, _, err := Decode(data); err == nil {
		t.Fatal("expected an error decoding truncated UTF-16")
	}
}

func TestEncodeUnsupportedCharsetIsError(t *testing.T) {
	if _, err := Encode("x", Charset("shift-jis")); err == nil {
		t.Fatal("expected an error encoding an unsupported charset")
	}
}

func TestSplitLinesLFOnlyMatchesOldTrimSuffixSplitBehavior(t *testing.T) {
	lines, eol := SplitLines("line one\n\ttabbed line\nline three\nline four\n")
	if eol != LF {
		t.Errorf("eol = %q, want LF (zero value)", eol)
	}
	want := []string{"line one", "\ttabbed line", "line three", "line four"}
	if len(lines) != len(want) {
		t.Fatalf("lines = %+v, want %+v", lines, want)
	}
	for i := range want {
		if lines[i] != want[i] {
			t.Errorf("lines[%d] = %q, want %q", i, lines[i], want[i])
		}
	}
}

func TestSplitLinesNoTrailingNewline(t *testing.T) {
	lines, eol := SplitLines("no trailing newline")
	if eol != LF {
		t.Errorf("eol = %q, want LF for a single line with no terminator at all", eol)
	}
	if len(lines) != 1 || lines[0] != "no trailing newline" {
		t.Fatalf("lines = %+v", lines)
	}
}

func TestSplitLinesEmptyTextIsOneEmptyLine(t *testing.T) {
	lines, _ := SplitLines("")
	if len(lines) != 1 || lines[0] != "" {
		t.Fatalf("lines = %+v, want a single empty line", lines)
	}
}

func TestSplitLinesDetectsCRLF(t *testing.T) {
	lines, eol := SplitLines("one\r\ntwo\r\nthree\r\n")
	if eol != CRLF {
		t.Fatalf("eol = %q, want CRLF", eol)
	}
	want := []string{"one", "two", "three"}
	if len(lines) != len(want) {
		t.Fatalf("lines = %+v, want %+v", lines, want)
	}
	for _, l := range lines {
		if strings.Contains(l, "\r") {
			t.Fatalf("line %q retains a stray \\r", l)
		}
	}
}

func TestSplitLinesDetectsBareCR(t *testing.T) {
	lines, eol := SplitLines("one\rtwo\rthree")
	if eol != CR {
		t.Fatalf("eol = %q, want CR", eol)
	}
	want := []string{"one", "two", "three"}
	if len(lines) != len(want) {
		t.Fatalf("lines = %+v, want %+v", lines, want)
	}
}

func TestSplitLinesMixedEndingsNormalizeCleanly(t *testing.T) {
	// The first terminator found (CRLF) is reported, but every boundary —
	// regardless of its own style — is still treated as a line break.
	lines, eol := SplitLines("one\r\ntwo\nthree\rfour")
	if eol != CRLF {
		t.Fatalf("eol = %q, want CRLF (the first terminator seen)", eol)
	}
	want := []string{"one", "two", "three", "four"}
	if len(lines) != len(want) {
		t.Fatalf("lines = %+v, want %+v", lines, want)
	}
	for i := range want {
		if lines[i] != want[i] {
			t.Errorf("lines[%d] = %q, want %q", i, lines[i], want[i])
		}
	}
}

func TestJoinLinesRoundTripsWithSplitLines(t *testing.T) {
	for _, eol := range []EOL{LF, CRLF, CR} {
		original := []string{"one", "two", "three"}
		joined := JoinLines(original, eol)
		lines, gotEOL := SplitLines(joined)
		if gotEOL != eol {
			t.Errorf("%q: detected eol = %q, want %q", eol, gotEOL, eol)
		}
		if len(lines) != len(original) {
			t.Fatalf("%q: lines = %+v, want %+v", eol, lines, original)
		}
		for i := range original {
			if lines[i] != original[i] {
				t.Errorf("%q: lines[%d] = %q, want %q", eol, i, lines[i], original[i])
			}
		}
	}
}
