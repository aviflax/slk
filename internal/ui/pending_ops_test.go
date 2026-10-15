package ui

import (
	"strings"
	"testing"

	"github.com/gammons/slk/internal/ui/messages"
)

// pendingApp is an App on C1 for the current user USELF, with no
// messages and no thread open.
func pendingApp(t *testing.T) *App {
	t.Helper()
	a := newTestApp(t, withActiveChannel("C1"))
	a.SetCurrentUserID("USELF")
	return a
}

// openThread shows thread threadTS of C1 in the thread panel.
func openThread(a *App, threadTS string) {
	a.threadPanel.SetThread(messages.MessageItem{TS: threadTS, Text: "parent"}, nil, "C1", threadTS)
	a.threadVisible = true
}

// sendTop sends text to C1 and returns the placeholder's TS.
func sendTop(t *testing.T, a *App, text string) string {
	t.Helper()
	a.Update(SendMessageMsg{ChannelID: "C1", Text: text})
	rows := a.messagepane.Messages()
	if len(rows) == 0 || !strings.HasPrefix(rows[len(rows)-1].TS, "local:") {
		t.Fatalf("no placeholder after send: %+v", rows)
	}
	return rows[len(rows)-1].TS
}

func echo(channelID, ts, threadTS, text string) NewMessageMsg {
	return NewMessageMsg{ChannelID: channelID, Message: messages.MessageItem{
		TS: ts, ThreadTS: threadTS, UserID: "USELF", Text: text,
	}}
}

// rowsOf lists a pane's rows as TS=text.
func rowsOf(rows []messages.MessageItem) []string {
	out := make([]string, len(rows))
	for i, r := range rows {
		out[i] = r.TS + "=" + r.Text
	}
	return out
}

func wantRows(t *testing.T, what string, rows []messages.MessageItem, want ...string) {
	t.Helper()
	if got := rowsOf(rows); strings.Join(got, " | ") != strings.Join(want, " | ") {
		t.Fatalf("%s rows = %q, want %q", what, got, want)
	}
}

// An echo that beats the HTTP result takes the placeholder's place at
// once, and the result then replaces it with Slack's response.
func TestPendingSend_EchoBeforeResultShowsOneRow(t *testing.T) {
	a := pendingApp(t)
	local := sendTop(t, a, "hello")

	_, cmd := a.Update(echo("C1", "10.000001", "", "hello (echo)"))
	if cmd != nil {
		t.Errorf("adopted echo returned a command; the HTTP result does the side effects")
	}
	wantRows(t, "after echo", a.messagepane.Messages(), "10.000001=hello (echo)")

	a.Update(MessageSentMsg{ChannelID: "C1", LocalTS: local, Message: messages.MessageItem{TS: "10.000001", UserID: "USELF", Text: "hello"}})
	wantRows(t, "after result", a.messagepane.Messages(), "10.000001=hello")
}

// An echo that arrives after the HTTP result is dropped.
func TestPendingSend_EchoAfterResultIsDropped(t *testing.T) {
	a := pendingApp(t)
	local := sendTop(t, a, "hello")
	a.Update(MessageSentMsg{ChannelID: "C1", LocalTS: local, Message: messages.MessageItem{TS: "10.000001", UserID: "USELF", Text: "hello"}})
	a.Update(echo("C1", "10.000001", "", "hello (echo)"))
	wantRows(t, "after echo", a.messagepane.Messages(), "10.000001=hello")
}

// slk's own file upload is only ever rendered from its echo, so a text
// send pending in the same channel must not swallow it.
func TestPendingSend_UploadEchoRenders(t *testing.T) {
	a := pendingApp(t)
	local := sendTop(t, a, "hello")
	up := echo("C1", "10.000002", "", "a file")
	up.Message.Attachments = []messages.Attachment{{Kind: "file", Name: "notes.txt"}}
	a.Update(up)
	wantRows(t, "after upload echo", a.messagepane.Messages(), local+"=hello", "10.000002=a file")
}

// A message the user sends from another client renders while a send is
// pending anywhere it does not match: another channel, or another
// thread of the same channel.
func TestPendingSend_SelfMessageElsewhereRenders(t *testing.T) {
	t.Run("top-level echo while a reply is pending", func(t *testing.T) {
		a := pendingApp(t)
		openThread(a, "5.000000")
		a.Update(SendThreadReplyMsg{ChannelID: "C1", ThreadTS: "5.000000", Text: "reply"})
		a.Update(echo("C1", "10.000003", "", "from my phone"))
		wantRows(t, "channel", a.messagepane.Messages(), "10.000003=from my phone")
		if r := a.threadPanel.Replies(); len(r) != 1 || !strings.HasPrefix(r[0].TS, "local:") {
			t.Fatalf("thread replies = %q, want the placeholder alone", rowsOf(r))
		}
	})
	t.Run("reply in another thread", func(t *testing.T) {
		a := pendingApp(t)
		openThread(a, "5.000000")
		a.Update(SendThreadReplyMsg{ChannelID: "C1", ThreadTS: "5.000000", Text: "reply"})
		a.threadPanel.SetThread(messages.MessageItem{TS: "6.000000"}, nil, "C1", "6.000000")
		a.Update(echo("C1", "10.000004", "6.000000", "other thread"))
		wantRows(t, "thread 6", a.threadPanel.Replies(), "10.000004=other thread")
	})
	t.Run("another channel", func(t *testing.T) {
		a := pendingApp(t)
		local := sendTop(t, a, "hello")
		a.Update(echo("C2", "10.000005", "", "elsewhere"))
		wantRows(t, "C1", a.messagepane.Messages(), local+"=hello")
		// The C2 echo did not use up the C1 send: C1's own echo is
		// still adopted.
		a.Update(echo("C1", "10.000006", "", "hello"))
		wantRows(t, "C1 after its echo", a.messagepane.Messages(), "10.000006=hello")
	})
}

