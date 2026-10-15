// Package proptest holds the rapid generators that property tests across
// the repo share: terminal text with escape sequences in it, Slack wire
// text, Slack IDs and timestamps.
//
// Only _test.go files may import it. It is an ordinary package (not a
// _test.go file) so that tests in every package can reach it.
package proptest

import (
	"fmt"
	"strings"

	"pgregory.net/rapid"
)

// Runes that exercise display-width and segmentation edge cases: ASCII,
// accented Latin, CJK (two cells), emoji (two cells, some multi-codepoint),
// a combining mark, a zero-width joiner and a variation selector.
var awkwardRunes = []rune{
	'a', 'b', 'z', 'A', '0', '9', '.', ',', '-', '_', '*', '~', '`', '>', '<', '&', '|', '#', '@', ':',
	'é', 'ñ', 'ß',
	'漢', '字', 'の',
	'😀', '🎉', '👍',
	'́', // combining acute
	'‍', // zero-width joiner
	'️', // variation selector 16
}

// Word is a short run of awkward runes with no whitespace.
func Word() *rapid.Generator[string] {
	return rapid.Custom(func(t *rapid.T) string {
		n := rapid.IntRange(1, 8).Draw(t, "wordLen")
		var b strings.Builder
		for range n {
			b.WriteRune(rapid.SampledFrom(awkwardRunes).Draw(t, "rune"))
		}
		return b.String()
	})
}

// PlainText is words joined by single spaces, double spaces and newlines.
// It never contains an escape sequence.
func PlainText() *rapid.Generator[string] {
	return rapid.Custom(func(t *rapid.T) string {
		n := rapid.IntRange(0, 20).Draw(t, "words")
		var b strings.Builder
		for i := range n {
			if i > 0 {
				b.WriteString(rapid.SampledFrom([]string{" ", " ", " ", "  ", "\n"}).Draw(t, "sep"))
			}
			b.WriteString(Word().Draw(t, "word"))
		}
		return b.String()
	})
}

// SGR is one Select Graphic Rendition escape: a reset (both forms), an
// attribute, a basic, 256 or 24-bit colour, or several of those combined
// in one sequence the way lipgloss emits them.
func SGR() *rapid.Generator[string] {
	param := rapid.Custom(func(t *rapid.T) string {
		switch rapid.IntRange(0, 6).Draw(t, "kind") {
		case 0:
			return rapid.SampledFrom([]string{"1", "3", "4", "7", "22", "23", "24", "27"}).Draw(t, "attr")
		case 1:
			return fmt.Sprint(rapid.IntRange(30, 37).Draw(t, "fg16"))
		case 2:
			return fmt.Sprint(rapid.IntRange(40, 47).Draw(t, "bg16"))
		case 3:
			return fmt.Sprint(rapid.SampledFrom([]int{90, 97, 100, 107}).Draw(t, "bright"))
		case 4:
			return fmt.Sprintf("%d;5;%d", rapid.SampledFrom([]int{38, 48}).Draw(t, "ext"), rapid.IntRange(0, 255).Draw(t, "c256"))
		default:
			return fmt.Sprintf("%d;2;%d;%d;%d", rapid.SampledFrom([]int{38, 48}).Draw(t, "ext"),
				rapid.IntRange(0, 255).Draw(t, "r"), rapid.IntRange(0, 255).Draw(t, "g"), rapid.IntRange(0, 255).Draw(t, "b"))
		}
	})
	return rapid.Custom(func(t *rapid.T) string {
		if rapid.IntRange(0, 4).Draw(t, "reset") == 0 {
			return rapid.SampledFrom([]string{"\x1b[m", "\x1b[0m"}).Draw(t, "resetForm")
		}
		ps := rapid.SliceOfN(param, 1, 3).Draw(t, "params")
		return "\x1b[" + strings.Join(ps, ";") + "m"
	})
}

// BgSGR is a background-only SGR in one of the forms the renderer
// substitutes: basic, bright, 256 or 24-bit.
func BgSGR() *rapid.Generator[string] {
	return rapid.Custom(func(t *rapid.T) string {
		switch rapid.IntRange(0, 2).Draw(t, "bgKind") {
		case 0:
			return fmt.Sprintf("\x1b[%dm", rapid.SampledFrom([]int{40, 41, 47, 100, 107}).Draw(t, "bg16"))
		case 1:
			return fmt.Sprintf("\x1b[48;5;%dm", rapid.IntRange(0, 255).Draw(t, "bg256"))
		default:
			return fmt.Sprintf("\x1b[48;2;%d;%d;%dm", rapid.IntRange(0, 255).Draw(t, "r"), rapid.IntRange(0, 255).Draw(t, "g"), rapid.IntRange(0, 255).Draw(t, "b"))
		}
	})
}

// OSC8 is an OSC 8 hyperlink opener or closer, as lipgloss emits them.
func OSC8() *rapid.Generator[string] {
	return rapid.Custom(func(t *rapid.T) string {
		if rapid.Bool().Draw(t, "close") {
			return "\x1b]8;;\x1b\\"
		}
		return "\x1b]8;;https://example.com/" + rapid.StringMatching(`[a-z0-9]{0,8}`).Draw(t, "path") + "\x1b\\"
	})
}

// StyledText is PlainText with SGR and OSC 8 escapes spliced in between
// and inside words. ansi.Strip of the result is the plain text drawn.
func StyledText() *rapid.Generator[string] {
	return StyledTextWith(SGR())
}

