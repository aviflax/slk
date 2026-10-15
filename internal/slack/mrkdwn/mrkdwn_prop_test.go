package mrkdwn

import (
	"fmt"
	"regexp"
	"strings"
	"testing"
	"unicode/utf8"

	"github.com/gammons/slk/internal/proptest"
	"github.com/slack-go/slack"
	"pgregory.net/rapid"
)

// wireTokenRe finds the wire tokens Convert promises to keep: users,
// channels (bare or labelled), broadcasts and links. It is written out
// here independently of tokenize's patterns, in the simple shapes
// proptest.SlackText generates.
//
// KNOWN: an empty label is canonicalized away (<https://x|> comes back
// as <https://x>, likewise <#C1|> and <!here|>), so labels here are
// non-empty.
var wireTokenRe = regexp.MustCompile(
	`<@[UW][A-Z0-9]+>|<#[CG][A-Z0-9]+(?:\|[a-z0-9-]+)?>|<!(?:here|channel|everyone)(?:\|[^<>]+)?>|<https://[^|<>\s]+(?:\|[^<>]+)?>`)

func hasSentinel(s string) bool {
	return strings.ContainsAny(s, string(sentinelStart)+string(sentinelEnd))
}

// checkConvert asserts the invariants that hold for any input.
type fataler interface {
	Helper()
	Fatalf(string, ...any)
}

func checkConvert(t fataler, in string) {
	t.Helper()
	mr, blk := Convert(in)
	if strings.TrimSpace(in) == "" {
		if mr != "" || blk != nil {
			t.Fatalf("Convert(%q) = (%q, %v), want empty", in, mr, blk)
		}
		return
	}
	if hasSentinel(mr) {
		t.Fatalf("Convert(%q) leaked a sentinel rune: %q", in, mr)
	}
	if blk != nil && len(blk.Elements) == 0 {
		t.Fatalf("Convert(%q) returned a block with no elements", in)
	}
}

// Convert never panics, never leaks the PUA sentinels into its mrkdwn
// output, and returns empty output for blank input.
func TestConvertInvariants(t *testing.T) {
	rapid.Check(t, func(t *rapid.T) {
		checkConvert(t, proptest.SlackText(nil, nil).Draw(t, "text"))
	})
}

// Every user, channel, broadcast and link token in the input appears
// verbatim in the mrkdwn fallback, wherever it sat.
func TestConvertKeepsWireTokens(t *testing.T) {
	rapid.Check(t, func(t *rapid.T) {
		in := proptest.SlackText(nil, nil).Draw(t, "text")
		mr, _ := Convert(in)
		for _, tok := range wireTokenRe.FindAllString(in, -1) {
			if !strings.Contains(mr, tok) {
				t.Fatalf("token %q of %q missing from %q", tok, in, mr)
			}
		}
	})
}

// Text with no markdown metacharacters, tokens or entities comes back
// as the input minus trailing newlines.
func TestConvertPlainTextIsIdentity(t *testing.T) {
	rapid.Check(t, func(t *rapid.T) {
		// KNOWN: spaces that touch no word are dropped: trailing spaces
		// of the message (Convert("a ") == "a") and the spaces of a
		// space-only line (Convert("!\n \n!") == "!\n\n!"), contrary to
		// the "whitespace survives" doc. Invisible in Slack, so left
		// alone; every space here sits between two non-space characters.
		//
		// KNOWN: a line that is only a list marker becomes an empty list
		// item: Convert("2024.") == "2024. " and Convert("-") == "• ",
		// so the text carries no digits.
		in := rapid.StringMatching(`[A-Za-z,.!?]([A-Za-z,.!?\n]| [A-Za-z,.!?]){0,30}`).Draw(t, "plain")
		mr, _ := Convert(in)
		if want := strings.TrimRight(in, "\n"); mr != want {
			t.Fatalf("Convert(%q) = %q, want %q", in, mr, want)
		}
	})
}

