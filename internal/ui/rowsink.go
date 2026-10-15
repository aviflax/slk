// internal/ui/rowsink.go
//
// The optimistic send paths (channel send, thread reply, broadcast)
// write the same pending row to every pane that shows it: an
// instant-display placeholder under a "local:N" TS, then the
// authoritative message from the chat.postMessage response, or the
// placeholder's removal when the post fails. rowSink is that write
// surface, sinksFor picks the panes, and localRow builds the
// placeholder.
package ui

import (
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

// stampLocalTS wraps a send's service call so its sent or failed result
// carries localTS, which the result's reducer arm needs to find the
// placeholder; services never set it. A failed thread reply also gets
// broadcast, so its arm knows to roll back the channel feed's copy. A
// sent reply's Broadcast is the service's to set.
func stampLocalTS(localTS string, broadcast bool, call func() tea.Msg) tea.Cmd {
	return func() tea.Msg {
		switch r := call().(type) {
		case MessageSentMsg:
			r.LocalTS = localTS
			return r
		case MessageSendFailedMsg:
			r.LocalTS = localTS
			return r
		case ThreadReplySentMsg:
			r.LocalTS = localTS
			return r
		case ThreadReplySendFailedMsg:
			r.LocalTS = localTS
			r.Broadcast = broadcast
			return r
		default:
			return r
		}
	}
}
