package text

import (
	"testing"
	"unicode/utf8"

	"github.com/gammons/slk/internal/proptest"
	"pgregory.net/rapid"
)

// Fold of valid UTF-8 is valid UTF-8, and folding twice changes
// nothing.
func TestFoldIdempotentAndValid(t *testing.T) {
	rapid.Check(t, func(t *rapid.T) {
		s := proptest.PlainText().Draw(t, "s") + rapid.StringN(0, 12, -1).Draw(t, "any")
		if !utf8.ValidString(s) {
			s = string([]rune(s))
		}
		once := Fold(s)
		if !utf8.ValidString(once) {
			t.Fatalf("Fold(%q) = %q is not valid UTF-8", s, once)
		}
		if twice := Fold(once); twice != once {
			t.Fatalf("Fold not idempotent: %q -> %q -> %q", s, once, twice)
		}
	})
}

// Folding is what makes matching symmetric: a string always contains
// its own fold, and fold of a concatenation is the concatenation of
// folds up to combining-mark merges, so for marks-free ASCII it is exact.
func TestFoldASCIIConcat(t *testing.T) {
	rapid.Check(t, func(t *rapid.T) {
		a := rapid.StringMatching(`[A-Za-z0-9_ -]{0,10}`).Draw(t, "a")
		b := rapid.StringMatching(`[A-Za-z0-9_ -]{0,10}`).Draw(t, "b")
		if Fold(a+b) != Fold(a)+Fold(b) {
			t.Fatalf("Fold(%q+%q) != Fold(a)+Fold(b)", a, b)
		}
	})
}

// FuzzFoldProperties checks idempotence and UTF-8 validity on
// arbitrary valid input.
func FuzzFoldProperties(f *testing.F) {
	for _, in := range foldInputs {
		f.Add(in)
	}
	f.Fuzz(func(t *testing.T, s string) {
		if !utf8.ValidString(s) {
			t.Skip()
		}
		once := Fold(s)
		if !utf8.ValidString(once) {
			t.Fatalf("Fold(%q) = %q invalid UTF-8", s, once)
		}
		if twice := Fold(once); twice != once {
			t.Fatalf("Fold(%q) = %q, Fold again = %q", s, once, twice)
		}
	})
}
