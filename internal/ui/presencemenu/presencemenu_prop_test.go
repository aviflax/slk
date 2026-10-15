package presencemenu

import (
	"testing"
	"time"

	"github.com/gammons/slk/internal/proptest"
	"pgregory.net/rapid"
)

// minutesUntilTomorrowMorning, for any instant in a zone with or without
// daylight saving: the answer is at least 1, now plus that many minutes
// lands within a minute before 09:00 local on a weekday that is a later
// calendar day, and no earlier weekday morning was skipped.
func TestMinutesUntilTomorrowMorning(t *testing.T) {
	rapid.Check(t, func(t *rapid.T) {
		loc := proptest.Zone().Draw(t, "zone")
		now := time.Unix(rapid.Int64Range(1_600_000_000, 1_900_000_000).Draw(t, "unix"), 0).In(loc)
		mins := minutesUntilTomorrowMorning(now)
		if mins < 1 {
			t.Fatalf("minutes = %d", mins)
		}
		// Reference: the first weekday after today, at 09:00 local.
		y, mo, d := now.Date()
		var cand time.Time
		for day := 1; ; day++ {
			cand = time.Date(y, mo, d+day, 9, 0, 0, 0, loc)
			if wd := cand.Weekday(); wd != time.Saturday && wd != time.Sunday {
				break
			}
		}
		if h, m, s := cand.Clock(); h != 9 || m != 0 || s != 0 {
			t.Skip("09:00 does not exist on that day in this zone")
		}
		gap := cand.Sub(now)
		if want := int(gap / time.Minute); mins != want {
			t.Fatalf("now %v: minutes = %d, want %d (target %v)", now, mins, want, cand)
		}
		// Rounding down never overshoots the morning.
		if now.Add(time.Duration(mins) * time.Minute).After(cand) {
			t.Fatalf("overshoots %v", cand)
		}
		if gap > 82*time.Hour {
			t.Fatalf("wait of %v is longer than Friday midnight to Monday 9:00", gap)
		}
	})
}
