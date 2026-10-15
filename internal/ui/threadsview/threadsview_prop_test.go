package threadsview

import (
	"strconv"
	"strings"
	"testing"
	"time"

	"charm.land/lipgloss/v2"
	"github.com/gammons/slk/internal/proptest"
	"pgregory.net/rapid"
)

// relAge turns a formatRelTimeAt label back into seconds (lower bound of
// its bucket): "now" is 0, "Nm ago" is 60N, and so on.
func relAge(t interface{ Fatalf(string, ...any) }, label string) int64 {
	if label == "now" {
		return 0
	}
	mult := relUnit(t, label)
	v, err := strconv.ParseInt(label[:len(label)-len("m ago")], 10, 64)
	if err != nil || v < 1 {
		t.Fatalf("bad count in %q", label)
	}
	return v * mult
}

func relUnit(t interface{ Fatalf(string, ...any) }, label string) int64 {
	switch {
	case strings.HasSuffix(label, "m ago"):
		return 60
	case strings.HasSuffix(label, "h ago"):
		return 3600
	case strings.HasSuffix(label, "d ago"):
		return 86400
	}
	t.Fatalf("unrecognized label %q", label)
	return 0
}

// The label's age never decreases as the message gets older, never
// overstates the real age, understates it by less than one bucket, and a
// timestamp in the future reads "now".
func TestFormatRelTimeAtMonotoneAndBounded(t *testing.T) {
	now := time.Unix(1_800_000_000, 0)
	rapid.Check(t, func(t *rapid.T) {
		a := rapid.Int64Range(-100_000, 3_000_000).Draw(t, "ageA")
		b := rapid.Int64Range(a, 3_000_000).Draw(t, "ageB")
		la := formatRelTimeAt(strconv.FormatInt(now.Unix()-a, 10)+".000100", now)
		lb := formatRelTimeAt(strconv.FormatInt(now.Unix()-b, 10)+".000100", now)
		if a < 60 && la != "now" {
			t.Fatalf("age %d = %q, want now", a, la)
		}
		if relAge(t, la) > relAge(t, lb) {
			t.Fatalf("age %d -> %q is older than age %d -> %q", a, la, b, lb)
		}
		if got := relAge(t, lb); b >= 60 && (got > b || b-got >= relUnit(t, lb)) {
			t.Fatalf("age %d labelled %q (bucket start %d)", b, lb, got)
		}
	})
}

// Bucket edges: 59s is "now", 60s is "1m ago", 3600s "1h ago", 86400s "1d ago".
func TestFormatRelTimeAtBoundaries(t *testing.T) {
	now := time.Unix(1_800_000_000, 0)
	for age, want := range map[int64]string{
		0: "now", 59: "now", 60: "1m ago", 3599: "59m ago", 3600: "1h ago",
		86399: "23h ago", 86400: "1d ago", 172800: "2d ago", -5: "now",
	} {
		if got := formatRelTimeAt(strconv.FormatInt(now.Unix()-age, 10), now); got != want {
			t.Errorf("age %d = %q, want %q", age, got, want)
		}
	}
}

// Anything that is not whole seconds, optionally followed by ".fraction",
// yields "".
func TestFormatRelTimeAtRejectsGarbage(t *testing.T) {
	now := time.Unix(1_800_000_000, 0)
	rapid.Check(t, func(t *rapid.T) {
		s := rapid.StringMatching(`[a-z:/ ]{1,8}(\.[0-9]*)?`).Draw(t, "ts")
		if got := formatRelTimeAt(s, now); got != "" {
			t.Fatalf("formatRelTimeAt(%q) = %q", s, got)
		}
	})
}

// clipToWidth never draws wider than its limit, and a string that already
// fits comes back untouched. Text is styled and holds wide runes.
func TestClipToWidthFits(t *testing.T) {
	rapid.Check(t, func(t *rapid.T) {
		s := proptest.StyledText().Draw(t, "text")
		s = proptest.NoVS16(strings.ReplaceAll(s, "\n", " "))
		w := rapid.IntRange(-2, 40).Draw(t, "width")
		got := clipToWidth(s, w)
		if w <= 0 {
			if got != "" {
				t.Fatalf("width %d gave %q", w, got)
			}
			return
		}
		if gw := lipgloss.Width(got); gw > w {
			t.Fatalf("clipToWidth(%q, %d) is %d wide: %q", s, w, gw, got)
		}
		if lipgloss.Width(s) <= w && got != s {
			t.Fatalf("fitting string changed: %q -> %q", s, got)
		}
	})
}
