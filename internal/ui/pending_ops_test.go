package ui

import (
	"fmt"
	"slices"
	"strings"
	"testing"
	"time"

	tea "charm.land/bubbletea/v2"

	"github.com/gammons/slk/internal/ui/mentionpicker"
	"github.com/gammons/slk/internal/ui/messages"
	"github.com/gammons/slk/internal/ui/statusbar"
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

// rowLog records a pane's rows after each step, to check what the
// user saw on the way and not only where it ended.
type rowLog struct {
	seen []string
}

func (l *rowLog) note(rows []messages.MessageItem) {
	l.seen = append(l.seen, strings.Join(rowsOf(rows), " | "))
}

// never fails when any recorded step showed text.
func (l *rowLog) never(t *testing.T, text string) {
	t.Helper()
	for i, s := range l.seen {
		if strings.Contains(s, "="+text) {
			t.Fatalf("step %d showed %q: %q", i, text, l.seen)
		}
	}
}

// drainMsgs runs cmd once, the members of a batch concurrently, and
// returns every message delivered within d.
func drainMsgs(cmd tea.Cmd, d time.Duration) []tea.Msg {
	ch := make(chan tea.Msg, 64)
	var run func(tea.Cmd)
	run = func(c tea.Cmd) {
		if c == nil {
			return
		}
		m := c()
		if b, ok := m.(tea.BatchMsg); ok {
			for _, x := range b {
				go run(x)
			}
			return
		}
		ch <- m
	}
	go run(cmd)
	var out []tea.Msg
	deadline := time.After(d)
	for {
		select {
		case m := <-ch:
			out = append(out, m)
		case <-deadline:
			return out
		}
	}
}

// sendFailed reports whether cmd delivers the send-failure toast.
func sendFailed(t *testing.T, cmd tea.Cmd) bool {
	t.Helper()
	if cmd == nil {
		return false
	}
	return len(deliveredMsgs[statusbar.SendFailedMsg](t, cmd)) == 1
}

// An echo that beats the HTTP result is held: the placeholder stays
// until the result replaces it with Slack's response, and the echo's
// own text (Slack's normalised version) is never shown.
func TestPendingSend_EchoBeforeResultHeld(t *testing.T) {
	a := pendingApp(t)
	var log rowLog
	local := sendTop(t, a, "hello")
	log.note(a.messagepane.Messages())

	a.Update(echo("C1", "10.000001", "", "hello (echo)"))
	log.note(a.messagepane.Messages())
	wantRows(t, "after echo", a.messagepane.Messages(), local+"=hello")

	a.Update(MessageSentMsg{ChannelID: "C1", LocalTS: local, Message: messages.MessageItem{TS: "10.000001", UserID: "USELF", Text: "hello!"}})
	log.note(a.messagepane.Messages())
	wantRows(t, "after result", a.messagepane.Messages(), "10.000001=hello!")
	log.never(t, "hello (echo)")
}

// An echo that arrives after the HTTP result is dropped.
func TestPendingSend_EchoAfterResultIsDropped(t *testing.T) {
	a := pendingApp(t)
	local := sendTop(t, a, "hello")
	a.Update(MessageSentMsg{ChannelID: "C1", LocalTS: local, Message: messages.MessageItem{TS: "10.000001", UserID: "USELF", Text: "hello"}})
	a.Update(echo("C1", "10.000001", "", "hello (echo)"))
	wantRows(t, "after echo", a.messagepane.Messages(), "10.000001=hello")
}

// slk's own file upload is only ever rendered from its echo. One that
// arrives while a send is pending in the channel is held with the rest
// and shown once the send resolves.
func TestPendingSend_UploadEchoRendersAfterRelease(t *testing.T) {
	a := pendingApp(t)
	local := sendTop(t, a, "hello")
	up := echo("C1", "10.000002", "", "a file")
	up.Message.Attachments = []messages.Attachment{{Kind: "file", Name: "notes.txt"}}
	a.Update(up)
	wantRows(t, "after upload echo", a.messagepane.Messages(), local+"=hello")
	a.Update(MessageSentMsg{ChannelID: "C1", LocalTS: local, Message: messages.MessageItem{TS: "10.000001", UserID: "USELF", Text: "hello"}})
	wantRows(t, "after result", a.messagepane.Messages(), "10.000001=hello", "10.000002=a file")
}

// A message the user sends from another client renders at once while a
// send is pending in another conversation: another channel, or another
// thread of the same channel, or the channel's top level while a reply
// is pending.
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
		a.Update(SendMessageMsg{ChannelID: "C2", Text: "elsewhere pending"})
		a.Update(echo("C1", "10.000005", "", "here"))
		wantRows(t, "C1", a.messagepane.Messages(), "10.000005=here")
	})
}

