package ui

import (
	"fmt"
	"testing"
	"time"

	"github.com/gammons/slk/internal/ui/messages"
)

// slackChannel and slackThread are the edit fixture's (editApp) rows as
// a fetch from Slack returns them, before any of the user's edits.
func slackChannel() []messages.MessageItem {
	parent := typedRow("5.000000", "parent", "")
	parent.ReplyCount = 1
	return []messages.MessageItem{typedRow("1.000000", "one", ""), parent}
}

func slackThread() []messages.MessageItem {
	return []messages.MessageItem{typedRow("6.000000", "reply", "5.000000")}
}

// reloadChannel replaces the channel pane's rows with rows, as a
// history fetch landing does.
func reloadChannel(a *App, rows []messages.MessageItem) {
	a.Update(MessagesLoadedMsg{ChannelID: "C1", Messages: rows})
}

// reloadThread replaces the thread pane's replies with replies, as a
// replies fetch landing does.
func reloadThread(a *App, replies []messages.MessageItem) {
	a.Update(ThreadRepliesLoadedMsg{ThreadTS: "5.000000", Replies: replies})
}

// A pane reloaded while an edit of one of its messages is pending shows
// the edit, not the body the fetch carried: the fetch knows nothing of
// it yet. So does the thread parent. The result then settles as usual.
func TestPendingEdit_SurvivesAReload(t *testing.T) {
	t.Run("channel", func(t *testing.T) {
		a := editApp(t, nil)
		res := editFrom(t, a, PanelMessages, "1.000000", "new")
		reloadChannel(a, slackChannel())
		wantText(t, "channel after reload", a.messagepane.Messages(), "1.000000", "new", true)
		a.Update(res)
		wantText(t, "channel after the result", a.messagepane.Messages(), "1.000000", "new", true)
	})
	t.Run("thread reply", func(t *testing.T) {
		a := editApp(t, nil)
		_ = editFrom(t, a, PanelThread, "6.000000", "new reply")
		reloadThread(a, slackThread())
		wantText(t, "thread after reload", a.threadPanel.Replies(), "6.000000", "new reply", true)
	})
	t.Run("thread parent", func(t *testing.T) {
		a := editApp(t, nil)
		_ = editFrom(t, a, PanelMessages, "5.000000", "new parent")
		reloadThread(a, slackThread())
		if p := a.threadPanel.ParentMsg(); p.Text != "new parent" || !p.IsEdited {
			t.Errorf("thread parent after reload = %q edited=%v, want the edit", p.Text, p.IsEdited)
		}
	})
	t.Run("a waiting edit", func(t *testing.T) {
		a := editApp(t, nil)
		_ = editFrom(t, a, PanelMessages, "1.000000", "first")
		queueEdit(t, a, PanelMessages, "1.000000", "second")
		reloadChannel(a, slackChannel())
		wantText(t, "channel after reload", a.messagepane.Messages(), "1.000000", "second", true)
	})
}

// A pane reloaded while a delete is pending does not bring the message
// back. If the delete then fails, the message returns as the fetch had
// it: Slack's state, which may be newer than what was deleted.
func TestPendingDelete_SurvivesAReload(t *testing.T) {
	for _, tc := range []struct {
		name   string
		panel  Panel
		ts     string
		reload func(a *App, edited string)
		rows   func(a *App) []messages.MessageItem
	}{
		{"channel", PanelMessages, "1.000000", func(a *App, edited string) {
			rows := slackChannel()
			rows[0] = typedRow("1.000000", edited, "")
			reloadChannel(a, rows)
		}, func(a *App) []messages.MessageItem { return a.messagepane.Messages() }},
		{"thread", PanelThread, "6.000000", func(a *App, edited string) {
			reloadThread(a, []messages.MessageItem{typedRow("6.000000", edited, "5.000000")})
		}, func(a *App) []messages.MessageItem { return a.threadPanel.Replies() }},
	} {
		t.Run(tc.name, func(t *testing.T) {
			a := editApp(t, nil)
			deleteFails(a)
			res := deleteFrom(t, a, tc.panel, tc.ts)
			tc.reload(a, "edited elsewhere")
			if messages.FirstIndexOfTS(tc.rows(a), tc.ts) >= 0 {
				t.Fatalf("%s back after reload: %q", tc.ts, rowsOf(tc.rows(a)))
			}
			a.Update(res)
			wantText(t, "after the failed delete", tc.rows(a), tc.ts, "edited elsewhere", false)
		})
	}
}

