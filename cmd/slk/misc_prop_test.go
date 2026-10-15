package main

import (
	"fmt"
	"strings"
	"testing"

	"pgregory.net/rapid"
)

// uniqueSlug never returns a slug already in use, and keeps the base as
// its prefix (the "workspace" fallback for an empty base).
func TestUniqueSlugProperties(t *testing.T) {
	slug := rapid.StringMatching(`[a-z]{0,3}(-[0-9])?`)
	rapid.Check(t, func(t *rapid.T) {
		base := slug.Draw(t, "base")
		existing := map[string]bool{}
		for _, s := range rapid.SliceOfN(slug, 0, 8).Draw(t, "existing") {
			existing[s] = true
		}
		if rapid.Bool().Draw(t, "crowd") {
			for i := 2; i < 6; i++ {
				existing[fmt.Sprintf("%s-%d", base, i)] = true
			}
			existing[base] = true
		}
		got := uniqueSlug(base, existing)
		if existing[got] {
			t.Fatalf("uniqueSlug(%q) = %q, already taken", base, got)
		}
		want := base
		if want == "" {
			want = "workspace"
		}
		if !strings.HasPrefix(got, want) {
			t.Fatalf("uniqueSlug(%q) = %q lost its base", base, got)
		}
	})
}

// slugifyHandle is idempotent, never starts or ends with "-", and only
// emits the handle alphabet.
func TestSlugifyHandleProperties(t *testing.T) {
	rapid.Check(t, func(t *rapid.T) {
		s := rapid.String().Draw(t, "s")
		out := slugifyHandle(s)
		if strings.HasPrefix(out, "-") || strings.HasSuffix(out, "-") {
			t.Fatalf("slugifyHandle(%q) = %q has an edge dash", s, out)
		}
		if strings.Contains(out, "--") {
			t.Fatalf("slugifyHandle(%q) = %q has a doubled dash", s, out)
		}
		for _, r := range out {
			if !(r >= 'a' && r <= 'z' || r >= '0' && r <= '9' || r == '_' || r == '.' || r == '-') {
				t.Fatalf("slugifyHandle(%q) = %q has %q", s, out, r)
			}
		}
		if again := slugifyHandle(out); again != out {
			t.Fatalf("not idempotent: %q -> %q -> %q", s, out, again)
		}
	})
}

func FuzzSlugifyHandle(f *testing.F) {
	for _, s := range []string{"", "Eng Team", "--a--", "a.b_c", "日本", "\xff-x"} {
		f.Add(s)
	}
	f.Fuzz(func(t *testing.T, s string) {
		out := slugifyHandle(s)
		if strings.HasSuffix(out, "-") || strings.HasPrefix(out, "-") || slugifyHandle(out) != out {
			t.Fatalf("slugifyHandle(%q) = %q", s, out)
		}
	})
}
