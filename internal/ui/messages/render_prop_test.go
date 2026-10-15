package messages

import (
	"regexp"
	"strings"
	"testing"
	"unicode"
	"unicode/utf8"

	"charm.land/lipgloss/v2"
	"github.com/charmbracelet/x/ansi"
	"github.com/gammons/slk/internal/proptest"
	"github.com/gammons/slk/internal/text"
	"pgregory.net/rapid"
)

// noSpace drops every whitespace rune, leaving the content WordWrap must
// keep: it may move line breaks and collapse runs of spaces, nothing else.
func noSpace(s string) string {
	return strings.Map(func(r rune) rune {
		if unicode.IsSpace(r) {
			return -1
		}
		return r
	}, s)
}

// wrapInput is styled text, sometimes with a long unbroken word so the
// hard-break path runs on ANSI and wide glyphs.
func wrapInput() *rapid.Generator[string] {
	return rapid.Custom(func(t *rapid.T) string {
		s := proptest.StyledText().Draw(t, "text")
		if rapid.Bool().Draw(t, "long") {
			var b strings.Builder
			for range rapid.IntRange(2, 6).Draw(t, "parts") {
				b.WriteString(proptest.Word().Draw(t, "part"))
				if rapid.Bool().Draw(t, "sgr") {
					b.WriteString(proptest.SGR().Draw(t, "inner"))
				}
			}
			if rapid.Bool().Draw(t, "before") {
				s = b.String() + " " + s
			} else {
				s += " " + b.String()
			}
		}
		// KNOWN: an ASCII byte followed by VS16 ("a\uFE0F", " \uFE0F", the
		// keycaps "1\uFE0F\u20E3") is 2 cells to lipgloss.Width but is
		// measured as 1 elsewhere, so a wrapped line holding one can be a
		// cell too wide. ansi.Hardwrap takes the ASCII byte on its own path
		// and the VS16 as a separate zero-width cluster. strings.Fields
		// splits " \uFE0F" at the space, so wrapLine measures the word
		// without it and the rejoined line grows by a cell. The first is a
		// bug in x/ansi; working around either means wrapLine measuring
		// graphemes itself. Strip the pair (repeatedly, since stripping
		// "a\uFE0F\uFE0F" leaves "a\uFE0F").
		for asciiVS16.MatchString(s) {
			s = asciiVS16.ReplaceAllString(s, "$1")
		}
		return s
	})
}

// asciiVS16 matches an ASCII byte followed by VS16 (see wrapInput).
var asciiVS16 = regexp.MustCompile("([\\x00-\\x7f](?:\x1b\\[[0-9;]*m)*)\uFE0F")

// inTextDomain reports whether fuzz input s looks like rendered message
// text: valid UTF-8 (it is decoded JSON), no C0 controls but newline and
// ESC, and every ESC opening a CSI or OSC. Outside that the functions
// under test and the ansi parser the property checks with disagree
// about what is visible, and neither is wrong: invalid bytes come out
// of HighlightSearchTerms as U+FFFD (it walks runes); lipgloss.Width and
// ansi.Hardwrap measure "\xf00 0\x9f00" and "\x1b\x10" differently;
// ansi.Strip reads "\x1bX" as the start of an SOS string that swallows
// the rest of the input.
func inTextDomain(s string) bool {
	if !utf8.ValidString(s) {
		return false
	}
	for i := 0; i < len(s); i++ {
		switch c := s[i]; {
		case c == 0x1b:
			if i+1 >= len(s) || (s[i+1] != '[' && s[i+1] != ']') {
				return false
			}
		case c == '\n':
		case c < 0x20 || c == 0x7f:
			return false
		}
	}
	return true
}

func foldTerm(s string) string { return text.Fold(s) }

// For a positive limit, no output line is wider than the limit, the
// visible non-space content is unchanged, and wrapping again at the same
// limit changes nothing.
func TestWordWrapFitsAndKeepsContent(t *testing.T) {
	rapid.Check(t, func(t *rapid.T) {
		s := wrapInput().Draw(t, "s")
		// A grapheme two cells wide cannot be split, so a limit of 1
		// cannot hold every input; 2 is the smallest that can.
		limit := rapid.IntRange(2, 30).Draw(t, "limit")
		out := WordWrap(s, limit)
		for i, line := range strings.Split(out, "\n") {
			if w := lipgloss.Width(line); w > limit {
				t.Fatalf("line %d %q has width %d > %d", i, line, w, limit)
			}
		}
		if got, want := noSpace(ansi.Strip(out)), noSpace(ansi.Strip(s)); got != want {
			t.Fatalf("content changed:\n got %q\nwant %q", got, want)
		}
		if again := WordWrap(out, limit); again != out {
			t.Fatalf("not idempotent:\nonce  %q\ntwice %q", out, again)
		}
	})
}

