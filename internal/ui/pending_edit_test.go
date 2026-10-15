package ui

import (
	"errors"
	"strings"
	"testing"

	"github.com/charmbracelet/x/ansi"

	"github.com/gammons/slk/internal/core"
	"github.com/gammons/slk/internal/ids"
	"github.com/gammons/slk/internal/ui/messages"
)

// editApp is a typedApp in normal mode on C1 whose own messages are
// 1.000000 ("one") and the parent of thread 5.000000 ("parent"), with
// that thread open on reply 6.000000 ("reply"). Every row carries the
// rich_text block Slack stores (typedRow). Its edit service answers
// with the text sent, or with err.
func editApp(t *testing.T, err error) *App {
	t.Helper()
	a := typedApp(t)
	a.mode = ModeNormal
	parent := typedRow("5.000000", "parent", "")
	parent.ReplyCount = 1
	a.messagepane.SetMessages([]messages.MessageItem{typedRow("1.000000", "one", ""), parent})
	a.threadPanel.SetThread(parent, []messages.MessageItem{
		typedRow("6.000000", "reply", "5.000000"),
	}, "C1", "5.000000")
	a.SetMessageService(core.NewMessageService(core.MessageServiceFuncs{
		Edit: func(ch ids.ChannelID, ts ids.MessageTS, text string) core.Msg {
			if err != nil {
				return MessageEditedMsg{ChannelID: string(ch), TS: string(ts), Err: err}
			}
			return MessageEditedMsg{ChannelID: string(ch), TS: string(ts), Text: text}
		},
	}))
	return a
}

// typedRow is one of the user's messages as Slack returns it: the
// mrkdwn text and the rich_text block slk and the web client attach to
// everything typed. The pane draws the body from that block.
func typedRow(ts, text, threadTS string) messages.MessageItem {
	row := messages.MessageItem{TS: ts, UserID: "USELF", ThreadTS: threadTS}
	body := messages.EditedBody(text)
	body.Edited = false
	body.Apply(&row)
	return row
}

// drawn is what the channel pane and the thread pane draw, without
// styling.
func drawn(a *App) (channel, thread string) {
	return ansi.Strip(a.messagepane.View(40, 100)), ansi.Strip(a.threadPanel.View(40, 100))
}

// wantDrawn checks that view draws want and not gone.
func wantDrawn(t *testing.T, what, view, want, gone string) {
	t.Helper()
	if !strings.Contains(view, want) || gone != "" && strings.Contains(view, gone) {
		t.Errorf("%s draws %q, want %q and not %q", what, strings.Join(strings.Fields(view), " "), want, gone)
	}
}

// editFrom edits message ts from panel's composer the way the user
// does (select, e, type, Enter), applies the edit and returns the
// service's result, not yet delivered.
func editFrom(t *testing.T, a *App, panel Panel, ts, typed string) MessageEditedMsg {
	t.Helper()
	a.focusedPanel = panel
	if panel == PanelThread {
		a.threadPanel.SelectByIndex(messages.FirstIndexOfTS(a.threadPanel.Replies(), ts))
	} else {
		a.messagepane.SelectByTS(ts)
	}
	_ = a.beginEditOfSelected()
	if !a.editing.Matches("C1", ts) {
		t.Fatalf("setup: no edit of %s began", ts)
	}
	c := &a.compose
	if panel == PanelThread {
		c = &a.threadCompose
	}
	c.SetValue(typed)
	edit := deliveredMsgs[EditMessageMsg](t, pressEnter(a))[0]
	_, cmd := a.Update(edit)
	return deliveredMsgs[MessageEditedMsg](t, cmd)[0]
}

// textOf is the text of the first row with ts, and whether it shows as
// edited.
func textOf(t *testing.T, rows []messages.MessageItem, ts string) (string, bool) {
	t.Helper()
	i := messages.FirstIndexOfTS(rows, ts)
	if i < 0 {
		t.Fatalf("no row %s in %q", ts, rowsOf(rows))
	}
	return rows[i].Text, rows[i].IsEdited
}

func wantText(t *testing.T, what string, rows []messages.MessageItem, ts, want string, wantEdited bool) {
	t.Helper()
	if got, edited := textOf(t, rows, ts); got != want || edited != wantEdited {
		t.Errorf("%s: %s = %q edited=%v, want %q edited=%v", what, ts, got, edited, want, wantEdited)
	}
}

