package main

import (
	"strings"
	"testing"
	"unicode/utf8"

	"github.com/pelletier/go-toml/v2"
	"pgregory.net/rapid"
)

func tomlRoundTrips(s string) (string, error) {
	var out struct{ V string }
	err := toml.Unmarshal([]byte("v = "+tomlString(s)+"\n"), &out)
	return out.V, err
}

// tomlString output decodes back to the input for any valid UTF-8 string.
func TestTomlStringRoundTrips(t *testing.T) {
	rapid.Check(t, func(t *rapid.T) {
		s := rapid.String().Draw(t, "s")
		got, err := tomlRoundTrips(s)
		if err != nil || got != s {
			t.Fatalf("round trip of %q = %q, %v", s, got, err)
		}
	})
}

// FuzzTomlString: for any bytes the output is valid TOML, and valid
// UTF-8 input round-trips.
func FuzzTomlString(f *testing.F) {
	for _, s := range []string{"", "a\"b", "\\", "line\nbreak", "\x00\x1f\x7f", "\xff", "日本語", " "} {
		f.Add(s)
	}
	f.Fuzz(func(t *testing.T, s string) {
		got, err := tomlRoundTrips(s)
		if err != nil {
			t.Fatalf("tomlString(%q) is not valid TOML: %v", s, err)
		}
		if utf8.ValidString(s) && got != s {
			t.Fatalf("round trip of %q = %q", s, got)
		}
	})
}

// sanitizeComment output has no control characters or newlines and no
// surrounding space, and is idempotent.
func TestSanitizeCommentProperties(t *testing.T) {
	rapid.Check(t, func(t *rapid.T) {
		s := rapid.String().Draw(t, "s")
		out := sanitizeComment(s)
		for _, r := range out {
			if r < 0x20 {
				t.Fatalf("control char %U in %q (from %q)", r, out, s)
			}
		}
		if out != strings.TrimSpace(out) {
			t.Fatalf("untrimmed output %q", out)
		}
		if sanitizeComment(out) != out {
			t.Fatalf("not idempotent: %q", out)
		}
	})
}