// A message the user posts from another client while a send is pending
// in the same conversation shows once that send resolves, after it,
// with the thread's reply count right.
func TestPendingSend_SelfMessageSameConversationRendersAfterResult(t *testing.T) {
	t.Run("channel", func(t *testing.T) {
		a := pendingApp(t)
		local := sendTop(t, a, "hello")
		a.Update(echo("C1", "10.000002", "", "from my phone"))
		wantRows(t, "while pending", a.messagepane.Messages(), local+"=hello")
		a.Update(MessageSentMsg{ChannelID: "C1", LocalTS: local, Message: messages.MessageItem{TS: "10.000001", UserID: "USELF", Text: "hello"}})
		wantRows(t, "after result", a.messagepane.Messages(), "10.000001=hello", "10.000002=from my phone")
	})
	t.Run("thread", func(t *testing.T) {
		a := pendingApp(t)
		a.messagepane.SetMessages([]messages.MessageItem{{TS: "5.000000", Text: "parent"}})
		openThread(a, "5.000000")
		a.Update(SendThreadReplyMsg{ChannelID: "C1", ThreadTS: "5.000000", Text: "reply"})
		local := a.threadPanel.Replies()[0].TS
		a.Update(echo("C1", "10.000002", "5.000000", "from my phone"))
		wantRows(t, "while pending", a.threadPanel.Replies(), local+"=reply")
		a.Update(ThreadReplySentMsg{ChannelID: "C1", ThreadTS: "5.000000", LocalTS: local,
			Message: messages.MessageItem{TS: "10.000001", ThreadTS: "5.000000", UserID: "USELF", Text: "reply"}})
		wantRows(t, "after result", a.threadPanel.Replies(), "10.000001=reply", "10.000002=from my phone")
		if n := a.messagepane.Messages()[0].ReplyCount; n != 2 {
			t.Errorf("parent reply count = %d, want 2", n)
		}
	})
}

// Two sends in one channel, their echoes and their results, in every
// order Slack can deliver them, with either send failing or neither,
// and a message from another client among them. The rows end with each
// message once (a failed send's echo, if Slack posted it anyway, shows
// from the echo), a successful send never shows its echo's text, the
// failed send gives its text back and reports it, and no placeholder is
// left.
func TestPendingSend_TwoSendsEveryInterleaving(t *testing.T) {
	events := []string{"echo1", "echo2", "result1", "result2", "phone"}
	for _, fail := range []int{0, 1, 2} {
		permute(events, func(order []string) {
			name := fmt.Sprintf("fail=%d %v", fail, order)
			a := typedApp(t)
			a.messagepane.SetMessages(nil)
			var log rowLog
			a.Update(SendMessageMsg{ChannelID: "C1", Text: "one", TeamID: "T1", Typed: "one typed"})
			a.Update(SendMessageMsg{ChannelID: "C1", Text: "two", TeamID: "T1", Typed: "two typed"})
			rows := a.messagepane.Messages()
			locals := []string{rows[0].TS, rows[1].TS}
			reals := []string{"10.000001", "10.000002"}
			failed := false
			for _, ev := range order {
				var cmd tea.Cmd
				switch ev {
				case "echo1", "echo2":
					i := int(ev[4] - '1')
					_, cmd = a.Update(echo("C1", reals[i], "", fmt.Sprintf("echo%d text", i+1)))
				case "phone":
					_, cmd = a.Update(echo("C1", "10.000003", "", "phone"))
				case "result1", "result2":
					i := int(ev[6] - '1')
					if fail == i+1 {
						_, cmd = a.Update(MessageSendFailedMsg{ChannelID: "C1", LocalTS: locals[i], Reason: "timeout"})
						failed = sendFailed(t, cmd)
					} else {
						_, cmd = a.Update(MessageSentMsg{ChannelID: "C1", LocalTS: locals[i],
							Message: messages.MessageItem{TS: reals[i], UserID: "USELF", Text: fmt.Sprintf("sent%d", i+1)}})
					}
				}
				_ = cmd
				log.note(a.messagepane.Messages())
			}
			want := map[string]bool{"10.000003=phone": true}
			for i, real := range reals {
				if fail == i+1 {
					want[real+"=echo"+fmt.Sprint(i+1)+" text"] = true
				} else {
					want[real+"=sent"+fmt.Sprint(i+1)] = true
					log.never(t, fmt.Sprintf("echo%d text", i+1))
				}
			}
			got := rowsOf(a.messagepane.Messages())
			if len(got) != len(want) {
				t.Fatalf("%s: rows %q, want %d rows %v", name, got, len(want), want)
			}
			for _, r := range got {
				if !want[r] {
					t.Fatalf("%s: rows %q, want %v", name, got, want)
				}
			}
			// In TS order, whatever order they arrived in.
			if !slices.IsSorted(got) {
				t.Fatalf("%s: rows %q are not in TS order", name, got)
			}
			for i, s := range log.seen {
				if err := paneOrderErr(s); err != "" {
					t.Fatalf("%s: step %d: %s", name, i, err)
				}
			}
			if fail != 0 {
				if !failed {
					t.Fatalf("%s: the failure was not reported", name)
				}
				if v := a.compose.Value(); v != []string{"one typed", "two typed"}[fail-1] {
					t.Fatalf("%s: composer = %q, want the failed text back", name, v)
				}
			}
		})
	}
}