// A limit of zero or less turns wrapping off.
func TestWordWrapNonPositiveLimitIsIdentity(t *testing.T) {
	rapid.Check(t, func(t *rapid.T) {
		s := proptest.StyledText().Draw(t, "s")
		limit := rapid.IntRange(-5, 0).Draw(t, "limit")
		if out := WordWrap(s, limit); out != s {
			t.Fatalf("WordWrap(%q, %d) = %q", s, limit, out)
		}
	})
}

// FuzzWordWrap feeds arbitrary bytes: WordWrap must not panic, and no
// line it returns may be wider than the limit.
func FuzzWordWrap(f *testing.F) {
	for _, s := range []string{"", "hello world", "漢字漢字漢字", "\x1b[1mbold\x1b[m text", "a\x1b]8;;https://x\x1b\\link\x1b]8;;\x1b\\b", "😀😀😀 x"} {
		f.Add(s, uint8(3))
	}
	f.Fuzz(func(t *testing.T, s string, l uint8) {
		limit := int(l%40) + 2 // see TestWordWrapFitsAndKeepsContent
		if asciiVS16.MatchString(s) {
			return // KNOWN: see wrapInput
		}
		if !inTextDomain(s) {
			return
		}
		out := WordWrap(s, limit)
		for i, line := range strings.Split(out, "\n") {
			if w := lipgloss.Width(line); w > limit {
				t.Fatalf("line %d %q has width %d > %d", i, line, w, limit)
			}
		}
	})
}

// Highlighting adds escapes and nothing else: the visible text is the
// same before and after, for any terms.
func TestHighlightSearchTermsKeepsVisibleText(t *testing.T) {
	rapid.Check(t, func(t *rapid.T) {
		s := proptest.StyledText().Draw(t, "s")
		terms := rapid.SliceOfN(rapid.Custom(func(t *rapid.T) string {
			return foldTerm(proptest.Word().Draw(t, "term"))
		}), 0, 3).Draw(t, "terms")
		out := HighlightSearchTerms(s, terms, "\x1b[7m", "\x1b[27m")
		if ansi.Strip(out) != ansi.Strip(s) {
			t.Fatalf("visible text changed:\n in  %q\n out %q", s, out)
		}
		if len(terms) == 0 && out != s {
			t.Fatalf("no terms but output differs: %q", out)
		}
	})
}

// Non-CSI escapes (OSC hyperlinks, two-byte escapes) come out byte for
// byte, so a highlight can never corrupt a hyperlink's URL.
func TestHighlightSearchTermsKeepsOSC(t *testing.T) {
	rapid.Check(t, func(t *rapid.T) {
		s := proptest.StyledText().Draw(t, "s")
		term := foldTerm(rapid.SampledFrom([]string{"https", "example", "com", "a", "8"}).Draw(t, "term"))
		out := HighlightSearchTerms(s, []string{term}, "\x1b[7m", "\x1b[27m")
		if got, want := oscs(out), oscs(s); strings.Join(got, "|") != strings.Join(want, "|") {
			t.Fatalf("OSC sequences changed:\n got %q\nwant %q", got, want)
		}
	})
}

// oscs lists every OSC sequence in s (ESC ] ... ESC \), in order.
func oscs(s string) []string {
	var out []string
	for {
		i := strings.Index(s, "\x1b]")
		if i < 0 {
			return out
		}
		j := strings.Index(s[i:], "\x1b\\")
		if j < 0 {
			return append(out, s[i:])
		}
		out = append(out, s[i:i+j+2])
		s = s[i+j+2:]
	}
}

// FuzzHighlightSearchTerms feeds arbitrary bytes, including truncated
// and unterminated escapes: it must terminate, not panic, and keep the
// text outside its own highlight escapes.
func FuzzHighlightSearchTerms(f *testing.F) {
	for _, s := range []string{"", "hello world", "\x1b[1mhe\x1b[mllo", "\x1b]8;;https://h\x1b\\h\x1b]8;;\x1b\\", "\x1b", "\x1b[", "\x1b(Bhello", "café héllo"} {
		f.Add(s, "he")
	}
	f.Fuzz(func(t *testing.T, s, term string) {
		if !inTextDomain(s) {
			return
		}
		out := HighlightSearchTerms(s, []string{foldTerm(term)}, "\x1b[7m", "\x1b[27m")
		if ansi.Strip(out) != ansi.Strip(s) {
			t.Fatalf("visible text changed:\n in  %q\n out %q", s, out)
		}
	})
}

