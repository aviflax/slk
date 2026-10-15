package notify

import (
	"testing"

	"github.com/gammons/slk/internal/proptest"
	"pgregory.net/rapid"
)

func genNotifyCase() *rapid.Generator[struct {
	ctx                        NotifyContext
	channelID, userID, text, t string
}] {
	type c = struct {
		ctx                        NotifyContext
		channelID, userID, text, t string
	}
	return rapid.Custom(func(rt *rapid.T) c {
		self := proptest.ID("U").Draw(rt, "self")
		ch := rapid.SampledFrom([]string{"C1", "C2"}).Draw(rt, "ch")
		user := proptest.ID("U").Draw(rt, "author")
		if rapid.Bool().Draw(rt, "ownMsg") {
			user = self
		}
		return c{
			ctx: NotifyContext{
				CurrentUserID:   self,
				ActiveChannelID: rapid.SampledFrom([]string{"C1", "C2"}).Draw(rt, "active"),
				IsActiveWS:      rapid.Bool().Draw(rt, "activeWS"),
				OnMention:       rapid.Bool().Draw(rt, "onMention"),
				OnDM:            rapid.Bool().Draw(rt, "onDM"),
				OnKeyword:       rapid.SliceOfN(rapid.SampledFrom([]string{"deploy", "URGENT", "tada", "x"}), 0, 3).Draw(rt, "kw"),
				IsDND:           rapid.Bool().Draw(rt, "dnd"),
				IsMuted:         rapid.Bool().Draw(rt, "muted"),
			},
			channelID: ch,
			userID:    user,
			text:      proptest.SlackText([]string{self}, nil).Draw(rt, "text") + rapid.SampledFrom([]string{"", " deploy", " Urgent"}).Draw(rt, "kwText"),
			t:         rapid.SampledFrom([]string{"channel", "dm", "group_dm", "private", "app"}).Draw(rt, "type"),
		}
	})
}

// Own messages, DND, a muted conversation and the channel on screen in
// the active workspace never notify, whatever the triggers say.
func TestShouldNotifySuppressions(t *testing.T) {
	rapid.Check(t, func(t *rapid.T) {
		c := genNotifyCase().Draw(t, "case")
		got := ShouldNotify(c.ctx, c.channelID, c.userID, c.text, c.t)
		switch {
		case c.userID == c.ctx.CurrentUserID, c.ctx.IsDND, c.ctx.IsMuted,
			c.ctx.IsActiveWS && c.channelID == c.ctx.ActiveChannelID:
			if got {
				t.Fatalf("notified despite suppression: %+v", c)
			}
		}
	})
}

// Turning on OnDM or OnMention, or adding keywords, can only turn a
// false answer into true, never the reverse.
func TestShouldNotifyMonotoneInTriggers(t *testing.T) {
	rapid.Check(t, func(t *rapid.T) {
		c := genNotifyCase().Draw(t, "case")
		base := ShouldNotify(c.ctx, c.channelID, c.userID, c.text, c.t)

		more := c.ctx
		more.OnDM = more.OnDM || rapid.Bool().Draw(t, "addDM")
		more.OnMention = more.OnMention || rapid.Bool().Draw(t, "addMention")
		more.OnKeyword = append(append([]string(nil), c.ctx.OnKeyword...),
			rapid.SliceOfN(rapid.SampledFrom([]string{"deploy", "urgent", "x"}), 0, 2).Draw(t, "extraKw")...)
		if base && !ShouldNotify(more, c.channelID, c.userID, c.text, c.t) {
			t.Fatalf("adding triggers turned true to false: %+v -> %+v", c.ctx, more)
		}

		off := c.ctx
		off.OnDM, off.OnMention, off.OnKeyword = false, false, nil
		if ShouldNotify(off, c.channelID, c.userID, c.text, c.t) {
			t.Fatalf("notified with every trigger off: %+v", c)
		}
	})
}

// An empty keyword entry (on_keyword = [""]) is no trigger: with
// mention and DM triggers off, no text notifies.
func TestShouldNotifyEmptyKeywordIsNoTrigger(t *testing.T) {
	rapid.Check(t, func(t *rapid.T) {
		ctx := NotifyContext{CurrentUserID: "USELF", OnKeyword: []string{""}}
		text := proptest.SlackText(nil, nil).Draw(t, "text")
		if ShouldNotify(ctx, "C1", "UOTHER", text, "channel") {
			t.Fatalf("empty keyword notified for %q", text)
		}
	})
}