// paneOrderErr checks a rowLog step: the rows Slack knows in TS order,
// placeholders after them.
func paneOrderErr(step string) string {
	if step == "" {
		return ""
	}
	last, local := "", false
	for _, r := range strings.Split(step, " | ") {
		ts, _, _ := strings.Cut(r, "=")
		if messages.IsLocalTS(ts) {
			local = true
			continue
		}
		if local {
			return "a row after a placeholder: " + step
		}
		if last != "" && messages.CompareTS(last, ts) >= 0 {
			return "rows out of TS order: " + step
		}
		last = ts
	}
	return ""
}

// A message held while the user's send was pending is released in TS
// order among the messages shown meanwhile, not appended at the end,
// and a settled send moves to its TS too. The cursor stays on the
// message it was on through both.
func TestPendingSend_ReleasedEchoLandsInTSOrder(t *testing.T) {
	t.Run("channel", func(t *testing.T) {
		a := pendingApp(t)
		local := sendTop(t, a, "mine")
		a.Update(echo("C1", "100.000000", "", "from my phone"))
		a.Update(NewMessageMsg{ChannelID: "C1", Message: messages.MessageItem{TS: "102.000000", UserID: "UOTHER", Text: "other"}})
		wantRows(t, "while pending", a.messagepane.Messages(), "102.000000=other", local+"=mine")
		if sel, _ := a.messagepane.SelectedMessage(); sel.TS != "102.000000" {
			t.Fatalf("selected %q, want the newest message", sel.TS)
		}
		a.Update(MessageSentMsg{ChannelID: "C1", LocalTS: local, Message: messages.MessageItem{TS: "101.000000", UserID: "USELF", Text: "mine"}})
		wantRows(t, "after result", a.messagepane.Messages(),
			"100.000000=from my phone", "101.000000=mine", "102.000000=other")
		if sel, _ := a.messagepane.SelectedMessage(); sel.TS != "102.000000" {
			t.Fatalf("selected %q after the release, want the message it was on", sel.TS)
		}
	})
	t.Run("thread", func(t *testing.T) {
		a := pendingApp(t)
		a.messagepane.SetMessages([]messages.MessageItem{{TS: "5.000000", Text: "parent"}})
		openThread(a, "5.000000")
		a.Update(SendThreadReplyMsg{ChannelID: "C1", ThreadTS: "5.000000", Text: "mine"})
		local := a.threadPanel.Replies()[0].TS
		a.Update(echo("C1", "100.000000", "5.000000", "from my phone"))
		a.Update(NewMessageMsg{ChannelID: "C1", Message: messages.MessageItem{TS: "102.000000", ThreadTS: "5.000000", UserID: "UOTHER", Text: "other"}})
		a.Update(ThreadReplySentMsg{ChannelID: "C1", ThreadTS: "5.000000", LocalTS: local,
			Message: messages.MessageItem{TS: "101.000000", ThreadTS: "5.000000", UserID: "USELF", Text: "mine"}})
		wantRows(t, "after result", a.threadPanel.Replies(),
			"100.000000=from my phone", "101.000000=mine", "102.000000=other")
	})
}

// permute calls f with every ordering of xs.
func permute(xs []string, f func([]string)) {
	var rec func(int)
	p := slices.Clone(xs)
	rec = func(k int) {
		if k == len(p) {
			f(slices.Clone(p))
			return
		}
		for i := k; i < len(p); i++ {
			p[k], p[i] = p[i], p[k]
			rec(k + 1)
			p[k], p[i] = p[i], p[k]
		}
	}
	rec(0)
}

