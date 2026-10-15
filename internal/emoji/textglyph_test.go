package emoji

import "testing"

func TestTextGlyph(t *testing.T) {
	cases := []struct {
		name  string
		input string
		want  string
		ok    bool
	}{
		{"single emoji unchanged", "\U0001F525", "\U0001F525", true},
		{"trailing space trimmed", "\U0001F308 ", "\U0001F308", true},
		{"heart + VS16 drops the selector", "❤️", "❤", true},
		{"warning + VS16", "⚠️", "⚠", true},
		{"flag base + VS16", "\U0001F3F3️", "\U0001F3F3", true},
		{"VS16 with trailing space", "❤️ ", "❤", true},
		{"ZWJ sequence stays out", "\U0001F3F3️‍\U0001F308", "", false},
		{"regional indicator pair", "\U0001F1FA\U0001F1F8", "", false},
		{"skin tone", "\U0001F44D\U0001F3FD", "", false},
		{"keycap", "1️⃣", "", false},
		{"empty", "", "", false},
		{"lone VS16", "️", "", false},
	}
	for _, c := range cases {
		t.Run(c.name, func(t *testing.T) {
			got, ok := TextGlyph(c.input)
			if got != c.want || ok != c.ok {
				t.Errorf("TextGlyph(%q) = %q, %v; want %q, %v", c.input, got, ok, c.want, c.ok)
			}
		})
	}
}

// The glyph TextGlyph returns is one codepoint with no selector, so
// lipgloss and the terminal both use the base codepoint's own width
// whether or not the terminal honours VS16.
func TestTextGlyphIsASingleRune(t *testing.T) {
	for _, in := range []string{"❤️", "⚠️", "☀️", "✔️"} {
		g, ok := TextGlyph(in)
		if !ok || len([]rune(g)) != 1 {
			t.Errorf("TextGlyph(%q) = %q, %v; want a single rune", in, g, ok)
		}
	}
}

func TestResolveShortcodesInTextRendersVS16AsGlyph(t *testing.T) {
	if got, want := ResolveShortcodesInText(":heart:"), "❤ "; got != want {
		t.Errorf("got %q, want %q", got, want)
	}
	// Sequences that cannot be made safe keep the readable shortcode.
	if got := ResolveShortcodesInText(":man-woman-girl:"); got != ":man-woman-girl:" {
		t.Errorf("ZWJ shortcode = %q, want it left literal", got)
	}
}
