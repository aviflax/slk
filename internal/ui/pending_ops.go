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
// An edit (BeginEdit) shows its text at once and is settled by its
// chat.update result (EndEdit). While any edit of a message is
// pending, the message shows the newest edit's text and the
// message_changed echoes of it are ignored: an echo cannot say which
// edit it carries, and an older edit's echo must not replace a newer
// edit's text. Success shows the text Slack stored; a failure puts
// back what was there, and gives the typed text back when no newer
// edit replaced it.
//
// A delete (BeginDelete) takes the message out of every pane at once
// and keeps the rows to put back at their old index if chat.delete
// fails (EndDelete). Its message_deleted echo is harmless (removing a
// missing row is a no-op) and proves the delete went through, so a
// failure after it restores nothing.
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

// pendingEdit is one optimistic edit waiting for its chat.update
// result.
type pendingEdit struct {
	id        uint64 // from BeginEdit; later edits have larger ids
	channelID string
	ts        string
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

// editedMessage is the edit state of one message with edits pending.
// The message shows the text of the newest edit that is pending or has
// succeeded, or base when there is none.
type editedMessage struct {
	// base is the body the message showed before its first pending
	// edit, then that of the newest edit to succeed (baseID). known
	// is false when no pane showed the message at the first edit, so
	// there is nothing to put back.
	base    messages.Body
	baseID  uint64
	known   bool
	pending []pendingEdit // oldest first
}

// editShown is the body a message should show once an edit settles.
// known is false when nothing is known to show (see editedMessage).
type editShown struct {
	body  messages.Body
	known bool
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

	// edits holds every message with an edit awaiting its result.
	edits  map[editKey]*editedMessage
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
		edits:    make(map[editKey]*editedMessage),
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
// prior is the message as a pane showed it before the edit (priorOK
// false when none showed it); it is what a failure puts back unless an
// earlier edit of the message is still pending, whose own prior holds.
func (p *pendingOps) BeginEdit(e pendingEdit, prior messages.MessageItem, priorOK bool) pendingEdit {
	p.editID++
	e.id = p.editID
	k := editKey{e.channelID, e.ts}
	m := p.edits[k]
	if m == nil {
		m = &editedMessage{base: messages.BodyOf(prior), known: priorOK}
		p.edits[k] = m
	}
	m.pending = append(m.pending, e)
	return e
}

// EndEdit settles the edit id; a success stores the edit's body, what
// chat.update sent. It returns the edit, what its message should show now, and whether
// the edit is the newest of that message (no later edit is pending or
// has succeeded), which a failure needs to know before giving the text
// back. found is false for an unknown id.
func (p *pendingOps) EndEdit(id uint64, ok bool) (e pendingEdit, shown editShown, newest, found bool) {
	for k, m := range p.edits {
		i := slices.IndexFunc(m.pending, func(x pendingEdit) bool { return x.id == id })
		if i < 0 {
			continue
		}
		e = m.pending[i]
		m.pending = slices.Delete(m.pending, i, i+1)
		newest = m.baseID < id
		for _, x := range m.pending {
			if x.id > id {
				newest = false
			}
		}
		if ok && id > m.baseID {
			m.base, m.baseID, m.known = e.body, id, true
		}
		shown = editShown{body: m.base, known: m.known}
		if n := len(m.pending); n > 0 && m.pending[n-1].id > m.baseID {
			shown = editShown{body: m.pending[n-1].body, known: true}
		}
		if len(m.pending) == 0 {
			delete(p.edits, k)
		}
		return e, shown, newest, true
	}
	return pendingEdit{}, editShown{}, false, false
}

// EditPending reports whether an edit of the message is awaiting its
// result.
func (p *pendingOps) EditPending(channelID, ts string) bool {
	return p.edits[editKey{channelID, ts}] != nil
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
