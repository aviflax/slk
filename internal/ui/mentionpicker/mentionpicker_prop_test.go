package mentionpicker

import (
	"strings"
	"testing"

	"github.com/gammons/slk/internal/text"
	"pgregory.net/rapid"
)

var mentionRunes = []rune{'a', 'b', 'c', 'e', '-', '_', '.', ' ', 'é', '漢'}

func mname() *rapid.Generator[string] {
	return rapid.StringOfN(rapid.SampledFrom(mentionRunes), 0, 10, -1)
}

// matchName: the empty query is a prefix match for anything; a folded
// prefix of the name is rankPrefix; a name never matches better than its
// reference rank (prefix beats word start beats squashed beats none),
// with the reference computed independently from the folded strings.
func TestMatchNameAgreesWithReference(t *testing.T) {
	rapid.Check(t, func(t *rapid.T) {
		name := mname().Draw(t, "name")
		query := text.Fold(mname().Draw(t, "query"))
		sq := squash(query)
		got := matchName(name, query, sq)

		n := text.Fold(name)
		want := rankNone
		switch {
		case query == "" || strings.HasPrefix(n, query):
			want = rankPrefix
		default:
			for i := 0; i < len(n); i++ {
				if isSeparator(n[i]) && strings.HasPrefix(n[i+1:], query) {
					want = rankWord
					break
				}
			}
			if want == rankNone && sq != "" && strings.HasPrefix(squash(n), sq) {
				want = rankSquashed
			}
		}
		if got != want {
			t.Fatalf("matchName(%q, %q) = %v, want %v", name, query, got, want)
		}
		if query == "" && got != rankPrefix {
			t.Fatalf("empty query ranked %v", got)
		}
	})
}

// Making the query a longer prefix of the same name can only keep or
// worsen the rank, never improve it (rank is monotone in the prefix).
func TestMatchNameMonotoneInQueryLength(t *testing.T) {
	rapid.Check(t, func(t *rapid.T) {
		name := text.Fold(mname().Draw(t, "name"))
		q := []rune(name)
		k := rapid.IntRange(0, len(q)).Draw(t, "k")
		j := rapid.IntRange(0, k).Draw(t, "j")
		short, long := string(q[:j]), string(q[:k])
		rs := matchName(name, short, squash(short))
		rl := matchName(name, long, squash(long))
		// Both are prefixes of the folded name, so both are rankPrefix.
		if rs != rankPrefix || rl != rankPrefix {
			t.Fatalf("prefixes of %q ranked %v and %v", name, rs, rl)
		}
	})
}

// squash removes exactly the separators: output has none, and the visible
// non-separator bytes are kept in order.
func TestSquash(t *testing.T) {
	rapid.Check(t, func(t *rapid.T) {
		s := mname().Draw(t, "s")
		got := squash(s)
		if strings.ContainsAny(got, " -_.") {
			t.Fatalf("squash(%q) = %q still has separators", s, got)
		}
		want := strings.NewReplacer(" ", "", "-", "", "_", "", ".", "").Replace(s)
		if got != want {
			t.Fatalf("squash(%q) = %q, want %q", s, got, want)
		}
	})
}
