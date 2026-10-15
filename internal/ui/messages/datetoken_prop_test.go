package messages

import (
	"strconv"
	"strings"
	"testing"
	"time"

	"github.com/gammons/slk/internal/proptest"
	"pgregory.net/rapid"
)

func drawZone(t *rapid.T) *time.Location { return proptest.Zone().Draw(t, "zone") }

// withDateTokenLayout sets the {time} layout for one test.
func withDateTokenLayout(t *testing.T, layout string) {
	old := dateTokenTimeLayout
	dateTokenTimeLayout = layout
	t.Cleanup(func() { dateTokenTimeLayout = old })
}

// A token rendering "{date_num} {time_secs}" parses back, in the same
// zone, to the timestamp it was given: the tokens lose nothing down to
// the second.
func TestFormatSlackDateTokenRoundTrips(t *testing.T) {
	withDateTokenLayout(t, "15:04")
	rapid.Check(t, func(t *rapid.T) {
		loc := drawZone(t)
		sec := rapid.Int64Range(0, 4_000_000_000).Draw(t, "sec")
		out := formatSlackDateTokenIn(strings.Repeat(" ", rapid.IntRange(0, 2).Draw(t, "pad"))+itoa(sec), "{date_num} {time_secs}", "fb", "raw", loc)
		got, err := time.ParseInLocation("2006-01-02 15:04:05", out, loc)
		if err != nil {
			t.Fatalf("output %q does not parse: %v", out, err)
		}
		// A repeated wall-clock hour (DST fall-back) parses to the first
		// of the two instants.
		if got.Unix() != sec && got.Unix() != sec-3600 {
			t.Fatalf("%d rendered as %q, parsed back as %d", sec, out, got.Unix())
		}
	})
}

// The pretty forms agree with the plain ones on the date: stripping the
// ordinal suffix from {date_pretty} gives {date}.
func TestFormatSlackDateTokenPrettyMatchesPlain(t *testing.T) {
	rapid.Check(t, func(t *rapid.T) {
		loc := drawZone(t)
		ts := itoa(rapid.Int64Range(0, 4_000_000_000).Draw(t, "sec"))
		plain := formatSlackDateTokenIn(ts, "{date}", "", "raw", loc)
		pretty := formatSlackDateTokenIn(ts, "{date_pretty}", "", "raw", loc)
		day := strings.Fields(plain)[1] // "January 2, 2006" -> "2,"
		n := strings.TrimSuffix(day, ",")
		want := strings.Replace(plain, " "+n+",", " "+n+ordinalSuffix(atoi(n))+",", 1)
		if pretty != want {
			t.Fatalf("{date_pretty} = %q, {date} = %q", pretty, plain)
		}
	})
}

// Without a recognized {token}, or with a non-numeric timestamp, the
// format is not expanded: the fallback is shown, or the raw token when
// there is none.
func TestFormatSlackDateTokenFallsBack(t *testing.T) {
	rapid.Check(t, func(t *rapid.T) {
		loc := drawZone(t)
		format := rapid.StringMatching(`[a-z {}_]{0,12}`).Draw(t, "format")
		ts := rapid.SampledFrom([]string{"", "abc", "1.5", "17e8", itoa(rapid.Int64Range(0, 2_000_000_000).Draw(t, "sec"))}).Draw(t, "ts")
		if slackDateFormatRe.MatchString(format) && ts != "" && ts != "abc" && ts != "1.5" && ts != "17e8" {
			return
		}
		fallback := rapid.SampledFrom([]string{"", "Tuesday"}).Draw(t, "fallback")
		want := fallback
		if want == "" {
			want = "raw"
		}
		if got := formatSlackDateTokenIn(ts, format, fallback, "raw", loc); got != want {
			t.Fatalf("formatSlackDateTokenIn(%q, %q, %q) = %q, want %q", ts, format, fallback, got, want)
		}
	})
}

