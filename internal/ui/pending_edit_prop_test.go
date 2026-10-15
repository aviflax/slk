package ui

import (
	"errors"
	"fmt"
	"reflect"
	"slices"
	"testing"
	"time"

	tea "charm.land/bubbletea/v2"
	"pgregory.net/rapid"

	"github.com/gammons/slk/internal/core"
	"github.com/gammons/slk/internal/ids"
	"github.com/gammons/slk/internal/ui/messages"
)

// firstEdited runs cmd (batch members concurrently, as the runtime
// does) and returns the first MessageEditedMsg it delivers, waiting
// only as long as it takes to arrive.
func firstEdited(cmd tea.Cmd) (MessageEditedMsg, bool) {
	ch := make(chan MessageEditedMsg, 1)
	go deliverFirst(cmd, ch)
	select {
	case m := <-ch:
		return m, true
	case <-time.After(2 * time.Second):
		return MessageEditedMsg{}, false
	}
}

// slackModel is Slack as the source of truth for the edit and delete
// test below: each message's stored body, which messages are deleted,
// and the WebSocket events it has sent and the App has not yet seen,
// in order. It applies a chat.update when it receives it.
type slackModel struct {
	body    map[string]messages.Body
	deleted map[string]bool
	events  []slackEvent
}

// slackEvent is a WebSocket event on its way. droppable marks the echo
// of an edit whose chat.update succeeded: the WebSocket may lose it (a
// reconnect), and the App must not need it.
type slackEvent struct {
	msg       tea.Msg
	droppable bool
}

// queued reports whether an event about ts is on its way.
func (s *slackModel) queued(ts string) bool {
	return slices.ContainsFunc(s.events, func(e slackEvent) bool {
		nm, ok := e.msg.(NewMessageMsg)
		return ok && nm.Message.TS == ts
	})
}

// change stores body for ts and sends its message_changed echo.
func (s *slackModel) change(ts string, body messages.Body, droppable bool) {
	s.body[ts] = body
	row := messages.MessageItem{TS: ts, UserID: "USELF"}
	if ts == "6.000000" {
		row.ThreadTS = "5.000000"
	}
	body.Apply(&row)
	s.events = append(s.events, slackEvent{NewMessageMsg{ChannelID: "C1", Message: row}, droppable})
}

