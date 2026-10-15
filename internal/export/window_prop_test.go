package export

import (
	"fmt"
	"strconv"
	"strings"
	"testing"
	"time"

	"github.com/gammons/slk/internal/proptest"
	"pgregory.net/rapid"
)

var zones = []string{
	"UTC", "America/New_York", "America/Sao_Paulo", "Europe/London",
	"Australia/Lord_Howe", "Asia/Kolkata", "Pacific/Auckland", "Asia/Tokyo",
}

type windowCase struct {
	since, until, tz string
	overlap          int
	now              time.Time
}

func (c windowCase) build() (Window, error) {
	return NewWindow(c.since, c.until, c.tz, c.overlap, c.now)
}

func genWindowCase() *rapid.Generator[windowCase] {
	return rapid.Custom(func(t *rapid.T) windowCase {
		base := time.Date(2000, 1, 1, 0, 0, 0, 0, time.UTC)
		since := base.AddDate(0, 0, rapid.IntRange(0, 365*40).Draw(t, "sinceDays"))
		until := since.AddDate(0, 0, rapid.IntRange(1, 400).Draw(t, "span"))
		tz := rapid.SampledFrom(zones).Draw(t, "tz")
		if rapid.IntRange(0, 2).Draw(t, "skipped") == 0 {
			// Sao Paulo started DST at midnight until 2018: these
			// dates have no 00:00, the day begins at 01:00.
			tz = "America/Sao_Paulo"
			since, _ = time.Parse(dateLayout, rapid.SampledFrom([]string{"2015-10-18", "2016-10-16", "2017-10-15", "2018-11-04"}).Draw(t, "skippedDay"))
			until = since.AddDate(0, 0, rapid.IntRange(1, 9).Draw(t, "span"))
		}
		c := windowCase{
			since:   since.Format(dateLayout),
			until:   until.Format(dateLayout),
			tz:      tz,
			overlap: rapid.IntRange(0, 30).Draw(t, "overlap"),
			now:     base.Add(time.Duration(rapid.Int64Range(0, 1_500_000_000).Draw(t, "now")) * time.Second),
		}
		return c
	})
}

// localDate is the calendar date of tm in loc.
func localDate(tm time.Time, loc *time.Location) (int, time.Month, int) { return tm.In(loc).Date() }

// A built window is ordered, Start and End fall on the dates Since and
// Until shifted by the overlap, and each bound is the first instant of its
// local date (local midnight, or 01:00 where the zone skipped midnight).
func TestNewWindowShape(t *testing.T) {
	rapid.Check(t, func(t *rapid.T) {
		c := genWindowCase().Draw(t, "case")
		w, err := c.build()
		if err != nil {
			t.Fatalf("%+v: %v", c, err)
		}
		if !(!w.Start.After(w.Since) && w.Since.Before(w.Until) && !w.Until.After(w.End)) {
			t.Fatalf("order broken: %v %v %v %v", w.Start, w.Since, w.Until, w.End)
		}
		sinceD, _ := time.Parse(dateLayout, c.since)
		untilD, _ := time.Parse(dateLayout, c.until)
		for _, b := range []struct {
			name string
			got  time.Time
			want time.Time
		}{
			{"Since", w.Since, sinceD},
			{"Until", w.Until, untilD},
			{"Start", w.Start, sinceD.AddDate(0, 0, -c.overlap)},
			{"End", w.End, untilD.AddDate(0, 0, c.overlap)},
		} {
			y, m, d := localDate(b.got, w.Location)
			wy, wm, wd := b.want.Date()
			if y != wy || m != wm || d != wd {
				t.Fatalf("%s is %v in %s, want date %v (%+v)", b.name, b.got.In(w.Location), c.tz, b.want.Format(dateLayout), c)
			}
			// The first instant of the day: the minute before is on
			// the previous date (midnight itself, unless the zone
			// skipped it).
			py, pm, pd := localDate(b.got.Add(-time.Minute), w.Location)
			if py == wy && pm == wm && pd == wd {
				t.Fatalf("%s = %v is not the start of %s in %s (%+v)", b.name, b.got.In(w.Location), b.want.Format(dateLayout), c.tz, c)
			}
		}
	})
}

// Contains(ts) holds exactly when Start <= TimeFromTS(ts) < End; the
// bounds' own timestamps sit on the edge (OldestTS in, LatestTS out),
// and a wider overlap never excludes what a narrower one included.
func TestWindowContains(t *testing.T) {
	rapid.Check(t, func(t *rapid.T) {
		c := genWindowCase().Draw(t, "case")
		w, _ := c.build()
		ts := fmt.Sprintf("%d.%06d", rapid.Int64Range(w.Start.Unix()-200_000, w.End.Unix()+200_000).Draw(t, "sec"), rapid.IntRange(0, 999_999).Draw(t, "us"))
		tm, ok := TimeFromTS(ts)
		if !ok {
			t.Fatalf("TimeFromTS(%q) not ok", ts)
		}
		if got, want := w.Contains(ts), !tm.Before(w.Start) && tm.Before(w.End); got != want {
			t.Fatalf("Contains(%q) = %v, want %v", ts, got, want)
		}
		if !w.Contains(w.OldestTS()) || w.Contains(w.LatestTS()) {
			t.Fatalf("edges wrong: oldest in=%v latest in=%v", w.Contains(w.OldestTS()), w.Contains(w.LatestTS()))
		}
		wider := c
		wider.overlap += rapid.IntRange(0, 10).Draw(t, "more")
		w2, _ := wider.build()
		if w.Contains(ts) && !w2.Contains(ts) {
			t.Fatalf("wider overlap excluded %q", ts)
		}
	})
}

