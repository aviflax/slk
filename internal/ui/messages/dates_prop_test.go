package messages

import (
	"strconv"
	"strings"
	"testing"
	"time"

	"pgregory.net/rapid"
)

// drawNow is a wall-clock instant in a pinned zone between 2001 and
// 2040, biased towards the minutes either side of midnight on New
// Year's Eve, where day and year boundaries fall.
func drawNow(t *rapid.T) time.Time {
	loc := drawZone(t)
	year := rapid.IntRange(2001, 2040).Draw(t, "year")
	if rapid.Bool().Draw(t, "newYear") {
		base := time.Date(year, 1, 1, 0, 0, 0, 0, loc)
		return base.Add(time.Duration(rapid.IntRange(-120, 120).Draw(t, "minutes")) * time.Minute)
	}
	return time.Date(year, time.Month(rapid.IntRange(1, 12).Draw(t, "month")), rapid.IntRange(1, 28).Draw(t, "day"),
		rapid.IntRange(0, 23).Draw(t, "hour"), rapid.IntRange(0, 59).Draw(t, "minute"), 0, 0, loc)
}

// daysAgo is the "2006-01-02" date k calendar days before now's date.
func daysAgo(now time.Time, k int) string {
	return time.Date(now.Year(), now.Month(), now.Day()-k, 12, 0, 0, 0, time.UTC).Format("2006-01-02")
}

// The day-divider label depends only on how many calendar days back the
// date is: Today (or any future date), Yesterday, the weekday within the
// last week, else the full date with its year. Counting calendar days
// with AddDate is the reference; the label must not change with the
// time of day.
func TestFormatDateSeparatorByCalendarDays(t *testing.T) {
	rapid.Check(t, func(t *rapid.T) {
		now := drawNow(t)
		k := rapid.IntRange(-3, 800).Draw(t, "daysAgo")
		date := daysAgo(now, k)
		got := formatDateSeparatorAt(date, now)
		d, _ := time.Parse("2006-01-02", date)
		var want string
		switch {
		case k <= 0:
			want = "Today"
		case k == 1:
			want = "Yesterday"
		case k < 7:
			want = d.Weekday().String()
		default:
			want = d.Format("Monday, January 2, 2006")
		}
		if got != want {
			t.Fatalf("formatDateSeparatorAt(%s, %v) = %q, want %q (%d days ago)", date, now, got, want, k)
		}
		first := time.Date(now.Year(), now.Month(), now.Day(), 0, 0, 0, 0, now.Location())
		last := time.Date(now.Year(), now.Month(), now.Day(), 23, 59, 59, 0, now.Location())
		for _, at := range []time.Time{first, last} {
			if again := formatDateSeparatorAt(date, at); again != got {
				t.Fatalf("label for %s is %q at %v but %q at %v", date, got, now, again, at)
			}
		}
	})
}

// The header's short date is the divider label when that has no comma
// (Today, Yesterday, a weekday), else "Jan 2", with the year added only
// for another year. It is never longer than the divider label.
func TestFormatShortDateAt(t *testing.T) {
	rapid.Check(t, func(t *rapid.T) {
		now := drawNow(t)
		k := rapid.IntRange(-3, 800).Draw(t, "daysAgo")
		date := daysAgo(now, k)
		d, _ := time.Parse("2006-01-02", date)
		long := formatDateSeparatorAt(date, now)
		got := formatShortDateAt(date, now)
		want := long
		if k >= 7 {
			want = d.Format("Jan 2")
			if d.Year() != now.Year() {
				want = d.Format("Jan 2, 2006")
			}
		}
		if got != want {
			t.Fatalf("formatShortDateAt(%s, %v) = %q, want %q", date, now, got, want)
		}
		if len(got) > len(long) {
			t.Fatalf("short %q longer than long %q", got, long)
		}
	})
}

// Anything that is not a date comes back as it was, from both labels.
func TestDateLabelsPassThroughNonDates(t *testing.T) {
	rapid.Check(t, func(t *rapid.T) {
		s := rapid.StringMatching(`[0-9a-z-]{0,12}`).Draw(t, "s")
		if _, err := time.Parse("2006-01-02", s); err == nil {
			return
		}
		now := drawNow(t)
		if got := formatDateSeparatorAt(s, now); got != s {
			t.Fatalf("formatDateSeparatorAt(%q) = %q", s, got)
		}
		if got := formatShortDateAt(s, now); got != s {
			t.Fatalf("formatShortDateAt(%q) = %q", s, got)
		}
	})
}

// The long timestamp is the short one with the message's local date in
// front, and that date carries a year exactly when it is not the current
// year. An empty short timestamp or an unparseable ts leaves short as
// it was.
func TestLongTimestampAt(t *testing.T) {
	rapid.Check(t, func(t *rapid.T) {
		now := drawNow(t).In(time.Local)
		msgAt := now.Add(-time.Duration(rapid.Int64Range(0, 3*365*24*3600).Draw(t, "age")) * time.Second)
		ts := strconv.FormatInt(msgAt.Unix(), 10) + ".000100"
		short := rapid.SampledFrom([]string{"", "3:04 PM", "15:04"}).Draw(t, "short")
		got := longTimestampAt(ts, short, now)
		if short == "" {
			if got != "" {
				t.Fatalf("empty short gave %q", got)
			}
			return
		}
		if bad := longTimestampAt("x"+ts, short, now); bad != short {
			t.Fatalf("unparseable ts gave %q", bad)
		}
		if !strings.HasSuffix(got, ", "+short) {
			t.Fatalf("longTimestampAt = %q, not ending in %q", got, short)
		}
		local := time.Unix(msgAt.Unix(), 0)
		if !strings.HasPrefix(got, local.Format("Mon Jan 2")) {
			t.Fatalf("longTimestampAt = %q for %v", got, local)
		}
		hasYear := strings.Contains(got, local.Format(" 2006,"))
		if hasYear != (local.Year() != now.Year()) {
			t.Fatalf("longTimestampAt = %q at %v (now %v): year shown %v", got, local, now, hasYear)
		}
	})
}
