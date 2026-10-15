package messages

import (
	"strings"
	"testing"

	"charm.land/lipgloss/v2"
	"github.com/gammons/slk/internal/mention"
	"github.com/gammons/slk/internal/proptest"
	"github.com/gammons/slk/internal/ui/styles"
	"pgregory.net/rapid"
)

// propMessage is a message from one of three users, about any Slack
// text, with optional reactions, replies and edit mark.
func propMessage(self string) *rapid.Generator[MessageItem] {
	users := []string{self, "U0OTHER001", "U0OTHER002"}
	return rapid.Custom(func(t *rapid.T) MessageItem {
		author := rapid.SampledFrom(users).Draw(t, "author")
		msg := MessageItem{
			TS:       proptest.TS().Draw(t, "ts"),
			UserID:   author,
			UserName: rapid.SampledFrom([]string{"kira", "dave", "a-much-longer-display-name"}).Draw(t, "name"),
			// KNOWN: see wrapInput; an ASCII byte before VS16 wraps a
			// cell too wide.
			Text:       stripASCIIVS16(proptest.SlackText(users, []string{"C0GENERAL1"}).Draw(t, "text")),
			Timestamp:  rapid.SampledFrom([]string{"3:04 PM", "15:04"}).Draw(t, "stamp"),
			ReplyCount: rapid.IntRange(0, 3).Draw(t, "replies"),
			IsEdited:   rapid.Bool().Draw(t, "edited"),
		}
		for range rapid.IntRange(0, 3).Draw(t, "reactions") {
			ApplyReaction(&msg, rapid.SampledFrom(propEmojis).Draw(t, "emoji"), rapid.SampledFrom(users).Draw(t, "reactor"), self, false)
		}
		return msg
	})
}

// RenderMessage then WrapRows, with the theme pinned, gives three
// parallel variants of one message: every unselected and selected row
// is exactly the pane width, the variants and the plain mirror have the
// same height, the plain mirror never holds the border glyph, and the
// mention bar is on the border exactly when the message mentions the
// current user (Mention, which must agree with mention.Classify, and is
// never set on the user's own message).
func TestRenderAndWrapRows(t *testing.T) {
	applyDarkTheme(t)
	const self = "U0SELF0001"
	bar := strings.TrimSuffix(strings.TrimPrefix(fgANSIFor(styles.MentionBar), "\x1b["), "m")
	rapid.Check(t, func(t *rapid.T) {
		msg := propMessage(self).Draw(t, "msg")
		// RenderMessage floors the content width at 20 (render_message.go),
		// so below 24 columns rows are wider than the pane by design.
		width := rapid.IntRange(24, 100).Draw(t, "width")
		m := New([]MessageItem{msg}, "general")
		m.SetCurrentUser(self)
		m.SetUserNames(map[string]string{self: "kira", "U0OTHER001": "dave"})
		opt := RenderOptions{
			ReplyAffordance: rapid.Bool().Draw(t, "affordance"),
			EditedMark:      rapid.Bool().Draw(t, "editedMark"),
			BroadcastLabel:  rapid.Bool().Draw(t, "broadcastLabel"),
			IsSelected:      rapid.Bool().Draw(t, "selected"),
		}
		r := RenderMessage(msg, width, m.renderContext(), opt)
		w := WrapRows(msg, r, width, NewWrapStyles(rapid.Bool().Draw(t, "focused")))

		wantKind := mention.None
		if msg.UserID != self {
			wantKind = mention.Classify(msg.Text, self)
		}
		if r.Mention != wantKind {
			t.Fatalf("Mention = %v, want %v for %q by %s", r.Mention, wantKind, msg.Text, msg.UserID)
		}
		if len(w.Lines) != len(w.Selected) || len(w.Lines) != len(w.Plain) {
			t.Fatalf("heights differ: lines %d, selected %d, plain %d", len(w.Lines), len(w.Selected), len(w.Plain))
		}
		for i := range w.Lines {
			if got := lipgloss.Width(w.Lines[i]); got != width {
				t.Fatalf("row %d is %d wide, want %d: %q", i, got, width, w.Lines[i])
			}
			if got := lipgloss.Width(w.Selected[i]); got != width {
				t.Fatalf("selected row %d is %d wide, want %d: %q", i, got, width, w.Selected[i])
			}
			if strings.Contains(w.Plain[i].Text, "▌") {
				t.Fatalf("plain row %d holds the border: %q", i, w.Plain[i].Text)
			}
			border, _, _ := strings.Cut(w.Lines[i], "▌")
			if got := strings.Contains(border, bar); got != (r.Mention != mention.None) {
				t.Fatalf("row %d: mention bar %v for Mention %v: %q", i, got, r.Mention, w.Lines[i])
			}
		}
	})
}
