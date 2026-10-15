package ui

import (
	"errors"
	"fmt"
	"reflect"
	"slices"
	"strings"
	"testing"

	tea "charm.land/bubbletea/v2"
	"pgregory.net/rapid"

	"github.com/gammons/slk/internal/core"
	"github.com/gammons/slk/internal/ids"
	"github.com/gammons/slk/internal/ui/messages"
)

// firstEdited runs cmd to completion and returns the first
// MessageEditedMsg it delivers.
func firstEdited(cmd tea.Cmd) (MessageEditedMsg, bool) {
	for _, m := range drainCmd(cmd) {
		if res, ok := m.(MessageEditedMsg); ok {
			return res, true
		}
	}
	return MessageEditedMsg{}, false
}

// slackModel is Slack as the source of truth for the edit and delete
// test below: each message's stored body, which messages are deleted,
// and the WebSocket events it has sent and the App has not yet seen,
// in order. The test applies a chat.update when the App sends it.
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
	s.events = append(s.events, slackEvent{NewMessageMsg{ChannelID: "C1", Changed: true, Message: row}, droppable})
}

// rows is what a history fetch returns of the messages tss: each one
// Slack has not deleted, with its stored body, as editApp's fixture
// shows it.
func (s *slackModel) rows(tss ...string) []messages.MessageItem {
	out := []messages.MessageItem{}
	for _, ts := range tss {
		if s.deleted[ts] {
			continue
		}
		row := messages.MessageItem{TS: ts, UserID: "USELF"}
		switch ts {
		case "5.000000":
			row.ReplyCount = 1
		case "6.000000":
			row.ThreadTS = "5.000000"
		}
		s.body[ts].Apply(&row)
		out = append(out, row)
	}
	return out
}

// keep makes every echo of ts on its way undroppable: the model's one
// exclusion (see TestEditsAndDeletesEndOnSlacksState).
func (s *slackModel) keep(ts string) {
	for i, e := range s.events {
		if nm, ok := e.msg.(NewMessageMsg); ok && nm.Message.TS == ts {
			s.events[i].droppable = false
		}
	}
}

