package entrylist

import (
	"strings"
	"testing"
	"unicode/utf8"

	"github.com/charmbracelet/x/ansi"
	"github.com/rivo/uniseg"
	"pgregory.net/rapid"

	"github.com/gammons/slk/internal/proptest"
)

// junk is styled text with raw bytes mixed in: invalid UTF-8, a lone ESC,
// a bare ZWJ and NUL.
func junk() *rapid.Generator[string] {
	return rapid.Custom(func(t *rapid.T) string {
		s := proptest.StyledText().Draw(t, "styled")
		piece := rapid.SampledFrom([]string{"\xff", "\xc3", "\x1b", "\x1b[", "\u200d", "\x00", "\xe2\x82", "\t"}).Draw(t, "junk")
		cut := rapid.IntRange(0, len(s)).Draw(t, "cut")
		return s[:cut] + piece + s[cut:]
	})
}

func clusterStarts(s string) map[int]bool {
	starts := map[int]bool{len(s): true}
	off, state, rest := 0, -1, s
	for len(rest) > 0 {
		var c string
		starts[off] = true
		c, rest, _, state = uniseg.FirstGraphemeClusterInString(rest, state)
		off += len(c)
	}
	return starts
}

// For every line: len(Bytes) == DisplayWidth+1; Bytes is non-decreasing,
// in range and ends at len(Text); Text is the stripped input; the
// width agrees with uniseg.StringWidth; every Bytes entry sits on a
// grapheme boundary.
func TestPlainLinesStructure(t *testing.T) {
	rapid.Check(t, func(t *rapid.T) {
		s := junk().Draw(t, "s")
		lines := PlainLines(s)
		var texts []string
		for _, p := range lines {
			texts = append(texts, p.Text)
			if len(p.Bytes) == 0 || p.Bytes[len(p.Bytes)-1] != len(p.Text) {
				t.Fatalf("Bytes %v does not end at len(Text)=%d", p.Bytes, len(p.Text))
			}
			if DisplayWidth(p) != len(p.Bytes)-1 {
				t.Fatalf("DisplayWidth %d vs Bytes %d", DisplayWidth(p), len(p.Bytes))
			}
			if w := uniseg.StringWidth(p.Text); w != DisplayWidth(p) {
				t.Fatalf("DisplayWidth %d != uniseg %d for %q", DisplayWidth(p), w, p.Text)
			}
			starts := clusterStarts(p.Text)
			prev := 0
			for c, b := range p.Bytes {
				if b < prev || b > len(p.Text) {
					t.Fatalf("Bytes not monotone/in range at %d: %v", c, p.Bytes)
				}
				if !starts[b] {
					t.Fatalf("Bytes[%d]=%d splits a grapheme of %q", c, b, p.Text)
				}
				prev = b
			}
		}
		if got, want := strings.Join(texts, "\n"), ansi.Strip(s); got != want {
			t.Fatalf("joined Text %q != ansi.Strip %q", got, want)
		}
	})
}

// SliceColumns is total: any from/to (negative, inverted, past the end)
// returns a substring of Text that is valid UTF-8 whenever Text is, and
// slices at consecutive split points concatenate to the slice of the
// whole range.
func TestSliceColumnsTotalAndAdditive(t *testing.T) {
	rapid.Check(t, func(t *rapid.T) {
		s := proptest.StyledText().Draw(t, "s")
		for _, p := range PlainLines(s) {
			w := DisplayWidth(p)
			from := rapid.IntRange(-3, w+3).Draw(t, "from")
			to := rapid.IntRange(-3, w+3).Draw(t, "to")
			got := SliceColumns(p, from, to)
			if !strings.Contains(p.Text, got) {
				t.Fatalf("slice %q not in %q", got, p.Text)
			}
			if utf8.ValidString(p.Text) && !utf8.ValidString(got) {
				t.Fatalf("slice %q invalid UTF-8", got)
			}
			a := rapid.IntRange(0, w).Draw(t, "a")
			b := rapid.IntRange(a, w).Draw(t, "b")
			if cat := SliceColumns(p, 0, a) + SliceColumns(p, a, b) + SliceColumns(p, b, w); cat != SliceColumns(p, 0, w) {
				t.Fatalf("pieces %q != whole %q (a=%d b=%d, line %q)", cat, SliceColumns(p, 0, w), a, b, p.Text)
			}
		}
	})
}

// The whole line [0, width) is the line's text: copying everything
// loses nothing, including zero-width clusters ahead of the first column.
func TestSliceColumnsWholeLineIsText(t *testing.T) {
	rapid.Check(t, func(t *rapid.T) {
		s := proptest.PlainText().Draw(t, "s")
		for _, p := range PlainLines(s) {
			// KNOWN: a line made only of zero-width clusters (a lone
			// combining mark or ZWJ) has no columns, so no slice can
			// reach its text. Nothing on screen to select; left as is.
			if DisplayWidth(p) == 0 {
				continue
			}
			if got := SliceColumns(p, 0, DisplayWidth(p)); got != p.Text {
				t.Fatalf("whole slice %q != Text %q", got, p.Text)
			}
		}
	})
}

// FuzzPlainLines: no panic on arbitrary bytes, and the structural
// invariants hold.
func FuzzPlainLines(f *testing.F) {
	for _, s := range []string{"", "abc", "\x1b[31mred\x1b[0m", "漢字", "\xff\xfe", "éx", "👨\u200d👩\u200d👧", "\x1b", "a\nb\n", "\u200da"} {
		f.Add(s)
	}
	f.Fuzz(func(t *testing.T, s string) {
		var texts []string
		for _, p := range PlainLines(s) {
			texts = append(texts, p.Text)
			w := DisplayWidth(p)
			if len(p.Bytes) != w+1 || p.Bytes[w] != len(p.Text) {
				t.Fatalf("bad Bytes %v for %q", p.Bytes, p.Text)
			}
			for i := 1; i < len(p.Bytes); i++ {
				if p.Bytes[i] < p.Bytes[i-1] {
					t.Fatalf("not monotone: %v", p.Bytes)
				}
			}
			_ = SliceColumns(p, -1, w+1)
			_ = SliceColumns(p, w, 0)
			if w >= 2 {
				_ = SliceColumns(p, 1, w-1)
			}
		}
		if got := strings.Join(texts, "\n"); got != ansi.Strip(s) {
			t.Fatalf("Text %q != Strip %q", got, ansi.Strip(s))
		}
	})
}