// However the user's edits and deletes of their messages interleave
// with their results (success, failure, or a failure after Slack
// stored the edit), Slack's echoes, other clients' edits and older
// history loading:
//
//   - at most one chat.update per message is in flight, and Slack
//     receives a message's edits in the order the user made them;
//   - the rows Slack knows stay in TS order in both panes;
//   - a failed edit's text that Slack does not have goes back to the
//     user: the editor, a composer or the clipboard;
//   - once nothing is in flight and every event has arrived (or been
//     lost, for the echo of an edit Slack accepted), every
//     pane shows exactly Slack's body for each message (text, blocks
//     and edited mark), and a message is absent exactly when Slack
//     deleted it.
func TestEditsAndDeletesEndOnSlacksState(t *testing.T) {
	rapid.Check(t, func(rt *rapid.T) {
		a, sent := editAppLog(t, nil)
		copied := clipboardLog(a)
		deleteRes := map[string]MessageDeletedMsg{} // results not yet delivered
		a.SetMessageService(core.NewMessageService(core.MessageServiceFuncs{
			Edit: a.messageSvc.Edit,
			Delete: func(ch ids.ChannelID, ts ids.MessageTS) core.Msg {
				return MessageDeletedMsg{ChannelID: string(ch), TS: string(ts)}
			},
		}))
		slack := &slackModel{body: map[string]messages.Body{}, deleted: map[string]bool{}}
		for _, ts := range []string{"1.000000", "5.000000", "6.000000"} {
			row, _ := findRow(a, ts)
			slack.body[ts] = messages.BodyOf(row)
		}

		// inFlight holds each message's chat.update result, not yet
		// delivered; wire is the text of each edit by id; echoed is set
		// when an echo of the in-flight edit's text has been delivered.
		inFlight := map[string]MessageEditedMsg{}
		wire := map[uint64]string{}
		echoed := map[string]bool{}
		typedOrder := map[string][]string{} // per message, every text typed
		older := 0
		words := 0

		// collect takes the result of the edit the App now has in
		// flight for ts, if it sent a new one, from cmd.
		collect := func(ts string, cmd tea.Cmd) {
			m := a.pendingOps.edits[editKey{"C1", ts}]
			if m == nil {
				return
			}
			if have, ok := inFlight[ts]; ok && have.EditID == m.inFlight.id {
				return
			}
			before := len(*sent)
			res, ok := firstEdited(cmd)
			if !ok || res.EditID != m.inFlight.id {
				rt.Fatalf("%s: edit %d in flight but its chat.update was not sent (got %+v)", ts, m.inFlight.id, res)
			}
			if len(*sent) != before+1 || (*sent)[before] != m.inFlight.wire {
				rt.Fatalf("%s: sent %q, want %q once", ts, (*sent)[before:], m.inFlight.wire)
			}
			inFlight[ts] = res
			echoed[ts] = false
		}

		shown := func(ts string) bool { _, ok := findRow(a, ts); return ok }

		step := func(action string) {
			switch action {
			case "edit":
				ts := rapid.SampledFrom([]string{"1.000000", "5.000000", "6.000000"}).Draw(rt, "editTS")
				if !shown(ts) {
					return
				}
				words++
				text := fmt.Sprintf("w%d", words)
				msg := EditMessageMsg{ChannelID: "C1", TS: ts, NewText: text, Typed: text, TeamID: "T1", Panel: PanelMessages}
				if ts == "6.000000" {
					msg.Panel, msg.ThreadTS = PanelThread, "5.000000"
				}
				_, cmd := a.Update(msg)
				typedOrder[ts] = append(typedOrder[ts], text)
				wire[a.pendingOps.editID] = text
				collect(ts, cmd)
			case "result":
				ts := rapid.SampledFrom([]string{"1.000000", "5.000000", "6.000000"}).Draw(rt, "resultTS")
				res, ok := inFlight[ts]
				if !ok {
					return
				}
				delete(inFlight, ts)
				text := wire[res.EditID]
				outcome := rapid.SampledFrom([]string{"ok", "fail", "timeout"}).Draw(rt, "outcome")
				switch {
				case slack.deleted[ts]:
					res.Err, res.NotFound = errors.New("message_not_found"), true
				case outcome == "ok":
					// Its echo may be lost, unless an older change of
					// the message is still on its way: an echo that
					// stale arriving after the result, with the newer
					// echo lost, is indistinguishable from a newer
					// change by another client.
					slack.change(ts, messages.EditedBody(text), !slack.queued(ts))
				case outcome == "timeout":
					slack.change(ts, messages.EditedBody(text), false)
					res.Err = errors.New("timeout")
				default:
					res.Err = errors.New("boom")
				}
				m := a.pendingOps.edits[editKey{"C1", ts}]
				queued := m != nil && m.queued != nil
				gone := slack.deleted[ts]
				// The user hears of it and gets the text back unless
				// Slack's echo proved it stored it, or a newer edit is
				// on its way.
				wantGiveBack := res.Err != nil && !echoed[ts] && (!queued || gone)
				giveBack := text
				if queued && gone {
					giveBack = m.queued.typed
				}
				_, cmd := a.Update(res)
				collect(ts, cmd)
				if wantGiveBack {
					got := a.compose.Value() == giveBack || a.threadCompose.Value() == giveBack || slices.Contains(*copied, giveBack)
					if !got {
						rt.Fatalf("%s: failed edit %q lost: composer %q, thread composer %q, clipboard %q",
							ts, giveBack, a.compose.Value(), a.threadCompose.Value(), *copied)
					}
					if gone && (a.compose.Value() == giveBack || a.threadCompose.Value() == giveBack) && !a.editing.IsActive() {
						rt.Fatalf("%s: text of an edit of a deleted message left as a draft", ts)
					}
				}
				if a.editing.IsActive() {
					a.cancelEdit()
				}
				a.compose.SetValue("")
				a.threadCompose.SetValue("")
				a.mode = ModeNormal
				*copied = nil
			case "event":
				if len(slack.events) == 0 {
					return
				}
				ev := slack.events[0].msg
				slack.events = slack.events[1:]
				if nm, ok := ev.(NewMessageMsg); ok {
					if res, pending := inFlight[nm.Message.TS]; pending && nm.Message.Text == messages.EditedBody(wire[res.EditID]).Text {
						echoed[nm.Message.TS] = true
					}
				}
				a.Update(ev)
			case "other":
				ts := rapid.SampledFrom([]string{"1.000000", "5.000000", "6.000000"}).Draw(rt, "otherTS")
				if slack.deleted[ts] {
					return
				}
				words++
				slack.change(ts, messages.EditedBody(fmt.Sprintf("o%d", words)), false)
			case "delete":
				ts := rapid.SampledFrom([]string{"1.000000", "6.000000"}).Draw(rt, "deleteTS")
				if _, pending := deleteRes[ts]; pending || !shown(ts) {
					return
				}
				_, cmd := a.Update(DeleteMessageMsg{ChannelID: "C1", TS: ts})
				deleteRes[ts] = deliveredMsgs[MessageDeletedMsg](t, cmd)[0]
			case "deleteResult":
				ts := rapid.SampledFrom([]string{"1.000000", "6.000000"}).Draw(rt, "deleteResultTS")
				res, pending := deleteRes[ts]
				if !pending {
					return
				}
				delete(deleteRes, ts)
				switch {
				case slack.deleted[ts]:
					res.Err, res.NotFound = errors.New("message_not_found"), true
				case rapid.Bool().Draw(rt, "deleteOK"):
					slack.deleted[ts] = true
					slack.events = append(slack.events, slackEvent{msg: WSMessageDeletedMsg{ChannelID: "C1", TS: ts}})
				default:
					res.Err = errors.New("boom")
				}
				a.Update(res)
			case "drop":
				if len(slack.events) > 0 && slack.events[0].droppable {
					slack.events = slack.events[1:]
				}
			case "prepend":
				older++
				a.messagepane.PrependMessages([]messages.MessageItem{
					{TS: fmt.Sprintf("0.%06d", 999_999-older), Text: "older"},
				})
			}
		}

		check := func() {
			for _, rows := range [][]messages.MessageItem{a.messagepane.Messages(), a.threadPanel.Replies()} {
				if !realSorted(rows) {
					rt.Fatalf("rows out of TS order: %q", rowsOf(rows))
				}
			}
			for ts, typed := range typedOrder {
				var got []string
				for _, s := range *sent {
					if slices.Contains(typed, s) {
						got = append(got, s)
					}
				}
				// Every text was typed once, so sent is in typed order
				// exactly when it is a subsequence of it.
				i := 0
				for _, s := range typed {
					if i < len(got) && got[i] == s {
						i++
					}
				}
				if i != len(got) {
					rt.Fatalf("%s: sent %q out of the typed order %q", ts, got, typed)
				}
			}
		}
		quiet := func() bool {
			return len(inFlight) == 0 && len(deleteRes) == 0 && len(slack.events) == 0
		}
		settled := func() {
			for ts, want := range slack.body {
				wantShown := !slack.deleted[ts]
				for _, pane := range panesOf(a, ts) {
					body, ok := pane.body, pane.ok
					if ok != wantShown {
						rt.Fatalf("%s in %s: shown=%v, want %v (Slack deleted=%v)", ts, pane.name, ok, wantShown, slack.deleted[ts])
					}
					if ok && !reflect.DeepEqual(body, want) {
						rt.Fatalf("%s in %s shows %+v, Slack has %+v", ts, pane.name, body, want)
					}
				}
			}
		}

		actions := []string{"edit", "edit", "result", "result", "event", "event", "drop", "other", "delete", "deleteResult", "prepend"}
		for range rapid.IntRange(1, 40).Draw(rt, "steps") {
			step(rapid.SampledFrom(actions).Draw(rt, "action"))
			check()
			if quiet() {
				settled()
			}
		}
		for !quiet() {
			switch {
			case len(inFlight) > 0:
				step("result")
			case len(deleteRes) > 0:
				step("deleteResult")
			default:
				step("event")
			}
			check()
		}
		settled()
	})
}

