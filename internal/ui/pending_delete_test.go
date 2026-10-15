package ui

import (
	"errors"
	"testing"
	"time"

	tea "charm.land/bubbletea/v2"

	"github.com/gammons/slk/internal/core"
	"github.com/gammons/slk/internal/ids"
	"github.com/gammons/slk/internal/ui/messages"
	"github.com/gammons/slk/internal/ui/statusbar"
)

// deleteApp is a typedApp in normal mode on C1 with the user's own
// messages 1, 2 and 3, the parent 5 of thread 5.000000, and 7, a reply
// to it also sent to the channel. The thread is open, its replies 6, 7
// and 8. The delete service answers with err.
func deleteApp(t *testing.T, err error) *App {
	t.Helper()
	a := typedApp(t)
	a.mode = ModeNormal
	own := func(ts, text, threadTS, subtype string) messages.MessageItem {
		return messages.MessageItem{TS: ts, UserID: "USELF", Text: text, ThreadTS: threadTS, Subtype: subtype}
	}
	parent := own("5.000000", "parent", "5.000000", "")
	parent.ReplyCount = 3
	a.messagepane.SetMessages([]messages.MessageItem{
		own("1.000000", "one", "", ""),
		own("2.000000", "two", "", ""),
		own("3.000000", "three", "", ""),
		parent,
		own("7.000000", "bcast", "5.000000", "thread_broadcast"),
	})
	a.threadPanel.SetThread(parent, []messages.MessageItem{
		own("6.000000", "reply", "5.000000", ""),
		own("7.000000", "bcast", "5.000000", "thread_broadcast"),
		own("8.000000", "last", "5.000000", ""),
	}, "C1", "5.000000")
	a.SetMessageService(core.NewMessageService(core.MessageServiceFuncs{
		Delete: func(ch ids.ChannelID, ts ids.MessageTS) core.Msg {
			return MessageDeletedMsg{ChannelID: string(ch), TS: string(ts), Err: err}
		},
	}))
	return a
}

// deleteFrom deletes message ts from panel the way the user does
// (select, d, confirm), applies the delete and returns the service's
// result, not yet delivered.
func deleteFrom(t *testing.T, a *App, panel Panel, ts string) MessageDeletedMsg {
	t.Helper()
	a.focusedPanel = panel
	if panel == PanelThread {
		a.threadPanel.SelectByIndex(messages.FirstIndexOfTS(a.threadPanel.Replies(), ts))
	} else {
		a.messagepane.SelectByTS(ts)
	}
	_ = a.beginDeleteOfSelected()
	del := deliveredMsgs[DeleteMessageMsg](t, a.handleConfirmMode(tea.KeyPressMsg{Code: 'y', Text: "y"}))[0]
	if del.TS != ts {
		t.Fatalf("setup: deleting %s, want %s", del.TS, ts)
	}
	_, cmd := a.Update(del)
	return deliveredMsgs[MessageDeletedMsg](t, cmd)[0]
}

func selectedTS(t *testing.T, a *App, panel Panel) string {
	t.Helper()
	var msg messages.MessageItem
	var ok bool
	if panel == PanelThread {
		msg, ok = a.threadPanel.SelectedMessage()
	} else {
		msg, ok = a.messagepane.SelectedMessage()
	}
	if !ok {
		return ""
	}
	return msg.TS
}

// A delete takes the message out of every pane showing it at once.
func TestPendingDelete_RemovesAtOnce(t *testing.T) {
	t.Run("channel message", func(t *testing.T) {
		a := deleteApp(t, nil)
		_ = deleteFrom(t, a, PanelMessages, "2.000000")
		wantRows(t, "channel", a.messagepane.Messages(),
			"1.000000=one", "3.000000=three", "5.000000=parent", "7.000000=bcast")
	})
	t.Run("reply also sent to the channel", func(t *testing.T) {
		a := deleteApp(t, nil)
		_ = deleteFrom(t, a, PanelThread, "7.000000")
		wantRows(t, "thread", a.threadPanel.Replies(), "6.000000=reply", "8.000000=last")
		wantRows(t, "channel", a.messagepane.Messages(),
			"1.000000=one", "2.000000=two", "3.000000=three", "5.000000=parent")
	})
}

