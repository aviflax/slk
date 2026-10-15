// internal/ui/pending_view.go
//
// What a pane shows when its rows are replaced wholesale: a history
// fetch, a cache read on entering a channel or opening a thread, older
// history prepended. Those rows come from Slack (or the cache of it)
// and know nothing of the user's operations still pending, so each is
// put back on top of them here, in one place:
//
//   - a pending or waiting edit shows its body, as BeginEdit showed it;
//   - a message with a delete pending stays out, and the pending delete
//     keeps the fetched row (Slack's current state) to put back if the
//     delete fails;
//   - the user's own messages that may be a pending send's stay hidden
//     until its result, as their echoes would be (held);
//   - ephemerals already on screen stay (Slack never returns them);
//   - each pending send's placeholder stays, after every row Slack
//     knows, in the order they were sent.
package ui

import (
	"slices"
	"time"

	"github.com/gammons/slk/internal/ui/messages"
)

// sendHoldWindow is how far before the oldest pending send in a
// conversation a fetched message of the user's may be stamped and still
// be held as possibly that send's (see holdFetched). It covers the
// difference between this machine's clock and Slack's.
const sendHoldWindow = time.Minute

// paneView names what one pane shows: a channel's feed (threadTS "")
// or one thread, in workspace teamID, for the user selfID.
type paneView struct {
	teamID, channelID, threadTS string
	selfID                      string
}

// overlay is fetched, rows Slack (or the cache) gave a pane for v, with
// every pending operation put back that applies to a row Slack knows:
// the shown body of a message with an edit pending, the message with a
// delete pending left out (and returned in deleted, with its body
// overlaid, for the caller to keep for a restore), and the user's
// messages that may be a pending send's left out and held, as their
// echoes would be. fetched is not modified.
func (p *pendingOps) overlay(v paneView, fetched []messages.MessageItem) (rows, deleted []messages.MessageItem) {
	rows = make([]messages.MessageItem, 0, len(fetched))
	for _, r := range fetched {
		if m := p.edits[editKey{v.channelID, r.TS}]; m != nil {
			if !m.known {
				// The first pane to load the message since the edit
				// began: what Slack had, as far as anything shows.
				m.server, m.known = messages.BodyOf(r), true
			}
			m.shown().Apply(&r)
		}
		if p.DeletePending(v.channelID, r.TS) {
			deleted = append(deleted, r)
			continue
		}
		if p.holdFetched(v, r) {
			continue
		}
		rows = append(rows, r)
	}
	return rows, deleted
}

// shown is the body a message with an edit pending shows: the newest
// edit, the waiting one if there is one.
func (m *messageEdits) shown() messages.Body {
	if m.queued != nil {
		return m.queued.body
	}
	return m.inFlight.body
}

// shownBody is the body message channelID/ts shows while an edit of it
// is pending; ok is false when none is.
func (p *pendingOps) shownBody(channelID, ts string) (messages.Body, bool) {
	if m := p.edits[editKey{channelID, ts}]; m != nil {
		return m.shown(), true
	}
	return messages.Body{}, false
}

// holdFetched reports whether r, a row fetched for v, must stay hidden
// because it may be a pending send's own message, and holds it if so.
// That is a message already held (its echo arrived first), or one of
// the user's own messages in a conversation with a send pending, not
// yet known to be any send's (IsSelfSent), stamped no earlier than
// sendHoldWindow before the oldest send pending there. Nothing in a
// fetched message says which send it is, exactly as for an echo, so it
// is treated as one: EndInsert drops it if a result shows it and
// releases it otherwise, so it shows once either way. Older messages of
// the user's cannot be a pending send's and show at once.
func (p *pendingOps) holdFetched(v paneView, r messages.MessageItem) bool {
	k := conversationOf(v.teamID, v.channelID, r)
	if slices.ContainsFunc(p.held[k], func(m NewMessageMsg) bool { return m.Message.TS == r.TS }) {
		return true
	}
	if r.Ephemeral || r.UserID == "" || r.UserID != v.selfID || messages.IsLocalTS(r.TS) || p.IsSelfSent(r.TS) {
		return false
	}
	var oldest time.Time
	for _, ins := range p.inserts {
		if ins.conversation() == k && (oldest.IsZero() || ins.begun.Before(oldest)) {
			oldest = ins.begun
		}
	}
	if oldest.IsZero() {
		return false
	}
	from := oldest.Add(-sendHoldWindow)
	if messages.CompareTS(r.TS, messages.TSAt(from)) < 0 {
		return false
	}
	return p.HoldEcho(NewMessageMsg{TeamID: v.teamID, ChannelID: v.channelID, Message: r})
}