// StyledTextWith is StyledText drawing its SGR escapes from sgr, for a
// test that needs particular sequences (say, a given background) to
// occur.
func StyledTextWith(sgr *rapid.Generator[string]) *rapid.Generator[string] {
	return rapid.Custom(func(t *rapid.T) string {
		n := rapid.IntRange(0, 24).Draw(t, "pieces")
		var b strings.Builder
		for range n {
			switch rapid.IntRange(0, 5).Draw(t, "piece") {
			case 0, 1:
				b.WriteString(sgr.Draw(t, "sgr"))
			case 2:
				b.WriteString(OSC8().Draw(t, "osc8"))
			case 3:
				b.WriteString(rapid.SampledFrom([]string{" ", "  ", "\n"}).Draw(t, "space"))
			default:
				b.WriteString(Word().Draw(t, "word"))
			}
		}
		return b.String()
	})
}

// ID is a Slack-shaped ID with the given prefix ("U", "C", "D", "S",
// "T", "B", "W"): the prefix and 8 to 10 uppercase letters and digits.
func ID(prefix string) *rapid.Generator[string] {
	return rapid.Custom(func(t *rapid.T) string {
		return prefix + rapid.StringMatching(`[A-Z0-9]{8,10}`).Draw(t, "id")
	})
}

// TS is a Slack message timestamp: whole seconds in a plausible range, a
// dot and six digits.
func TS() *rapid.Generator[string] {
	return rapid.Custom(func(t *rapid.T) string {
		return fmt.Sprintf("%d.%06d", rapid.Int64Range(1_000_000_000, 2_000_000_000).Draw(t, "sec"), rapid.IntRange(0, 999_999).Draw(t, "micro"))
	})
}

// SlackText is message text in Slack's wire form: words mixed with user,
// channel, usergroup and broadcast mentions (bare and pipe-labelled),
// links, emphasis markers, inline and fenced code, blockquotes, entities
// and emoji shortcodes. users and channels are the IDs mentions draw
// from, so a test can make some of them resolvable.
func SlackText(users, channels []string) *rapid.Generator[string] {
	return rapid.Custom(func(t *rapid.T) string {
		user := func() string {
			if len(users) > 0 && rapid.Bool().Draw(t, "knownUser") {
				return rapid.SampledFrom(users).Draw(t, "user")
			}
			return ID("U").Draw(t, "user")
		}
		channel := func() string {
			if len(channels) > 0 && rapid.Bool().Draw(t, "knownChannel") {
				return rapid.SampledFrom(channels).Draw(t, "channel")
			}
			return ID("C").Draw(t, "channel")
		}
		n := rapid.IntRange(0, 16).Draw(t, "pieces")
		var b strings.Builder
		for i := range n {
			if i > 0 {
				b.WriteString(rapid.SampledFrom([]string{" ", " ", "\n"}).Draw(t, "sep"))
			}
			switch rapid.IntRange(0, 14).Draw(t, "piece") {
			case 0:
				b.WriteString("<@" + user() + ">")
			case 1:
				b.WriteString("<@" + user() + "|" + Word().Draw(t, "label") + ">")
			case 2:
				b.WriteString("<#" + channel() + ">")
			case 3:
				b.WriteString("<#" + channel() + "|" + rapid.StringMatching(`[a-z0-9-]{1,12}`).Draw(t, "chname") + ">")
			case 4:
				b.WriteString(rapid.SampledFrom([]string{"<!here>", "<!channel>", "<!everyone>", "<!here|@here>"}).Draw(t, "broadcast"))
			case 5:
				b.WriteString("<!subteam^" + ID("S").Draw(t, "group") + rapid.SampledFrom([]string{"", "|@eng"}).Draw(t, "glabel") + ">")
			case 6:
				url := "https://example.com/" + rapid.StringMatching(`[a-z0-9/]{0,10}`).Draw(t, "path")
				b.WriteString(rapid.SampledFrom([]string{"<" + url + ">", "<" + url + "|" + Word().Draw(t, "ltext") + ">", url}).Draw(t, "link"))
			case 7:
				m := rapid.SampledFrom([]string{"*", "_", "~", "`"}).Draw(t, "mark")
				b.WriteString(m + Word().Draw(t, "inner") + m)
			case 8:
				b.WriteString("```" + rapid.SampledFrom([]string{"", "\n"}).Draw(t, "fenceNL") + PlainText().Draw(t, "code") + "```")
			case 9:
				b.WriteString(rapid.SampledFrom([]string{"&gt; ", "> "}).Draw(t, "quote") + Word().Draw(t, "quoted"))
			case 10:
				b.WriteString(rapid.SampledFrom([]string{"&amp;", "&lt;", "&gt;"}).Draw(t, "entity"))
			case 11:
				b.WriteString(":" + rapid.SampledFrom([]string{"tada", "heart", "+1", "thumbsup::skin-tone-3", "no_such_emoji"}).Draw(t, "emoji") + ":")
			case 12:
				b.WriteString("<!date^" + fmt.Sprint(rapid.Int64Range(0, 2_000_000_000).Draw(t, "date")) + "^{date_short} at {time}|fallback>")
			default:
				b.WriteString(Word().Draw(t, "word"))
			}
		}
		return b.String()
	})
}
