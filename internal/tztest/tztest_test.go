package tztest

import (
	"testing"
	"time"
)

// Each runs body under each zone in a child, and the parent gets what
// body returned there, in order.
func TestEachRunsBodyUnderEachZone(t *testing.T) {
	zones := []string{"UTC", "Asia/Tokyo", "Pacific/Kiritimati"}
	got := Each(t, zones, func(t *testing.T) string {
		return time.Local.String()
	})
	for i, zone := range zones {
		if got[i] != zone {
			t.Errorf("zone %d: body saw time.Local = %q, want %q", i, got[i], zone)
		}
	}
}
