// internal/ui/reducer_send.go
//
// Message-lifecycle reducer for App.Update (Phase 4i).
//
// Owns the eleven Update arms that cover the inbound and outbound
// message lifecycle for channel messages (thread-reply lifecycle
// lives in reducer_threads.go):
//
//	NewMessageMsg            - inbound WS event for any channel:
//	                           edit-echo update, self-send echo
//	                           handling (drop by recorded TS, or
//	                           adoption by a pending send),
//	                           append-to-pane, staging a
//	                           focus-gated read-cursor advance or
//	                           surfacing the unread dot, and
//	                           threads-list dirty-bump for replies.
//	SendMessageMsg           - user send: optimistic placeholder +
//	                           chat.postMessage call.
//	MessageSentMsg           - send landed: swap placeholder for
//	                           authoritative message.
//	MessageSendFailedMsg     - send failed: roll back placeholder
//	                           + fire SendFailed toast, unless the
//	                           echo already showed it posted.
//	EditMessageMsg           - user edit: chat.update call.
//	MessageEditedMsg         - edit result: leave edit mode + on
//	                           failure fire EditFailed toast.
//	DeleteMessageMsg         - user delete: chat.delete call.
//	MessageDeletedMsg        - delete result: on failure fire
//	                           DeleteFailed toast.
//	MarkUnreadMsg            - user mark-unread: subscriptions
//	                           mark call.
//	MessageMarkedUnreadMsg   - mark-unread result: apply local
//	                           read-state mark + fire success or
//	                           failure toast.
//	WSMessageDeletedMsg      - inbound WS delete echo: remove from
//	                           both panes, cancel any in-flight
//	                           edit of this message, close the
//	                           thread panel if the deleted message
//	                           is the open thread's parent.
//
// Free reducer (not controller-absorbed): these arms cooperate on
// the messagepane, threadPanel, pendingOps, editController, sidebar
// read-state, and the message service. No single existing
// controller owns all of that cross-section.
//
// Helpers (applyChannelMark, applyThreadMarkUnread, scheduleThreadsDirty,
// recordChannelMark, recordThreadMark, scheduleMarkFlush,
// notifyReadStateChanged, userNameFor, nowFormatted, cancelEdit,
// CloseThread) stay on App; the reducer calls them via `a`.
package ui

import (
	tea "charm.land/bubbletea/v2"

	"github.com/gammons/slk/internal/debuglog"
	"github.com/gammons/slk/internal/ids"
	"github.com/gammons/slk/internal/ui/messages"
	"github.com/gammons/slk/internal/ui/statusbar"
)

