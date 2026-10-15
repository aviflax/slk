package config

import (
	"fmt"
	"regexp"
	"strconv"
	"strings"
	"testing"

	slack "github.com/gammons/slk/internal/slack"
	"pgregory.net/rapid"
)

var slugShape = regexp.MustCompile(`^[a-z0-9]+(-[a-z0-9]+)*$|^$`)

// Slugify output has the slug shape (or is empty) and is idempotent.
func TestSlugifyProperties(t *testing.T) {
	rapid.Check(t, func(t *rapid.T) {
		s := rapid.String().Draw(t, "s")
		out := Slugify(s)
		if !slugShape.MatchString(out) {
			t.Fatalf("Slugify(%q) = %q is not slug-shaped", s, out)
		}
		if again := Slugify(out); again != out {
			t.Fatalf("not idempotent: %q -> %q -> %q", s, out, again)
		}
	})
}

func FuzzSlugify(f *testing.F) {
	for _, s := range []string{"", "Acme Corp", "--a--b--", "日本語", "KK", "\xff"} {
		f.Add(s)
	}
	f.Fuzz(func(t *testing.T, s string) {
		out := Slugify(s)
		if !slugShape.MatchString(out) || Slugify(out) != out {
			t.Fatalf("Slugify(%q) = %q", s, out)
		}
	})
}

// "<pattern>:<N>" round-trips through parseChannelPattern for any
// pattern (colons included) and any N >= 0, and order is never negative
// for any input.
func TestParseChannelPatternRoundTrip(t *testing.T) {
	rapid.Check(t, func(t *rapid.T) {
		p := rapid.StringMatching(`[a-z*?:\[\]-]{0,8}`).Draw(t, "pattern")
		n := rapid.IntRange(0, 1_000_000).Draw(t, "n")
		gp, gn := parseChannelPattern(p + ":" + strconv.Itoa(n))
		if gp != p || gn != n {
			t.Fatalf("parse(%q:%d) = (%q, %d)", p, n, gp, gn)
		}
		raw := rapid.String().Draw(t, "raw")
		pat, order := parseChannelPattern(raw)
		if order < 0 || !strings.HasPrefix(raw, pat) {
			t.Fatalf("parse(%q) = (%q, %d)", raw, pat, order)
		}
		if order == 0 && pat != raw && !strings.HasSuffix(raw, ":0") && !strings.HasSuffix(raw, ":+0") && !strings.HasSuffix(raw, ":-0") {
			t.Fatalf("parse(%q) = (%q, 0) dropped a suffix that is not zero", raw, pat)
		}
	})
}

func genTokens() *rapid.Generator[[]slack.Token] {
	return rapid.Map(
		rapid.SliceOfNDistinct(rapid.StringMatching(`T[A-Z0-9]{3}`), 0, 8, func(s string) string { return s }),
		func(ids []string) []slack.Token {
			var toks []slack.Token
			for _, id := range ids {
				toks = append(toks, slack.Token{TeamID: id})
			}
			return toks
		})
}

func genOrderConfig(toks []slack.Token) *rapid.Generator[Config] {
	return rapid.Custom(func(t *rapid.T) Config {
		cfg := Config{Workspaces: map[string]Workspace{}}
		_ = rapid.Bool().Draw(t, "seedBit") // rapid rejects a generator that draws nothing (no tokens)
		for i, tok := range toks {
			if rapid.Bool().Draw(t, "configured") {
				cfg.Workspaces[fmt.Sprintf("ws%d", i)] = Workspace{TeamID: tok.TeamID, Order: rapid.IntRange(-1, 4).Draw(t, "order")}
			}
		}
		return cfg
	})
}

// OrderTokens returns a permutation of its input, ordered by bucket
// (ordered-config, plain-config, unconfigured) with the documented sort
// key inside each bucket, and gives the same answer for any input order.
func TestOrderTokensProperties(t *testing.T) {
	rapid.Check(t, func(t *rapid.T) {
		toks := genTokens().Draw(t, "tokens")
		cfg := genOrderConfig(toks).Draw(t, "cfg")
		out := OrderTokens(toks, cfg)

		if len(out) != len(toks) {
			t.Fatalf("len %d, want %d", len(out), len(toks))
		}
		want := map[string]int{}
		for _, tk := range toks {
			want[tk.TeamID]++
		}
		for _, o := range out {
			want[o.Token.TeamID]--
		}
		for id, n := range want {
			if n != 0 {
				t.Fatalf("not a permutation: %q off by %d", id, n)
			}
		}
		for i := 1; i < len(out); i++ {
			a, b := out[i-1], out[i]
			ba, bb := bucket(a), bucket(b)
			if ba > bb {
				t.Fatalf("bucket %d before %d", ba, bb)
			}
			if ba != bb {
				continue
			}
			switch ba {
			case 1:
				if a.Order > b.Order || (a.Order == b.Order && a.Slug > b.Slug) {
					t.Fatalf("bucket 1 out of order: %+v then %+v", a, b)
				}
			case 2:
				if a.Slug > b.Slug {
					t.Fatalf("bucket 2 out of order: %q then %q", a.Slug, b.Slug)
				}
			default:
				if a.Token.TeamID > b.Token.TeamID {
					t.Fatalf("bucket 3 out of order: %q then %q", a.Token.TeamID, b.Token.TeamID)
				}
			}
		}

		shuffled := rapid.Permutation(toks).Draw(t, "shuffled")
		out2 := OrderTokens(shuffled, cfg)
		for i := range out {
			if out[i].Token.TeamID != out2[i].Token.TeamID {
				t.Fatalf("order depends on input order: %v vs %v", out, out2)
			}
		}
	})
}
