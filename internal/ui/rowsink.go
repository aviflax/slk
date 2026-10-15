// internal/ui/rowsink.go
//
// The optimistic send paths (channel send, thread reply, broadcast)
// write the same pending row to every pane that shows it: an
// instant-display placeholder under a "local:N" TS, then the
// authoritative message from the chat.postMessage response, or the
// placeholder's removal when the post fails. The optimistic edit
// patches an existing row the same way. rowSink is that write surface,
// sinksFor, replySinksFor and shownSinks pick the panes, and localRow
// builds the placeholder.
package ui

import (
	"errors"
	"slices"

	tea "charm.land/bubbletea/v2"

	"github.com/gammons/slk/internal/slack/mrkdwn"
	"github.com/gammons/slk/internal/ui/messages"
	"github.com/gammons/slk/internal/ui/thread"
)

// rowSink is a pane that shows pending rows.
type rowSink interface {
	// InsertLocal adds a placeholder row (append unless its TS is
	// already shown) and scrolls to it.
	InsertLocal(item messages.MessageItem)
	// Settle lands item, the authoritative message, by
	// messages.Settle: it replaces the placeholder localTS, or, when
	// a row with item.TS is already shown, replaces that row and drops
	// the placeholder, so the message is never shown twice. When the
	// placeholder is not there it upserts item by its TS.
	Settle(localTS string, item messages.MessageItem)
	// RemoveLocal drops the placeholder localTS, reporting whether it
	// was there.
	RemoveLocal(localTS string) bool
	// Patch applies f to the row with TS ts (in the thread pane, the
	// parent as well) and returns that row as it was. ok is false when
	// the pane does not show ts.
	Patch(ts string, f func(*messages.MessageItem)) (prior messages.MessageItem, ok bool)
	// Remove takes out the row with TS ts (never the thread pane's
	// parent) and returns what Restore needs to put it back. A missing
	// row changes nothing.
	Remove(ts string) (messages.RemovedRow, bool)
	// Restore puts a removed row back at its index, selected again if
	// it was; nothing when the row is shown already.
	Restore(r messages.RemovedRow)
}

var (
	_ rowSink = (*messages.Model)(nil)
	_ rowSink = (*thread.Model)(nil)
)

// rowSinks is every pane showing one pending row. The thread pane and
// the channel panes are kept apart because the paths write them
// differently: a broadcast placeholder is a thread_broadcast row only in
// the channel feed, and only the channel panes get their own clone of
// the settled message.
type rowSinks struct {
	thread  rowSink   // the thread panel, when it shows the row's thread
	channel []rowSink // every window showing the row's channel
}

// sinksFor returns the panes showing a top-level row in channelID:
// every window on the channel. The thread panel is never one of them.
func (a *App) sinksFor(channelID string) rowSinks {
	var s rowSinks
	for _, mm := range a.modelsForChannel(channelID) {
		s.channel = append(s.channel, mm)
	}
	return s
}

// replySinksFor returns the panes showing a reply in threadTS: the
// thread panel when it is visible and shows that thread, and every
// window on the channel as well when the reply is broadcast. The thread
// panel is matched on its own channel, not activeChannelID: a thread
// opened from the threads view leaves activeChannelID on another
// channel.
func (a *App) replySinksFor(channelID, threadTS string, broadcast bool) rowSinks {
	var s rowSinks
	if broadcast {
		s = a.sinksFor(channelID)
	}
	if a.threadVisible && threadTS == a.threadPanel.ThreadTS() && channelID == a.threadPanel.ChannelID() {
		s.thread = a.threadPanel
	}
	return s
}

// settle lands the authoritative item in every pane. Each channel window
// gets its own cloneMessageItem, so sibling windows never share a
// Reactions array; the thread pane takes item as it is.
func (s rowSinks) settle(localTS string, item messages.MessageItem) {
	if s.thread != nil {
		s.thread.Settle(localTS, item)
	}
	for _, c := range s.channel {
		c.Settle(localTS, cloneMessageItem(item))
	}
}

// shownSinks returns the panes that may show an existing message of
// channelID, wherever it sits: every window on the channel, and the
// thread panel when it holds a thread of that channel (as a reply or
// as the parent). The edit and delete paths and their echoes write
// these.
func (a *App) shownSinks(channelID string) rowSinks {
	s := a.sinksFor(channelID)
	if channelID != "" && channelID == a.threadPanel.ChannelID() {
		s.thread = a.threadPanel
	}
	return s
}

// patch applies f to the row with TS ts in every pane, and returns the
// row as the first pane holding it showed it before.
func (s rowSinks) patch(ts string, f func(*messages.MessageItem)) (prior messages.MessageItem, ok bool) {
	for _, c := range s.channel {
		if p, found := c.Patch(ts, f); found && !ok {
			prior, ok = p, true
		}
	}
	if s.thread != nil {
		if p, found := s.thread.Patch(ts, f); found && !ok {
			prior, ok = p, true
		}
	}
	return prior, ok
}

// removedRow is a row the optimistic delete took out of one pane.
// threadTS is the thread the pane showed, for the thread pane.
type removedRow struct {
	sink     rowSink
	threadTS string
	row      messages.RemovedRow
}

