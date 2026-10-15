package styles

import (
	"image/color"
	"math"
	"testing"

	"github.com/gammons/slk/internal/proptest"
	"pgregory.net/rapid"
)

// opaqueColor is an arbitrary fully opaque 24-bit colour.
func opaqueColor() *rapid.Generator[color.Color] {
	return rapid.Custom(func(t *rapid.T) color.Color {
		return color.RGBA{
			R: uint8(rapid.IntRange(0, 255).Draw(t, "r")),
			G: uint8(rapid.IntRange(0, 255).Draw(t, "g")),
			B: uint8(rapid.IntRange(0, 255).Draw(t, "b")),
			A: 0xFF,
		}
	})
}

func between(x, a, b uint8) bool {
	lo, hi := min(a, b), max(a, b)
	return x >= lo && x <= hi
}

// mixColors clamps: any alpha at or below 0 is bg, at or above 1 is fg,
// whatever the colours are.
func TestMixColorsClampsAlpha(t *testing.T) {
	rapid.Check(t, func(t *rapid.T) {
		fg := opaqueColor().Draw(t, "fg")
		bg := opaqueColor().Draw(t, "bg")
		lo := -rapid.Float64Range(0, 1e9).Draw(t, "below")
		hi := 1 + rapid.Float64Range(0, 1e9).Draw(t, "above")
		if got := mixColors(fg, bg, lo); got != bg {
			t.Fatalf("alpha %v gave %v, want bg %v", lo, got, bg)
		}
		if got := mixColors(fg, bg, hi); got != fg {
			t.Fatalf("alpha %v gave %v, want fg %v", hi, got, fg)
		}
	})
}

// Every channel of the mix lies between the fg and bg channels, and the
// mix moves monotonically from bg towards fg as alpha grows.
func TestMixColorsChannelsBetween(t *testing.T) {
	rapid.Check(t, func(t *rapid.T) {
		fg := opaqueColor().Draw(t, "fg")
		bg := opaqueColor().Draw(t, "bg")
		a1 := rapid.Float64Range(0, 1).Draw(t, "a1")
		a2 := rapid.Float64Range(a1, 1).Draw(t, "a2")
		fr, fgc, fb := rgb(fg)
		br, bgc, bb := rgb(bg)
		r1, g1, b1 := rgb(mixColors(fg, bg, a1))
		r2, g2, b2 := rgb(mixColors(fg, bg, a2))
		for _, c := range [][3]uint8{{r1, fr, br}, {g1, fgc, bgc}, {b1, fb, bb}, {r2, fr, br}, {g2, fgc, bgc}, {b2, fb, bb}} {
			if !between(c[0], c[1], c[2]) {
				t.Fatalf("channel %d not between fg %d and bg %d", c[0], c[1], c[2])
			}
		}
		// Distance to bg never shrinks as fg's share grows.
		dist := func(x, b uint8) int { return int(math.Abs(float64(int(x) - int(b)))) }
		if dist(r2, br) < dist(r1, br) || dist(g2, bgc) < dist(g1, bgc) || dist(b2, bb) < dist(b1, bb) {
			t.Fatalf("mix at %v is closer to bg than at %v", a2, a1)
		}
	})
}

// Mixing a colour with itself gives that colour, at any alpha.
func TestMixColorsSameColourIsFixed(t *testing.T) {
	rapid.Check(t, func(t *rapid.T) {
		c := opaqueColor().Draw(t, "c")
		a := rapid.Float64Range(-1, 2).Draw(t, "alpha")
		gr, gg, gb := rgb(mixColors(c, c, a))
		wr, wg, wb := rgb(c)
		if gr != wr || gg != wg || gb != wb {
			t.Fatalf("mix(c, c, %v) = #%02X%02X%02X, want #%02X%02X%02X", a, gr, gg, gb, wr, wg, wb)
		}
	})
}

// UserColor is a function of the ID alone, every non-empty ID gets a
// palette colour, and the empty ID gets Primary.
func TestUserColorDeterministicAndInPalette(t *testing.T) {
	rapid.Check(t, func(t *rapid.T) {
		id := rapid.OneOf(proptest.ID("U"), proptest.ID("B"), rapid.String()).Draw(t, "id")
		c := UserColor(id)
		if c != UserColor(id) {
			t.Fatalf("UserColor(%q) not deterministic", id)
		}
		if id == "" {
			if c != Primary {
				t.Fatalf("UserColor(\"\") = %v, want Primary", c)
			}
			return
		}
		for _, p := range UserColorPalette {
			if p == c {
				return
			}
		}
		t.Fatalf("UserColor(%q) = %v, not in the palette", id, c)
	})
}
