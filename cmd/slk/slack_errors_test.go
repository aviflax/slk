package main

import (
	"errors"
	"fmt"
	"testing"

	"github.com/slack-go/slack"
)

// message_not_found is recognised however the client wrapped it, and
// nothing else is.
func TestIsMessageNotFound(t *testing.T) {
	for _, tc := range []struct {
		err  error
		want bool
	}{
		{fmt.Errorf("deleting message: %w", slack.SlackErrorResponse{Err: "message_not_found"}), true},
		{slack.SlackErrorResponse{Err: "message_not_found"}, true},
		{fmt.Errorf("editing message: %w", slack.SlackErrorResponse{Err: "cant_update_message"}), false},
		{errors.New("message_not_found"), false},
		{nil, false},
	} {
		if got := isMessageNotFound(tc.err); got != tc.want {
			t.Errorf("isMessageNotFound(%v) = %v, want %v", tc.err, got, tc.want)
		}
	}
}
