package main

import (
	"fmt"
	"path/filepath"
	"strings"
	"testing"

	"pgregory.net/rapid"
)

// A version string with an optional "v", numeric triple and optional
// prerelease, drawn from small numbers so equal triples are common.
func genVersion(allowPre bool) *rapid.Generator[string] {
	return rapid.Custom(func(t *rapid.T) string {
		n := func(name string) int { return rapid.IntRange(0, 12).Draw(t, name) }
		s := fmt.Sprintf("%d.%d.%d", n("major"), n("minor"), n("patch"))
		if allowPre && rapid.Bool().Draw(t, "pre") {
			ids := rapid.SliceOfN(rapid.SampledFrom([]string{"0", "1", "2", "10", "alpha", "beta", "rc"}), 1, 3).Draw(t, "ids")
			s += "-" + strings.Join(ids, ".")
		}
		if rapid.Bool().Draw(t, "v") {
			s = "v" + s
		}
		return s
	})
}

// A version is never newer than itself.
func TestIsNewerVersionIrreflexive(t *testing.T) {
	rapid.Check(t, func(t *rapid.T) {
		v := genVersion(true).Draw(t, "v")
		if isNewerVersion(v, v) {
			t.Fatalf("isNewerVersion(%q, %q) = true", v, v)
		}
	})
}

// Newer is antisymmetric: a is never newer than b while b is newer than a.
func TestIsNewerVersionAntisymmetric(t *testing.T) {
	rapid.Check(t, func(t *rapid.T) {
		a, b := genVersion(true).Draw(t, "a"), genVersion(true).Draw(t, "b")
		if isNewerVersion(a, b) && isNewerVersion(b, a) {
			t.Fatalf("both %q > %q and %q > %q", a, b, b, a)
		}
	})
}

// Among releases (no prerelease), newer is transitive and the "v" prefix
// never changes the answer.
func TestIsNewerVersionTransitiveAndPrefixBlind(t *testing.T) {
	rapid.Check(t, func(t *rapid.T) {
		a, b, c := genVersion(false).Draw(t, "a"), genVersion(false).Draw(t, "b"), genVersion(false).Draw(t, "c")
		if isNewerVersion(a, b) && isNewerVersion(b, c) && !isNewerVersion(a, c) {
			t.Fatalf("%q > %q > %q but not %q > %q", a, b, c, a, c)
		}
		strip := func(s string) string { return strings.TrimPrefix(s, "v") }
		if isNewerVersion(a, b) != isNewerVersion(strip(a), "v"+strip(b)) {
			t.Fatalf("v prefix changed the answer for %q, %q", a, b)
		}
	})
}

// With prereleases in play, transitivity still holds whenever the
// middle version is itself comparable (prerelease latest is ignored
// against a release current, which is deliberately not an ordering, so
// chains are only checked among prereleases of the same triple).
func TestIsNewerVersionTransitiveAmongPrereleases(t *testing.T) {
	pre := rapid.Custom(func(t *rapid.T) string {
		ids := rapid.SliceOfN(rapid.SampledFrom([]string{"0", "1", "2", "10", "alpha", "beta", "rc"}), 1, 3).Draw(t, "ids")
		return "1.2.3-" + strings.Join(ids, ".")
	})
	rapid.Check(t, func(t *rapid.T) {
		a, b, c := pre.Draw(t, "a"), pre.Draw(t, "b"), pre.Draw(t, "c")
		if isNewerVersion(a, b) && isNewerVersion(b, c) && !isNewerVersion(a, c) {
			t.Fatalf("%q > %q > %q but not %q > %q", a, b, c, a, c)
		}
	})
}

// A prerelease latest is ignored unless current is a prerelease; a
// release is newer than a prerelease of the same triple.
func TestIsNewerVersionPrereleaseRules(t *testing.T) {
	rapid.Check(t, func(t *rapid.T) {
		pre, rel := genVersion(true).Draw(t, "pre"), genVersion(false).Draw(t, "rel")
		p, _ := parseSemver(pre)
		if len(p.pre) > 0 && isNewerVersion(pre, rel) {
			t.Fatalf("prerelease %q reported newer than release %q", pre, rel)
		}
		base := strings.TrimPrefix(rel, "v")
		if !isNewerVersion(base, base+"-rc.1") {
			t.Fatalf("release %q not newer than its own prerelease", base)
		}
	})
}

