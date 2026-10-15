package main

import (
	"testing"
	"time"

	"github.com/gammons/slk/internal/export"
	"github.com/gammons/slk/internal/proptest"
	"github.com/slack-go/slack"
	"pgregory.net/rapid"
)

var propWindow = func() export.Window {
	w, err := export.NewWindow("2026-01-01", "2026-03-01", "UTC", 0, time.Unix(1_780_000_000, 0))
	if err != nil {
		panic(err)
	}
	return w
}()

func genWindowMessages(parentTS string) *rapid.Generator[[]slack.Message] {
	ts := rapid.Custom(func(t *rapid.T) string {
		// Straddles both window edges (1767225600, 1772323200). Always 10
		// digits of seconds and 6 of sequence, as Slack sends them.
		return rapid.SampledFrom([]string{
			"1767225599.000001", "1767225600.000000", "1767225600.000500", "1770000000.000100",
			"1770000000.000200", "1770000001.000100", "1772323199.999999", "1772323200.000000", parentTS,
		}).Draw(t, "ts")
	})
	msg := rapid.Custom(func(t *rapid.T) slack.Message {
		return slack.Message{Msg: slack.Msg{Timestamp: ts.Draw(t, "ts"), Text: proptest.Word().Draw(t, "text")}}
	})
	return rapid.SliceOfN(msg, 0, 12)
}

// windowReplies output is ordered by numeric timestamp, has no
// duplicate timestamps, excludes the parent and anything outside the
// window, is a subset of the input, loses no distinct in-window reply,
// and is idempotent.
func TestWindowRepliesProperties(t *testing.T) {
	rapid.Check(t, func(t *rapid.T) {
		parentTS := "1770000000.000100"
		in := genWindowMessages(parentTS).Draw(t, "msgs")
		out := windowReplies(in, parentTS, propWindow)

		inSet := map[string]bool{}
		for _, m := range in {
			inSet[m.Timestamp] = true
		}
		seen := map[string]bool{}
		for i, m := range out {
			if m.Timestamp == parentTS || !propWindow.Contains(m.Timestamp) || !inSet[m.Timestamp] {
				t.Fatalf("bad member %q", m.Timestamp)
			}
			if seen[m.Timestamp] {
				t.Fatalf("duplicate %q", m.Timestamp)
			}
			seen[m.Timestamp] = true
			if i > 0 && compareNumericTS(out[i-1].Timestamp, m.Timestamp) > 0 {
				t.Fatalf("out of order: %q before %q", out[i-1].Timestamp, m.Timestamp)
			}
		}
		for ts := range inSet {
			if ts != parentTS && propWindow.Contains(ts) && !seen[ts] {
				t.Fatalf("in-window reply %q dropped", ts)
			}
		}
		again := windowReplies(out, parentTS, propWindow)
		if len(again) != len(out) {
			t.Fatalf("not idempotent: %d then %d", len(out), len(again))
		}
		for i := range out {
			if again[i].Timestamp != out[i].Timestamp {
				t.Fatalf("not idempotent at %d", i)
			}
		}
	})
}

// compareNumericTS orders "sec.micro" timestamps numerically.
func compareNumericTS(a, b string) int {
	pa, pb := splitTS(a), splitTS(b)
	for i := range pa {
		if pa[i] != pb[i] {
			if pa[i] < pb[i] {
				return -1
			}
			return 1
		}
	}
	return 0
}

func splitTS(s string) [2]int64 {
	var sec, micro int64
	i := 0
	for ; i < len(s) && s[i] != '.'; i++ {
		sec = sec*10 + int64(s[i]-'0')
	}
	for i++; i < len(s); i++ {
		micro = micro*10 + int64(s[i]-'0')
	}
	return [2]int64{sec, micro}
}

// For Slack-shaped timestamps (10-digit seconds, 6-digit sequence) the
// string order compareMessagesByTS uses is the numeric order.
//
// KNOWN: compareMessagesByTS compares strings, so timestamps with
// different digit widths ("999999999.000100" vs "1000000000.000100")
// would sort wrongly. Real Slack timestamps are 10-digit seconds from
// 2001-09-09 to 2286, with a 6-digit sequence, so this is unreachable
// from Slack today; the generator keeps the widths fixed.
func TestCompareMessagesByTSMatchesNumericOrder(t *testing.T) {
	rapid.Check(t, func(t *rapid.T) {
		a, b := proptest.TS().Draw(t, "a"), proptest.TS().Draw(t, "b")
		got := compareMessagesByTS(slack.Message{Msg: slack.Msg{Timestamp: a}}, slack.Message{Msg: slack.Msg{Timestamp: b}})
		if want := compareNumericTS(a, b); got != want {
			t.Fatalf("compare(%q, %q) = %d, numeric says %d", a, b, got, want)
		}
	})
}

// repliesReachWindow is false without replies, true when latest_reply
// is missing or malformed, and otherwise true exactly when latest_reply
// is at or after the window start.
func TestRepliesReachWindowProperties(t *testing.T) {
	rapid.Check(t, func(t *rapid.T) {
		latest := rapid.SampledFrom([]string{"", "garbage", "1767225599.000001", "1767225600.000000", "1700000000.000100", "1790000000.000100"}).Draw(t, "latest")
		count := rapid.IntRange(0, 3).Draw(t, "count")
		m := slack.Message{Msg: slack.Msg{ReplyCount: count, LatestReply: latest}}
		got := repliesReachWindow(m, propWindow)
		var want bool
		switch {
		case count == 0:
			want = false
		case latest == "" || latest == "garbage":
			want = true
		default:
			sec := splitTS(latest)[0]
			want = sec >= 1767225600
		}
		if got != want {
			t.Fatalf("repliesReachWindow(count=%d, latest=%q) = %v, want %v", count, latest, got, want)
		}
	})
}
