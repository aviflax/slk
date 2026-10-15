package messages

import (
	"slices"
	"strings"

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

// EchoCarries reports whether echo, a body Slack sent back for a
// message (a message_changed), carries sent, a body the user sent it.
// Slack stores mrkdwn with &, < and > escaped as entities outside its
// own <...> tokens (and & inside them too), and keeps the rich_text
// block as it was sent. So echo carries sent when echo's mrkdwn with
// those escapes undone once is sent's, or when both have a rich_text
// block and the blocks draw the same text (RichTextToMrkdwn), whatever
// Slack did to the mrkdwn. Surrounding space and the edited mark are
// ignored. Undoing the escapes on the echo alone keeps text the user
// typed as an entity ("&gt;", stored "&amp;gt;") apart from the
// character.
func EchoCarries(echo, sent Body) bool {
	if strings.TrimSpace(slackUnescape.Replace(echo.Text)) == strings.TrimSpace(sent.Text) {
		return true
	}
	re, eok := richText(echo)
	rs, sok := richText(sent)
	return eok && sok && re == rs
}

// slackUnescape undoes the three entities Slack escapes in mrkdwn, in
// one pass, so "&amp;lt;" becomes "&lt;".
var slackUnescape = strings.NewReplacer("&amp;", "&", "&lt;", "<", "&gt;", ">")

// richText is the text b's rich_text block draws, trimmed; ok is false
// when b has none.
func richText(b Body) (string, bool) {
	for _, blk := range b.Blocks {
		if rt, ok := blk.(blockkit.RichTextBlock); ok {
			if text := blockkit.RichTextToMrkdwn(rt); text != "" {
				return strings.TrimSpace(text), true
			}
		}
	}
	return "", false
}