var reduceSend reducerFunc = func(a *App, msg tea.Msg) (tea.Cmd, bool) {
	switch m := msg.(type) {
	case NewMessageMsg:
		return reduceNewMessage(a, m), true

	case SendMessageMsg:
		return reduceSendMessage(a, m), true

	case messageForwardedMsg:
		if m.teamID != a.activeTeamID {
			return nil, true
		}
		// A concurrent compose send in this channel can adopt the forward's
		// self-authored WS echo as its own. Reconcile the HTTP result without
		// touching drafts or placeholders. AppendMessage deduplicates echoes
		// that arrived first.
		for _, mm := range a.modelsForChannel(m.channelID) {
			mm.AppendMessage(cloneMessageItem(m.message))
		}
		return func() tea.Msg { return ToastMsg{Text: "Message forwarded to " + m.destination} }, true

	case MessageSentMsg:
		// The chat.postMessage HTTP response landed. Settle (see
		// rowSink) swaps the "local:..." placeholder from the
		// instant-display path (SendMessageMsg above) for the
		// authoritative message. When the echo got in first and was
		// adopted, the placeholder is already gone and the echo's
		// row takes the authoritative message. With no placeholder
		// (test paths firing MessageSentMsg directly, or the user
		// navigated away and back between Enter and the HTTP
		// response) the message is upserted by its Slack TS.
		ins, ok := a.pendingOps.EndInsert(m.LocalTS)
		if m.Message.TS == "" {
			// Nothing to settle the placeholder onto. Remove it
			// rather than leave a row that never resolves, unless
			// an echo already settled it (that row is the posted
			// message).
			if !ins.adopted && m.LocalTS != "" {
				if !ok {
					ins = pendingInsert{channelID: m.ChannelID}
				}
				a.insertSinks(ins).removeLocal(m.LocalTS)
			}
			return nil, true
		}
		a.pendingOps.RecordSent(m.Message.TS)
		// settle gives each channel window its own clone.
		a.sinksFor(m.ChannelID).settle(m.LocalTS, m.Message)
		return nil, true

	case MessageSendFailedMsg:
		// The chat.postMessage HTTP call failed; roll back the
		// optimistic placeholder so the user can see the send
		// didn't go through. A toast surfaces the reason. If the
		// echo was adopted, Slack posted the message (the call
		// failed after it was accepted, e.g. a timeout), so the row
		// stays and nothing is reported.
		if ins, ok := a.pendingOps.EndInsert(m.LocalTS); ok && ins.adopted {
			return nil, true
		}
		if m.LocalTS != "" {
			a.sinksFor(m.ChannelID).removeLocal(m.LocalTS)
		}
		reason := m.Reason
		return func() tea.Msg {
			return statusbar.SendFailedMsg{Reason: reason}
		}, true

	case EditMessageMsg:
		messageSvc := a.messageSvc
		chID, ts, text := ids.ChannelID(m.ChannelID), ids.MessageTS(m.TS), m.NewText
		return func() tea.Msg {
			return messageSvc.Edit(chID, ts, text)
		}, true

	case MessageEditedMsg:
		// Only exit edit mode if this result matches the edit
		// that's currently in flight. A stale result from a
		// previously cancelled or replaced edit must not clobber
		// the current one.
		if a.editing.Matches(m.ChannelID, m.TS) {
			a.cancelEdit()
		}
		if m.Err == nil {
			return nil, true
		}
		reason := m.Err.Error()
		return func() tea.Msg {
			return statusbar.EditFailedMsg{Reason: reason}
		}, true

	case DeleteMessageMsg:
		messageSvc := a.messageSvc
		chID, ts := ids.ChannelID(m.ChannelID), ids.MessageTS(m.TS)
		return func() tea.Msg {
			return messageSvc.Delete(chID, ts)
		}, true

	case MarkUnreadMsg:
		messageSvc := a.messageSvc
		chID := ids.ChannelID(m.ChannelID)
		threadTS := ids.ThreadTS(m.ThreadTS)
		boundaryTS := ids.MessageTS(m.BoundaryTS)
		n := m.UnreadCount
		return func() tea.Msg {
			return messageSvc.MarkUnread(chID, threadTS, boundaryTS, n)
		}, true

	case MessageDeletedMsg:
		if m.Err == nil {
			return nil, true
		}
		reason := m.Err.Error()
		return func() tea.Msg {
			return statusbar.DeleteFailedMsg{Reason: reason}
		}, true

	case MessageMarkedUnreadMsg:
		if m.Err != nil {
			reason := m.Err.Error()
			return func() tea.Msg {
				return statusbar.MarkUnreadFailedMsg{Reason: reason}
			}, true
		}
		if m.ThreadTS == "" {
			a.applyChannelMark(m.ChannelID, m.BoundaryTS, m.UnreadCount)
		} else {
			a.applyThreadMarkUnread(m.ChannelID, m.ThreadTS, m.BoundaryTS)
		}
		return func() tea.Msg {
			return statusbar.MarkedUnreadMsg{}
		}, true

	case WSMessageDeletedMsg:
		debuglog.Cache("WSMessageDeletedMsg: channel=%s ts=%s active=%s",
			m.ChannelID, m.TS, a.activeChannelID)
		for _, mm := range a.modelsForChannel(m.ChannelID) {
			mm.RemoveMessageByTS(m.TS)
		}
		if m.ChannelID == a.threadPanel.ChannelID() {
			a.threadPanel.RemoveMessageByTS(m.TS)
		}
		// If the deleted message is the one currently being
		// edited, cancel the edit (the message is gone --
		// submitting would fail).
		if a.editing.Matches(m.ChannelID, m.TS) {
			a.cancelEdit()
		}
		// If the deleted message was the open thread's parent,
		// close the thread panel -- Slack deletes the entire
		// thread when the parent is deleted. Cancel any in-flight
		// edit first so we don't leave the user in insert mode
		// with a hidden compose.
		if a.threadVisible && a.threadPanel.ThreadTS() == m.TS && m.ChannelID == a.threadPanel.ChannelID() {
			a.cancelEdit()
			if a.compose.Uploading() || a.threadCompose.Uploading() {
				a.threadCloseAfterUpload = true
			} else {
				a.CloseThread()
			}
		}
		return a.dropActionPrompt(m.ChannelID, m.TS), true
	}
	return nil, false
}

