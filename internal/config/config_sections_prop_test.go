package config

import (
	"path/filepath"
	"testing"

	"pgregory.net/rapid"
)

func genSections() *rapid.Generator[map[string]SectionDef] {
	pat := rapid.SampledFrom([]string{"*", "gen*", "general", "dev-*", "r?ndom", "general:3", "dev-*:1", "x"})
	return rapid.MapOfN(rapid.SampledFrom([]string{"alpha", "beta", "gamma", "delta", "eng", "misc"}),
		rapid.Custom(func(t *rapid.T) SectionDef {
			return SectionDef{Channels: rapid.SliceOfN(pat, 0, 3).Draw(t, "pats"), Order: rapid.IntRange(0, 2).Draw(t, "order")}
		}), 0, 6)
}

// matchSectionAndOrderIn returns a section that actually matches the
// channel, among those with the lowest Order that match, and returns
// the same answer on every call for the same input (map iteration order
// must not leak into the choice).
func TestMatchSectionAndOrderInDeterministicAndCorrect(t *testing.T) {
	rapid.Check(t, func(t *rapid.T) {
		sections := genSections().Draw(t, "sections")
		ch := rapid.SampledFrom([]string{"general", "random", "dev-api", "x", "other"}).Draw(t, "channel")
		name, order := matchSectionAndOrderIn(sections, ch)

		matches := func(def SectionDef) (int, bool) {
			for _, raw := range def.Channels {
				p, o := parseChannelPattern(raw)
				if ok, _ := filepath.Match(p, ch); ok {
					return o, true
				}
			}
			return 0, false
		}
		best := -1
		for _, def := range sections {
			if _, ok := matches(def); ok && (best < 0 || def.Order < best) {
				best = def.Order
			}
		}
		if best < 0 {
			if name != "" || order != 0 {
				t.Fatalf("no section matches %q but got (%q, %d)", ch, name, order)
			}
			return
		}
		def, ok := sections[name]
		if !ok {
			t.Fatalf("returned unknown section %q", name)
		}
		if o, ok := matches(def); !ok || o != order {
			t.Fatalf("section %q does not match %q with order %d", name, ch, order)
		}
		if def.Order != best {
			t.Fatalf("section %q has Order %d, lowest matching is %d", name, def.Order, best)
		}
		for range 20 {
			if n2, o2 := matchSectionAndOrderIn(sections, ch); n2 != name || o2 != order {
				t.Fatalf("nondeterministic: (%q, %d) then (%q, %d)", name, order, n2, o2)
			}
		}
	})
}

// Through the config loader: sections with no explicit order all tie at
// 0, and a channel several of them match must resolve to the same
// section on every call.
func TestMatchSectionTiesAreStableAcrossLoad(t *testing.T) {
	cfg := loadConfigString(t, `
[sections.a]
channels = ["general"]
[sections.b]
channels = ["gen*"]
[sections.c]
channels = ["*"]
[sections.d]
channels = ["general"]
`)
	first := cfg.MatchSection("T1", "general")
	for range 200 {
		if got := cfg.MatchSection("T1", "general"); got != first {
			t.Fatalf("MatchSection flipped between %q and %q", first, got)
		}
	}
}