// clockLayout draws a Go clock layout of the kind timestamp_format
// holds: 12- or 24-hour, padded or zero-suppressed minutes, maybe
// seconds, maybe an AM/PM marker or a weekday.
func clockLayout() *rapid.Generator[string] {
	return rapid.Custom(func(t *rapid.T) string {
		hour := rapid.SampledFrom([]string{"3", "03", "15"}).Draw(t, "hour")
		sep := rapid.SampledFrom([]string{":", ".", "h"}).Draw(t, "sep")
		min := rapid.SampledFrom([]string{"04", "4"}).Draw(t, "min")
		secs := rapid.SampledFrom([]string{"", ":05", ".05"}).Draw(t, "secs")
		suffix := ""
		if hour != "15" {
			suffix = rapid.SampledFrom([]string{" PM", "PM", " pm"}).Draw(t, "ampm")
		}
		prefix := rapid.SampledFrom([]string{"", "Mon "}).Draw(t, "prefix")
		return prefix + hour + sep + min + secs + suffix
	})
}

// For any clock layout, the seconds layout renders seconds ("05"), is
// idempotent, and renders the same time with its seconds read back
// exactly.
func TestTimeLayoutWithSecondsAddsSeconds(t *testing.T) {
	rapid.Check(t, func(t *rapid.T) {
		layout := clockLayout().Draw(t, "layout")
		withSecs := timeLayoutWithSeconds(layout)
		if !strings.Contains(withSecs, "05") {
			t.Fatalf("timeLayoutWithSeconds(%q) = %q has no seconds", layout, withSecs)
		}
		if again := timeLayoutWithSeconds(withSecs); again != withSecs {
			t.Fatalf("not idempotent: %q -> %q -> %q", layout, withSecs, again)
		}
		at := time.Date(2026, 3, 4, rapid.IntRange(0, 23).Draw(t, "h"), rapid.IntRange(0, 59).Draw(t, "m"), rapid.IntRange(0, 59).Draw(t, "s"), 0, time.UTC)
		parsed, err := time.Parse(withSecs, at.Format(withSecs))
		if err != nil {
			t.Fatalf("%q does not round-trip: %v", withSecs, err)
		}
		if parsed.Second() != at.Second() || parsed.Minute() != at.Minute() {
			t.Fatalf("%q rendered %v as %q, read back %v", withSecs, at, at.Format(withSecs), parsed)
		}
	})
}

// The ordinal suffix depends only on the day mod 100, the teens are all
// "th", and outside them the last digit decides.
func TestOrdinalSuffix(t *testing.T) {
	rapid.Check(t, func(t *rapid.T) {
		d := rapid.IntRange(1, 1000).Draw(t, "day")
		got := ordinalSuffix(d)
		if got != ordinalSuffix(d%100+100) {
			t.Fatalf("ordinalSuffix(%d) = %q depends on more than d mod 100", d, got)
		}
		want := map[int]string{1: "st", 2: "nd", 3: "rd"}[d%10]
		if want == "" || (d%100 >= 11 && d%100 <= 13) {
			want = "th"
		}
		if got != want {
			t.Fatalf("ordinalSuffix(%d) = %q, want %q", d, got, want)
		}
	})
}

// renderDateToken never eats a token: for anything dateTokenRe matches,
// it returns non-empty text, and when the timestamp or format is
// unusable that text is the fallback or, failing that, the raw token.
func TestRenderDateTokenKeepsData(t *testing.T) {
	rapid.Check(t, func(t *rapid.T) {
		body := rapid.StringMatching(`[0-9a-z^|{}_ ]{0,24}`).Draw(t, "body")
		match := "<!date^" + body + ">"
		if dateTokenRe.FindString(match) != match {
			t.Fatalf("generator produced a non-token %q", match)
		}
		out := renderDateToken(match)
		if out == "" {
			t.Fatalf("renderDateToken(%q) is empty", match)
		}
		parts := strings.SplitN(body, "|", 3)
		fallback := ""
		if len(parts) > 1 {
			fallback = parts[1]
		}
		if !slackDateFormatRe.MatchString(parts[0]) {
			want := fallback
			if want == "" {
				want = match
			}
			if out != want {
				t.Fatalf("renderDateToken(%q) = %q, want %q", match, out, want)
			}
		}
	})
}

func itoa(n int64) string { return strconv.FormatInt(n, 10) }

func atoi(s string) int {
	n, _ := strconv.Atoi(s)
	return n
}