// reduceNewMessage handles NewMessageMsg. Extracted because the
// arm is ~100 lines covering six decision branches (edit echo,
// self-send dedup, adoption by a pending send, the ephemeral
// display-only return, the read-state decision for the channel and
// thread cursors, threads-list dirty bump). The ephemeral branch
// returns after the message is displayed, before any read-state work.
func reduceNewMessage(a *App, m NewMessageMsg) tea.Cmd {
	debuglog.Cache("NewMessageMsg: channel=%s ts=%s thread_ts=%s active=%s",
		m.ChannelID, m.Message.TS, m.Message.ThreadTS, a.activeChannelID)
	if m.Message.IsEdited {
		debuglog.Cache("NewMessageMsg: channel=%s ts=%s decision=skipped_edit_echo",
			m.ChannelID, m.Message.TS)
		// Edit echo: update existing message in place rather than
		// appending. Fan out to every window viewing the channel;
		// gate on the thread panel's channel for the thread cache
		// -- avoids touching panes showing a different channel.
		// This branch must run BEFORE the isSelfSent dedup below,
		// since edits to messages we recently sent would otherwise
		// be silently dropped (the TS is still in selfSentTSes).
		for _, mm := range a.modelsForChannel(m.ChannelID) {
			mm.UpdateMessageInPlace(m.Message.TS, m.Message.Text)
		}
		if m.ChannelID == a.threadPanel.ChannelID() {
			a.threadPanel.UpdateMessageInPlace(m.Message.TS, m.Message.Text)
			a.threadPanel.UpdateParentInPlace(m.Message.TS, m.Message.Text)
		}
		return nil
	}
	// Skip the WS echo of our own optimistic add. The corresponding
	// MessageSentMsg / ThreadReplySentMsg already updated the UI
	// and scheduled side effects; redoing them here would
	// double-render.
	if a.pendingOps.IsSelfSent(m.Message.TS) {
		debuglog.Cache("NewMessageMsg: channel=%s ts=%s decision=skipped_self_send",
			m.ChannelID, m.Message.TS)
		return nil
	}
	// An echo of a send that is still waiting for its HTTP result
	// takes the place of that send's placeholder, in every pane the
	// result would write, instead of being appended beside it. The
	// result then lands Slack's response on it and does the side
	// effects (reply count, threads dirty), so none are done here.
	// An ephemeral is never an echo of chat.postMessage.
	if !m.Message.Ephemeral && m.Message.UserID != "" && m.Message.UserID == a.currentUserID {
		if ins, ok := a.pendingOps.AdoptEcho(m.ChannelID, m.Message); ok {
			debuglog.Cache("NewMessageMsg: channel=%s ts=%s decision=adopted_pending_send local=%s",
				m.ChannelID, m.Message.TS, ins.localTS)
			a.pendingOps.RecordSent(m.Message.TS)
			a.insertSinks(ins).settle(ins.localTS, m.Message)
			return nil
		}
	}
	// An app's message ends its "working" status in this channel even
	// when the clearing ai_assistant_status never arrives.
	if m.Message.UserID != "" {
		a.typing.ClearBot(m.ChannelID, m.Message.UserID)
	}
	// Model writes fan out to EVERY window viewing the channel,
	// focused or not (Phase 3): visible-but-unfocused windows show
	// realtime traffic too.
	for _, mm := range a.modelsForChannel(m.ChannelID) {
		// Always add to the pane if it's a top-level message (no
		// ThreadTS or is the parent) or a thread_broadcast reply;
		// update the parent's reply count when a thread reply
		// arrives. cloneMessageItem: fresh WS messages carry no
		// reactions today, but a shared Reactions array across
		// sibling models would corrupt on the first in-place
		// UpdateReaction — clone is the cheap insurance.
		isBroadcast := m.Message.Subtype == "thread_broadcast"
		if m.Message.ThreadTS == "" || m.Message.ThreadTS == m.Message.TS || isBroadcast {
			mm.AppendMessage(cloneMessageItem(m.Message))
		}
		if m.Message.ThreadTS != "" && m.Message.ThreadTS != m.Message.TS && !m.Message.Ephemeral {
			mm.IncrementReplyCount(m.Message.ThreadTS, m.Message.TS)
		}
	}
	// Route thread replies to the open thread panel keyed on the
	// PANEL's identity (its channel + thread ts), not the active
	// channel. The panel can be showing a thread from a non-active
	// channel — opened from the Threads view, or from a permalink
	// pointing into another channel; neither path touches
	// activeChannelID — and live replies must still land.
	// (Previously this lived inside the active-channel branch below,
	// so those panels went stale until a reopen forced a refetch.)
	//
	// Shared with the thread read-cursor gate below, which must not
	// drift from this one: the message is staged for a read-cursor
	// advance exactly when it is rendered into the open panel. Whether
	// that advance is actually issued is decided later, at flush time,
	// by terminal focus.
	inOpenThreadPanel := a.threadVisible &&
		m.ChannelID == a.threadPanel.ChannelID() &&
		m.Message.ThreadTS == a.threadPanel.ThreadTS()
	if inOpenThreadPanel {
		a.threadPanel.AddReply(m.Message)
	}
	if m.Message.Ephemeral {
		// Only the current user can see it, and Slack does not know it
		// as a message in the conversation: no read cursor may land on
		// its ts (Slack never returns it, so the cursor would sit
		// behind a ghost), it changes no read state, and it does not
		// make the threads list dirty. Display only.
		debuglog.Cache("NewMessageMsg: channel=%s ts=%s decision=ephemeral_display_only",
			m.ChannelID, m.Message.TS)
		onScreen := inOpenThreadPanel ||
			(a.view == ViewChannels && m.ChannelID == a.activeChannelID && (m.Message.ThreadTS == "" || m.Message.ThreadTS == m.Message.TS))
		if onScreen && messages.HasButtonActions(m.Message) {
			return a.enqueueActionPrompt(actionPromptRef{
				channelID: m.ChannelID, ts: m.Message.TS, threadTS: m.Message.ThreadTS,
			})
		}
		return nil
	}
	isThreadReply := m.Message.ThreadTS != "" && m.Message.ThreadTS != m.Message.TS
	isBroadcast := m.Message.Subtype == "thread_broadcast"

	// Thread-eligible: a reply the user has on screen in the open
	// thread panel. The visibility gate mirrors the channel rule --
	// "the panel is open on this thread", not "the thread pane holds
	// slk-internal focus". Terminal focus is NOT checked here; it
	// gates the flush (scheduleMarkFlush), so a blurred reply stays
	// staged until the FocusMsg catch-up.
	if isThreadReply && inOpenThreadPanel {
		a.recordThreadMark(m.ChannelID, m.Message.ThreadTS, m.Message.TS)
	}

	// Channel-eligible: top-level messages and thread_broadcasts. Plain
	// thread replies never advance the parent channel cursor on Slack.
	if !isThreadReply || isBroadcast {
		switch {
		case m.ChannelID != a.activeChannelID:
			// Not the channel on screen. The has_unread=true DB write
			// already happened in the WS handler; force the sidebar and
			// workspace rail to re-read it.
			debuglog.Cache("NewMessageMsg: channel=%s ts=%s decision=mark_unread",
				m.ChannelID, m.Message.TS)
			a.notifyReadStateChanged()
		case a.terminalFocused:
			// On screen AND the user can actually see it: stage a read-
			// cursor advance. No notifyReadStateChanged, so this event
			// triggers no sidebar repaint -- the has_unread=true the WS
			// handler just wrote is cleared by the flush before
			// anything normally repaints the dot. Inside a tmux
			// session that has not yet proven focus reporting no flush
			// is armed (see autoMarkArmed), so there has_unread
			// survives and the dot appears at the next repaint --
			// which is what this arm did before it marked anything.
			debuglog.Cache("NewMessageMsg: channel=%s ts=%s decision=active_focused_mark_read",
				m.ChannelID, m.Message.TS)
			a.recordChannelMark(m.ChannelID, m.Message.TS)
		default:
			// Selected, but slk is in a background terminal / inactive
			// tmux window or pane. The user has NOT seen this, so show
			// the dot from the has_unread the WS handler just wrote.
			// The advance is still staged, but scheduleMarkFlush arms
			// no tick while blurred, so it sits until the FocusMsg
			// catch-up in reducer_focus.go issues it.
			debuglog.Cache("NewMessageMsg: channel=%s ts=%s decision=active_blurred_stay_unread",
				m.ChannelID, m.Message.TS)
			a.recordChannelMark(m.ChannelID, m.Message.TS)
			a.notifyReadStateChanged()
		}
	} else if inOpenThreadPanel && a.terminalFocused {
		// On screen and visible: the thread mark staged above clears
		// it before anything would repaint the dot, the same reason
		// the channel arm skips the notification.
		debuglog.Cache("NewMessageMsg: channel=%s ts=%s decision=thread_reply_on_screen_mark_read",
			m.ChannelID, m.Message.TS)
	} else {
		// The reply never touches the channel's has_unread, but the WS
		// handler cached it before sending this message, and the rail's
		// thread half (railThreadsUnread in cmd/slk) reads that cache:
		// a subscribed thread with a newer reply is unread. Re-read now,
		// as the channel arm does, rather than waiting for the threads
		// list refresh scheduled below -- that fetch is dropped if the
		// user switches workspaces inside its debounce window.
		debuglog.Cache("NewMessageMsg: channel=%s ts=%s decision=thread_reply_refresh_rail",
			m.ChannelID, m.Message.TS)
		a.notifyReadStateChanged()
	}

	var cmds []tea.Cmd
	if c := a.scheduleMarkFlush(); c != nil {
		cmds = append(cmds, c)
	}
	// A thread reply (regardless of channel) may have changed the
	// involved-threads list -- ask for a re-query. A burst of replies
	// sends one dirty message each; the receiving arm in reduceThreads
	// is what collapses them into a single fetch.
	if m.Message.ThreadTS != "" {
		if c := a.scheduleThreadsDirty(); c != nil {
			cmds = append(cmds, c)
		}
	}
	if len(cmds) == 0 {
		return nil
	}
	return tea.Batch(cmds...)
}