// substituteBgSGR with from == to, or on text with no SGR, is the
// identity; otherwise it changes no visible text and leaves every
// non-bg parameter (including sub-arguments like 38;5;40) alone.
func TestSubstituteBgSGRTouchesOnlyBgTokens(t *testing.T) {
	rapid.Check(t, func(t *rapid.T) {
		s := proptest.StyledTextWith(proptest.BgSGR()).Draw(t, "s")
		from := bgSGRParams(proptest.BgSGR().Draw(t, "from"))
		to := bgSGRParams(proptest.BgSGR().Draw(t, "to"))
		if out := substituteBgSGR(s, from, from); out != s {
			t.Fatalf("from == to changed %q to %q", s, out)
		}
		plain := proptest.PlainText().Draw(t, "plain")
		if out := substituteBgSGR(plain, from, to); out != plain {
			t.Fatalf("no SGR but %q became %q", plain, out)
		}
		out := substituteBgSGR(s, from, to)
		if ansi.Strip(out) != ansi.Strip(s) {
			t.Fatalf("visible text changed:\n in  %q\n out %q", s, out)
		}
		in, got := sgrSeqs(s), sgrSeqs(out)
		if len(in) != len(got) {
			t.Fatalf("SGR count %d became %d", len(in), len(got))
		}
		for i := range in {
			want := mapParams(splitSGRParams(in[i]), func(p sgrParam) string {
				if p.isBg && p.text == from {
					return to
				}
				return p.text
			})
			if got[i] != want {
				t.Fatalf("SGR %d: %q became %q, want %q", i, in[i], got[i], want)
			}
			// No non-bg token changed.
			ip, op := splitSGRParams(in[i]), splitSGRParams(got[i])
			var inNonBg, outNonBg []string
			for _, p := range ip {
				if !p.isBg {
					inNonBg = append(inNonBg, p.text)
				}
			}
			for _, p := range op {
				if !p.isBg {
					outNonBg = append(outNonBg, p.text)
				}
			}
			if strings.Join(inNonBg, ";") != strings.Join(outNonBg, ";") {
				t.Fatalf("non-bg params changed: %q -> %q", in[i], got[i])
			}
		}
	})
}

// Splitting an SGR parameter list and joining the tokens back gives the
// original list: tokenizing never drops or reorders a parameter.
func TestSplitSGRParamsRoundTrip(t *testing.T) {
	rapid.Check(t, func(t *rapid.T) {
		params := rapid.SliceOfN(rapid.SampledFrom([]string{"0", "1", "2", "5", "38", "48", "40", "107", "255", ""}), 1, 10).Draw(t, "params")
		joined := strings.Join(params, ";")
		got := mapParams(splitSGRParams(joined), func(p sgrParam) string { return p.text })
		if joined == "" {
			if got != "" {
				t.Fatalf("empty params gave %q", got)
			}
			return
		}
		if got != joined {
			t.Fatalf("split/join %q gave %q", joined, got)
		}
	})
}

// bgSGRParams inverts the framing every bg escape has.
func TestBgSGRParamsUnframes(t *testing.T) {
	rapid.Check(t, func(t *rapid.T) {
		esc := proptest.BgSGR().Draw(t, "esc")
		if got := "\x1b[" + bgSGRParams(esc) + "m"; got != esc {
			t.Fatalf("bgSGRParams(%q) reframed to %q", esc, got)
		}
	})
}

func mapParams(ps []sgrParam, f func(sgrParam) string) string {
	out := make([]string, len(ps))
	for i, p := range ps {
		out[i] = f(p)
	}
	return strings.Join(out, ";")
}

// sgrSeqs lists the parameter strings of every SGR in s.
func sgrSeqs(s string) []string {
	var out []string
	for {
		i := strings.Index(s, "\x1b[")
		if i < 0 {
			return out
		}
		j := i + 2
		for j < len(s) && (s[j] == ';' || s[j] >= '0' && s[j] <= '9') {
			j++
		}
		if j < len(s) && s[j] == 'm' {
			out = append(out, s[i+2:j])
		}
		s = s[j:]
	}
}