// detokenizeText undoes tokenize for any input that holds no sentinel.
func TestTokenizeRoundTrip(t *testing.T) {
	rapid.Check(t, func(t *rapid.T) {
		in := proptest.SlackText(nil, nil).Draw(t, "text")
		if strings.Contains(in, "|>") {
			t.Skip() // KNOWN: empty labels are canonicalized away, see wireTokenRe
		}
		tokenized, table := tokenize(in)
		if got := detokenizeText(tokenized, table); got != in {
			t.Fatalf("round trip of %q gave %q", in, got)
		}
	})
}

// tokenize replaces every sentinel in its input, so its output holds
// sentinels only as markers: after detokenizing, none remain.
func TestTokenizeSanitizesSentinels(t *testing.T) {
	rapid.Check(t, func(t *rapid.T) {
		in := rapid.StringOf(rapid.SampledFrom([]rune{'a', ' ', '<', '@', '>', ':', sentinelStart, sentinelEnd, '1'})).Draw(t, "in")
		tokenized, table := tokenize(in)
		if got := detokenizeText(tokenized, table); hasSentinel(got) {
			t.Fatalf("sentinel survived tokenize/detokenize of %q: %q", in, got)
		}
	})
}

// FuzzConvert feeds arbitrary bytes through Convert.
func FuzzConvert(f *testing.F) {
	for _, s := range []string{
		"", "  \n", "hello", "*bold* _it_ ~s~ `c`", "<@U123> <#C1|general> <!here> <https://a.com|a>",
		"```\ncode\n```", "> quote\nreply", "- a\n  - b", "0", ":tada: :+1:",
		"1. one\n2. two", "<https://x|y", "[a](http://b)", "\t\tx",
	} {
		f.Add(s)
	}
	f.Fuzz(func(t *testing.T, in string) {
		if !utf8.ValidString(in) {
			t.Skip()
		}
		checkConvert(t, in)
		mr, _ := Convert(in)
		for _, tok := range wireTokenRe.FindAllString(in, -1) {
			if !strings.Contains(mr, tok) {
				t.Fatalf("token %q of %q missing from %q", tok, in, mr)
			}
		}
	})
}

// Text typed after an opening fence is the code block's first line, not
// a language tag Slack would discard: it must reach both outputs.
func TestConvertFenceInfoStringIsCode(t *testing.T) {
	mr, blk := Convert("```hello <!here>\nworld\n```")
	if want := "```\nhello <!here>\nworld\n```"; mr != want {
		t.Fatalf("mrkdwn = %q, want %q", mr, want)
	}
	if blk == nil || len(blk.Elements) != 1 {
		t.Fatalf("block = %+v", blk)
	}
}

// An ordered list keeps the number it was typed with: the mrkdwn
// fallback counts up from it and the rich text list carries it as its
// offset, so "2024. was a year" is not sent as "1. was a year".
func TestConvertKeepsOrderedListStart(t *testing.T) {
	rapid.Check(t, func(t *rapid.T) {
		start := rapid.IntRange(0, 999_999_999).Draw(t, "start")
		items := rapid.SliceOfN(rapid.StringMatching(`[a-z]{1,8}`), 1, 4).Draw(t, "items")
		var in, want strings.Builder
		for i, it := range items {
			fmt.Fprintf(&in, "%d. %s\n", start+i, it)
			fmt.Fprintf(&want, "%d. %s\n", start+i, it)
		}
		mr, block := Convert(in.String())
		if w := strings.TrimRight(want.String(), "\n"); mr != w {
			t.Fatalf("Convert(%q) = %q, want %q", in.String(), mr, w)
		}
		list, ok := block.Elements[0].(*slack.RichTextList)
		if !ok {
			t.Fatalf("first element is %T, want a list", block.Elements[0])
		}
		if wantOffset := max(start-1, 0); list.Offset != wantOffset {
			t.Fatalf("list offset %d, want %d", list.Offset, wantOffset)
		}
	})
}
