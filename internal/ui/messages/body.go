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

// EditedBody is what Slack stores when the user edits a message to
// typed (the text with mentions already turned into wire tokens), as
// chat.update sends it: the mrkdwn and rich_text block of
// blockkit.FromTyped, marked edited. With no block the message keeps
// no blocks, so its Text is drawn.
func EditedBody(typed string) Body {
	text, blocks := blockkit.FromTyped(typed)
	return Body{Text: text, Blocks: blocks, Edited: true}
}
