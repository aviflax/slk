package cache

import (
	"strings"
	"testing"

	"pgregory.net/rapid"
)

// splitQuoted parses a buildFTSQuery result back into its terms, failing
// on anything that is not exactly a space-separated list of `"..."*`
// tokens with doubled inner quotes.
type fataler interface{ Fatalf(string, ...any) }

func splitQuoted(t fataler, q string) []string {
	var terms []string
	i := 0
	for i < len(q) {
		if q[i] != '"' {
			t.Fatalf("expected opening quote at %d in %q", i, q)
		}
		i++
		var b strings.Builder
		closed := false
		for i < len(q) {
			if q[i] == '"' {
				if i+1 < len(q) && q[i+1] == '"' {
					b.WriteByte('"')
					i += 2
					continue
				}
				i++
				closed = true
				break
			}
			b.WriteByte(q[i])
			i++
		}
		if !closed {
			t.Fatalf("unbalanced quote in %q", q)
		}
		if i >= len(q) || q[i] != '*' {
			t.Fatalf("term not followed by * in %q", q)
		}
		i++
		terms = append(terms, b.String())
		if i < len(q) {
			if q[i] != ' ' {
				t.Fatalf("expected a space after term in %q", q)
			}
			i++
		}
	}
	return terms
}

func checkFTSQuery(t fataler, input string) {
	got := splitQuoted(t, buildFTSQuery(input))
	want := strings.Fields(input)
	if len(got) != len(want) {
		t.Fatalf("%d terms for %d fields of %q", len(got), len(want), input)
	}
	for i := range want {
		if got[i] != want[i] {
			t.Fatalf("term %d = %q, want %q (input %q)", i, got[i], want[i], input)
		}
	}
}

// buildFTSQuery emits exactly one quoted prefix term per whitespace
// field; parsing the output back yields the fields, so no input can
// unbalance a quote or smuggle an FTS5 operator outside one.
func TestBuildFTSQueryRoundTrip(t *testing.T) {
	rapid.Check(t, func(t *rapid.T) {
		words := rapid.SliceOf(rapid.StringMatching(`[a-zA-Z0-9"*():^+\-]{0,6}`)).Draw(t, "words")
		checkFTSQuery(t, strings.Join(words, rapid.SampledFrom([]string{" ", "  ", "\t", "\n"}).Draw(t, "sep")))
		checkFTSQuery(t, rapid.String().Draw(t, "any"))
	})
}

func FuzzBuildFTSQuery(f *testing.F) {
	for _, s := range []string{"", "foo bar", `a"b`, `"`, `""`, `" OR "`, "NEAR(a b)", "col:x", "-x *", "\xff\"", "a b c"} {
		f.Add(s)
	}
	f.Fuzz(func(t *testing.T, s string) { checkFTSQuery(t, s) })
}
