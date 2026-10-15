package ansi

import (
	"strings"
	"testing"

	xansi "github.com/charmbracelet/x/ansi"
	"pgregory.net/rapid"

	"github.com/gammons/slk/internal/proptest"
)

// An empty style is the identity.
func TestReapplyEmptyStyleIsIdentity(t *testing.T) {
	rapid.Check(t, func(t *rapid.T) {
		s := proptest.StyledText().Draw(t, "s")
		if got := ReapplyAfterResets(s, ""); got != s {
			t.Fatalf("changed %q to %q", s, got)
		}
	})
}

// The visible text is untouched when the style is a pure SGR, and the
// style is inserted once per reset (short and explicit-zero forms).
func TestReapplyInsertsOncePerReset(t *testing.T) {
	rapid.Check(t, func(t *rapid.T) {
		s := proptest.StyledText().Draw(t, "s")
		style := proptest.BgSGR().Draw(t, "style")
		got := ReapplyAfterResets(s, style)
		if xansi.Strip(got) != xansi.Strip(s) {
			t.Fatalf("visible text changed: %q -> %q", s, got)
		}
		resets := strings.Count(s, "\x1b[m") + strings.Count(s, "\x1b[0m")
		if n := strings.Count(got, style) - strings.Count(s, style); n != resets {
			t.Fatalf("inserted %d times, %d resets in %q", n, resets, s)
		}
		if len(got) != len(s)+resets*len(style) {
			t.Fatalf("length %d, want %d", len(got), len(s)+resets*len(style))
		}
		// Every reset is now followed by the style.
		for _, reset := range []string{"\x1b[m", "\x1b[0m"} {
			if strings.Count(got, reset) != strings.Count(got, reset+style) {
				t.Fatalf("a %q is not followed by the style in %q", reset, got)
			}
		}
	})
}

// FuzzReapplyAfterResets: no panic on arbitrary bytes, and the result
// has one style per reset when the style cannot itself form a reset.
func FuzzReapplyAfterResets(f *testing.F) {
	f.Add("a\x1b[mb\x1b[0mc", "\x1b[48;5;1m")
	f.Add("\x1b[", "x")
	f.Add("", "\x1b[m")
	f.Fuzz(func(t *testing.T, text, style string) {
		got := ReapplyAfterResets(text, style)
		if style == "" && got != text {
			t.Fatal("empty style changed text")
		}
		if style != "" && strings.Contains(style, "\x1b") {
			return
		}
		resets := strings.Count(text, "\x1b[m") + strings.Count(text, "\x1b[0m")
		if len(got) != len(text)+resets*len(style) {
			t.Fatalf("length %d, want %d", len(got), len(text)+resets*len(style))
		}
	})
}