// A failed delete puts the message back where it was in every pane,
// selected again, and reports the failure.
func TestPendingDelete_FailureRestores(t *testing.T) {
	t.Run("channel", func(t *testing.T) {
		a := deleteApp(t, errors.New("boom"))
		res := deleteFrom(t, a, PanelMessages, "2.000000")
		if got := selectedTS(t, a, PanelMessages); got == "2.000000" || got == "" {
			t.Fatalf("selection after removal = %q", got)
		}
		_, cmd := a.Update(res)
		if cmd == nil {
			t.Error("failure reported nothing")
		}
		wantRows(t, "channel", a.messagepane.Messages(),
			"1.000000=one", "2.000000=two", "3.000000=three", "5.000000=parent", "7.000000=bcast")
		if got := selectedTS(t, a, PanelMessages); got != "2.000000" {
			t.Errorf("selection = %q, want the restored message", got)
		}
	})
	t.Run("reply in both panes", func(t *testing.T) {
		a := deleteApp(t, errors.New("boom"))
		a.messagepane.SelectByTS("3.000000")
		res := deleteFrom(t, a, PanelThread, "7.000000")
		a.Update(res)
		wantRows(t, "thread", a.threadPanel.Replies(), "6.000000=reply", "7.000000=bcast", "8.000000=last")
		wantRows(t, "channel", a.messagepane.Messages(),
			"1.000000=one", "2.000000=two", "3.000000=three", "5.000000=parent", "7.000000=bcast")
		if got := selectedTS(t, a, PanelThread); got != "7.000000" {
			t.Errorf("thread selection = %q, want the restored reply", got)
		}
		if got := selectedTS(t, a, PanelMessages); got != "3.000000" {
			t.Errorf("channel selection = %q, want it left on 3.000000", got)
		}
	})
	t.Run("not into a pane that moved on", func(t *testing.T) {
		a := deleteApp(t, errors.New("boom"))
		res := deleteFrom(t, a, PanelThread, "6.000000")
		a.threadPanel.SetThread(messages.MessageItem{TS: "9.000000"}, []messages.MessageItem{
			{TS: "9.000001", Text: "other"},
		}, "C1", "9.000000")
		a.Update(res)
		wantRows(t, "other thread", a.threadPanel.Replies(), "9.000001=other")
	})
}

// The message_deleted echo of the user's own delete is harmless before
// or after the result, and an echo before a failed result proves the
// delete went through.
func TestPendingDelete_Echo(t *testing.T) {
	t.Run("before success", func(t *testing.T) {
		a := deleteApp(t, nil)
		res := deleteFrom(t, a, PanelMessages, "2.000000")
		sel := selectedTS(t, a, PanelMessages)
		a.Update(WSMessageDeletedMsg{ChannelID: "C1", TS: "2.000000"})
		a.Update(res)
		wantRows(t, "channel", a.messagepane.Messages(),
			"1.000000=one", "3.000000=three", "5.000000=parent", "7.000000=bcast")
		if got := selectedTS(t, a, PanelMessages); got != sel {
			t.Errorf("selection = %q, want %q kept", got, sel)
		}
	})
	t.Run("after success", func(t *testing.T) {
		a := deleteApp(t, nil)
		res := deleteFrom(t, a, PanelMessages, "2.000000")
		a.Update(res)
		sel := selectedTS(t, a, PanelMessages)
		a.Update(WSMessageDeletedMsg{ChannelID: "C1", TS: "2.000000"})
		wantRows(t, "channel", a.messagepane.Messages(),
			"1.000000=one", "3.000000=three", "5.000000=parent", "7.000000=bcast")
		if got := selectedTS(t, a, PanelMessages); got != sel {
			t.Errorf("selection = %q, want %q kept", got, sel)
		}
	})
	t.Run("before failure", func(t *testing.T) {
		a := deleteApp(t, errors.New("timeout"))
		res := deleteFrom(t, a, PanelMessages, "2.000000")
		a.Update(WSMessageDeletedMsg{ChannelID: "C1", TS: "2.000000"})
		_, cmd := a.Update(res)
		if cmd != nil {
			t.Errorf("failure after the echo returned %#v, want nothing reported", cmd())
		}
		wantRows(t, "channel", a.messagepane.Messages(),
			"1.000000=one", "3.000000=three", "5.000000=parent", "7.000000=bcast")
	})
}

