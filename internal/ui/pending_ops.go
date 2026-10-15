// internal/ui/pending_ops.go
//
// The ledger of the user's optimistic sends and edits, from Enter until
// Slack answers, and how the WebSocket echoes of those sends are told
// apart from every other message the user posts.
//
// A send (channel message, thread reply, broadcast) records a pending
// insert under its "local:N" placeholder id. Its echo can arrive before
// or after the chat.postMessage HTTP result, and nothing in an echo
// says which send it belongs to: the user may have two sends in flight
// in one conversation (their results and echoes can arrive in any
// order), or may post the same conversation from another client or
// through a forward or upload. So an echo is never matched to a send:
//
//  1. Before: while any send is pending in a conversation, the user's
//     own messages echoed in it are held (HoldEcho), not shown.
//
//  2. The HTTP result ends the insert (EndInsert) and records its real
//     TS (RecordSent). Once no send is pending in the conversation,
//     the held echoes are released in TS order: those carrying a
//     recorded TS are dropped (the result already showed them), the
//     rest render as any new message does. While other sends are
//     still pending there, only the recorded ones are dropped.
//
//  3. After: IsSelfSent drops the echo carrying a recorded TS.
//
// A failed send gives the text as typed back to the composer it came
// from, or to the clipboard when that composer has a newer draft
// (giveTextBack). If Slack posted it anyway (a timeout after Slack accepted the
// post), its held echo is released like any other and the message
// shows once, from the echo.
//
// An edit (BeginEdit) shows its body at once. One chat.update per
// message is in flight at a time: an edit typed while one is out waits,
// replacing any older waiting edit (the newest text wins), and is sent
// when the one in flight ends (EndEdit). Slack then applies the edits
// in the order the user made them. While an edit is in flight, a
// message_changed echo of the message is recorded (EditEcho), not
// shown: it cannot say which edit it carries, and an older state must
// not flicker over a newer edit. Once nothing is in flight or waiting,
// the message shows Slack's state as best known: the newest echo if one
// proved Slack stored the last edit, else that edit's body if it
// succeeded, else the newest echo, else what it showed before. An echo
// carrying the in-flight edit's text proves Slack stored it, so a
// failed HTTP call after it (a timeout) is not a failure.
//
// A delete (BeginDelete) takes the message out of every pane at once
// and keeps the rows to put back if chat.delete fails (EndDelete). Its
// message_deleted echo is harmless (removing a missing row is a no-op)
// and proves the delete went through, so a failure after it restores
// nothing. Whatever the message shows meanwhile (an edit settling, an
// echo) is written into the kept rows too (PatchRemoved), so a restore
// puts back the message as it is now.
package ui

import (
	"cmp"
	"slices"
	"time"

	tea "charm.land/bubbletea/v2"

	"github.com/gammons/slk/internal/debuglog"
	"github.com/gammons/slk/internal/ui/messages"
)

// pendingInsert is one optimistic send waiting for its HTTP result.
type pendingInsert struct {
	localTS   string // the placeholder's "local:N" id
	channelID string
	threadTS  string // the parent's TS for a reply, "" for a top-level send
	broadcast bool   // a reply also sent to the channel
	// teamID and typed are the workspace the text was typed in and the
	// text as typed, given back to its composer if the send fails.
	teamID string
	typed  string
}

// conversation is the conversation ins posts to.
func (ins pendingInsert) conversation() conversationKey {
	return conversationKey{ins.channelID, ins.threadTS}
}

// origin is where ins was typed, for offerTypedBack.
func (ins pendingInsert) origin() commandOrigin {
	return commandOrigin{teamID: ins.teamID, channelID: ins.channelID, threadTS: ins.threadTS, typed: ins.typed}
}

// pendingEdit is one optimistic edit: sent and awaiting its
// chat.update result, or waiting behind the one that is.
type pendingEdit struct {
	id        uint64 // from BeginEdit; later edits have larger ids
	channelID string
	ts        string
	wire      string        // the text to send: mentions in wire form
	body      messages.Body // the new body as shown (messages.EditedBody)
	// panel, threadTS, teamID and typed are where the edit was typed
	// (threadTS is the open thread for a PanelThread edit) and the text
	// as typed, so a failed edit can be reopened there or given back.
	panel    Panel
	threadTS string
	teamID   string
	typed    string
}

// origin is where e was typed, for offerTypedBack.
func (e pendingEdit) origin() commandOrigin {
	return commandOrigin{teamID: e.teamID, channelID: e.channelID, threadTS: e.threadTS, typed: e.typed}
}

// conversationKey names a conversation: a channel's top level
// (threadTS "") or one thread of it.
type conversationKey struct{ channelID, threadTS string }

