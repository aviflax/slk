package ui

import (
	"errors"
	"fmt"
	"strings"
	"testing"
	"time"

	tea "charm.land/bubbletea/v2"

	"github.com/charmbracelet/x/ansi"

	"github.com/gammons/slk/internal/core"
	"github.com/gammons/slk/internal/ids"
	"github.com/gammons/slk/internal/ui/messages"
	"github.com/gammons/slk/internal/ui/statusbar"
)

// editApp is a typedApp in normal mode on C1 whose own messages are
// 1.000000 ("one") and the parent of thread 5.000000 ("parent"), with
// that thread open on reply 6.000000 ("reply"). Every row carries the
// rich_text block Slack stores (typedRow). Its edit service answers
// with the text sent, or with err.
func editApp(t *testing.T, err error) *App {
	t.Helper()
	a, _ := editAppLog(t, err)
	return a
}

// editAppLog is editApp, with the log of the texts its edit service
// was asked to send, in order.
func editAppLog(t *testing.T, err error) (*App, *[]string) {
	t.Helper()
	a := typedApp(t)
	a.mode = ModeNormal
	parent := typedRow("5.000000", "parent", "")
	parent.ReplyCount = 1
	a.messagepane.SetMessages([]messages.MessageItem{typedRow("1.000000", "one", ""), parent})
	a.threadPanel.SetThread(parent, []messages.MessageItem{
		typedRow("6.000000", "reply", "5.000000"),
	}, "C1", "5.000000")
	var sent []string
	a.SetMessageService(core.NewMessageService(core.MessageServiceFuncs{
		Edit: func(ch ids.ChannelID, ts ids.MessageTS, text string) core.Msg {
			sent = append(sent, text)
			if err != nil {
				return MessageEditedMsg{ChannelID: string(ch), TS: string(ts), Err: err}
			}
			return MessageEditedMsg{ChannelID: string(ch), TS: string(ts), Text: text}
		},
	}))
	return a, &sent
}

// typedRow is one of the user's messages as Slack returns it: the
// mrkdwn text and the rich_text block slk and the web client attach to
// everything typed. The pane draws the body from that block.
func typedRow(ts, text, threadTS string) messages.MessageItem {
	row := messages.MessageItem{TS: ts, UserID: "USELF", ThreadTS: threadTS}
	body := editedBody(text)
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

// typeEdit edits message ts from panel's composer the way the user
// does (select, e, type, Enter), applies the edit and returns its
// command: the chat.update, or nil when the edit waits behind one in
// flight.
func typeEdit(t *testing.T, a *App, panel Panel, ts, typed string) tea.Cmd {
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
	return cmd
}

// editFrom is typeEdit for an edit sent at once, and returns the
// service's result, not yet delivered.
func editFrom(t *testing.T, a *App, panel Panel, ts, typed string) MessageEditedMsg {
	t.Helper()
	return deliveredMsgs[MessageEditedMsg](t, typeEdit(t, a, panel, ts, typed))[0]
}

// queueEdit is typeEdit for an edit that must wait behind the one in
// flight.
func queueEdit(t *testing.T, a *App, panel Panel, ts, typed string) {
	t.Helper()
	if cmd := typeEdit(t, a, panel, ts, typed); cmd != nil {
		t.Fatalf("edit %q was sent while another was in flight", typed)
	}
}

// settleEdit delivers res and returns the result of the edit it sends
// next, the one that waited behind it.
func settleEdit(t *testing.T, a *App, res MessageEditedMsg) MessageEditedMsg {
	t.Helper()
	_, cmd := a.Update(res)
	for _, m := range drainMsgs(cmd, 200*time.Millisecond) {
		if next, ok := m.(MessageEditedMsg); ok {
			return next
		}
	}
	t.Fatalf("result of %d sent no waiting edit", res.EditID)
	return MessageEditedMsg{}
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
		// A draft would post the text as a new message: it goes to
		// the clipboard.
		for _, how := range []string{"deleted elsewhere", "delete pending", "not found"} {
			t.Run(how, func(t *testing.T) {
				a := editApp(t, errors.New("boom"))
				copied := clipboardLog(a)
				res := editFrom(t, a, PanelMessages, "1.000000", "new")
				switch how {
				case "deleted elsewhere":
					a.Update(WSMessageDeletedMsg{ChannelID: "C1", TS: "1.000000"})
				case "delete pending":
					a.Update(DeleteMessageMsg{ChannelID: "C1", TS: "1.000000"})
				case "not found":
					res.NotFound = true
				}
				_, cmd := a.Update(res)
				if toast := shownToast(t, a, cmd); !strings.Contains(toast, "clipboard") {
					t.Errorf("toast = %q, want it to say the text is on the clipboard", toast)
				}
				if a.editing.IsActive() || a.compose.Value() != "" || len(*copied) != 1 || (*copied)[0] != "new" {
					t.Errorf("editing=%v composer=%q clipboard=%q, want the text on the clipboard only",
						a.editing.IsActive(), a.compose.Value(), *copied)
				}
			})
		}
	})
}