// However the user's edits and deletes of their messages interleave
// with their results (success, failure, or a failure after Slack
// stored the edit), Slack's echoes (before or after the result),
// other clients' edits, older history loading and both panes being
// reloaded from Slack:
//
//   - at most one chat.update per message is in flight, and Slack
//     receives a message's edits in the order the user made them;
//   - the rows Slack knows stay in TS order in both panes;
//   - a message being deleted is never shown, and one with an edit in
//     flight shows the newest edit typed;
//   - a failed edit's text that Slack does not have goes back to the
//     user: the editor, a composer or the clipboard;
//   - once nothing is in flight and every event has arrived (or been
//     lost, for the echo of an edit Slack accepted), every pane shows
//     exactly Slack's body for each message (text, blocks and edited
//     mark), a message is absent exactly when Slack deleted it, no
//     editor is open and nothing is left pending.
//
// Slack applies a chat.update when it receives it, so the model applies
// each edit when the App sends it (not when its result arrives), and
// another client's edit can land between that and the result.
//
// One case is excluded, because the App cannot decide it: our edit A
// succeeds, another client changes the message after it, A's echo is
// lost and the other change's echo arrives before A's result. The App
// then shows A while Slack has the other change, until the next
// change. Telling the two orders apart needs A's edited.ts, which
// chat.update's response (as slack-go returns it) does not carry. So an
// echo of an edit is never lost once another change of the message
// follows it while it is in flight (slackModel.keep), and no other
// change follows one whose echo was lost while it is in flight. Nor is
// an edit's
// echo lost while an older change's echo is still on its way: that
// older echo arriving after the result, the newer one lost, looks like
// a newer change by another client.
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
		all := []string{"1.000000", "5.000000", "6.000000"}
		for _, ts := range all {
			row, _ := findRow(a, ts)
			slack.body[ts] = messages.BodyOf(row)
		}

		// Per message: the result of the edit in flight, not yet
		// delivered, and its text; the newest edit typed while it is
		// out, waiting ("" for none); whether an echo carrying the
		// in-flight edit has been delivered; and whether the App has
		// been told the message is deleted.
		inFlight := map[string]MessageEditedMsg{}
		inFlightText := map[string]string{}
		waiting := map[string]string{}
		echoed := map[string]bool{}
		appGone := map[string]bool{}
		lost := map[string]bool{}           // the in-flight edit's echo was lost
		typedOrder := map[string][]string{} // per message, every text typed
		older := 0
		words := 0

		// send checks that cmd sends text's chat.update for ts, once,
		// and has Slack receive it: Slack applies it now, or refuses
		// it (failure), or applies it but the call fails (timeout).
		send := func(ts, text string, cmd tea.Cmd) {
			before := len(*sent)
			res, ok := firstEdited(cmd)
			if !ok || res.TS != ts {
				rt.Fatalf("%s: edit %q due but its chat.update was not sent (got %+v)", ts, text, res)
			}
			if len(*sent) != before+1 || (*sent)[before] != text {
				rt.Fatalf("%s: sent %q, want %q once", ts, (*sent)[before:], text)
			}
			switch outcome := rapid.SampledFrom([]string{"ok", "fail", "timeout"}).Draw(rt, "outcome"); {
			case slack.deleted[ts]:
				res.Err, res.NotFound = errors.New("message_not_found"), true
			case outcome == "ok":
				// Its echo may be lost, unless an older change's echo
				// is still on its way (see the test's comment).
				slack.change(ts, editedBody(text), !slack.queued(ts))
			case outcome == "timeout":
				slack.change(ts, editedBody(text), false)
				res.Err = errors.New("timeout")
			default:
				res.Err = errors.New("boom")
			}
			inFlight[ts], inFlightText[ts], echoed[ts], lost[ts] = res, text, false, false
		}

		shown := func(ts string) bool { _, ok := findRow(a, ts); return ok }

		// deliver hands the App Slack's next WebSocket event.
		deliver := func() {
			ev := slack.events[0].msg
			slack.events = slack.events[1:]
			switch m := ev.(type) {
			case NewMessageMsg:
				if text, out := inFlightText[m.Message.TS]; out && messages.EchoCarries(messages.BodyOf(m.Message), editedBody(text)) {
					echoed[m.Message.TS] = true
				}
			case WSMessageDeletedMsg:
				appGone[m.TS] = true
			}
			a.Update(ev)
		}

		step := func(action string) {
			switch action {
			case "edit":
				ts := rapid.SampledFrom(all).Draw(rt, "editTS")
				if !shown(ts) {
					return
				}
				words++
				text := fmt.Sprintf("w%d", words)
				msg := editRequest{ChannelID: "C1", TS: ts, NewText: text, Typed: text, TeamID: "T1", Panel: PanelMessages}
				if ts == "6.000000" {
					msg.Panel, msg.ThreadTS = PanelThread, "5.000000"
				}
				cmd := a.beginEdit(msg)
				typedOrder[ts] = append(typedOrder[ts], text)
				if _, out := inFlight[ts]; out {
					waiting[ts] = text // replaces any older waiting edit
					return
				}
				send(ts, text, cmd)
			case "result":
				ts := rapid.SampledFrom(all).Draw(rt, "resultTS")
				res, ok := inFlight[ts]
				if !ok {
					return
				}
				text := inFlightText[ts]
				if !res.NotFound && rapid.Bool().Draw(rt, "echoFirst") {
					// Slack's echo of the edit, if it stored it,
					// arrives before the HTTP result.
					for slices.ContainsFunc(slack.events, func(e slackEvent) bool {
						nm, ok := e.msg.(NewMessageMsg)
						return ok && nm.Message.TS == ts && messages.EchoCarries(messages.BodyOf(nm.Message), editedBody(text))
					}) {
						deliver()
					}
				}
				delete(inFlight, ts)
				delete(inFlightText, ts)
				next := waiting[ts]
				delete(waiting, ts)
				_, deleting := deleteRes[ts]
				gone := appGone[ts] || res.NotFound
				// The user hears of it and gets the text back unless
				// Slack's echo proved it stored it, or a newer edit is
				// on its way.
				wantGiveBack := res.Err != nil && !echoed[ts] && (next == "" || gone)
				giveBack := text
				if next != "" && gone {
					giveBack = next
				}
				_, cmd := a.Update(res)
				if res.NotFound {
					appGone[ts] = true
				}
				if next != "" && !gone {
					send(ts, next, cmd)
				}
				if wantGiveBack {
					got := a.compose.Value() == giveBack || a.threadCompose.Value() == giveBack || clipboardHas(*copied, giveBack)
					if !got {
						rt.Fatalf("%s: failed edit %q lost: composer %q, thread composer %q, clipboard %q",
							ts, giveBack, a.compose.Value(), a.threadCompose.Value(), *copied)
					}
					if (gone || deleting) && (a.compose.Value() == giveBack || a.threadCompose.Value() == giveBack) && !a.editing.IsActive() {
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
				if len(slack.events) > 0 {
					deliver()
				}
			case "other":
				ts := rapid.SampledFrom(all).Draw(rt, "otherTS")
				if slack.deleted[ts] {
					return
				}
				if _, out := inFlight[ts]; out {
					// The excluded case: the echo of the edit in
					// flight must not be lost when another change
					// follows it.
					if lost[ts] {
						return
					}
					slack.keep(ts)
				}
				words++
				slack.change(ts, editedBody(fmt.Sprintf("o%d", words)), false)
			case "delete":
				ts := rapid.SampledFrom([]string{"1.000000", "6.000000"}).Draw(rt, "deleteTS")
				if _, pending := deleteRes[ts]; pending || !shown(ts) {
					return
				}
				cmd := a.beginDelete("C1", ts)
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
				if res.Err == nil || res.NotFound {
					appGone[ts] = true
				}
				a.Update(res)
			case "drop":
				if len(slack.events) > 0 && slack.events[0].droppable {
					if nm, ok := slack.events[0].msg.(NewMessageMsg); ok {
						if _, out := inFlight[nm.Message.TS]; out {
							lost[nm.Message.TS] = true
						}
					}
					slack.events = slack.events[1:]
				}
			case "refetch":
				// A history fetch of the channel and of the open thread
				// lands with Slack's state as it is now, knowing
				// nothing of the user's edits and deletes in flight.
				a.Update(MessagesLoadedMsg{ChannelID: "C1", Messages: slack.rows("1.000000", "5.000000")})
				a.Update(ThreadRepliesLoadedMsg{ThreadTS: "5.000000", Replies: slack.rows("6.000000")})
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
			// Whatever reloaded the panes, a message being deleted
			// stays out of them, and one with an edit in flight shows
			// the newest edit typed.
			for _, ts := range all {
				for _, pane := range panesOf(a, ts) {
					_, deleting := deleteRes[ts]
					_, editing := inFlight[ts]
					switch {
					case !pane.ok:
					case deleting:
						rt.Fatalf("%s shown in %s while its delete is pending", ts, pane.name)
					case editing:
						typed := typedOrder[ts]
						if want := editedBody(typed[len(typed)-1]); !reflect.DeepEqual(pane.body, want) {
							rt.Fatalf("%s in %s shows %+v while its edit to %q is in flight", ts, pane.name, pane.body, typed[len(typed)-1])
						}
					}
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
			p := a.pendingOps
			if len(p.edits) != 0 || len(p.deletes) != 0 || len(p.inserts) != 0 {
				rt.Fatalf("pending at quiescence: edits %v, deletes %v, inserts %v", p.edits, p.deletes, p.inserts)
			}
			for k, held := range p.held {
				if len(held) != 0 {
					rt.Fatalf("held in %v at quiescence: %+v", k, held)
				}
			}
			if a.editing.IsActive() {
				rt.Fatalf("an editor is open at quiescence")
			}
		}

		actions := []string{"edit", "edit", "result", "result", "event", "event", "drop", "other", "delete", "deleteResult", "prepend", "refetch"}
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

// clipboardHas reports whether any clipboard write held text among the
// unsent texts it joins.
func clipboardHas(copied []string, text string) bool {
	return slices.ContainsFunc(copied, func(c string) bool {
		return slices.Contains(strings.Split(c, "\n\n"), text)
	})
}