// findRow is the row with ts in the channel pane, the thread pane's
// replies, or the thread parent, whichever has it first.
func findRow(a *App, ts string) (messages.MessageItem, bool) {
	for _, p := range panesOf(a, ts) {
		if p.ok {
			r := messages.MessageItem{TS: ts}
			p.body.Apply(&r)
			return r, true
		}
	}
	return messages.MessageItem{}, false
}

// paneBody is what one pane shows for a message.
type paneBody struct {
	name string
	body messages.Body
	ok   bool
}

// panesOf is what each pane that holds message ts in the edit fixture
// (editApp) shows for it: the channel pane for 1.000000 and the parent
// 5.000000, the thread pane for the parent and the reply 6.000000.
func panesOf(a *App, ts string) []paneBody {
	in := func(name string, rows []messages.MessageItem) paneBody {
		if i := messages.FirstIndexOfTS(rows, ts); i >= 0 {
			return paneBody{name, messages.BodyOf(rows[i]), true}
		}
		return paneBody{name: name}
	}
	switch ts {
	case "1.000000":
		return []paneBody{in("channel", a.messagepane.Messages())}
	case "5.000000":
		return []paneBody{in("channel", a.messagepane.Messages()), in("thread parent", []messages.MessageItem{a.threadPanel.ParentMsg()})}
	default:
		return []paneBody{in("thread", a.threadPanel.Replies())}
	}
}

// realSorted reports whether the rows Slack knows are in time order.
func realSorted(rows []messages.MessageItem) bool {
	real := slices.DeleteFunc(slices.Clone(rows), func(r messages.MessageItem) bool { return messages.IsLocalTS(r.TS) })
	return slices.IsSortedFunc(real, func(x, y messages.MessageItem) int { return messages.CompareTS(x.TS, y.TS) })
}
