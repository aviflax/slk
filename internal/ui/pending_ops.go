// internal/ui/pending_ops.go
//
// The ledger of the user's optimistic sends and edits, from Enter until
// Slack answers, and how the WebSocket echoes of those sends are told
// apart from every other message the user posts.
//
// A send (channel message, thread reply, broadcast) records a pending
// insert under its "local:N" placeholder id. Its echo can arrive before
// or after the chat.postMessage HTTP result:
//
//  1. Before: the echo is adopted (AdoptEcho). It settles the oldest
//     pending insert for the same conversation, in place of being
//     appended, and the HTTP result later replaces it with Slack's
//     response. An echo that matches no pending insert (slk's own file
//     uploads, a message from another Slack client) renders normally.
//
//  2. After: the HTTP result has called RecordSent with the real TS,
//     and IsSelfSent drops the echo carrying it.
//
// The HTTP result ends the insert (EndInsert). A failed send gives the
// text as typed back to the composer it came from. A failure on an
// adopted insert is not a failure the user needs to see: the echo
// proves Slack posted the message.
//
// An edit (BeginEdit) shows its text at once and is settled by its
// chat.update result (EndEdit). While any edit of a message is
// pending, the message shows the newest edit's text and the
// message_changed echoes of it are ignored: an echo cannot say which
// edit it carries, and an older edit's echo must not replace a newer
// edit's text. Success shows the text Slack stored; a failure puts
// back what was there, and gives the typed text back when no newer
// edit replaced it.
package ui

import (
	"fmt"
	"slices"
	"strings"
	"time"

	tea "charm.land/bubbletea/v2"

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
	// adopted is set once the send's echo has settled the placeholder;
	// the message is then known to be posted.
	adopted bool
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
	text      string // the new text as shown, converted to mrkdwn
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

// editKey names a message by channel and TS.
type editKey struct{ channelID, ts string }

// editedMessage is the edit state of one message with edits pending.
// The message shows the text of the newest edit that is pending or has
// succeeded, or base when there is none.
type editedMessage struct {
	// base is the text the message showed before its first pending
	// edit, then that of the newest edit to succeed (baseID). known
	// is false when no pane showed the message at the first edit, so
	// there is nothing to put back.
	base       string
	baseEdited bool
	baseID     uint64
	known      bool
	pending    []pendingEdit // oldest first
}

// editShown is the text a message should show once an edit settles.
// known is false when nothing is known to show (see editedMessage).
type editShown struct {
	text   string
	edited bool
	known  bool
}

// pendingOps tracks the user's sends and edits from this session. See
// the package comment.
type pendingOps struct {
	// inserts are the sends without an HTTP result, oldest first.
	inserts []*pendingInsert

	// edits holds every message with an edit awaiting its result.
	edits  map[editKey]*editedMessage
	editID uint64

	// sentTSes records authoritative TSes the App has observed via
	// MessageSentMsg / ThreadReplySentMsg or an adopted echo. WS
	// echoes carrying any of these are dropped.
	sentTSes map[string]time.Time

	// localTSCounter is incremented per optimistic placeholder so each
	// one carries a unique TS-shaped ("local:<counter>") id.
	localTSCounter uint64
}

func newPendingOps() *pendingOps {
	return &pendingOps{sentTSes: make(map[string]time.Time), edits: make(map[editKey]*editedMessage)}
}

// BeginInsert records a send whose placeholder is ins.localTS.
func (p *pendingOps) BeginInsert(ins pendingInsert) {
	p.inserts = append(p.inserts, &ins)
}

// EndInsert removes the send with placeholder localTS and returns it;
// ok is false when there is none (a result fired without a send, as
// tests do).
func (p *pendingOps) EndInsert(localTS string) (ins pendingInsert, ok bool) {
	for i, e := range p.inserts {
		if e.localTS == localTS {
			p.inserts = append(p.inserts[:i], p.inserts[i+1:]...)
			return *e, true
		}
	}
	return pendingInsert{}, false
}

// AdoptEcho finds the oldest send that msg, a message the current user
// posted in channelID, can be the echo of, marks it adopted and returns
// it. A send matches when it is in the same conversation (the same
// thread, or top level for both) and no echo has adopted it yet. An
// echo carrying files never matches: slk sends files through the
// upload path, which has no placeholder.
func (p *pendingOps) AdoptEcho(channelID string, msg messages.MessageItem) (pendingInsert, bool) {
	if len(msg.Attachments) > 0 {
		return pendingInsert{}, false
	}
	threadTS := msg.ThreadTS
	if threadTS == msg.TS {
		threadTS = ""
	}
	for _, e := range p.inserts {
		if !e.adopted && e.channelID == channelID && e.threadTS == threadTS {
			e.adopted = true
			return *e, true
		}
	}
	return pendingInsert{}, false
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
		m = &editedMessage{base: prior.Text, baseEdited: prior.IsEdited, known: priorOK}
		p.edits[k] = m
	}
	m.pending = append(m.pending, e)
	return e
}

// EndEdit settles the edit id: result is the text Slack stored when ok.
// It returns the edit, what its message should show now, and whether
// the edit is the newest of that message (no later edit is pending or
// has succeeded), which a failure needs to know before giving the text
// back. found is false for an unknown id.
func (p *pendingOps) EndEdit(id uint64, ok bool, result string) (e pendingEdit, shown editShown, newest, found bool) {
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
			if result == "" {
				result = e.text
			}
			m.base, m.baseEdited, m.baseID, m.known = result, true, id, true
		}
		shown = editShown{text: m.base, edited: m.baseEdited, known: m.known}
		if n := len(m.pending); n > 0 && m.pending[n-1].id > m.baseID {
			shown = editShown{text: m.pending[n-1].text, edited: true, known: true}
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

// localTSPrefix starts every placeholder id.
const localTSPrefix = "local:"

// isLocalTS reports whether ts is a placeholder id from NextLocalTS
// rather than a TS Slack assigned.
func isLocalTS(ts string) bool { return strings.HasPrefix(ts, localTSPrefix) }

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
	if !isLocalTS(ts) {
		return nil, false
	}
	return toastWithClear(a, stillSendingText, 2*time.Second), true
}

// NextLocalTS mints a unique placeholder id for an optimistic instant-
// display message. The "local:" prefix makes it trivially distinguishable
// from a Slack-assigned TS (which is always of the form
// "<seconds>.<microseconds>").
func (p *pendingOps) NextLocalTS() string {
	p.localTSCounter++
	return fmt.Sprintf("%s%d", localTSPrefix, p.localTSCounter)
}
