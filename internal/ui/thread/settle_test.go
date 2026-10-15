package thread

import (
	"testing"

	"github.com/gammons/slk/internal/ui/messages"
)

// When the echo of a sent reply is already in the thread, settling the
// placeholder onto the same TS shows the reply once, with the sent
// message's text, and keeps the cursor on the reply it was on.
func TestSettleAfterEchoShowsReplyOnce(t *testing.T) {
	m := New()
	m.SetThread(messages.MessageItem{TS: "100.000000", Text: "parent"}, []messages.MessageItem{
		{TS: "101.000001", Text: "earlier"},
		{TS: "local:1", Text: "pending"},
		{TS: "101.000002", Text: "echo"},
	}, "C1", "100.000000")
	m.selected = 2 // the echo

	m.Settle("local:1", messages.MessageItem{TS: "101.000002", Text: "sent"})

	var got []string
	for _, r := range m.replies {
		got = append(got, r.TS+"="+r.Text)
	}
	if len(m.replies) != 2 || m.replies[1].TS != "101.000002" || m.replies[1].Text != "sent" {
		t.Fatalf("replies = %v, want [101.000001=earlier 101.000002=sent]", got)
	}
	if m.selected != 1 {
		t.Fatalf("selected = %d, want 1 (the same reply)", m.selected)
	}
}