// Editing a message no longer silences the user's own messages in the
// channel.
func TestPendingSend_EditDoesNotSuppressSelfMessages(t *testing.T) {
	a := pendingApp(t)
	a.Update(EditMessageMsg{ChannelID: "C1", TS: "1.000000", NewText: "edited"})
	a.Update(echo("C1", "10.000007", "", "from my phone"))
	wantRows(t, "channel", a.messagepane.Messages(), "10.000007=from my phone")
}

// When the echo already showed the message, a failed HTTP result means
// the post went through anyway (a timeout after Slack accepted it): the
// row stays and no failure is reported.
func TestPendingSend_FailureAfterAdoptionKeepsRow(t *testing.T) {
	a := pendingApp(t)
	local := sendTop(t, a, "hello")
	a.Update(echo("C1", "10.000008", "", "hello"))
	_, cmd := a.Update(MessageSendFailedMsg{ChannelID: "C1", LocalTS: local, Reason: "timeout"})
	if cmd != nil {
		t.Errorf("failure after adoption returned %#v, want no toast", cmd())
	}
	wantRows(t, "channel", a.messagepane.Messages(), "10.000008=hello")
}

// Two quick sends are adopted by their echoes in send order, whichever
// order their HTTP results come back in.
func TestPendingSend_TwoSendsAdoptInOrder(t *testing.T) {
	a := pendingApp(t)
	first := sendTop(t, a, "one")
	second := sendTop(t, a, "two")
	a.Update(echo("C1", "10.000001", "", "one"))
	wantRows(t, "after first echo", a.messagepane.Messages(), "10.000001=one", second+"=two")
	a.Update(echo("C1", "10.000002", "", "two"))
	wantRows(t, "after second echo", a.messagepane.Messages(), "10.000001=one", "10.000002=two")

	a.Update(MessageSentMsg{ChannelID: "C1", LocalTS: second, Message: messages.MessageItem{TS: "10.000002", Text: "two!"}})
	a.Update(MessageSentMsg{ChannelID: "C1", LocalTS: first, Message: messages.MessageItem{TS: "10.000001", Text: "one!"}})
	wantRows(t, "after results", a.messagepane.Messages(), "10.000001=one!", "10.000002=two!")
}

// A reply's echo is adopted in the thread panel, and a broadcast's in
// the channel feed as well.
func TestPendingReply_EchoAdoptedInEveryPane(t *testing.T) {
	a := pendingApp(t)
	openThread(a, "5.000000")
	a.Update(SendThreadReplyMsg{ChannelID: "C1", ThreadTS: "5.000000", Text: "reply", Broadcast: true})
	e := echo("C1", "10.000009", "5.000000", "reply")
	e.Message.Subtype = "thread_broadcast"
	a.Update(e)
	wantRows(t, "thread", a.threadPanel.Replies(), "10.000009=reply")
	wantRows(t, "channel", a.messagepane.Messages(), "10.000009=reply")
}

// A sent result without a TS cannot settle the placeholder, so it
// removes it rather than leaving a row that never resolves; a row an
// echo already settled is the posted message and stays.
func TestPendingSend_EmptyTSResult(t *testing.T) {
	t.Run("channel", func(t *testing.T) {
		a := pendingApp(t)
		local := sendTop(t, a, "hello")
		a.Update(MessageSentMsg{ChannelID: "C1", LocalTS: local})
		wantRows(t, "channel", a.messagepane.Messages())
	})
	t.Run("broadcast reply", func(t *testing.T) {
		a := pendingApp(t)
		openThread(a, "5.000000")
		a.Update(SendThreadReplyMsg{ChannelID: "C1", ThreadTS: "5.000000", Text: "reply", Broadcast: true})
		local := a.threadPanel.Replies()[0].TS
		a.Update(ThreadReplySentMsg{ChannelID: "C1", ThreadTS: "5.000000", LocalTS: local, Broadcast: true})
		wantRows(t, "thread", a.threadPanel.Replies())
		wantRows(t, "channel", a.messagepane.Messages())
	})
	t.Run("after adoption", func(t *testing.T) {
		a := pendingApp(t)
		local := sendTop(t, a, "hello")
		a.Update(echo("C1", "10.000001", "", "hello"))
		a.Update(MessageSentMsg{ChannelID: "C1", LocalTS: local})
		wantRows(t, "channel", a.messagepane.Messages(), "10.000001=hello")
	})
}