// conversationOf is the conversation msg, a message in channelID, was
// posted to. A thread parent (ThreadTS == TS) is top level; a reply or
// a thread_broadcast belongs to its parent's thread.
func conversationOf(channelID string, msg messages.MessageItem) conversationKey {
	threadTS := msg.ThreadTS
	if threadTS == msg.TS {
		threadTS = ""
	}
	return conversationKey{channelID, threadTS}
}

// editKey names a message by channel and TS.
type editKey struct{ channelID, ts string }

// messageEdits is the edit state of one message while an edit of it is
// in flight.
type messageEdits struct {
	// server is Slack's state of the message as best known: the body
	// a pane showed before the first edit, then that of each edit Slack
	// accepted and of each message_changed echo. known is false while
	// nothing is known (no pane showed the message), so there is
	// nothing to put back.
	server messages.Body
	known  bool
	// inFlight is the edit whose chat.update is awaiting its result.
	inFlight pendingEdit
	// echoed is set when an echo carrying inFlight's text arrived after
	// it was sent: Slack stored it, whatever the HTTP result says, and
	// server holds Slack's own form of it (or a newer state).
	echoed bool
	// queued is the newest edit typed while inFlight was out, sent when
	// it ends; nil for none.
	queued *pendingEdit
	// gone is set once the message is known to be deleted.
	gone bool
}

// editSettled is what EndEdit decided.
type editSettled struct {
	// show is what the message shows now; showKnown is false when
	// nothing is known to show (see messageEdits.server).
	show      messages.Body
	showKnown bool
	// next is the waiting edit to send now, if any.
	next *pendingEdit
	// failed is an edit whose text Slack does not have and that no
	// newer edit replaced: the user must hear of it and get the text
	// back. With the message gone, a waiting edit that can no longer
	// be sent is the one reported.
	failed *pendingEdit
	// superseded is set when the edit that ended failed but a newer
	// one is on its way: a toast, nothing to give back.
	superseded bool
	// gone is set when the message is known to be deleted.
	gone bool
}

// pendingDelete is one optimistic delete waiting for its chat.delete
// result: the rows it took out, to put back if it fails.
type pendingDelete struct {
	channelID string
	ts        string
	removed   []removedRow
	// echoed is set when the message_deleted echo arrives first; the
	// message is then known to be gone.
	echoed bool
}

// pendingOps tracks the user's sends, edits and deletes from this
// session. See the package comment.
type pendingOps struct {
	// inserts are the sends without an HTTP result, oldest first.
	inserts []pendingInsert

	// held are the user's own messages echoed in a conversation while
	// a send was pending there, in arrival order. See HoldEcho.
	held map[conversationKey][]NewMessageMsg

	// edits holds every message with an edit in flight.
	edits  map[editKey]*messageEdits
	editID uint64

	// deletes holds every message with a delete awaiting its result.
	deletes map[editKey]*pendingDelete

	// sentTSes records authoritative TSes the App has observed via
	// MessageSentMsg / ThreadReplySentMsg. WS echoes carrying any of
	// these are dropped.
	sentTSes map[string]time.Time

	// localTSCounter is incremented per optimistic placeholder so each
	// one carries a unique TS-shaped ("local:<counter>") id.
	localTSCounter uint64
}

func newPendingOps() *pendingOps {
	return &pendingOps{
		sentTSes: make(map[string]time.Time),
		edits:    make(map[editKey]*messageEdits),
		deletes:  make(map[editKey]*pendingDelete),
		held:     make(map[conversationKey][]NewMessageMsg),
	}
}

// BeginInsert records a send whose placeholder is ins.localTS.
func (p *pendingOps) BeginInsert(ins pendingInsert) {
	p.inserts = append(p.inserts, ins)
}

// pendingIn reports whether a send to conversation k is pending.
func (p *pendingOps) pendingIn(k conversationKey) bool {
	return slices.ContainsFunc(p.inserts, func(ins pendingInsert) bool { return ins.conversation() == k })
}

// HoldEcho holds m, a message the current user posted, when a send is
// pending in its conversation, and reports whether it did. The caller
// shows nothing for a held message: EndInsert hands it back once the
// conversation has no send pending, or drops it if a result showed it.
func (p *pendingOps) HoldEcho(m NewMessageMsg) bool {
	k := conversationOf(m.ChannelID, m.Message)
	if !p.pendingIn(k) {
		return false
	}
	p.held[k] = append(p.held[k], m)
	return true
}