// An edit shows at once, converted to mrkdwn and marked edited, in
// every pane showing the message, and the editor closes as a send's
// composer does.
func TestPendingEdit_ShowsAtOnce(t *testing.T) {
	t.Run("channel message", func(t *testing.T) {
		a := editApp(t, nil)
		a.compose.SetValue("stashed draft")
		_ = editFrom(t, a, PanelMessages, "1.000000", "**new**")
		wantText(t, "channel", a.messagepane.Messages(), "1.000000", "*new*", true)
		if a.editing.IsActive() || a.mode != ModeNormal {
			t.Errorf("edit active=%v mode=%v, want the editor closed", a.editing.IsActive(), a.mode)
		}
		if got := a.compose.Value(); got != "stashed draft" {
			t.Errorf("composer = %q, want the stashed draft back", got)
		}
	})
	t.Run("thread reply", func(t *testing.T) {
		a := editApp(t, nil)
		_ = editFrom(t, a, PanelThread, "6.000000", "new reply")
		wantText(t, "thread", a.threadPanel.Replies(), "6.000000", "new reply", true)
	})
	t.Run("thread parent from the channel", func(t *testing.T) {
		a := editApp(t, nil)
		_ = editFrom(t, a, PanelMessages, "5.000000", "new parent")
		wantText(t, "channel", a.messagepane.Messages(), "5.000000", "new parent", true)
		if p := a.threadPanel.ParentMsg(); p.Text != "new parent" || !p.IsEdited {
			t.Errorf("thread parent = %q edited=%v, want the edit", p.Text, p.IsEdited)
		}
	})
}

// The result lands the body chat.update sent.
func TestPendingEdit_SuccessShowsTheSentBody(t *testing.T) {
	a := editApp(t, nil)
	res := editFrom(t, a, PanelMessages, "1.000000", "new")
	if res.EditID == 0 {
		t.Fatalf("result = %+v, want the edit id stamped", res)
	}
	a.Update(res)
	wantText(t, "channel", a.messagepane.Messages(), "1.000000", "new", true)
	if a.pendingOps.EditPending("C1", "1.000000") {
		t.Error("edit still pending after its result")
	}
}

// A failed edit puts back what was there and reopens the editor on the
// message with the text as typed, in the composer it came from.
func TestPendingEdit_FailureRestoresAndReopens(t *testing.T) {
	t.Run("channel", func(t *testing.T) {
		a := editApp(t, errors.New("boom"))
		a.compose.SetValue("stashed draft")
		res := editFrom(t, a, PanelMessages, "1.000000", "hi @alice")
		_, cmd := a.Update(res)
		if cmd == nil {
			t.Error("failure reported nothing")
		}
		wantText(t, "channel", a.messagepane.Messages(), "1.000000", "one", false)
		if !a.editing.Matches("C1", "1.000000") || a.editing.Panel() != PanelMessages || a.mode != ModeInsert {
			t.Fatalf("editing=%+v mode=%v, want the edit reopened", *a.editing, a.mode)
		}
		if got := a.compose.Value(); got != "hi @alice" {
			t.Errorf("composer = %q, want the typed text", got)
		}
		a.cancelEdit()
		if got := a.compose.Value(); got != "stashed draft" {
			t.Errorf("composer after cancel = %q, want the stashed draft", got)
		}
	})
	t.Run("thread", func(t *testing.T) {
		a := editApp(t, errors.New("boom"))
		res := editFrom(t, a, PanelThread, "6.000000", "new reply")
		a.Update(res)
		wantText(t, "thread", a.threadPanel.Replies(), "6.000000", "reply", false)
		if !a.editing.Matches("C1", "6.000000") || a.editing.Panel() != PanelThread {
			t.Fatalf("editing=%+v, want the thread edit reopened", *a.editing)
		}
		if got := a.threadCompose.Value(); got != "new reply" {
			t.Errorf("thread composer = %q, want the typed text", got)
		}
	})
}