// A pane reloaded while a send is pending keeps the placeholder after
// the rows Slack knows, and the result settles it once.
func TestPendingSend_SurvivesAReload(t *testing.T) {
	t.Run("channel", func(t *testing.T) {
		a := editApp(t, nil)
		local := sendTop(t, a, "hello")
		reloadChannel(a, slackChannel())
		wantRows(t, "channel after reload", a.messagepane.Messages(), "1.000000=one", "5.000000=parent", local+"=hello")
		a.Update(MessageSentMsg{ChannelID: "C1", LocalTS: local, Message: messages.MessageItem{TS: "9.000000", UserID: "USELF", Text: "hello"}})
		wantRows(t, "channel after the result", a.messagepane.Messages(), "1.000000=one", "5.000000=parent", "9.000000=hello")
	})
	t.Run("thread reply", func(t *testing.T) {
		a := editApp(t, nil)
		a.Update(SendThreadReplyMsg{ChannelID: "C1", ThreadTS: "5.000000", Text: "re"})
		replies := a.threadPanel.Replies()
		local := replies[len(replies)-1].TS
		reloadThread(a, slackThread())
		wantRows(t, "thread after reload", a.threadPanel.Replies(), "6.000000=reply", local+"=re")
	})
	t.Run("broadcast reply in the channel", func(t *testing.T) {
		a := editApp(t, nil)
		a.Update(SendThreadReplyMsg{ChannelID: "C1", ThreadTS: "5.000000", Text: "all", Broadcast: true})
		replies := a.threadPanel.Replies()
		local := replies[len(replies)-1].TS
		reloadChannel(a, slackChannel())
		rows := a.messagepane.Messages()
		wantRows(t, "channel after reload", rows, "1.000000=one", "5.000000=parent", local+"=all")
		if rows[2].Subtype != "thread_broadcast" {
			t.Errorf("broadcast placeholder subtype = %q, want thread_broadcast", rows[2].Subtype)
		}
	})
}

// A fetch that lands while a send is pending may already hold the
// user's own messages in that conversation: one whose echo is held, or
// the send's own message, posted but not yet answered. Both stay hidden
// until the send's result, as their echoes would be, so nothing shows
// twice; then each shows exactly once.
func TestPendingSend_ReloadHidesTheUsersNewMessages(t *testing.T) {
	now := time.Now().Unix()
	ts := func(n int) string { return fmt.Sprintf("%d.%06d", now, n) }
	own := func(ts, text string) messages.MessageItem {
		return messages.MessageItem{TS: ts, UserID: "USELF", Text: text}
	}
	a := editApp(t, nil)
	local := sendTop(t, a, "hello")
	a.Update(echo("C1", ts(2), "", "from my phone"))
	reloadChannel(a, append(slackChannel(), own(ts(1), "hello"), own(ts(2), "from my phone")))
	wantRows(t, "channel after reload", a.messagepane.Messages(), "1.000000=one", "5.000000=parent", local+"=hello")
	a.Update(MessageSentMsg{ChannelID: "C1", LocalTS: local, Message: own(ts(1), "hello")})
	wantRows(t, "channel after the result", a.messagepane.Messages(),
		"1.000000=one", "5.000000=parent", ts(1)+"=hello", ts(2)+"=from my phone")
	a.Update(echo("C1", ts(1), "", "hello"))
	wantRows(t, "channel after the late echo", a.messagepane.Messages(),
		"1.000000=one", "5.000000=parent", ts(1)+"=hello", ts(2)+"=from my phone")
}

// The user's older messages in a fetch are shown even while a send is
// pending: only messages that may be that send are held.
func TestPendingSend_ReloadShowsTheUsersOlderMessages(t *testing.T) {
	a := editApp(t, nil)
	local := sendTop(t, a, "hello")
	old := messages.MessageItem{TS: "3.000000", UserID: "USELF", Text: "years ago"}
	reloadChannel(a, append(slackChannel()[:1], old, slackChannel()[1]))
	wantRows(t, "channel after reload", a.messagepane.Messages(), "1.000000=one", "3.000000=years ago", "5.000000=parent", local+"=hello")
}
