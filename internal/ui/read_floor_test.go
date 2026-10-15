package ui

import (
	"errors"
	"slices"
	"testing"
	"time"

	"github.com/gammons/slk/internal/core"
	"github.com/gammons/slk/internal/ids"
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
		app.beginSend(sendRequest{ChannelID: "C1", Text: "mine"})
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
		app.beginReply(replyRequest{ChannelID: "C1", ThreadTS: "5.000000", Text: "mine"})
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

// mark builds the NewMessageMsg of another user's message in C1, a
// reply in thread 5.000000 when thread is set.
func mark(ts string, thread bool) NewMessageMsg {
	m := messages.MessageItem{TS: ts, UserID: "UOTHER", Text: "other"}
	if thread {
		m.ThreadTS = "5.000000"
	}
	return NewMessageMsg{ChannelID: "C1", Message: m}
}

// reshow puts C1 and its thread back on screen after the App moved
// away from them.
func reshow(app *App) {
	app.activeChannelID = "C1"
	app.threadPanel.SetThread(messages.MessageItem{TS: "5.000000", Text: "parent"}, nil, "C1", "5.000000")
	app.threadVisible = true
}

// Floors only stop out-of-order automatic marks while slk watches a
// workspace without a gap. Away from it, or across a reconnect, slk
// misses the mark-unread that moves Slack's cursor back, so a floor
// kept from before would block a mark Slack needs. Leaving the
// workspace and every connection change forget its floors.
func TestReadFloor_ForgottenWhenSlkStopsWatching(t *testing.T) {
	gaps := map[string]func(app *App){
		"workspace switch": func(app *App) {
			app.Update(WorkspaceSwitchedMsg{TeamID: "T2", TeamName: "Other"})
			app.Update(WorkspaceSwitchedMsg{TeamID: "T1", TeamName: "Mine"})
		},
		"reconnect": func(app *App) {
			app.Update(ConnectionStateMsg{TeamID: "T1", State: 2})
			app.Update(ConnectionStateMsg{TeamID: "T1", State: 1})
		},
	}
	for name, gap := range gaps {
		for _, thread := range []bool{false, true} {
			app, calls := floorApp(t)
			app.activeTeamID = "T1"
			arriveAndFlush(t, app, mark("105.000000", thread))
			gap(app)
			reshow(app)
			arriveAndFlush(t, app, mark("104.000000", thread))
			if len(*calls) != 2 {
				t.Errorf("%s (thread %v): marks = %v, want 104 marked once the floor is gone", name, thread, *calls)
			}
		}
	}
}

// Another workspace's connection changes leave this one's floors.
func TestReadFloor_KeptAcrossAnotherWorkspacesReconnect(t *testing.T) {
	app, calls := floorApp(t)
	app.activeTeamID = "T1"
	arriveAndFlush(t, app, mark("105.000000", false))
	app.Update(ConnectionStateMsg{TeamID: "T2", State: 2})
	arriveAndFlush(t, app, mark("104.000000", false))
	if len(*calls) != 1 {
		t.Errorf("marks = %v, want 104 held behind T1's floor", *calls)
	}
}

// Opening a thread or entering a channel marks the newest message the
// user is looking at whatever the floor says: the user's own act is
// never what moves a cursor back by accident, and a floor can be stale.
func TestReadFloor_OpenAndEntryMarksIgnoreTheFloor(t *testing.T) {
	t.Run("thread open", func(t *testing.T) {
		app, calls := floorApp(t)
		arriveAndFlush(t, app, mark("105.000000", true))
		replies := []messages.MessageItem{{TS: "105.000000", ThreadTS: "5.000000", UserID: "UOTHER"}}
		feed(t, app, app.markOpenThreadRead("C1", "5.000000", replies), 0)
		if !slices.Equal(*calls, []string{"th:C1/5.000000/105.000000", "th:C1/5.000000/105.000000"}) {
			t.Errorf("marks = %v, want the open to mark 105 again", *calls)
		}
	})
	t.Run("channel entry", func(t *testing.T) {
		app, calls := floorApp(t)
		arriveAndFlush(t, app, mark("105.000000", false))
		setChannelFuncsForTest(app, core.ChannelServiceFuncs{
			ReadCache: func(ids.ChannelID) []messages.MessageItem {
				return []messages.MessageItem{{TS: "105.000000", UserID: "UOTHER"}}
			},
			SyncedAt: func(ids.ChannelID) int64 { return time.Now().Unix() },
			MarkRead: func(channelID ids.ChannelID, ts ids.MessageTS) core.Msg {
				*calls = append(*calls, "ch:"+string(channelID)+"/"+string(ts))
				return nil
			},
		})
		_, cmd := app.Update(ChannelSelectedMsg{ID: "C1", Name: "general"})
		feed(t, app, cmd, 0)
		if !slices.Equal(*calls, []string{"ch:C1/105.000000", "ch:C1/105.000000"}) {
			t.Errorf("marks = %v, want the entry to mark 105 again", *calls)
		}
	})
}

// A floor rises only when Slack accepts the mark. While a mark is in
// flight nothing marks behind it; once Slack rejects it, a mark behind
// it goes out, since Slack's cursor never moved.
func TestReadFloor_RisesOnlyWhenSlackAcceptsTheMark(t *testing.T) {
	t.Run("channel", func(t *testing.T) {
		app, calls := floorApp(t)
		app.activeTeamID = "T1"
		arriveAndFlush(t, app, mark("105.000000", false))
		arriveAndFlush(t, app, mark("104.000000", false))
		if len(*calls) != 1 {
			t.Fatalf("marks = %v, want nothing behind the 105 in flight", *calls)
		}
		app.Update(ChannelMarkedReadMsg{TeamID: "T1", ChannelID: "C1", TS: "105.000000", Err: errors.New("ratelimited")})
		arriveAndFlush(t, app, mark("104.500000", false))
		if len(*calls) != 2 {
			t.Errorf("marks = %v, want 104.5 marked after Slack rejected 105", *calls)
		}
	})
	t.Run("thread", func(t *testing.T) {
		app, calls := floorApp(t)
		app.activeTeamID = "T1"
		app.SetThreadService(core.NewThreadService(core.ThreadServiceFuncs{
			Mark: func(channelID ids.ChannelID, threadTS ids.ThreadTS, ts ids.MessageTS) core.Cmd {
				*calls = append(*calls, "th:"+string(ts))
				return func() core.Msg {
					return ThreadMarkedLocalMsg{TeamID: "T1", ChannelID: string(channelID), ThreadTS: string(threadTS),
						TS: string(ts), Err: errors.New("ratelimited")}
				}
			},
		}))
		arriveAndFlush(t, app, mark("105.000000", true))
		arriveAndFlush(t, app, mark("104.000000", true))
		if len(*calls) != 2 {
			t.Errorf("marks = %v, want 104 marked after Slack rejected 105", *calls)
		}
	})
}

// A mark staged in one workspace is never issued after the user has
// switched to another: the channel service marks in the active
// workspace, where that channel is not the one the user read.
func TestReadFloor_StagedMarkStaysInItsWorkspace(t *testing.T) {
	app, calls := floorApp(t)
	app.activeTeamID = "T1"
	_, tick := app.Update(mark("105.000000", false))
	app.Update(WorkspaceSwitchedMsg{TeamID: "T2", TeamName: "Other"})
	feed(t, app, tick, 0)
	if len(*calls) != 0 {
		t.Errorf("marks = %v, want T1's staged mark dropped in T2", *calls)
	}
}
