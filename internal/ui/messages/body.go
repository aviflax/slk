package messages

import (
	"slices"

	"github.com/gammons/slk/internal/ui/messages/blockkit"
)

// Body is the part of a message an edit replaces: its mrkdwn text, its
// Block Kit blocks and whether it shows as edited. The pane draws the
// body from a rich_text block when there is one (MessageTextSource), so
// a change to Text alone does not show on a message that carries one,
// which is every message the user typed in slk or the web client.
type Body struct {
	Text   string
	Blocks []blockkit.Block
	Edited bool
}

// BodyOf is msg's body as it stands.
func BodyOf(msg MessageItem) Body {
	return Body{Text: msg.Text, Blocks: msg.Blocks, Edited: msg.IsEdited}
}

// Apply shows b on msg: its text, its blocks (in place of all of msg's)
// and the edited mark.
func (b Body) Apply(msg *MessageItem) {
	msg.Text, msg.Blocks, msg.IsEdited = b.Text, slices.Clone(b.Blocks), b.Edited
}

// TypedBody is what Slack stores for text the user typed (with mentions
// already turned into wire tokens), as chat.postMessage and chat.update
// both send it: the mrkdwn and rich_text block of blockkit.FromTyped.
// With no block the message has no blocks, so its Text is drawn. The
// send placeholder and the sent message are drawn from it, so they
// look as the echo and later loads will; an edit marks it Edited.
func TypedBody(typed string) Body {
	text, blocks := blockkit.FromTyped(typed)
	return Body{Text: text, Blocks: blocks}
}