// Slack's message_not_found for an edit means the message is gone: it
// leaves every pane, as its deletion would, rather than staying with
// its old text beside a toast that says it does not exist.
func TestPendingEdit_NotFoundRemovesTheRow(t *testing.T) {
	for _, tc := range []struct {
		name  string
		panel Panel
		ts    string
	}{
		{"channel message", PanelMessages, "1.000000"},
		{"thread reply", PanelThread, "6.000000"},
	} {
		t.Run(tc.name, func(t *testing.T) {
			a := editApp(t, errors.New("message_not_found"))
			res := editFrom(t, a, tc.panel, tc.ts, "new")
			res.NotFound = true
			a.Update(res)
			for _, rows := range [][]messages.MessageItem{a.messagepane.Messages(), a.threadPanel.Replies()} {
				if messages.FirstIndexOfTS(rows, tc.ts) >= 0 {
					t.Errorf("%s still shown after message_not_found: %q", tc.ts, rowsOf(rows))
				}
			}
		})
	}
}

// A message_changed that Slack does not mark edited (an app updating
// its message, an unfurl, a parent's reply count) shows Slack's body in
// place, without an "(edited)" mark, and is not taken for a new
// message.
func TestMessageChangedWithoutEditIsNotMarkedEdited(t *testing.T) {
	a := editApp(t, nil)
	updated := typedRow("1.000000", "updated by an app", "")
	a.Update(NewMessageMsg{ChannelID: "C1", Changed: true, Message: updated})
	wantText(t, "channel", a.messagepane.Messages(), "1.000000", "updated by an app", false)
	if n := len(a.messagepane.Messages()); n != 2 {
		t.Errorf("channel has %d rows, want the 2 it had", n)
	}
}