// reduceSendMessage handles SendMessageMsg. Extracted to keep the
// reduceSend dispatch switch readable -- this arm does optimistic
// placeholder + async chat.postMessage + LocalTS attachment.
func reduceSendMessage(a *App, m SendMessageMsg) tea.Cmd {
	// Instant-display: append an optimistic placeholder for the
	// active channel immediately, before the chat.postMessage HTTP
	// round-trip. The placeholder carries a "local:<n>" TS so the
	// MessageSentMsg / MessageSendFailedMsg handler can find and
	// swap (or remove) it once the HTTP result lands.
	//
	// We only render the placeholder in windows viewing the send's
	// channel (the focused window plus any same-channel siblings —
	// they must show the optimistic message too). For background
	// sends (rare -- would require sending while in a different
	// view) no window matches and we skip the placeholder; the HTTP
	// response will fall back to Settle's upsert.
	//
	// localRow converts the user-typed CommonMark to Slack mrkdwn
	// with the converter client.SendMessage uses, so "**bold**" is
	// styled in the placeholder rather than literal until the swap.
	localTS := a.pendingOps.NextLocalTS()
	// Recorded whether or not a pane shows the placeholder, so the
	// echo is adopted rather than appended either way.
	a.pendingOps.BeginInsert(pendingInsert{localTS: localTS, channelID: m.ChannelID})
	for _, s := range a.sinksFor(m.ChannelID).channel {
		s.InsertLocal(a.localRow(localTS, m.Text, "", ""))
	}
	messageSvc := a.messageSvc
	chID, text := ids.ChannelID(m.ChannelID), m.Text
	return stampLocalTS(localTS, false, func() tea.Msg {
		return messageSvc.Send(chID, text)
	})
}
