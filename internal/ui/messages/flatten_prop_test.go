package messages

import (
	"fmt"
	"strings"
	"testing"

	"github.com/gammons/slk/internal/proptest"
	"pgregory.net/rapid"
)

// wellFormedEntities is Slack wire text built only from well-formed
// entity tokens and words that hold no '<', '>', '&' or '|', so every
// '<' in it opens a token FlattenMrkdwn knows.
func wellFormedEntities() *rapid.Generator[string] {
	label := rapid.StringMatching(`[a-zA-Z0-9 ._-]{1,10}`)
	return rapid.Custom(func(t *rapid.T) string {
		var b strings.Builder
		for i := range rapid.IntRange(0, 10).Draw(t, "pieces") {
			if i > 0 {
				b.WriteString(rapid.SampledFrom([]string{" ", "\n", ""}).Draw(t, "sep"))
			}
			switch rapid.IntRange(0, 9).Draw(t, "kind") {
			case 0:
				b.WriteString("<@" + proptest.ID("U").Draw(t, "user") + ">")
			case 1:
				b.WriteString("<@" + proptest.ID("U").Draw(t, "user") + "|" + label.Draw(t, "ulabel") + ">")
			case 2:
				b.WriteString("<#" + proptest.ID("C").Draw(t, "channel") + rapid.SampledFrom([]string{"", "|general"}).Draw(t, "clabel") + ">")
			case 3:
				b.WriteString(rapid.SampledFrom([]string{"<!here>", "<!channel>", "<!everyone>", "<!here|@here>"}).Draw(t, "broadcast"))
			case 4:
				b.WriteString("<!subteam^" + proptest.ID("S").Draw(t, "group") + rapid.SampledFrom([]string{"", "|@eng"}).Draw(t, "glabel") + ">")
			case 5:
				url := rapid.SampledFrom([]string{"https://", "http://", "mailto:"}).Draw(t, "scheme") + rapid.StringMatching(`[a-z0-9./]{1,12}`).Draw(t, "rest")
				if rapid.Bool().Draw(t, "labelled") {
					b.WriteString("<" + url + "|" + label.Draw(t, "ltext") + ">")
				} else {
					b.WriteString("<" + url + ">")
				}
			case 6:
				fmt.Fprintf(&b, "<!date^%d^{date_num}|%s>", rapid.Int64Range(0, 2_000_000_000).Draw(t, "ts"), label.Draw(t, "fallback"))
			case 7:
				b.WriteString(rapid.SampledFrom([]string{"&amp;", "&gt;"}).Draw(t, "entity"))
			default:
				b.WriteString(label.Draw(t, "word"))
			}
		}
		return b.String()
	})
}

// Every well-formed entity token is flattened: no '<' survives (the
// generator emits no "&lt;", the only other source of one), so neither
// does any "<@", "<#" or "<!".
func TestFlattenMrkdwnLeavesNoTokens(t *testing.T) {
	rapid.Check(t, func(t *rapid.T) {
		text := wellFormedEntities().Draw(t, "text")
		resolve := func(id string) (string, bool) { return "name-" + id, rapid.Bool().Draw(t, "known") }
		out := FlattenMrkdwnWithUserGroups(text, resolve, resolve, map[string]string{})
		if strings.Contains(out, "<") {
			t.Fatalf("FlattenMrkdwn(%q) = %q keeps a token", text, out)
		}
	})
}

// Text with no '<' and no '&' has nothing to flatten and comes back
// unchanged.
func TestFlattenMrkdwnIdentityWithoutEntities(t *testing.T) {
	rapid.Check(t, func(t *rapid.T) {
		text := strings.NewReplacer("<", "", "&", "").Replace(proptest.PlainText().Draw(t, "text"))
		if out := FlattenMrkdwn(text, nil, nil); out != text {
			t.Fatalf("FlattenMrkdwn(%q) = %q", text, out)
		}
	})
}

// FuzzFlattenMrkdwn feeds arbitrary bytes, with and without resolvers:
// FlattenMrkdwn must not panic.
func FuzzFlattenMrkdwn(f *testing.F) {
	for _, s := range []string{"", "<@U1>", "<@U1|bob>", "<#C1|gen>", "<!here>", "<!subteam^S1>", "<https://x|y>", "<!date^1700000000^{date} {time}|x>", "<!date^>", "<!date^x^|>", "&amp;lt;"} {
		f.Add(s)
	}
	f.Fuzz(func(t *testing.T, s string) {
		resolve := func(id string) (string, bool) { return "", len(id)%2 == 0 }
		_ = FlattenMrkdwn(s, nil, nil)
		_ = FlattenMrkdwnWithUserGroups(s, resolve, resolve, map[string]string{"S1": "eng"})
	})
}