// The message_changed echo of the user's own edit changes nothing,
// whether it beats the result or follows it.
func TestPendingEdit_EchoIsHarmless(t *testing.T) {
	editEcho := func(text string) NewMessageMsg {
		m := echo("C1", "1.000000", "", text)
		m.Changed, m.Message.IsEdited = true, true
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
	t.Run("another client's, before a failure", func(t *testing.T) {
		// The echo is Slack's state; the failed edit never replaced it.
		a := editApp(t, errors.New("boom"))
		res := editFrom(t, a, PanelMessages, "1.000000", "new")
		a.Update(editEcho("from another client"))
		wantText(t, "channel", a.messagepane.Messages(), "1.000000", "new", true)
		a.Update(res)
		wantText(t, "channel", a.messagepane.Messages(), "1.000000", "from another client", true)
		if !a.editing.Matches("C1", "1.000000") {
			t.Error("failed edit not reopened")
		}
	})
	t.Run("no edit pending", func(t *testing.T) {
		a := editApp(t, nil)
		a.Update(editEcho("from another client"))
		wantText(t, "channel", a.messagepane.Messages(), "1.000000", "from another client", true)
	})
}

// Quick edits of one message: one chat.update is in flight at a time,
// a newer edit replaces the one waiting, and the screen shows the
// newest edit throughout, whatever order the older edit's echo and
// result arrive in.
func TestPendingEdit_QuickEdits(t *testing.T) {
	older := func(m NewMessageMsg) NewMessageMsg { m.Changed, m.Message.IsEdited = true, true; return m }
	t.Run("both succeed", func(t *testing.T) {
		a, sent := editAppLog(t, nil)
		first := editFrom(t, a, PanelMessages, "1.000000", "first")
		queueEdit(t, a, PanelMessages, "1.000000", "second")
		wantText(t, "after both edits", a.messagepane.Messages(), "1.000000", "second", true)
		a.Update(older(echo("C1", "1.000000", "", "first")))
		wantText(t, "after the first's echo", a.messagepane.Messages(), "1.000000", "second", true)
		second := settleEdit(t, a, first)
		a.Update(older(echo("C1", "1.000000", "", "first")))
		wantText(t, "after the first's result and a late echo", a.messagepane.Messages(), "1.000000", "second", true)
		a.Update(second)
		wantText(t, "after the second's result", a.messagepane.Messages(), "1.000000", "second", true)
		if strings.Join(*sent, ",") != "first,second" {
			t.Errorf("sent %q, want first then second", *sent)
		}
	})
	t.Run("the newest waiting edit wins", func(t *testing.T) {
		a, sent := editAppLog(t, nil)
		first := editFrom(t, a, PanelMessages, "1.000000", "first")
		queueEdit(t, a, PanelMessages, "1.000000", "second")
		queueEdit(t, a, PanelMessages, "1.000000", "third")
		wantText(t, "while waiting", a.messagepane.Messages(), "1.000000", "third", true)
		a.Update(settleEdit(t, a, first))
		wantText(t, "settled", a.messagepane.Messages(), "1.000000", "third", true)
		if strings.Join(*sent, ",") != "first,third" {
			t.Errorf("sent %q, want first then third", *sent)
		}
		if a.pendingOps.EditPending("C1", "1.000000") {
			t.Error("edit still pending")
		}
	})
	t.Run("newer one fails", func(t *testing.T) {
		a := editApp(t, nil)
		first := editFrom(t, a, PanelMessages, "1.000000", "first")
		queueEdit(t, a, PanelMessages, "1.000000", "second")
		second := settleEdit(t, a, first)
		second.Err, second.Text = errors.New("boom"), ""
		a.Update(second)
		wantText(t, "channel", a.messagepane.Messages(), "1.000000", "first", true)
		if got := a.compose.Value(); got != "second" || !a.editing.IsActive() {
			t.Errorf("composer = %q, editing=%v, want the second edit reopened", got, a.editing.IsActive())
		}
	})
	t.Run("older one fails", func(t *testing.T) {
		a := editApp(t, nil)
		first := editFrom(t, a, PanelMessages, "1.000000", "first")
		queueEdit(t, a, PanelMessages, "1.000000", "second")
		first.Err, first.Text = errors.New("boom"), ""
		second := settleEdit(t, a, first)
		wantText(t, "channel", a.messagepane.Messages(), "1.000000", "second", true)
		if a.editing.IsActive() || a.compose.Value() != "" {
			t.Errorf("superseded edit given back: editing=%v composer=%q", a.editing.IsActive(), a.compose.Value())
		}
		a.Update(second)
		wantText(t, "channel", a.messagepane.Messages(), "1.000000", "second", true)
	})
	t.Run("another client's edit lands after ours", func(t *testing.T) {
		a := editApp(t, nil)
		first := editFrom(t, a, PanelMessages, "1.000000", "first")
		a.Update(older(echo("C1", "1.000000", "", "first")))
		a.Update(older(echo("C1", "1.000000", "", "theirs")))
		a.Update(first)
		wantText(t, "channel", a.messagepane.Messages(), "1.000000", "theirs", true)
	})
}

// Slack stores a message's text with &, < and > escaped as entities
// (outside its own <...> tokens), and keeps the rich_text block as sent.
// An echo in that form carries the edit just as an echo of the exact
// text does: a timeout after it is no failure, and the message keeps
// Slack's own form of the body, whether the result failed or succeeded.
func TestPendingEdit_EchoInSlacksFormProvesTheEdit(t *testing.T) {
	for _, tc := range []struct{ name, typed, stored string }{
		{"ampersand", "a & b", "a &amp; b"},
		{"angle brackets", "x < y > z", "x &lt; y &gt; z"},
		{"url with a query", "see https://x.test/?a=1&b=2", "see <https://x.test/?a=1&amp;b=2>"},
		{"mention", "hi <@U1> & co", "hi <@U1> &amp; co"},
	} {
		for _, blocks := range []bool{true, false} {
			for _, outcome := range []string{"timeout", "ok"} {
				name := fmt.Sprintf("%s/blocks=%v/%s", tc.name, blocks, outcome)
				t.Run(name, func(t *testing.T) {
					var err error
					if outcome == "timeout" {
						err = errors.New("timeout")
					}
					a := editApp(t, err)
					copied := clipboardLog(a)
					res := editFrom(t, a, PanelMessages, "1.000000", tc.typed)
					body := editedBody(tc.typed)
					body.Text = tc.stored
					if !blocks {
						body.Blocks = nil
					}
					changed := messages.MessageItem{TS: "1.000000", UserID: "USELF"}
					body.Apply(&changed)
					a.Update(NewMessageMsg{ChannelID: "C1", Changed: true, Message: changed})
					a.Update(res)
					if a.editing.IsActive() || a.compose.Value() != "" || len(*copied) != 0 {
						t.Errorf("editing=%v composer=%q clipboard=%q, want an edit Slack stored left alone",
							a.editing.IsActive(), a.compose.Value(), *copied)
					}
					wantText(t, "channel", a.messagepane.Messages(), "1.000000", tc.stored, true)
				})
			}
		}
	}
}

// An edit whose chat.update failed after Slack stored it (a timeout) is
// not a failure: its echo carried the text, so the message keeps it,
// and the editor does not reopen to invite the same edit again.
func TestPendingEdit_TimeoutAfterSlackStoredIt(t *testing.T) {
	a := editApp(t, errors.New("timeout"))
	copied := clipboardLog(a)
	res := editFrom(t, a, PanelMessages, "1.000000", "fresh words")
	changed := typedRow("1.000000", "fresh words", "")
	changed.IsEdited = true
	a.Update(NewMessageMsg{ChannelID: "C1", Changed: true, Message: changed})
	_, cmd := a.Update(res)
	for _, m := range drainMsgs(cmd, 50*time.Millisecond) {
		if f, ok := m.(statusbar.EditFailedMsg); ok {
			t.Errorf("reported %+v for an edit Slack stored", f)
		}
	}
	ch, _ := drawn(a)
	wantDrawn(t, "channel", ch, "fresh words", "one")
	if a.editing.IsActive() || a.compose.Value() != "" || len(*copied) != 0 {
		t.Errorf("editing=%v composer=%q clipboard=%q, want the text not given back", a.editing.IsActive(), a.compose.Value(), *copied)
	}
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
		a.Update(NewMessageMsg{ChannelID: "C1", Changed: true, Message: changed})
		ch, _ := drawn(a)
		wantDrawn(t, "channel", ch, "slack blocks", "one")
	})
}
