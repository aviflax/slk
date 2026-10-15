package proptest

import (
	"strings"
	"time"
	_ "time/tzdata" // Zone must not depend on the host's zoneinfo

	"pgregory.net/rapid"
)

// zones are the awkward ones: half- and quarter-hour offsets, a
// half-hour DST shift (Lord Howe), both sides of the date line, and a
// zone whose DST once started at midnight (Sao Paulo, so some days had
// no 00:00).
var zones = []string{
	"UTC", "America/New_York", "Europe/London", "Asia/Kolkata", "Asia/Kathmandu",
	"Australia/Lord_Howe", "Pacific/Auckland", "America/St_Johns",
	"Pacific/Kiritimati", "Pacific/Pago_Pago", "America/Sao_Paulo",
}

// Zone draws a time zone from a fixed set of awkward ones. The tz
// database is embedded, so it never needs the host's.
func Zone() *rapid.Generator[*time.Location] {
	return rapid.Custom(func(t *rapid.T) *time.Location {
		loc, err := time.LoadLocation(rapid.SampledFrom(zones).Draw(t, "zone"))
		if err != nil {
			panic(err) // embedded tzdata: cannot happen
		}
		return loc
	})
}

// NoVS16 removes every U+FE0F. Terminals, lipgloss and x/ansi disagree
// about the width of a base+VS16 pair (AGENTS.md, #301), so a width
// property that is not about that pair draws its text through this.
func NoVS16(s string) string { return strings.ReplaceAll(s, "️", "") }
