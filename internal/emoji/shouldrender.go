package emoji

import "strings"

// ShouldRenderUnicode reports whether the resolved Unicode form of an
// emoji is safe to render as a glyph. Returns true iff the string
// contains exactly one codepoint. Any multi-codepoint sequence —
// including base+VS16 (`❤️`, `⚠️`, `🌩️`), ZWJ sequences,
// regional-indicator flag pairs, and skin-tone modifiers — returns
// false.
//
// VS16 (U+FE0F) was previously treated as a "safe composition" but
// field experience disagreed: many terminal+font combos render
// VS16-anchored emoji from the legacy blocks (U+2600-2BFF and
// U+1F300-1F5FF) at a different visual width than lipgloss reports,
// breaking border alignment. This predicate therefore stays strict
// about the pair; display code should call TextGlyph, which drops the
// selector so `:heart:`, `:warning:` and friends still show a glyph.
//
// Trailing whitespace is ignored — kyokomi/emoji's Sprint appends a
// trailing space after each resolved emoji and callers occasionally
// pass that form. Empty strings (after trim) return false.
func ShouldRenderUnicode(s string) bool {
	s = strings.TrimRight(s, " \t")
	if s == "" {
		return false
	}
	runes := []rune(s)
	return len(runes) == 1
}

// TextGlyph returns the form of a resolved emoji that is safe to draw in
// a fixed-width row, and whether there is one.
//
// A single codepoint is returned as is. A base codepoint followed by VS16
// (heart, warning sign, sun) comes back as the bare base. lipgloss
// measures the pair as 2 cells and terminals disagree (some draw 1, some
// 2, depending on whether they honour the selector), but the bare base is
// one codepoint whose width every terminal and lipgloss share. The cost
// is the colourful presentation: the glyph may draw in text style. A base
// that is wide by default (emoji presentation without VS16) stays wide.
//
// Everything else (ZWJ sequences, flags, skin tones, keycaps) returns
// "", false, and callers keep the :shortcode: text. Trailing whitespace
// is ignored, as in ShouldRenderUnicode.
func TextGlyph(s string) (string, bool) {
	s = strings.TrimRight(s, " \t")
	runes := []rune(s)
	switch {
	case len(runes) == 1 && runes[0] != vs16:
		return s, true
	case len(runes) == 2 && runes[1] == vs16 && runes[0] != vs16:
		return string(runes[0]), true
	}
	return "", false
}
