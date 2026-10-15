package newmessagepicker

import (
	"strings"
	"testing"

	"pgregory.net/rapid"
)

var pickerRunes = []rune{'a', 'b', 'c', '-', '_', '.', ' ', 'é', '漢', '😀'}

func textGen() *rapid.Generator[string] {
	return rapid.StringOfN(rapid.SampledFrom(pickerRunes), 0, 10, -1)
}

// isSubsequence agrees with a greedy rune-by-rune reference, and the empty
// query matches everything.
func TestIsSubsequenceMatchesReference(t *testing.T) {
	rapid.Check(t, func(t *rapid.T) {
		s, q := textGen().Draw(t, "s"), textGen().Draw(t, "q")
		rest := []rune(q)
		for _, r := range s {
			if len(rest) > 0 && r == rest[0] {
				rest = rest[1:]
			}
		}
		if got, want := isSubsequence(s, q), len(rest) == 0; got != want {
			t.Fatalf("isSubsequence(%q, %q) = %v, want %v", s, q, got, want)
		}
	})
}

// Matching tiers nest: a prefix hit implies a substring hit implies a
// subsequence hit, so matchTier reports the best tier and never rejects
// something a weaker test would accept.
func TestMatchTierNesting(t *testing.T) {
	rapid.Check(t, func(t *rapid.T) {
		name, handle, q := textGen().Draw(t, "name"), textGen().Draw(t, "handle"), textGen().Draw(t, "q")
		u := User{DisplayName: name, Username: handle}
		tier, ok := matchTier(u, q)
		ln, lh := strings.ToLower(name), strings.ToLower(handle)
		switch {
		case strings.HasPrefix(ln, q) || strings.HasPrefix(lh, q):
			if !ok || tier != 0 {
				t.Fatalf("prefix hit gave tier %d ok %v", tier, ok)
			}
		case strings.Contains(ln, q) || strings.Contains(lh, q):
			if !ok || tier != 1 {
				t.Fatalf("substring hit gave tier %d ok %v", tier, ok)
			}
		case isSubsequence(ln, q) || isSubsequence(lh, q):
			if !ok || tier != 2 {
				t.Fatalf("subsequence hit gave tier %d ok %v", tier, ok)
			}
		default:
			if ok {
				t.Fatalf("no match but tier %d", tier)
			}
		}
	})
}