// Numeric parts compare as numbers, not text: 1.10.0 > 1.9.0.
func TestIsNewerVersionNumericOrdering(t *testing.T) {
	rapid.Check(t, func(t *rapid.T) {
		a := rapid.IntRange(0, 100000).Draw(t, "a")
		b := rapid.IntRange(0, 100000).Draw(t, "b")
		got := isNewerVersion(fmt.Sprintf("1.%d.0", a), fmt.Sprintf("1.%d.0", b))
		if got != (a > b) {
			t.Fatalf("1.%d.0 newer than 1.%d.0 = %v", a, b, got)
		}
	})
}

// FuzzIsNewerVersion: arbitrary strings (including huge digit runs that
// overflow Atoi) never panic, and unparseable input is never newer.
func FuzzIsNewerVersion(f *testing.F) {
	for _, s := range []string{"1.2.3", "v1.2.3-rc.1", "1.2.3+build", "99999999999999999999.0.0", "1.0.0-99999999999999999999", "", "-", "1.2", "1.2.3-"} {
		f.Add(s, "1.0.0")
		f.Add("2.0.0", s)
	}
	f.Fuzz(func(t *testing.T, a, b string) {
		got := isNewerVersion(a, b)
		_, okA := parseSemver(a)
		_, okB := parseSemver(b)
		if got && (!okA || !okB) {
			t.Fatalf("isNewerVersion(%q, %q) = true with unparseable input", a, b)
		}
		if isNewerVersion(a, b) && isNewerVersion(b, a) {
			t.Fatalf("antisymmetry broken for %q, %q", a, b)
		}
	})
}

// underDir agrees with a path-segment reference: a lookalike sibling
// (/opt/homebrew-evil vs /opt/homebrew) is not under the directory.
func TestUnderDirIsSegmentWise(t *testing.T) {
	seg := rapid.StringMatching(`[a-z]{1,5}`)
	rapid.Check(t, func(t *rapid.T) {
		dir := "/" + strings.Join(rapid.SliceOfN(seg, 1, 3).Draw(t, "dir"), "/")
		suffix := rapid.StringMatching(`[a-z\-]{1,4}`).Draw(t, "suffix")
		if underDir(dir+suffix, dir) {
			t.Fatalf("lookalike %q reported under %q", dir+suffix, dir)
		}
		child := dir + "/" + strings.Join(rapid.SliceOfN(seg, 0, 3).Draw(t, "rest"), "/")
		if !underDir(filepath.Clean(child), dir) {
			t.Fatalf("%q not under %q", child, dir)
		}
		if underDir(child, "") {
			t.Fatalf("empty dir contains %q", child)
		}
	})
}

// detectInstallMethod always returns a member of the enum, answers Other
// for an empty path, and answers Brew for anything under the brew prefix.
func TestDetectInstallMethodProperties(t *testing.T) {
	seg := rapid.StringMatching(`[a-z]{1,6}`)
	rapid.Check(t, func(t *rapid.T) {
		env := installEnv{
			Home:       "/home/u",
			GOPATH:     rapid.SampledFrom([]string{"", "/home/u/go", "/a:/b"}).Draw(t, "gopath"),
			GOBIN:      rapid.SampledFrom([]string{"", "/gobin"}).Draw(t, "gobin"),
			BrewPrefix: rapid.SampledFrom([]string{"", "/opt/homebrew", "/usr/local"}).Draw(t, "brew"),
		}
		exe := "/" + strings.Join(rapid.SliceOfN(seg, 0, 4).Draw(t, "exe"), "/")
		switch m := detectInstallMethod(exe, env); m {
		case installOther, installBrew, installGo:
		default:
			t.Fatalf("method %d outside enum", m)
		}
		if detectInstallMethod("", env) != installOther {
			t.Fatal("empty exe not Other")
		}
		if env.BrewPrefix != "" {
			under := env.BrewPrefix + "/bin/" + rapid.StringMatching(`[a-z]{1,6}`).Draw(t, "bin")
			if detectInstallMethod(under, env) != installBrew {
				t.Fatalf("%q under prefix %q not Brew", under, env.BrewPrefix)
			}
			if detectInstallMethod(env.BrewPrefix+"-evil/bin/slk", installEnv{BrewPrefix: env.BrewPrefix}) != installOther {
				t.Fatalf("lookalike of %q treated as Brew", env.BrewPrefix)
			}
		}
	})
}
