// internal/ui/pending_ops.go
//
// The ledger of the user's optimistic sends, from Enter until Slack
// answers, and how the WebSocket echoes of those sends are told apart
// from every other message the user posts.
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
package ui

import (
	"fmt"
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

// pendingOps tracks the user's sends from this session. See the
// package comment.
type pendingOps struct {
	// inserts are the sends without an HTTP result, oldest first.
	inserts []*pendingInsert

	// sentTSes records authoritative TSes the App has observed via
	// MessageSentMsg / ThreadReplySentMsg or an adopted echo. WS
	// echoes carrying any of these are dropped.
	sentTSes map[string]time.Time

	// localTSCounter is incremented per optimistic placeholder so each
	// one carries a unique TS-shaped ("local:<counter>") id.
	localTSCounter uint64
}

func newPendingOps() *pendingOps {
	return &pendingOps{sentTSes: make(map[string]time.Time)}
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