// A reply whose echo arrived first and whose post then failed is shown
// from its echo (Slack posted it), with the parent's reply count and a
// threads-list refresh as for any reply, and the failure still gives
// the text back and reports it.
func TestPendingReply_EchoThenFailureShowsEcho(t *testing.T) {
	a := typedApp(t)
	a.threadsDirtyDebounce = time.Millisecond
	a.messagepane.SetMessages([]messages.MessageItem{{TS: "5.000000", Text: "parent"}})
	_, _, local := sendFromComposer(t, a, PanelThread, "hi")
	a.Update(echo("C1", "10.000001", "5.000000", "hi"))
	wantRows(t, "while pending", a.threadPanel.Replies(), local+"=hi")
	_, cmd := a.Update(ThreadReplySendFailedMsg{ChannelID: "C1", ThreadTS: "5.000000", LocalTS: local, Reason: "timeout"})
	wantRows(t, "after failure", a.threadPanel.Replies(), "10.000001=hi")
	if n := a.messagepane.Messages()[0].ReplyCount; n != 1 {
		t.Errorf("parent reply count = %d, want 1", n)
	}
	// One run: a tick's timer starts when the tick is built, so a
	// second run of the same command would never fire it.
	got := drainMsgs(cmd, 500*time.Millisecond)
	if !slices.ContainsFunc(got, func(m tea.Msg) bool { _, ok := m.(statusbar.SendFailedMsg); return ok }) {
		t.Errorf("failure not reported: %#v", got)
	}
	if !slices.ContainsFunc(got, func(m tea.Msg) bool { _, ok := m.(ThreadsListDirtyMsg); return ok }) {
		t.Errorf("no threads-list refresh for the released reply: %#v", got)
	}
	if v := a.threadCompose.Value(); v != "hi" {
		t.Errorf("thread composer = %q, want the text back", v)
	}
}

// A broadcast reply's echo is held in the thread panel and the channel
// feed alike, and its result settles both.
func TestPendingReply_BroadcastEchoHeldInEveryPane(t *testing.T) {
	a := pendingApp(t)
	openThread(a, "5.000000")
	a.Update(SendThreadReplyMsg{ChannelID: "C1", ThreadTS: "5.000000", Text: "reply", Broadcast: true})
	local := a.threadPanel.Replies()[0].TS
	e := echo("C1", "10.000009", "5.000000", "reply (echo)")
	e.Message.Subtype = "thread_broadcast"
	a.Update(e)
	wantRows(t, "thread", a.threadPanel.Replies(), local+"=reply")
	wantRows(t, "channel", a.messagepane.Messages(), local+"=reply")
	a.Update(ThreadReplySentMsg{ChannelID: "C1", ThreadTS: "5.000000", LocalTS: local, Broadcast: true,
		Message: messages.MessageItem{TS: "10.000009", ThreadTS: "5.000000", Subtype: "thread_broadcast", UserID: "USELF", Text: "reply"}})
	wantRows(t, "thread", a.threadPanel.Replies(), "10.000009=reply")
	wantRows(t, "channel", a.messagepane.Messages(), "10.000009=reply")
}

// A sent result without a TS cannot settle the placeholder, so it
// removes it rather than leaving a row that never resolves. An echo
// held meanwhile is the posted message and shows.
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
	t.Run("echo held first", func(t *testing.T) {
		a := pendingApp(t)
		local := sendTop(t, a, "hello")
		a.Update(echo("C1", "10.000001", "", "hello"))
		a.Update(MessageSentMsg{ChannelID: "C1", LocalTS: local})
		wantRows(t, "channel", a.messagepane.Messages(), "10.000001=hello")
	})
}

// A held message edited or deleted before it is released shows as
// edited, or not at all.
func TestPendingSend_HeldEchoEditedOrDeleted(t *testing.T) {
	a := pendingApp(t)
	local := sendTop(t, a, "hello")
	a.Update(echo("C1", "10.000002", "", "phone"))
	a.Update(echo("C1", "10.000003", "", "phone 2"))
	edited := echo("C1", "10.000002", "", "phone edited")
	edited.Message.IsEdited = true
	a.Update(edited)
	a.Update(WSMessageDeletedMsg{ChannelID: "C1", TS: "10.000003"})
	a.Update(MessageSentMsg{ChannelID: "C1", LocalTS: local, Message: messages.MessageItem{TS: "10.000001", UserID: "USELF", Text: "hello"}})
	wantRows(t, "channel", a.messagepane.Messages(), "10.000001=hello", "10.000002=phone edited")
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
	t.Run("echo held, then failure", func(t *testing.T) {
		a := typedApp(t)
		a.messagepane.SetMessages(nil)
		_, _, local := sendFromComposer(t, a, PanelMessages, "posted")
		a.Update(echo("C1", "10.000001", "", "posted"))
		a.Update(MessageSendFailedMsg{ChannelID: "C1", LocalTS: local, Reason: "timeout"})
		if got := a.compose.Value(); got != "posted" {
			t.Errorf("composer = %q, want the text back: nothing proves the echo is this send's", got)
		}
		wantRows(t, "channel", a.messagepane.Messages(), "10.000001=posted")
	})
}
