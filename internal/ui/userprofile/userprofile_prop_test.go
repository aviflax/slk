package userprofile

import (
	"strconv"
	"strings"
	"testing"
	"time"

	"pgregory.net/rapid"
)

// parseDelta reads formatOffsetDelta's output back into seconds, with the
// seconds part dropped (the format only has minutes).
func parseDelta(t *rapid.T, s string) int {
	if s == "same time as you" {
		return 0
	}
	body, ok := strings.CutSuffix(s, " from you")
	if !ok {
		t.Fatalf("no suffix: %q", s)
	}
	sign := 1
	switch {
	case strings.HasPrefix(body, "+"):
		body = body[1:]
	case strings.HasPrefix(body, "−"):
		sign = -1
		body = body[len("−"):]
	default:
		t.Fatalf("no sign: %q", s)
	}
	hs, rest, ok := strings.Cut(body, "h")
	if !ok {
		t.Fatalf("no hours: %q", s)
	}
	h, err := strconv.Atoi(hs)
	if err != nil {
		t.Fatalf("bad hours in %q", s)
	}
	m := 0
	if rest != "" {
		ms, ok := strings.CutSuffix(rest, "m")
		if !ok {
			t.Fatalf("bad minutes in %q", s)
		}
		if m, err = strconv.Atoi(ms); err != nil || m < 1 || m > 59 {
			t.Fatalf("bad minutes in %q", s)
		}
	}
	return sign * (h*3600 + m*60)
}

// formatOffsetDelta round trips: parsing its output gives the delta
// truncated to whole minutes, "same time as you" is exactly delta 0, and
// a nonzero delta carries the sign of the input.
func TestFormatOffsetDeltaRoundTrip(t *testing.T) {
	rapid.Check(t, func(t *rapid.T) {
		d := rapid.IntRange(-26*3600, 26*3600).Draw(t, "delta")
		s := formatOffsetDelta(d)
		want := d / 60 * 60 // Go division truncates toward zero, like the formatter
		if got := parseDelta(t, s); got != want {
			t.Fatalf("formatOffsetDelta(%d) = %q, parses to %d, want %d", d, s, got, want)
		}
		if (d == 0) != (s == "same time as you") {
			t.Fatalf("formatOffsetDelta(%d) = %q", d, s)
		}
	})
}

// KNOWN: an offset under a minute ("+30s") renders as "+0h from you"
// rather than "same time as you". Real zone offsets are whole minutes,
// so nothing reaches it; the property above draws any seconds value and
// still holds because "+0h" parses back to 0.

// formatLocalTime shows the author's wall clock (now seen at tzOffset), the
// abbreviation when there is one, and the delta against now's own zone.
func TestFormatLocalTimeProperty(t *testing.T) {
	zones := []string{"UTC", "America/New_York", "Europe/London", "Asia/Kolkata", "Australia/Lord_Howe", "Pacific/Auckland"}
	rapid.Check(t, func(t *rapid.T) {
		loc, err := time.LoadLocation(rapid.SampledFrom(zones).Draw(t, "zone"))
		if err != nil {
			t.Skip("no tzdata")
		}
		now := time.Unix(rapid.Int64Range(1_500_000_000, 2_000_000_000).Draw(t, "unix"), 0).In(loc)
		off := rapid.IntRange(-12*3600, 14*3600).Draw(t, "off")
		abbrev := rapid.SampledFrom([]string{"", "PST", "IST"}).Draw(t, "abbrev")
		got := formatLocalTime(now, off, abbrev)

		clock := now.In(time.FixedZone("", off)).Format("3:04 PM")
		if !strings.HasPrefix(got, clock+" (") || !strings.HasSuffix(got, ")") {
			t.Fatalf("formatLocalTime = %q, want clock %q first", got, clock)
		}
		inner := got[len(clock)+2 : len(got)-1]
		if abbrev != "" {
			var ok bool
			if inner, ok = strings.CutPrefix(inner, abbrev+", "); !ok {
				t.Fatalf("abbrev missing in %q", got)
			}
		}
		_, local := now.Zone()
		if inner != formatOffsetDelta(off-local) {
			t.Fatalf("delta %q in %q, want %q", inner, got, formatOffsetDelta(off-local))
		}
	})
}