// Deleting the open thread's parent takes it out of the channel at
// once; the thread panel closes when Slack confirms, and stays when the
// delete fails.
func TestPendingDelete_ThreadParent(t *testing.T) {
	t.Run("success", func(t *testing.T) {
		a := deleteApp(t, nil)
		a.threadVisible = true
		res := deleteFrom(t, a, PanelMessages, "5.000000")
		wantRows(t, "channel", a.messagepane.Messages(),
			"1.000000=one", "2.000000=two", "3.000000=three", "7.000000=bcast")
		if !a.threadVisible {
			t.Fatal("thread panel closed before Slack answered")
		}
		a.Update(res)
		if a.threadVisible {
			t.Error("thread panel still open after its parent was deleted")
		}
	})
	t.Run("failure", func(t *testing.T) {
		a := deleteApp(t, errors.New("boom"))
		a.threadVisible = true
		res := deleteFrom(t, a, PanelMessages, "5.000000")
		a.Update(res)
		wantRows(t, "channel", a.messagepane.Messages(),
			"1.000000=one", "2.000000=two", "3.000000=three", "5.000000=parent", "7.000000=bcast")
		if !a.threadVisible || a.threadPanel.ThreadTS() != "5.000000" {
			t.Error("thread panel closed by a failed delete")
		}
	})
}

// With no active workspace the service answers nil; the delete fails
// like any other.
func TestPendingDelete_NilResultFails(t *testing.T) {
	a := deleteApp(t, nil)
	a.SetMessageService(core.NewMessageService(core.MessageServiceFuncs{
		Delete: func(ids.ChannelID, ids.MessageTS) core.Msg { return nil },
	}))
	res := deleteFrom(t, a, PanelMessages, "2.000000")
	if res.Err == nil || res.Err.Error() != noWorkspaceReason || res.ChannelID != "C1" || res.TS != "2.000000" {
		t.Fatalf("nil result became %+v, want a failure", res)
	}
	a.Update(res)
	wantRows(t, "channel", a.messagepane.Messages(),
		"1.000000=one", "2.000000=two", "3.000000=three", "5.000000=parent", "7.000000=bcast")
}

// A failed delete puts the message back in TS order, so older history
// loaded while the delete was pending does not push it out of place.
func TestPendingDelete_FailureRestoresInTSOrder(t *testing.T) {
	a := deleteApp(t, errors.New("boom"))
	res := deleteFrom(t, a, PanelMessages, "2.000000")
	a.messagepane.PrependMessages([]messages.MessageItem{
		{TS: "0.100000", Text: "old a"}, {TS: "0.200000", Text: "old b"},
	})
	a.Update(res)
	wantRows(t, "channel", a.messagepane.Messages(),
		"0.100000=old a", "0.200000=old b", "1.000000=one", "2.000000=two",
		"3.000000=three", "5.000000=parent", "7.000000=bcast")
	if got := selectedTS(t, a, PanelMessages); got != "2.000000" {
		t.Errorf("selection = %q, want the restored message", got)
	}
}