// When the user has moved on by the time an edit fails, the editor
// stays shut and the text goes back as the conversation's draft.
func TestPendingEdit_FailureAfterMovingOnGivesDraft(t *testing.T) {
	t.Run("typing elsewhere", func(t *testing.T) {
		a := editApp(t, errors.New("boom"))
		res := editFrom(t, a, PanelThread, "6.000000", "new reply")
		a.focusedPanel = PanelMessages
		a.mode = ModeInsert
		a.Update(res)
		if a.editing.IsActive() {
			t.Fatal("edit reopened while the user was typing elsewhere")
		}
		if got := a.threadCompose.Value(); got != "new reply" {
			t.Errorf("thread composer = %q, want the text as its draft", got)
		}
	})
	t.Run("editing another message", func(t *testing.T) {
		a := editApp(t, errors.New("boom"))
		res := editFrom(t, a, PanelThread, "6.000000", "new reply")
		a.focusedPanel = PanelMessages
		a.messagepane.SelectByTS("1.000000")
		_ = a.beginEditOfSelected()
		a.Update(res)
		if !a.editing.Matches("C1", "1.000000") {
			t.Fatalf("editing=%+v, want the other edit left alone", *a.editing)
		}
		if got := a.threadCompose.Value(); got != "new reply" {
			t.Errorf("thread composer = %q, want the text as its draft", got)
		}
	})
	t.Run("message gone", func(t *testing.T) {
		a := editApp(t, errors.New("boom"))
		res := editFrom(t, a, PanelMessages, "1.000000", "new")
		a.Update(WSMessageDeletedMsg{ChannelID: "C1", TS: "1.000000"})
		a.Update(res)
		if a.editing.IsActive() {
			t.Fatal("edit reopened on a deleted message")
		}
		if got := a.compose.Value(); got != "new" {
			t.Errorf("composer = %q, want the text as a draft", got)
		}
	})
}

// The message_changed echo of the user's own edit changes nothing,
// whether it beats the result or follows it.
func TestPendingEdit_EchoIsHarmless(t *testing.T) {
	editEcho := func(text string) NewMessageMsg {
		m := echo("C1", "1.000000", "", text)
		m.Message.IsEdited = true
		return m
	}
	t.Run("before the result", func(t *testing.T) {
		a := editApp(t, nil)
		res := editFrom(t, a, PanelMessages, "1.000000", "new")
		a.Update(editEcho("new"))
		a.Update(res)
		wantText(t, "channel", a.messagepane.Messages(), "1.000000", "new", true)
	})
	t.Run("after the result", func(t *testing.T) {
		a := editApp(t, nil)
		res := editFrom(t, a, PanelMessages, "1.000000", "new")
		a.Update(res)
		a.Update(editEcho("new"))
		wantText(t, "channel", a.messagepane.Messages(), "1.000000", "new", true)
	})
	t.Run("before a failure", func(t *testing.T) {
		a := editApp(t, errors.New("boom"))
		res := editFrom(t, a, PanelMessages, "1.000000", "new")
		a.Update(editEcho("from another client"))
		wantText(t, "channel", a.messagepane.Messages(), "1.000000", "new", true)
		a.Update(res)
		wantText(t, "channel", a.messagepane.Messages(), "1.000000", "one", false)
	})
	t.Run("no edit pending", func(t *testing.T) {
		a := editApp(t, nil)
		a.Update(editEcho("from another client"))
		wantText(t, "channel", a.messagepane.Messages(), "1.000000", "from another client", true)
	})
}

