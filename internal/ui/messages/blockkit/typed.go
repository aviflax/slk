package blockkit

import "github.com/gammons/slk/internal/slack/mrkdwn"

// FromTyped converts text the user typed (CommonMark, with Slack wire
// tokens such as <@U…>) the way chat.postMessage and chat.update send
// it: mrkdwn.Convert's mrkdwn fallback, and its rich_text block as this
// package's blocks. blocks is nil when Convert makes no block (blank
// input).
func FromTyped(input string) (text string, blocks []Block) {
	text, rt := mrkdwn.Convert(input)
	if rt == nil {
		return text, nil
	}
	return text, []Block{parseOne(rt)}
}