// OldestTS and LatestTS are Start and End as whole-second timestamps.
func TestWindowTSRoundTrip(t *testing.T) {
	rapid.Check(t, func(t *rapid.T) {
		w, _ := genWindowCase().Draw(t, "case").build()
		for _, p := range []struct {
			ts   string
			want time.Time
		}{{w.OldestTS(), w.Start}, {w.LatestTS(), w.End}} {
			got, ok := TimeFromTS(p.ts)
			if !ok || !got.Equal(p.want) {
				t.Fatalf("TimeFromTS(%q) = %v, %v; want %v", p.ts, got, ok, p.want)
			}
		}
	})
}

// An empty until runs through the end of today in the zone: Until is
// the first instant of the day after now's local date. That is checked
// as its definition (on tomorrow, and a nanosecond earlier still today)
// rather than as time.Date(y, m, d+1, 0, 0, 0, 0, loc), which on a day
// the zone skipped midnight (Sao Paulo until 2018) is the evening before.
func TestNewWindowEmptyUntil(t *testing.T) {
	rapid.Check(t, func(t *rapid.T) {
		c := genWindowCase().Draw(t, "case")
		loc, _ := time.LoadLocation(c.tz)
		today := c.now.In(loc)
		// Noon is never skipped, so its date is tomorrow's.
		tomorrow := time.Date(today.Year(), today.Month(), today.Day()+1, 12, 0, 0, 0, loc)
		c.since = tomorrow.AddDate(0, 0, -rapid.IntRange(1, 100).Draw(t, "back")).Format(dateLayout)
		c.until = ""
		w, err := c.build()
		if err != nil {
			t.Fatal(err)
		}
		if got := w.Until.In(loc).Format(dateLayout); got != tomorrow.Format(dateLayout) {
			t.Fatalf("Until = %v, on %s; want the start of %s", w.Until, got, tomorrow.Format(dateLayout))
		}
		if got := w.Until.Add(-time.Nanosecond).In(loc).Format(dateLayout); got != today.Format(dateLayout) {
			t.Fatalf("Until = %v is not the first instant of %s: a nanosecond earlier is %s", w.Until, tomorrow.Format(dateLayout), got)
		}
	})
}

// A since that is not before until, or a negative overlap, is refused.
func TestNewWindowRefusesEmpty(t *testing.T) {
	rapid.Check(t, func(t *rapid.T) {
		c := genWindowCase().Draw(t, "case")
		c.since, c.until = c.until, c.since
		if rapid.Bool().Draw(t, "equal") {
			c.until = c.since
		}
		if _, err := c.build(); err == nil {
			t.Fatalf("accepted %+v", c)
		}
		c.overlap = -rapid.IntRange(1, 5).Draw(t, "neg")
		if _, err := c.build(); err == nil {
			t.Fatalf("accepted negative overlap %+v", c)
		}
	})
}

// TimeFromTS is ok exactly when the text before the first dot is a
// base-10 int64, and then returns that many Unix seconds whatever
// follows the dot.
func TestTimeFromTSAgreesWithReference(t *testing.T) {
	rapid.Check(t, func(t *rapid.T) {
		ts := rapid.OneOf(
			proptest.TS(),
			rapid.StringMatching(`[+-]?[0-9]{0,22}(\.[0-9a-z]{0,8})?`),
			rapid.StringMatching(`[ 0-9.a-z]{0,10}`),
		).Draw(t, "ts")
		secs, _, _ := strings.Cut(ts, ".")
		want, err := strconv.ParseInt(secs, 10, 64)
		got, ok := TimeFromTS(ts)
		if ok != (err == nil) {
			t.Fatalf("TimeFromTS(%q) ok = %v, reference err = %v", ts, ok, err)
		}
		if ok && got.Unix() != want {
			t.Fatalf("TimeFromTS(%q) = %d, want %d", ts, got.Unix(), want)
		}
	})
}

func FuzzTimeFromTS(f *testing.F) {
	for _, s := range []string{"", "1700000001.000100", ".5", "x.1", "-1.0", "99999999999999999999.1"} {
		f.Add(s)
	}
	f.Fuzz(func(t *testing.T, s string) {
		if tm, ok := TimeFromTS(s); ok {
			if again, ok2 := TimeFromTS(formatTS(tm)); !ok2 || !again.Equal(tm) {
				t.Fatalf("formatTS round trip of %q failed", s)
			}
		}
	})
}

// Sao Paulo had no 00:00 on 2018-11-04 (DST began at midnight): a
// window starting that day begins at 01:00, not the evening before.
func TestNewWindowSkippedMidnight(t *testing.T) {
	w, err := NewWindow("2018-11-04", "2018-11-06", "America/Sao_Paulo", 1, time.Now())
	if err != nil {
		t.Fatal(err)
	}
	if got, want := w.Since.In(w.Location).Format("2006-01-02 15:04"), "2018-11-04 01:00"; got != want {
		t.Errorf("Since = %s, want %s", got, want)
	}
	if got, want := w.Start.In(w.Location).Format("2006-01-02 15:04"), "2018-11-03 00:00"; got != want {
		t.Errorf("Start = %s, want %s", got, want)
	}
}
