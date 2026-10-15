package ui

import (
	"strings"
	"testing"

	"github.com/gammons/slk/internal/ui/mentionpicker"
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

// sendFromComposer types text into the composer of panel (the channel
// composer for C1, or the thread composer for thread 5.000000 of C1),
// presses Enter and applies the send it produces. It returns the
// placeholder id.
func sendFromComposer(t *testing.T, a *App, panel Panel, text string) (SendMessageMsg, SendThreadReplyMsg, string) {
	t.Helper()
	a.mode = ModeInsert
	a.focusedPanel = panel
	if panel == PanelThread {
		a.threadCompose.SetValue(text)
		got := deliveredMsgs[SendThreadReplyMsg](t, pressEnter(a))[0]
		a.Update(got)
		return SendMessageMsg{}, got, a.threadPanel.Replies()[len(a.threadPanel.Replies())-1].TS
	}
	a.compose.SetValue(text)
	got := deliveredMsgs[SendMessageMsg](t, pressEnter(a))[0]
	a.Update(got)
	return got, SendThreadReplyMsg{}, a.messagepane.Messages()[len(a.messagepane.Messages())-1].TS
}

// typedApp is a slashApp whose composers know user alice (U9), with
// thread 5.000000 of C1 open and bound to the thread composer.
func typedApp(t *testing.T) *App {
	t.Helper()
	a, _ := slashApp(t, nil)
	a.SetCurrentUserID("USELF")
	users := []mentionpicker.User{{ID: "U9", DisplayName: "alice"}}
	a.compose.SetUsers(users)
	a.threadCompose.SetUsers(users)
	openThread(a, "5.000000")
	a.threadCompose.SetDraftContext("T1", "C1", "5.000000")
	return a
}

// A send that fails gives the text back to the composer it was typed
// in, as typed (with @names, not the <@U…> tokens sent to Slack), and
// still reports the failure.
func TestPendingSend_FailureGivesTypedTextBack(t *testing.T) {
	t.Run("channel", func(t *testing.T) {
		a := typedApp(t)
		sent, _, local := sendFromComposer(t, a, PanelMessages, "hi @alice")
		if sent.Typed != "hi @alice" || sent.TeamID != "T1" || !strings.Contains(sent.Text, "<@U9>") {
			t.Fatalf("send = %+v, want Typed as typed, TeamID T1 and the mention translated in Text", sent)
		}
		if a.compose.Value() != "" {
			t.Fatalf("composer = %q after Enter, want it reset", a.compose.Value())
		}
		_, cmd := a.Update(MessageSendFailedMsg{ChannelID: "C1", LocalTS: local, Reason: "boom"})
		if cmd == nil {
			t.Error("failure reported nothing")
		}
		if got := a.compose.Value(); got != "hi @alice" {
			t.Errorf("composer = %q, want the typed text back", got)
		}
	})
	t.Run("thread", func(t *testing.T) {
		a := typedApp(t)
		_, sent, local := sendFromComposer(t, a, PanelThread, "hi @alice")
		if sent.Typed != "hi @alice" || sent.TeamID != "T1" {
			t.Fatalf("reply = %+v, want Typed and TeamID", sent)
		}
		_, cmd := a.Update(ThreadReplySendFailedMsg{ChannelID: "C1", ThreadTS: "5.000000", LocalTS: local, Reason: "boom"})
		if cmd == nil {
			t.Error("failure reported nothing")
		}
		if got := a.threadCompose.Value(); got != "hi @alice" {
			t.Errorf("thread composer = %q, want the typed text back", got)
		}
		if got := a.compose.Value(); got != "" {
			t.Errorf("channel composer = %q, want it untouched", got)
		}
	})
	t.Run("newer draft wins", func(t *testing.T) {
		a := typedApp(t)
		_, _, local := sendFromComposer(t, a, PanelMessages, "first")
		a.compose.SetValue("second")
		a.Update(MessageSendFailedMsg{ChannelID: "C1", LocalTS: local, Reason: "boom"})
		if got := a.compose.Value(); got != "second" {
			t.Errorf("composer = %q, want the newer text kept", got)
		}
	})
	t.Run("not after adoption", func(t *testing.T) {
		a := typedApp(t)
		_, _, local := sendFromComposer(t, a, PanelMessages, "posted")
		a.Update(echo("C1", "10.000001", "", "posted"))
		a.Update(MessageSendFailedMsg{ChannelID: "C1", LocalTS: local, Reason: "timeout"})
		if got := a.compose.Value(); got != "" {
			t.Errorf("composer = %q, want nothing given back for a posted message", got)
		}
	})
}