// removeFrom takes the row with TS ts out of every pane in s and returns what
// each removed, to restore on failure.
func (a *App) removeFrom(s rowSinks, ts string) []removedRow {
	var out []removedRow
	for _, c := range s.channel {
		if r, ok := c.Remove(ts); ok {
			out = append(out, removedRow{sink: c, row: r})
		}
	}
	if s.thread != nil {
		if r, ok := s.thread.Remove(ts); ok {
			out = append(out, removedRow{sink: s.thread, threadTS: a.threadPanel.ThreadTS(), row: r})
		}
	}
	return out
}

// restoreTo puts each removed row back into its pane, if that pane
// still shows the conversation it came from: a window still on the
// channel, the thread panel still on that thread. A pane that has moved
// on would take the row into the wrong list.
func (a *App) restoreTo(channelID string, removed []removedRow) {
	now := a.shownSinks(channelID)
	for _, r := range removed {
		if r.sink == now.thread && r.threadTS == a.threadPanel.ThreadTS() ||
			slices.Contains(now.channel, r.sink) {
			r.sink.Restore(r.row)
		}
	}
}

// insertSinks returns the panes showing a pending send's row: the same
// ones its HTTP result writes.
func (a *App) insertSinks(ins pendingInsert) rowSinks {
	if ins.threadTS == "" {
		return a.sinksFor(ins.channelID)
	}
	return a.replySinksFor(ins.channelID, ins.threadTS, ins.broadcast)
}

// removeLocal rolls the placeholder localTS back out of every pane.
func (s rowSinks) removeLocal(localTS string) {
	if s.thread != nil {
		s.thread.RemoveLocal(localTS)
	}
	for _, c := range s.channel {
		c.RemoveLocal(localTS)
	}
}

// localRow is the placeholder for text the user just sent, under
// localTS until the post returns the real TS. The text is converted
// from the user's CommonMark to Slack mrkdwn with the converter the
// send itself uses, so bold, code and links render before the swap.
// threadTS and subtype are empty for a top-level send.
func (a *App) localRow(localTS, text, threadTS, subtype string) messages.MessageItem {
	converted, _ := mrkdwn.Convert(text)
	return messages.MessageItem{
		TS:        localTS,
		UserID:    a.currentUserID,
		UserName:  a.userNameFor(a.currentUserID),
		Text:      converted,
		Timestamp: a.nowFormatted(),
		ThreadTS:  threadTS,
		Subtype:   subtype,
	}
}

// noWorkspaceReason is the failure a service result of nil stands for:
// the cmd/slk closures return nil when no workspace is active.
const noWorkspaceReason = "no active workspace"

// stampLocalTS wraps a send's service call so its sent or failed result
// carries ins.localTS, which the result's reducer arm needs to find the
// placeholder; services never set it. A failed thread reply also gets
// broadcast, so its arm knows to roll back the channel feed's copy. A
// sent reply's Broadcast is the service's to set. A nil result (no
// active workspace) becomes the send's failure, so the pending insert
// ends and the placeholder goes.
func stampLocalTS(ins pendingInsert, call func() tea.Msg) tea.Cmd {
	return func() tea.Msg {
		switch r := call().(type) {
		case nil:
			if ins.threadTS == "" {
				return MessageSendFailedMsg{ChannelID: ins.channelID, LocalTS: ins.localTS, Reason: noWorkspaceReason}
			}
			return ThreadReplySendFailedMsg{
				ChannelID: ins.channelID, ThreadTS: ins.threadTS, LocalTS: ins.localTS,
				Reason: noWorkspaceReason, Broadcast: ins.broadcast,
			}
		case MessageSentMsg:
			r.LocalTS = ins.localTS
			return r
		case MessageSendFailedMsg:
			r.LocalTS = ins.localTS
			return r
		case ThreadReplySentMsg:
			r.LocalTS = ins.localTS
			return r
		case ThreadReplySendFailedMsg:
			r.LocalTS = ins.localTS
			r.Broadcast = ins.broadcast
			return r
		default:
			return r
		}
	}
}

// stampEditID wraps an edit's service call so its result carries the
// pending edit's id, which the result's reducer arm settles; services
// never set it. A nil result (no active workspace) becomes a failed
// edit.
func stampEditID(e pendingEdit, call func() tea.Msg) tea.Cmd {
	return func() tea.Msg {
		switch r := call().(type) {
		case nil:
			return MessageEditedMsg{ChannelID: e.channelID, TS: e.ts, EditID: e.id, Err: errors.New(noWorkspaceReason)}
		case MessageEditedMsg:
			r.EditID = e.id
			return r
		default:
			return r
		}
	}
}

// failNilDelete wraps a delete's service call so a nil result (no
// active workspace) becomes a failed delete of channelID/ts.
func failNilDelete(channelID, ts string, call func() tea.Msg) tea.Cmd {
	return failNil(call, MessageDeletedMsg{ChannelID: channelID, TS: ts, Err: errNoWorkspace})
}

// errNoWorkspace is noWorkspaceReason as the error a failure carries.
var errNoWorkspace = errors.New(noWorkspaceReason)

// failNil wraps a service call so a nil result (no active workspace)
// becomes fail, the operation's failure. An operation the user started
// must end: one whose result never comes leaves its pending state (an
// in-flight flag, a spinner, an upload) stuck, with nothing said.
func failNil(call func() tea.Msg, fail tea.Msg) tea.Cmd {
	return func() tea.Msg {
		if r := call(); r != nil {
			return r
		}
		return fail
	}
}
