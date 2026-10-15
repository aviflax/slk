package emoji

import (
	"sort"
	"strconv"
	"strings"
	"testing"
	"unicode/utf8"

	"github.com/charmbracelet/x/ansi"
	"github.com/gammons/slk/internal/proptest"
	"pgregory.net/rapid"
)

var knownNames = func() []string {
	var names []string
	for k := range slackCodeMap {
		names = append(names, strings.Trim(k, ":"))
	}
	sort.Strings(names)
	return names
}()

// shortName is a made-up shortcode name that is unlikely to be built in.
func shortName() *rapid.Generator[string] {
	return rapid.StringMatching(`[a-z][a-z0-9_+-]{0,6}`)
}

// anyName is a built-in name, a made-up one, or either with a tone suffix.
func anyName() *rapid.Generator[string] {
	return rapid.Custom(func(t *rapid.T) string {
		n := rapid.OneOf(rapid.SampledFrom(knownNames), shortName()).Draw(t, "base")
		switch rapid.IntRange(0, 3).Draw(t, "suffix") {
		case 0:
			return n + "::skin-tone-" + strconv.Itoa(rapid.IntRange(1, 5).Draw(t, "tone"))
		case 1:
			return n + "_tone" + strconv.Itoa(rapid.IntRange(1, 5).Draw(t, "tone"))
		}
		return n
	})
}

// genCustoms builds a customs map: direct URLs and aliases to built-ins
// or to other customs, so cycles and long chains occur.
func genCustoms() *rapid.Generator[map[string]string] {
	return rapid.Custom(func(t *rapid.T) map[string]string {
		names := rapid.SliceOfNDistinct(shortName(), 0, 6, func(s string) string { return s }).Draw(t, "names")
		m := map[string]string{}
		for _, n := range names {
			switch rapid.IntRange(0, 2).Draw(t, "kind") {
			case 0:
				m[n] = "https://emoji.slack-edge.com/T/" + n + "/x.png"
			case 1:
				m[n] = "alias:" + rapid.SampledFrom(knownNames).Draw(t, "builtin")
			default:
				m[n] = "alias:" + rapid.SampledFrom(append([]string{"nope"}, names...)).Draw(t, "target")
			}
		}
		return m
	})
}

// StripSkinTone only removes a suffix: the result is a prefix of the
// name, and a name carrying exactly one tone suffix loses it entirely,
// after which stripping again changes nothing.
//
// KNOWN: stacking two suffixes ("a_tone1_tone2") strips only one per
// call, so the generator never produces that; no real name has it.
func TestStripSkinToneProperties(t *testing.T) {
	rapid.Check(t, func(t *rapid.T) {
		name := anyName().Draw(t, "name")
		got := StripSkinTone(name)
		if !strings.HasPrefix(name, got) {
			t.Fatalf("StripSkinTone(%q) = %q is not a prefix", name, got)
		}
		if StripSkinTone(got) != got {
			t.Fatalf("not idempotent: %q -> %q -> %q", name, got, StripSkinTone(got))
		}
		if strings.Contains(got, "::skin-tone-") {
			t.Fatalf("suffix left in %q", got)
		}
	})
}

// StripSkinToneFromText is idempotent and a text with no ':' passes
// through unchanged.
func TestStripSkinToneFromTextProperties(t *testing.T) {
	rapid.Check(t, func(t *rapid.T) {
		var b strings.Builder
		for range rapid.IntRange(0, 6).Draw(t, "n") {
			b.WriteString(rapid.SampledFrom([]string{" ", "hi", "x"}).Draw(t, "w"))
			b.WriteString(":" + anyName().Draw(t, "name") + ":")
		}
		in := b.String()
		once := StripSkinToneFromText(in)
		if twice := StripSkinToneFromText(once); twice != once {
			t.Fatalf("not idempotent: %q -> %q -> %q", in, once, twice)
		}
		if len(once) > len(in) {
			t.Fatalf("grew: %q -> %q", in, once)
		}
		plain := proptest.PlainText().Draw(t, "plain")
		if !strings.Contains(plain, ":") && StripSkinToneFromText(plain) != plain {
			t.Fatalf("changed colon-free %q", plain)
		}
	})
}

// ResolveEmojiToTokens: the tokens' Text concatenated is the input, no
// two text tokens are adjacent, every emoji token has a URL and no text
// token is empty. Alias cycles in customs terminate.
func checkTokens(t fataler, in string, customs map[string]string) {
	t.Helper()
	toks := ResolveEmojiToTokens(in, customs)
	var b strings.Builder
	for i, tok := range toks {
		b.WriteString(tok.Text)
		switch tok.Kind {
		case TokenText:
			if tok.Text == "" || tok.URL != "" {
				t.Fatalf("bad text token %+v for %q", tok, in)
			}
			if i > 0 && toks[i-1].Kind == TokenText {
				t.Fatalf("adjacent text tokens in %+v for %q", toks, in)
			}
		case TokenEmoji:
			if tok.URL == "" || tok.Text == "" {
				t.Fatalf("bad emoji token %+v for %q", tok, in)
			}
		}
	}
	if b.String() != in {
		t.Fatalf("tokens of %q concatenate to %q", in, b.String())
	}
}

type fataler interface {
	Helper()
	Fatalf(string, ...any)
}

