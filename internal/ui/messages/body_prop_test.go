package messages

import (
	"strings"
	"testing"

	"pgregory.net/rapid"

	"github.com/gammons/slk/internal/proptest"
)

// Slack's echo of text the user typed carries it: Slack stores the
// mrkdwn with every & escaped (a literal < or > too, but those cannot
// be told from its tokens here, so the property escapes & alone; the
// example tests in internal/ui cover them), and the echo may or may
// not carry the rich_text block it was sent with. A body carries
// itself, and an echo of other text does not.
func TestEchoCarriesSlacksFormOfTheTypedText(t *testing.T) {
	rapid.Check(t, func(rt *rapid.T) {
		typed := rapid.OneOf(proptest.PlainText(), proptest.SlackText([]string{"U1"}, []string{"C1"})).Draw(rt, "typed")
		ours := TypedBody(typed)
		echo := Body{Text: strings.ReplaceAll(ours.Text, "&", "&amp;"), Edited: true}
		if rapid.Bool().Draw(rt, "withBlocks") {
			echo.Blocks = ours.Blocks
		}
		if !EchoCarries(ours, ours) {
			rt.Fatalf("EchoCarries(%q, itself) = false", typed)
		}
		if !EchoCarries(echo, ours) {
			rt.Fatalf("typed %q: echo %q (blocks=%v) does not carry %q", typed, echo.Text, echo.Blocks != nil, ours.Text)
		}
		other := TypedBody(typed + " x")
		if EchoCarries(other, ours) {
			rt.Fatalf("typed %q: echo of %q carries it", typed, other.Text)
		}
	})
}
