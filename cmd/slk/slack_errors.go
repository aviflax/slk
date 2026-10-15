package main

import (
	"errors"

	"github.com/slack-go/slack"
)

// isMessageNotFound reports whether err is Slack's message_not_found:
// the message a chat.update or chat.delete named does not exist (it
// was deleted).
func isMessageNotFound(err error) bool {
	var refused slack.SlackErrorResponse
	return errors.As(err, &refused) && refused.Err == "message_not_found"
}