// EndInsert removes the send with placeholder localTS and returns it;
// ok is false when there is none (a result fired without a send, as
// tests do). sentTS is the TS Slack gave the message, "" when the send
// failed; it is recorded (RecordSent) before the held echoes are
// looked at, so the send's own echo is dropped. release is what the
// caller must now show as new messages: when no other send is pending
// in the conversation, every held echo that no result showed, in TS
// order.
func (p *pendingOps) EndInsert(localTS, sentTS string) (ins pendingInsert, release []NewMessageMsg, ok bool) {
	p.RecordSent(sentTS)
	i := slices.IndexFunc(p.inserts, func(e pendingInsert) bool { return e.localTS == localTS })
	if i < 0 {
		return pendingInsert{}, nil, false
	}
	ins = p.inserts[i]
	p.inserts = slices.Delete(p.inserts, i, i+1)
	k := ins.conversation()
	held := slices.DeleteFunc(p.held[k], func(m NewMessageMsg) bool {
		if p.IsSelfSent(m.Message.TS) {
			debuglog.Cache("NewMessageMsg: channel=%s ts=%s decision=dropped_held_self_send",
				m.ChannelID, m.Message.TS)
			return true
		}
		return false
	})
	if p.pendingIn(k) {
		p.held[k] = held
		return ins, nil, true
	}
	delete(p.held, k)
	slices.SortStableFunc(held, func(x, y NewMessageMsg) int { return cmp.Compare(x.Message.TS, y.Message.TS) })
	return ins, held, true
}

// PatchHeld applies f to the held message channelID/ts, if any: an
// edit of a message the user posted elsewhere can arrive while it is
// held.
func (p *pendingOps) PatchHeld(channelID, ts string, f func(*messages.MessageItem)) {
	for k, held := range p.held {
		if k.channelID != channelID {
			continue
		}
		for i := range held {
			if held[i].Message.TS == ts {
				f(&held[i].Message)
			}
		}
	}
}

// DropHeld forgets the held message channelID/ts, if any: it was
// deleted before it could be shown.
func (p *pendingOps) DropHeld(channelID, ts string) {
	for k, held := range p.held {
		if k.channelID == channelID {
			p.held[k] = slices.DeleteFunc(held, func(m NewMessageMsg) bool { return m.Message.TS == ts })
		}
	}
}

// BeginEdit records e under a fresh id and returns it with the id set.
// send is true when its chat.update should go now: no edit of the
// message is in flight. Otherwise e waits in place of any older waiting
// edit, which is never sent (EndEdit sends e). prior is the message as
// a pane showed it before the edit (priorOK false when none showed it);
// it is Slack's state only while nothing else is known.
func (p *pendingOps) BeginEdit(e pendingEdit, prior messages.MessageItem, priorOK bool) (_ pendingEdit, send bool) {
	p.editID++
	e.id = p.editID
	k := editKey{e.channelID, e.ts}
	m := p.edits[k]
	if m == nil {
		p.edits[k] = &messageEdits{server: messages.BodyOf(prior), known: priorOK, inFlight: e}
		return e, true
	}
	if !m.known && priorOK {
		// A pane loaded the message since the first edit: what it
		// shows came from Slack.
		m.server, m.known = messages.BodyOf(prior), true
	}
	m.queued = &e
	return e, false
}

// EditEcho records body, a message_changed echo of channelID/ts, and
// reports whether an edit of it is in flight. When it is, the caller
// shows nothing: EndEdit decides what to show.
func (p *pendingOps) EditEcho(channelID, ts string, body messages.Body) bool {
	m := p.edits[editKey{channelID, ts}]
	if m == nil {
		return false
	}
	m.server, m.known = body, true
	if body.Text == m.inFlight.body.Text {
		m.echoed = true
	}
	return true
}

// EndEdit settles the edit id, the one in flight for its message: ok
// is whether chat.update succeeded. It reports what the message shows
// now and what the caller must do (editSettled); found is false for an
// id that is not in flight.
//
// A failure after an echo carried the edit's text is a success: Slack
// stored it (the HTTP call failed after Slack accepted it). The test is
// the text being equal, not merely an echo having arrived, because an
// echo of another client's change would otherwise swallow a real
// failure and lose the user's text. A false failure (Slack stored a
// different form of the text) reopens an edit Slack has, which loses
// nothing.
func (p *pendingOps) EndEdit(id uint64, ok bool) (s editSettled, found bool) {
	for k, m := range p.edits {
		if m.inFlight.id != id {
			continue
		}
		e := m.inFlight
		switch {
		case m.echoed:
			// m.server is the echo, Slack's own form of e or newer.
		case ok:
			m.server, m.known = e.body, true
		}
		failed := !ok && !m.echoed
		s.gone = m.gone
		if m.queued != nil && !m.gone {
			s.next = m.queued
			m.inFlight, m.queued, m.echoed = *m.queued, nil, false
			s.show, s.showKnown = s.next.body, true
			s.superseded = failed
			return s, true
		}
		switch {
		case m.queued != nil:
			// The message is gone: the waiting edit can never be sent.
			s.failed = m.queued
		case failed:
			s.failed = &e
		}
		s.show, s.showKnown = m.server, m.known
		delete(p.edits, k)
		return s, true
	}
	return editSettled{}, false
}