// Two quick edits of one message: the screen shows the newer one until
// its result, whatever order the older edit's echo and result arrive in.
func TestPendingEdit_TwoQuickEdits(t *testing.T) {
	older := func(m NewMessageMsg) NewMessageMsg { m.Message.IsEdited = true; return m }
	t.Run("both succeed", func(t *testing.T) {
		a := editApp(t, nil)
		first := editFrom(t, a, PanelMessages, "1.000000", "first")
		second := editFrom(t, a, PanelMessages, "1.000000", "second")
		wantText(t, "after both edits", a.messagepane.Messages(), "1.000000", "second", true)
		a.Update(older(echo("C1", "1.000000", "", "first")))
		wantText(t, "after the first's echo", a.messagepane.Messages(), "1.000000", "second", true)
		a.Update(first)
		a.Update(older(echo("C1", "1.000000", "", "first")))
		wantText(t, "after the first's result and a late echo", a.messagepane.Messages(), "1.000000", "second", true)
		a.Update(second)
		wantText(t, "after the second's result", a.messagepane.Messages(), "1.000000", "second", true)
	})
	t.Run("results out of order", func(t *testing.T) {
		a := editApp(t, nil)
		first := editFrom(t, a, PanelMessages, "1.000000", "first")
		second := editFrom(t, a, PanelMessages, "1.000000", "second")
		a.Update(second)
		a.Update(first)
		wantText(t, "channel", a.messagepane.Messages(), "1.000000", "second", true)
	})
	t.Run("newer one fails", func(t *testing.T) {
		a := editApp(t, nil)
		first := editFrom(t, a, PanelMessages, "1.000000", "first")
		second := editFrom(t, a, PanelMessages, "1.000000", "second")
		second.Err, second.Text = errors.New("boom"), ""
		a.Update(first)
		a.Update(second)
		wantText(t, "channel", a.messagepane.Messages(), "1.000000", "first", true)
		if got := a.compose.Value(); got != "second" || !a.editing.IsActive() {
			t.Errorf("composer = %q, editing=%v, want the second edit reopened", got, a.editing.IsActive())
		}
	})
	t.Run("older one fails", func(t *testing.T) {
		a := editApp(t, nil)
		first := editFrom(t, a, PanelMessages, "1.000000", "first")
		second := editFrom(t, a, PanelMessages, "1.000000", "second")
		first.Err, first.Text = errors.New("boom"), ""
		a.Update(first)
		wantText(t, "channel", a.messagepane.Messages(), "1.000000", "second", true)
		if a.editing.IsActive() || a.compose.Value() != "" {
			t.Errorf("superseded edit given back: editing=%v composer=%q", a.editing.IsActive(), a.compose.Value())
		}
		a.Update(second)
		wantText(t, "channel", a.messagepane.Messages(), "1.000000", "second", true)
	})
}

// With no active workspace the service answers nil; the edit fails
// like any other.
func TestPendingEdit_NilResultFails(t *testing.T) {
	a := editApp(t, nil)
	a.SetMessageService(core.NewMessageService(core.MessageServiceFuncs{
		Edit: func(ids.ChannelID, ids.MessageTS, string) core.Msg { return nil },
	}))
	res := editFrom(t, a, PanelMessages, "1.000000", "new")
	if res.Err == nil || res.Err.Error() != noWorkspaceReason || res.ChannelID != "C1" || res.TS != "1.000000" {
		t.Fatalf("nil result became %+v, want a failure", res)
	}
	a.Update(res)
	wantText(t, "channel", a.messagepane.Messages(), "1.000000", "one", false)
	if a.pendingOps.EditPending("C1", "1.000000") {
		t.Error("edit still pending")
	}
}

// What the panes draw follows every edit, on a message whose body is
// drawn from its rich_text block: the optimistic edit, its success,
// its failure and another client's message_changed, whose text and
// blocks are Slack's and are shown as they are.
func TestPendingEdit_DrawsTheEditedBody(t *testing.T) {
	t.Run("optimistic, then success", func(t *testing.T) {
		a := editApp(t, nil)
		res := editFrom(t, a, PanelMessages, "5.000000", "fresh *words*")
		ch, th := drawn(a)
		wantDrawn(t, "channel", ch, "fresh words", "parent")
		wantDrawn(t, "thread parent", th, "fresh words", "parent")
		a.Update(res)
		ch, th = drawn(a)
		wantDrawn(t, "channel after success", ch, "fresh words", "parent")
		wantDrawn(t, "thread parent after success", th, "fresh words", "parent")
	})
	t.Run("thread reply", func(t *testing.T) {
		a := editApp(t, nil)
		_ = editFrom(t, a, PanelThread, "6.000000", "fresh words")
		_, th := drawn(a)
		wantDrawn(t, "thread", th, "fresh words", "▌reply")
	})
	t.Run("failure", func(t *testing.T) {
		a := editApp(t, errors.New("boom"))
		res := editFrom(t, a, PanelMessages, "1.000000", "fresh words")
		a.Update(res)
		ch, _ := drawn(a)
		wantDrawn(t, "channel", ch, "one", "fresh words")
	})
	t.Run("another client's edit", func(t *testing.T) {
		a := editApp(t, nil)
		changed := typedRow("1.000000", "slack blocks", "")
		changed.Text, changed.IsEdited = "stale fallback", true
		a.Update(NewMessageMsg{ChannelID: "C1", Message: changed})
		ch, _ := drawn(a)
		wantDrawn(t, "channel", ch, "slack blocks", "one")
	})
}
