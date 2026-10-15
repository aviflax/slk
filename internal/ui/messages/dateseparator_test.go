// internal/ui/messages/dateseparator_test.go
package messages

import (
	"strconv"
	"testing"
	"time"

	"github.com/gammons/slk/internal/tztest"
)

// FormatDateSeparator must agree with DateFromTS, which derives its date
// string from the local day. Running the pair under a zone east of UTC
// used to label yesterday's messages "Today", because the parsed date
// (UTC midnight) was subtracted from a local midnight.
//
// Each zone runs in a child process started with TZ set (tztest.Each):
// setting time.Local here would race with every time.Now in other
// goroutines.
func TestFormatDateSeparatorAcrossTimezones(t *testing.T) {
	zones := []string{"UTC", "Europe/Kyiv", "Europe/Berlin", "Asia/Tokyo", "America/New_York", "Pacific/Kiritimati"}
	tztest.Each(t, zones, func(t *testing.T) string {
		now := time.Now()
		cases := []struct {
			offsetDays int
			want       string
		}{
			{0, "Today"},
			{-1, "Yesterday"},
			{-2, now.AddDate(0, 0, -2).Format("Monday")},
			{-6, now.AddDate(0, 0, -6).Format("Monday")},
			{-7, now.AddDate(0, 0, -7).Format("Monday, January 2, 2006")},
		}
		for _, tc := range cases {
			// Mid-day so the timestamp is unambiguously on its day.
			ts := time.Date(now.Year(), now.Month(), now.Day(), 12, 0, 0, 0, time.Local).
				AddDate(0, 0, tc.offsetDays)
			got := FormatDateSeparator(DateFromTS(formatSlackTS(ts)))
			if got != tc.want {
				t.Errorf("%s, offset %d days: got %q, want %q", time.Local, tc.offsetDays, got, tc.want)
			}
		}
		return ""
	})
}

func formatSlackTS(t time.Time) string {
	return strconv.FormatInt(t.Unix(), 10) + ".000000"
}