// The restored message takes the cursor back only when the cursor is
// still where the delete left it.
func TestPendingDelete_FailureLeavesAMovedCursor(t *testing.T) {
	t.Run("channel", func(t *testing.T) {
		a := deleteApp(t, errors.New("boom"))
		res := deleteFrom(t, a, PanelMessages, "2.000000")
		a.messagepane.SelectByTS("5.000000")
		a.Update(res)
		if got := selectedTS(t, a, PanelMessages); got != "5.000000" {
			t.Errorf("selection = %q, want it left on 5.000000", got)
		}
	})
	t.Run("thread", func(t *testing.T) {
		a := deleteApp(t, errors.New("boom"))
		res := deleteFrom(t, a, PanelThread, "7.000000")
		a.threadPanel.SelectByIndex(messages.FirstIndexOfTS(a.threadPanel.Replies(), "8.000000"))
		a.Update(res)
		if got := selectedTS(t, a, PanelThread); got != "8.000000" {
			t.Errorf("thread selection = %q, want it left on 8.000000", got)
		}
	})
}

// A failed delete puts back the message as Slack has it now: an edit
// of it that settled while the delete was pending, failed or not, and
// another client's edit, are what comes back.
func TestPendingDelete_FailureRestoresTheCurrentBody(t *testing.T) {
	t.Run("edit fails, then the delete", func(t *testing.T) {
		a := editApp(t, errors.New("boom"))
		deleteFails(a)
		edited := editFrom(t, a, PanelMessages, "1.000000", "fresh words")
		del := deleteFrom(t, a, PanelMessages, "1.000000")
		a.Update(edited)
		a.Update(del)
		ch, _ := drawn(a)
		wantDrawn(t, "channel", ch, "one", "fresh words")
	})
	t.Run("edit succeeds, then the delete fails", func(t *testing.T) {
		a := editApp(t, nil)
		deleteFails(a)
		edited := editFrom(t, a, PanelMessages, "1.000000", "fresh words")
		del := deleteFrom(t, a, PanelMessages, "1.000000")
		a.Update(edited)
		a.Update(del)
		ch, _ := drawn(a)
		wantDrawn(t, "channel", ch, "fresh words", "▌one")
	})
	t.Run("another client's edit", func(t *testing.T) {
		a := editApp(t, nil)
		deleteFails(a)
		del := deleteFrom(t, a, PanelMessages, "1.000000")
		changed := typedRow("1.000000", "slack blocks", "")
		changed.IsEdited = true
		a.Update(NewMessageMsg{ChannelID: "C1", Changed: true, Message: changed})
		a.Update(del)
		ch, _ := drawn(a)
		wantDrawn(t, "channel", ch, "slack blocks", "▌one")
	})
}

// deleteFails keeps a's edit service and makes every delete fail.
func deleteFails(a *App) {
	a.SetMessageService(core.NewMessageService(core.MessageServiceFuncs{
		Edit: a.messageSvc.Edit,
		Delete: func(ch ids.ChannelID, ts ids.MessageTS) core.Msg {
			return MessageDeletedMsg{ChannelID: string(ch), TS: string(ts), Err: errors.New("boom")}
		},
	}))
}

// A delete Slack answers with message_not_found leaves the message
// gone and reports nothing: it is gone, which is what was asked.
func TestPendingDelete_NotFoundIsDone(t *testing.T) {
	a := deleteApp(t, errors.New("message_not_found"))
	res := deleteFrom(t, a, PanelMessages, "2.000000")
	res.NotFound = true
	_, cmd := a.Update(res)
	for _, m := range drainMsgs(cmd, 50*time.Millisecond) {
		if f, ok := m.(statusbar.DeleteFailedMsg); ok {
			t.Errorf("reported %+v", f)
		}
	}
	wantRows(t, "channel", a.messagepane.Messages(),
		"1.000000=one", "3.000000=three", "5.000000=parent", "7.000000=bcast")
}
