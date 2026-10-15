package ui

import (
	"slices"
	"testing"

	"github.com/gammons/slk/internal/ui/messages"
)

// floorApp is markCapture's App, focused on C1 as USELF with a thread
// 5.000000 of C1 open, so arrivals in either stage a read mark.
func floorApp(t *testing.T) (*App, *[]string) {
	t.Helper()
	app, calls := markCapture(t)
	app.activeChannelID = "C1"
	app.SetCurrentUserID("USELF")
	app.messagepane.SetMessages([]messages.MessageItem{{TS: "5.000000", Text: "parent"}})
	app.threadPanel.SetThread(messages.MessageItem{TS: "5.000000", Text: "parent"}, nil, "C1", "5.000000")
	app.threadVisible = true
	return app, calls
}

// arriveAndFlush delivers m and runs what it schedules, the mark flush
// included.
func arriveAndFlush(t *testing.T, app *App, m any) {
	t.Helper()
	_, cmd := app.Update(m)
	feed(t, app, cmd, 0)
}

// A message the user posted from another client is held while their
// send is pending, and released after a newer message was shown and
// marked. Marking the released one would move Slack's cursor back and
// make the newer message unread again: no mark at or below the last
// one goes out.
func TestReadFloor_ReleasedEchoNeverMarksBackward(t *testing.T) {
	t.Run("channel", func(t *testing.T) {
		app, calls := floorApp(t)
		app.Update(SendMessageMsg{ChannelID: "C1", Text: "mine"})
		rows := app.messagepane.Messages()
		local := rows[len(rows)-1].TS
		arriveAndFlush(t, app, echo("C1", "100.000000", "", "from my phone"))
		arriveAndFlush(t, app, NewMessageMsg{ChannelID: "C1", Message: messages.MessageItem{TS: "102.000000", UserID: "UOTHER", Text: "other"}})
		if !slices.Equal(*calls, []string{"ch:C1/102.000000"}) {
			t.Fatalf("marks after the newer message = %v, want it marked", *calls)
		}
		arriveAndFlush(t, app, MessageSentMsg{ChannelID: "C1", LocalTS: local,
			Message: messages.MessageItem{TS: "101.000000", UserID: "USELF", Text: "mine"}})
		if !slices.Equal(*calls, []string{"ch:C1/102.000000"}) {
			t.Fatalf("marks after the release = %v, want no mark behind 102", *calls)
		}
	})
	t.Run("thread", func(t *testing.T) {
		app, calls := floorApp(t)
		app.Update(SendThreadReplyMsg{ChannelID: "C1", ThreadTS: "5.000000", Text: "mine"})
		local := app.threadPanel.Replies()[0].TS
		arriveAndFlush(t, app, echo("C1", "100.000000", "5.000000", "from my phone"))
		arriveAndFlush(t, app, NewMessageMsg{ChannelID: "C1", Message: messages.MessageItem{TS: "102.000000", ThreadTS: "5.000000", UserID: "UOTHER", Text: "other"}})
		if !slices.Equal(*calls, []string{"th:C1/5.000000/102.000000"}) {
			t.Fatalf("marks after the newer reply = %v, want it marked", *calls)
		}
		arriveAndFlush(t, app, ThreadReplySentMsg{ChannelID: "C1", ThreadTS: "5.000000", LocalTS: local,
			Message: messages.MessageItem{TS: "101.000000", ThreadTS: "5.000000", UserID: "USELF", Text: "mine"}})
		if !slices.Equal(*calls, []string{"th:C1/5.000000/102.000000"}) {
			t.Fatalf("marks after the release = %v, want no mark behind 102", *calls)
		}
	})
}

// A mark-unread moves the cursor back on purpose, so it moves the floor
// with it: a message after the boundary marks again, even one older
// than the last automatic mark. A staged mark is dropped, since issuing
// it would undo the mark-unread.
func TestReadFloor_MarkUnreadResetsTheFloor(t *testing.T) {
	t.Run("channel", func(t *testing.T) {
		app, calls := floorApp(t)
		arriveAndFlush(t, app, NewMessageMsg{ChannelID: "C1", Message: messages.MessageItem{TS: "102.000000", UserID: "UOTHER"}})
		// 103 is staged, its flush tick still to fire when the
		// mark-unread lands.
		_, tick := app.Update(NewMessageMsg{ChannelID: "C1", Message: messages.MessageItem{TS: "103.000000", UserID: "UOTHER"}})
		app.Update(MessageMarkedUnreadMsg{ChannelID: "C1", BoundaryTS: "100.000000", UnreadCount: 3})
		feed(t, app, tick, 0)
		if !slices.Equal(*calls, []string{"ch:C1/102.000000"}) {
			t.Fatalf("marks = %v, want the staged 103 dropped by the mark-unread", *calls)
		}
		arriveAndFlush(t, app, NewMessageMsg{ChannelID: "C1", Message: messages.MessageItem{TS: "101.000000", UserID: "UOTHER"}})
		if !slices.Equal(*calls, []string{"ch:C1/102.000000", "ch:C1/101.000000"}) {
			t.Fatalf("marks = %v, want 101 marked after the mark-unread at 100", *calls)
		}
	})
	t.Run("thread", func(t *testing.T) {
		app, calls := floorApp(t)
		arriveAndFlush(t, app, NewMessageMsg{ChannelID: "C1", Message: messages.MessageItem{TS: "102.000000", ThreadTS: "5.000000", UserID: "UOTHER"}})
		app.Update(MessageMarkedUnreadMsg{ChannelID: "C1", ThreadTS: "5.000000", BoundaryTS: "100.000000"})
		arriveAndFlush(t, app, NewMessageMsg{ChannelID: "C1", Message: messages.MessageItem{TS: "101.000000", ThreadTS: "5.000000", UserID: "UOTHER"}})
		if !slices.Equal(*calls, []string{"th:C1/5.000000/102.000000", "th:C1/5.000000/101.000000"}) {
			t.Fatalf("marks = %v, want 101 marked after the mark-unread at 100", *calls)
		}
	})
}

// A mark made in another client is Slack's cursor too: slk does not
// mark behind it.
func TestReadFloor_RemoteMarkRaisesTheFloor(t *testing.T) {
	app, calls := floorApp(t)
	app.applyChannelMarkEcho("C1", "105.000000", 0)
	arriveAndFlush(t, app, NewMessageMsg{ChannelID: "C1", Message: messages.MessageItem{TS: "104.000000", UserID: "UOTHER"}})
	if len(*calls) != 0 {
		t.Fatalf("marks = %v, want none behind another client's 105", *calls)
	}
}