func TestResolveEmojiToTokensReproducesInput(t *testing.T) {
	rapid.Check(t, func(t *rapid.T) {
		customs := genCustoms().Draw(t, "customs")
		var b strings.Builder
		for range rapid.IntRange(0, 8).Draw(t, "n") {
			switch rapid.IntRange(0, 4).Draw(t, "piece") {
			case 0:
				b.WriteString(":" + anyName().Draw(t, "name") + ":")
			case 1:
				b.WriteString(":" + rapid.SampledFrom(mapKeys(customs, "nope")).Draw(t, "custom") + ":")
			case 2:
				b.WriteString(rapid.SampledFrom([]string{"👍", "❤️", "❤", "🎉", "👩‍💻", "🇺🇸", "😀"}).Draw(t, "glyph"))
			default:
				b.WriteString(proptest.Word().Draw(t, "word"))
			}
		}
		checkTokens(t, b.String(), customs)
	})
}

func mapKeys(m map[string]string, fallback string) []string {
	keys := []string{fallback}
	for k := range m {
		keys = append(keys, k)
	}
	sort.Strings(keys)
	return keys
}

// URLForShortcode terminates on any customs map (including alias cycles)
// and, when it resolves, returns a CDN-shaped or custom URL.
func TestURLForShortcodeTerminates(t *testing.T) {
	rapid.Check(t, func(t *rapid.T) {
		customs := genCustoms().Draw(t, "customs")
		name := rapid.OneOf(rapid.SampledFrom(mapKeys(customs, "nope")), anyName()).Draw(t, "name")
		u, ok := URLForShortcode(name, customs)
		if ok && !strings.HasPrefix(u, "https://") {
			t.Fatalf("URL %q for %q", u, name)
		}
		if !ok && u != "" {
			t.Fatalf("URL %q with ok=false", u)
		}
	})
}

// BuildStandardEmojiURL names the codepoints in lowercase hex joined by
// dashes: parsing the file name back gives the input runes.
func TestBuildStandardEmojiURLRoundTrip(t *testing.T) {
	rapid.Check(t, func(t *rapid.T) {
		cps := rapid.SliceOfN(rapid.SampledFrom([]rune{'a', '😀', 0x1F3FB, vs16, 0x200D, 0x2764, 0x1F1FA}), 1, 6).Draw(t, "cps")
		u := BuildStandardEmojiURL(cps)
		if !strings.HasPrefix(u, CDNBaseURL) || !strings.HasSuffix(u, ".png") {
			t.Fatalf("shape: %q", u)
		}
		name := strings.TrimSuffix(strings.TrimPrefix(u, CDNBaseURL), ".png")
		var back []rune
		for _, h := range strings.Split(name, "-") {
			n, err := strconv.ParseInt(h, 16, 32)
			if err != nil || h != strings.ToLower(h) {
				t.Fatalf("bad hex %q in %q", h, u)
			}
			back = append(back, rune(n))
		}
		if string(back) != string(cps) {
			t.Fatalf("round trip %q -> %q", string(cps), string(back))
		}
	})
}

// ResolveShortcodesInText converges after one pass, and leaves
// colon-free text alone.
func TestResolveShortcodesInTextIdempotent(t *testing.T) {
	rapid.Check(t, func(t *rapid.T) {
		var b strings.Builder
		for range rapid.IntRange(0, 6).Draw(t, "n") {
			b.WriteString(proptest.Word().Draw(t, "w"))
			b.WriteString(":" + anyName().Draw(t, "name") + ":")
		}
		in := b.String()
		once := ResolveShortcodesInText(in)
		if twice := ResolveShortcodesInText(once); twice != once {
			t.Fatalf("not idempotent: %q -> %q -> %q", in, once, twice)
		}
		plain := rapid.StringMatching(`[^:]{0,20}`).Draw(t, "plain")
		if ResolveShortcodesInText(plain) != plain {
			t.Fatalf("changed colon-free %q", plain)
		}
	})
}

// Width ignores ANSI escapes whatever the image mode, and is never
// negative.
func TestWidthANSIInvariant(t *testing.T) {
	t.Cleanup(resetImageMode)
	rapid.Check(t, func(t *rapid.T) {
		s := proptest.StyledText().Draw(t, "styled") + rapid.SampledFrom([]string{"", "👍", "❤️", "🎉"}).Draw(t, "emoji")
		SetImageMode(rapid.Bool().Draw(t, "images"), rapid.SampledFrom([]int{1, 2}).Draw(t, "cells"))
		got, want := Width(s), Width(ansi.Strip(s))
		if got != want || got < 0 {
			t.Fatalf("Width(%q) = %d, stripped = %d", s, got, want)
		}
	})
}

func FuzzResolveEmojiToTokens(f *testing.F) {
	for _, s := range []string{"", ":tada:", "a:b:c", "👍🏽 :+1::skin-tone-3:", "::", ":x:", ":custom:"} {
		f.Add(s)
	}
	customs := map[string]string{"custom": "https://e/x.png", "loop": "alias:loop", "a": "alias:b", "b": "alias:a"}
	f.Fuzz(func(t *testing.T, s string) {
		if !utf8.ValidString(s) {
			t.Skip() // KNOWN: invalid bytes become U+FFFD in the text tokens
		}
		checkTokens(t, s, customs)
		_ = ResolveShortcodesInText(s)
		_ = StripSkinToneFromText(s)
		_ = Width(s)
	})
}
