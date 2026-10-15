package channelfinder

import (
	"strings"
	"testing"

	"pgregory.net/rapid"
)

var nameRunes = []rune{'a', 'b', 'c', 'd', '-', '_', '.', ' ', 'é', '漢', '字', '😀'}

func nameGen() *rapid.Generator[string] {
	return rapid.StringOfN(rapid.SampledFrom(nameRunes), 0, 12, -1)
}

// subsequenceScore reports ok exactly when q is a rune subsequence of
// name (checked against an independent greedy reference), and the score
// is never negative.
func TestSubsequenceScoreOkMatchesReference(t *testing.T) {
	rapid.Check(t, func(t *rapid.T) {
		name, q := nameGen().Draw(t, "name"), nameGen().Draw(t, "q")
		score, ok := subsequenceScore(name, q)
		rest := []rune(q)
		for _, r := range name {
			if len(rest) > 0 && r == rest[0] {
				rest = rest[1:]
			}
		}
		if want := len(rest) == 0; ok != want {
			t.Fatalf("subsequenceScore(%q, %q) ok = %v, want %v", name, q, ok, want)
		}
		if score < 0 {
			t.Fatalf("negative score %d", score)
		}
		if q == "" && (score != 0 || !ok) {
			t.Fatalf("empty query gave %d, %v", score, ok)
		}
	})
}

// The score depends on which runes match and where, in runes, never on
// how many bytes the runes between the matches take: a gap of the same
// number of runes scores the same whether it is ASCII or multibyte, and
// so does a query that is itself multibyte.
func TestSubsequenceScoreIgnoresByteWidth(t *testing.T) {
	rapid.Check(t, func(t *rapid.T) {
		qr := rapid.SliceOfN(rapid.SampledFrom([]rune{'a', 'b', 'é', '漢'}), 2, 4).Draw(t, "q")
		gap := rapid.IntRange(0, 4).Draw(t, "gap")
		var narrow, wide strings.Builder
		for i, r := range qr {
			narrow.WriteRune(r)
			wide.WriteRune(r)
			if i == 0 {
				narrow.WriteString(strings.Repeat("x", gap))
				wide.WriteString(strings.Repeat("😀", gap))
			}
		}
		q := string(qr)
		sa, oka := subsequenceScore(narrow.String(), q)
		sb, okb := subsequenceScore(wide.String(), q)
		if !oka || !okb {
			t.Fatal("match not found")
		}
		if sa != sb {
			t.Fatalf("score depends on byte width: %q=%d, %q=%d (q=%q)", narrow.String(), sa, wide.String(), sb, q)
		}
	})
}

// A name holding q contiguously scores at least as well as one holding the
// same runes spread out by a rune of padding, same boundary position.
func TestSubsequenceScoreTighterIsBetter(t *testing.T) {
	rapid.Check(t, func(t *rapid.T) {
		a := rapid.SampledFrom([]rune{'a', 'b', 'c'}).Draw(t, "a")
		b := rapid.SampledFrom([]rune{'a', 'b', 'c'}).Draw(t, "b")
		pad := rapid.SampledFrom([]string{"x", "漢", "😀"}).Draw(t, "pad")
		q := string(a) + string(b)
		tight, _ := subsequenceScore(q, q)
		loose, ok := subsequenceScore(string(a)+pad+string(b), q)
		if !ok || loose > tight {
			t.Fatalf("loose %d beats tight %d for %q", loose, tight, q)
		}
	})
}