// EditPending reports whether an edit of the message is in flight.
func (p *pendingOps) EditPending(channelID, ts string) bool {
	return p.edits[editKey{channelID, ts}] != nil
}

// EditGone records that channelID/ts was deleted, so an edit of it in
// flight is not followed by the one waiting, and a failure gives its
// text back without making it a draft (it would post a new message).
func (p *pendingOps) EditGone(channelID, ts string) {
	if m := p.edits[editKey{channelID, ts}]; m != nil {
		m.gone = true
	}
}

// BeginDelete records d. A second delete of the same message while the
// first is pending (another pane still showed it) adds its rows to the
// first.
func (p *pendingOps) BeginDelete(d pendingDelete) {
	k := editKey{d.channelID, d.ts}
	if e := p.deletes[k]; e != nil {
		e.removed = append(e.removed, d.removed...)
		return
	}
	p.deletes[k] = &d
}

// EndDelete removes the pending delete of channelID/ts and returns it;
// ok is false when there is none.
func (p *pendingOps) EndDelete(channelID, ts string) (pendingDelete, bool) {
	k := editKey{channelID, ts}
	d := p.deletes[k]
	if d == nil {
		return pendingDelete{}, false
	}
	delete(p.deletes, k)
	return *d, true
}

// DeletePending reports whether a delete of channelID/ts is awaiting
// its result.
func (p *pendingOps) DeletePending(channelID, ts string) bool {
	return p.deletes[editKey{channelID, ts}] != nil
}

// PatchRemoved applies f to the rows a pending delete of channelID/ts
// took out, so a failed delete puts back the message as it is now, and
// returns the first as it was. ok is false when there is none.
func (p *pendingOps) PatchRemoved(channelID, ts string, f func(*messages.MessageItem)) (prior messages.MessageItem, ok bool) {
	d := p.deletes[editKey{channelID, ts}]
	if d == nil {
		return messages.MessageItem{}, false
	}
	for i := range d.removed {
		if !ok {
			prior, ok = d.removed[i].row.Item, true
		}
		f(&d.removed[i].row.Item)
	}
	return prior, ok
}

// DeleteEchoed marks the pending delete of channelID/ts, if any, as
// confirmed by its message_deleted echo.
func (p *pendingOps) DeleteEchoed(channelID, ts string) {
	if d := p.deletes[editKey{channelID, ts}]; d != nil {
		d.echoed = true
	}
}

// RecordSent marks a message TS as one the user just posted from this
// session, so the WS echo (if any) can be skipped to avoid double-
// rendering. Old entries are GC'd opportunistically; even if they
// leak, they're tiny and only checked when echoes arrive.
func (p *pendingOps) RecordSent(ts string) {
	if ts == "" {
		return
	}
	p.sentTSes[ts] = time.Now()
	// Opportunistic cleanup: drop entries older than 5 minutes. WS
	// echoes arrive within seconds; anything older is stale.
	if len(p.sentTSes) > 64 {
		cutoff := time.Now().Add(-5 * time.Minute)
		for k, v := range p.sentTSes {
			if v.Before(cutoff) {
				delete(p.sentTSes, k)
			}
		}
	}
}

// IsSelfSent reports whether ts matches a message we recently posted
// from this session.
func (p *pendingOps) IsSelfSent(ts string) bool {
	if ts == "" {
		return false
	}
	_, ok := p.sentTSes[ts]
	return ok
}

// stillSendingText is the toast for an action on a message that is
// still sending.
const stillSendingText = "Still sending…"

// sendingSelectionGuard blocks a message op, edit and delete included,
// on a selected placeholder: its "local:N" TS is slk's own until the
// post returns, so the op would send Slack a TS it never issued. It is
// the placeholder twin of ephemeralSelectionGuard (action_prompt.go).
func (a *App) sendingSelectionGuard() (tea.Cmd, bool) {
	msg, _, ok := a.selectedMessageInFocus()
	if !ok {
		return nil, false
	}
	return a.sendingGuard(msg.TS)
}

// sendingGuard is the verdict on an action targeting the message with
// TS ts: a placeholder is refused with a toast.
func (a *App) sendingGuard(ts string) (tea.Cmd, bool) {
	if !messages.IsLocalTS(ts) {
		return nil, false
	}
	return toastWithClear(a, stillSendingText, 2*time.Second), true
}

// NextLocalTS mints a unique placeholder id (messages.LocalTS) for an
// optimistic instant-display message.
func (p *pendingOps) NextLocalTS() string {
	p.localTSCounter++
	return messages.LocalTS(p.localTSCounter)
}
