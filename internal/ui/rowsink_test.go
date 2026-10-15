package ui

import (
	"testing"

	tea "charm.land/bubbletea/v2"

	"github.com/gammons/slk/internal/core"
	"github.com/gammons/slk/internal/ids"
	"github.com/gammons/slk/internal/ui/messages"
)

// stampLocalTS puts the placeholder id on every send result, adds
// Broadcast only to a failed reply (a sent reply's comes from the
// service), and passes anything else through untouched.
func TestStampLocalTS(t *testing.T) {
	reply := pendingInsert{localTS: "local:7", channelID: "C1", threadTS: "5.0", broadcast: true}
	run := func(result tea.Msg) tea.Msg {
		return stampLocalTS(reply, func() tea.Msg { return result })()
	}
	if got := run(MessageSentMsg{}).(MessageSentMsg); got.LocalTS != "local:7" {
		t.Errorf("MessageSentMsg.LocalTS = %q", got.LocalTS)
	}
	if got := run(MessageSendFailedMsg{}).(MessageSendFailedMsg); got.LocalTS != "local:7" {
		t.Errorf("MessageSendFailedMsg.LocalTS = %q", got.LocalTS)
	}
	if got := run(ThreadReplySentMsg{}).(ThreadReplySentMsg); got.LocalTS != "local:7" || got.Broadcast {
		t.Errorf("ThreadReplySentMsg = %+v, want LocalTS stamped and Broadcast left alone", got)
	}
	if got := run(ThreadReplySendFailedMsg{}).(ThreadReplySendFailedMsg); got.LocalTS != "local:7" || !got.Broadcast {
		t.Errorf("ThreadReplySendFailedMsg = %+v, want LocalTS and Broadcast stamped", got)
	}
	if got := run(ToastMsg{Text: "x"}); got != (ToastMsg{Text: "x"}) {
		t.Errorf("other result = %#v, want it unchanged", got)
	}
}

// A service with no active workspace returns nil. For a send that is a
// failure: without one the pending insert would never end and its
// placeholder would never leave the screen.
func TestStampLocalTS_NilIsFailure(t *testing.T) {
	top := pendingInsert{localTS: "local:1", channelID: "C1"}
	got, ok := stampLocalTS(top, func() tea.Msg { return nil })().(MessageSendFailedMsg)
	if !ok || got != (MessageSendFailedMsg{ChannelID: "C1", LocalTS: "local:1", Reason: noWorkspaceReason}) {
		t.Errorf("top-level nil result = %#v, want MessageSendFailedMsg", got)
	}
	reply := pendingInsert{localTS: "local:2", channelID: "C1", threadTS: "5.0", broadcast: true}
	gotR, ok := stampLocalTS(reply, func() tea.Msg { return nil })().(ThreadReplySendFailedMsg)
	want := ThreadReplySendFailedMsg{ChannelID: "C1", ThreadTS: "5.0", LocalTS: "local:2", Reason: noWorkspaceReason, Broadcast: true}
	if !ok || gotR != want {
		t.Errorf("reply nil result = %#v, want %#v", gotR, want)
	}

	// End to end, through a service that answers nil: the placeholder
	// goes and the text is given back.
	a := typedApp(t)
	a.SetMessageService(core.NewMessageService(core.MessageServiceFuncs{
		Send: func(ids.ChannelID, string) core.Msg { return nil },
	}))
	a.mode = ModeInsert
	a.focusedPanel = PanelMessages
	a.compose.SetValue("hi")
	cmd := pressEnter(a)
	a.Update(deliveredMsgs[MessageSendFailedMsg](t, cmd)[0])
	wantRows(t, "channel", a.messagepane.Messages(), "1.0=msg-1", "2.0=msg-2", "3.0=msg-3")
	if got := a.compose.Value(); got != "hi" {
		t.Errorf("composer = %q, want the typed text back", got)
	}
	if n := len(a.pendingOps.inserts); n != 0 {
		t.Errorf("%d pending inserts left, want 0", n)
	}
}

// localRow is the user's own row under the placeholder id, drawn from
// the typed body: its text converted to mrkdwn, and its rich_text block.
func TestLocalRow(t *testing.T) {
	a := newTestApp(t)
	a.currentUserID = "U-self"
	a.userNames = map[string]string{"U-self": "me"}
	a.nowTimestampFormatter = func() string { return "9:41 AM" }
	got := a.localRow("local:3", messages.TypedBody("**hi**"), "1700000000.000100", "thread_broadcast")
	want := messages.MessageItem{
		TS: "local:3", UserID: "U-self", UserName: "me", Text: "*hi*",
		Timestamp: "9:41 AM", ThreadTS: "1700000000.000100", Subtype: "thread_broadcast",
	}
	if got.TS != want.TS || got.UserID != want.UserID || got.UserName != want.UserName || got.Text != want.Text ||
		got.Timestamp != want.Timestamp || got.ThreadTS != want.ThreadTS || got.Subtype != want.Subtype ||
		len(got.Blocks) == 0 {
		t.Errorf("localRow = %+v, want %+v", got, want)
	}
}

// A top-level row never reaches the thread panel; a reply reaches the
// channel windows only when broadcast, and the thread panel only when it
// is visible on that thread.
func TestSinksForRouting(t *testing.T) {
	a := newTestApp(t, withActiveChannel("C1"))
	if s := a.sinksFor("C1"); s.thread != nil || len(s.channel) != 1 {
		t.Fatalf("sinksFor(C1) = %+v, want the one channel window and no thread", s)
	}
	if s := a.replySinksFor("C1", "1.000001", false); s.thread != nil || len(s.channel) != 0 {
		t.Fatalf("plain reply, no thread open = %+v, want no sinks", s)
	}
	if s := a.replySinksFor("C1", "1.000001", true); s.thread != nil || len(s.channel) != 1 {
		t.Fatalf("broadcast reply, no thread open = %+v, want the channel window", s)
	}
	a.threadPanel.SetThread(messages.MessageItem{TS: "1.000001"}, nil, "C1", "1.000001")
	a.threadVisible = true
	if s := a.replySinksFor("C1", "1.000001", false); s.thread == nil || len(s.channel) != 0 {
		t.Fatalf("plain reply, thread open = %+v, want the thread only", s)
	}
	if s := a.replySinksFor("C1", "2.000002", false); s.thread != nil {
		t.Fatalf("reply to another thread reached the open thread: %+v", s)
	}
	if s := a.replySinksFor("C2", "1.000001", false); s.thread != nil {
		t.Fatalf("reply in another channel reached the open thread: %+v", s)
	}
}