// view is what a pane for v shows given fetched, the rows Slack (or the
// cache) gave it: overlay, then the ephemerals in shown (the rows the
// pane showed for the same conversation; nil when it showed another,
// as on entering a channel) merged in by TS, then the placeholder of
// every send pending in v, in the order sent. deleted is overlay's.
func (p *pendingOps) view(v paneView, fetched, shown []messages.MessageItem) (rows, deleted []messages.MessageItem) {
	rows, deleted = p.overlay(v, fetched)
	rows = keepEphemerals(rows, shown)
	for _, ins := range p.inserts {
		row, ok := ins.placeholderIn(v)
		if ok && messages.FirstIndexOfTS(rows, row.TS) < 0 {
			rows = append(rows, row)
		}
	}
	return rows, deleted
}

// placeholderIn is the row ins's placeholder shows as in a pane for v,
// and whether that pane shows it: a top-level send in its channel's
// feed, a reply in its thread, and a broadcast reply in the channel
// feed too, as a thread_broadcast.
func (ins pendingInsert) placeholderIn(v paneView) (messages.MessageItem, bool) {
	if ins.postTeam != v.teamID || ins.channelID != v.channelID {
		return messages.MessageItem{}, false
	}
	row := cloneMessageItem(ins.row)
	switch {
	case ins.threadTS == v.threadTS:
		return row, true
	case v.threadTS == "" && ins.broadcast:
		row.Subtype = "thread_broadcast"
		return row, true
	}
	return messages.MessageItem{}, false
}

// keepDeleted records rows, the fetched rows of messages with a delete
// pending that a reload of sink (showing threadTS, "" for a channel
// window) left out, as what the delete takes from sink: a failed delete
// then restores Slack's state as the fetch had it, in that pane too.
func (p *pendingOps) keepDeleted(channelID string, sink rowSink, threadTS string, rows []messages.MessageItem) {
	for _, r := range rows {
		d := p.deletes[editKey{channelID, r.TS}]
		if d == nil {
			continue
		}
		i := slices.IndexFunc(d.removed, func(rr removedRow) bool { return rr.sink == sink && rr.threadTS == threadTS })
		if i < 0 {
			d.removed = append(d.removed, removedRow{sink: sink, threadTS: threadTS, row: messages.RemovedRow{Item: r}})
			continue
		}
		d.removed[i].row.Item = r
	}
}

// paneRows is what sink, a pane showing channelID's feed (threadTS "")
// or thread threadTS, shows when its rows are replaced by fetched (from
// Slack or the cache): pendingOps.view, with the rows a pending delete
// left out kept for its restore. shown is what the pane showed for the
// same conversation, for its ephemerals, or nil. Every wholesale
// replacement of a pane's rows goes through it
// (TestPaneRowsGoThroughPaneRows).
func (a *App) paneRows(sink rowSink, channelID, threadTS string, fetched, shown []messages.MessageItem) []messages.MessageItem {
	rows, deleted := a.pendingOps.view(a.paneViewOf(channelID, threadTS), fetched, shown)
	a.pendingOps.keepDeleted(channelID, sink, threadTS, deleted)
	return rows
}

// olderRows is paneRows for a page of older history prepended to a
// channel window: the overlay alone, since placeholders and ephemerals
// are already in the window.
func (a *App) olderRows(sink rowSink, channelID string, fetched []messages.MessageItem) []messages.MessageItem {
	rows, deleted := a.pendingOps.overlay(a.paneViewOf(channelID, ""), fetched)
	a.pendingOps.keepDeleted(channelID, sink, "", deleted)
	return rows
}

// threadParent is parent, a thread's parent row about to be shown, with
// the body of an edit of it pending.
func (a *App) threadParent(channelID string, parent messages.MessageItem) messages.MessageItem {
	if body, ok := a.pendingOps.shownBody(channelID, parent.TS); ok {
		body.Apply(&parent)
	}
	return parent
}

// paneViewOf is the paneView of channelID's feed or thread threadTS in
// the active workspace.
func (a *App) paneViewOf(channelID, threadTS string) paneView {
	return paneView{teamID: a.activeTeamID, channelID: channelID, threadTS: threadTS, selfID: a.currentUserID}
}
