package avatar

import (
	"path"
	"strconv"
	"strings"
	"testing"

	"pgregory.net/rapid"
)

func genOriginalURL() *rapid.Generator[string] {
	return rapid.Custom(func(t *rapid.T) string {
		date := rapid.StringMatching(`20[0-9]{2}-[01][0-9]-[0-3][0-9]`).Draw(t, "date")
		name := rapid.StringMatching(`[a-z0-9]{4,20}`).Draw(t, "name")
		ext := rapid.SampledFrom([]string{"jpg", "png", "gif", "jpeg"}).Draw(t, "ext")
		host := rapid.SampledFrom([]string{
			"https://avatars.slack-edge.com",
			"https://s3-us-west-2.amazonaws.com/slack-files2/avatars",
		}).Draw(t, "host")
		return host + "/" + date + "/" + name + "_original." + ext
	})
}

// A Slack original rewrites to the CDN's sized form, keeping date, id
// and extension.
func TestSizedURLRewritesOriginals(t *testing.T) {
	rapid.Check(t, func(t *rapid.T) {
		orig := genOriginalURL().Draw(t, "orig")
		px := rapid.IntRange(1, 1024).Draw(t, "px")
		got := SizedURL(orig, px)
		if !strings.HasPrefix(got, "https://avatars.slack-edge.com/") {
			t.Fatalf("got %q", got)
		}
		if path.Ext(got) != path.Ext(orig) {
			t.Fatalf("extension changed: %q -> %q", orig, got)
		}
		if !strings.Contains(got, "_"+strconv.Itoa(px)+".") || strings.Contains(got, "_original") {
			t.Fatalf("size not applied: %q", got)
		}
		// Idempotent: the sized URL is no original any more.
		if again := SizedURL(got, px); again != got {
			t.Fatalf("not idempotent: %q -> %q", got, again)
		}
		// px <= 0 is a no-op.
		if SizedURL(orig, -px) != orig || SizedURL(orig, 0) != orig {
			t.Fatal("non-positive px changed the URL")
		}
	})
}

// Anything that is not an original (a lookalike host, a query string,
// a fragment, arbitrary text) comes back unchanged.
func TestSizedURLLeavesOthers(t *testing.T) {
	rapid.Check(t, func(t *rapid.T) {
		orig := genOriginalURL().Draw(t, "orig")
		suffix := rapid.SampledFrom([]string{"?x=1", "#f", "/more", "x"}).Draw(t, "suffix")
		for _, u := range []string{
			orig + suffix,
			strings.Replace(orig, "slack-edge.com", "slack-edge.com.evil.io", 1),
			strings.Replace(orig, "https://", "http://", 1),
			strings.Replace(orig, "slack-edge", "slack-edgex", 1),
			rapid.String().Draw(t, "any"),
		} {
			if got := SizedURL(u, 72); got != u && !originalAvatarRE.MatchString(u) {
				t.Fatalf("SizedURL(%q) = %q, want unchanged", u, got)
			}
		}
	})
}

func FuzzSizedURL(f *testing.F) {
	f.Add("https://avatars.slack-edge.com/2020-01-01/abc_original.png", 72)
	f.Add("", 0)
	f.Fuzz(func(t *testing.T, s string, px int) {
		got := SizedURL(s, px)
		if again := SizedURL(got, px); again != got {
			t.Fatalf("not idempotent: %q -> %q -> %q", s, got, again)
		}
		if (px <= 0 || !originalAvatarRE.MatchString(s)) && got != s {
			t.Fatalf("changed %q to %q", s, got)
		}
	})
}
