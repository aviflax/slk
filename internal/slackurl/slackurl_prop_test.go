package slackurl

import (
	"net/url"
	"regexp"
	"strings"
	"testing"

	"github.com/gammons/slk/internal/proptest"
	"pgregory.net/rapid"
)

var tsShape = regexp.MustCompile(`^\d+\.\d{6}$`)

// permalinkFor builds the archive URL Slack emits for a message: the
// p-value is the ts without its dot.
func permalinkFor(sub, channel, ts string) string {
	return "https://" + sub + ".slack.com/archives/" + channel + "/p" + strings.Replace(ts, ".", "", 1)
}

// Build-then-parse: every permalink made from a subdomain, channel and
// ts parses back to exactly those, whatever thread_ts, cid and other
// query parameters ride along.
func TestParseRoundTrip(t *testing.T) {
	rapid.Check(t, func(t *rapid.T) {
		sub := rapid.StringMatching(`[a-z][a-z0-9-]{0,15}`).Draw(t, "sub")
		ch := proptest.ID("C").Draw(t, "ch")
		ts := proptest.TS().Draw(t, "ts")
		link := permalinkFor(sub, ch, ts)
		var wantThread string
		q := url.Values{}
		if rapid.Bool().Draw(t, "thread") {
			wantThread = proptest.TS().Draw(t, "thread_ts")
			q.Set("thread_ts", wantThread)
		}
		if rapid.Bool().Draw(t, "cid") {
			q.Set("cid", proptest.ID("C").Draw(t, "cid"))
		}
		if len(q) > 0 {
			link += "?" + q.Encode()
		}
		p, ok := Parse(link)
		if !ok {
			t.Fatalf("Parse(%q) not ok", link)
		}
		if p.Subdomain != sub || string(p.ChannelID) != ch || string(p.MessageTS) != ts || string(p.ThreadTS) != wantThread {
			t.Fatalf("Parse(%q) = %+v", link, p)
		}
	})
}

type fataler interface {
	Helper()
	Fatalf(string, ...any)
}

// For any string at all: no panic, and ok implies an https
// *.slack.com link whose MessageTS has the ts shape and whose digits
// are the p-value's digits.
func checkParse(t fataler, raw string) {
	t.Helper()
	p, ok := Parse(raw)
	if !ok {
		return
	}
	u, err := url.Parse(raw)
	if err != nil || u.Scheme != "https" || !strings.HasSuffix(u.Host, ".slack.com") {
		t.Fatalf("ok for %q", raw)
	}
	if !tsShape.MatchString(string(p.MessageTS)) {
		t.Fatalf("MessageTS %q has wrong shape for %q", p.MessageTS, raw)
	}
	if want := "/p" + strings.Replace(string(p.MessageTS), ".", "", 1); !strings.HasSuffix(u.Path, want) {
		t.Fatalf("path %q does not end with %q", u.Path, want)
	}
	if p.Subdomain == "" || p.ChannelID == "" {
		t.Fatalf("empty field in %+v for %q", p, raw)
	}
}

func TestParseOKImpliesShape(t *testing.T) {
	rapid.Check(t, func(t *rapid.T) {
		sub := rapid.StringMatching(`[a-z.-]{0,10}`).Draw(t, "sub")
		scheme := rapid.SampledFrom([]string{"https", "http", "ftp"}).Draw(t, "scheme")
		ch := rapid.StringMatching(`[A-Z0-9]{0,10}`).Draw(t, "ch")
		digits := rapid.StringMatching(`[0-9]{0,20}`).Draw(t, "digits")
		checkParse(t, scheme+"://"+sub+".slack.com/archives/"+ch+"/p"+digits)
	})
}

// The cid query parameter never changes the result.
func TestParseIgnoresCid(t *testing.T) {
	rapid.Check(t, func(t *rapid.T) {
		link := permalinkFor("acme", proptest.ID("C").Draw(t, "ch"), proptest.TS().Draw(t, "ts"))
		a, _ := Parse(link)
		b, ok := Parse(link + "?cid=" + proptest.ID("C").Draw(t, "cid"))
		if !ok || a != b {
			t.Fatalf("cid changed %+v to %+v", a, b)
		}
	})
}

func FuzzParse(f *testing.F) {
	for _, s := range []string{
		"", "https://a.slack.com/archives/C1/p1700000000000100", "https://a.slack.com/archives/C1/p1700000000000100?thread_ts=1.2&cid=C2",
		"http://a.slack.com/archives/C1/p1700000000000100", "https://.slack.com/archives/C1/p1700000000000100", "%zz",
	} {
		f.Add(s)
	}
	f.Fuzz(func(t *testing.T, s string) { checkParse(t, s) })
}
