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
//	                           held while a send is pending),
//	                           append-to-pane, staging a
//	                           focus-gated read-cursor advance or
//	                           surfacing the unread dot, and
//	                           threads-list dirty-bump for replies.
//	SendMessageMsg           - user send: optimistic placeholder +
//	                           chat.postMessage call.
//	MessageSentMsg           - send landed: swap placeholder for
//	                           authoritative message.
//	MessageSendFailedMsg     - send failed: roll back placeholder,
//	                           give the typed text back + fire
//	                           SendFailed toast.
//	EditMessageMsg           - user edit: show the new body at
//	                           once, record a pending edit, and
//	                           send its chat.update unless one
//	                           for the message is in flight.
//	MessageEditedMsg         - edit result: show Slack's state as
//	                           best known, send the edit waiting
//	                           behind it, and on a real failure
//	                           reopen the edit (or give its text
//	                           back) + fire EditFailed toast.
//	DeleteMessageMsg         - user delete: remove the message
//	                           from every pane at once, record a
//	                           pending delete, chat.delete call.
//	MessageDeletedMsg        - delete result: on success (or
//	                           message_not_found) apply it as the
//	                           echo does; on failure put
//	                           the rows back + fire DeleteFailed
//	                           toast, unless the echo already
//	                           showed it deleted.
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
	"errors"

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
		// The forward's self-authored WS echo may be held by a compose
		// send pending in this channel, so show the HTTP result here
		// without touching drafts or placeholders. AppendMessage
		// deduplicates the echo whichever arrives first, including one
		// released later.
		for _, mm := range a.modelsForChannel(m.channelID) {
			mm.AppendMessage(cloneMessageItem(m.message))
		}
		return func() tea.Msg { return ToastMsg{Text: "Message forwarded to " + m.destination} }, true

	case MessageSentMsg:
		// The chat.postMessage HTTP response landed. Settle (see
		// rowSink) swaps the "local:..." placeholder from the
		// instant-display path (SendMessageMsg above) for the
		// authoritative message. With no placeholder (test paths
		// firing MessageSentMsg directly, or the user navigated
		// away and back between Enter and the HTTP response) the
		// message is upserted by its Slack TS. Echoes held while the
		// send was pending are released after it.
		ins, release, ok := a.pendingOps.EndInsert(m.LocalTS, m.Message.TS)
		if m.Message.TS == "" {
			// Nothing to settle the placeholder onto. Remove it
			// rather than leave a row that never resolves.
			if m.LocalTS != "" {
				if !ok {
					ins = pendingInsert{channelID: m.ChannelID}
				}
				a.insertSinks(ins).removeLocal(m.LocalTS)
			}
			return a.releaseHeld(release), true
		}
		if ok {
			a.recovered(ins.typed)
		}
		// settle gives each channel window its own clone.
		a.sinksFor(m.ChannelID).settle(m.LocalTS, sentItem(ins, ok, m.Message))
		return a.releaseHeld(release), true

	case MessageSendFailedMsg:
		// The chat.postMessage HTTP call failed; roll back the
		// optimistic placeholder so the user can see the send
		// didn't go through. A toast surfaces the reason. The
		// composer was reset at Enter, so the text as typed goes
		// back to it unless it holds something newer. Echoes held
		// while the send was pending are released after it: if
		// Slack posted the message anyway (the call failed after it
		// was accepted, e.g. a timeout), its echo shows it.
		ins, release, ok := a.pendingOps.EndInsert(m.LocalTS, "")
		if m.LocalTS != "" {
			a.sinksFor(m.ChannelID).removeLocal(m.LocalTS)
		}
		var copyText tea.Cmd
		var onClipboard bool
		if ok {
			copyText, _, onClipboard = a.giveTextBack(ins.origin())
		}
		reason := m.Reason
		return tea.Batch(copyText, func() tea.Msg {
			return statusbar.SendFailedMsg{Reason: reason, OnClipboard: onClipboard}
		}, a.releaseHeld(release)), true

	case EditMessageMsg:
		return reduceEditMessage(a, m), true

	case MessageEditedMsg:
		return reduceMessageEdited(a, m), true

	case DeleteMessageMsg:
		if cmd, blocked := a.sendingGuard(m.TS); blocked {
			return cmd, true
		}
		// The message leaves every pane that shows it at once; the
		// rows are kept to put back if the delete fails. A deleted
		// thread parent's panel closes only once Slack confirms
		// (applyMessageDeleted), since a closed panel cannot be put
		// back as it was.
		a.pendingOps.BeginDelete(pendingDelete{
			channelID: m.ChannelID, ts: m.TS,
			removed: a.removeFrom(a.shownSinks(m.ChannelID), m.TS),
		})
		messageSvc := a.messageSvc
		chID, ts := ids.ChannelID(m.ChannelID), ids.MessageTS(m.TS)
		return failNilDelete(m.ChannelID, m.TS, func() tea.Msg {
			return messageSvc.Delete(chID, ts)
		}), true

	case MarkUnreadMsg:
		if cmd, blocked := a.sendingGuard(m.BoundaryTS); blocked {
			return cmd, true
		}
		if cmd, blocked := a.sendingGuard(m.ThreadTS); blocked {
			return cmd, true
		}
		messageSvc := a.messageSvc
		chID := ids.ChannelID(m.ChannelID)
		threadTS := ids.ThreadTS(m.ThreadTS)
		boundaryTS := ids.MessageTS(m.BoundaryTS)
		n := m.UnreadCount
		return failNil(func() tea.Msg {
			return messageSvc.MarkUnread(chID, threadTS, boundaryTS, n)
		}, MessageMarkedUnreadMsg{
			ChannelID: m.ChannelID, ThreadTS: m.ThreadTS, BoundaryTS: m.BoundaryTS,
			UnreadCount: n, Err: errNoWorkspace,
		}), true

	case MessageDeletedMsg:
		d, ok := a.pendingOps.EndDelete(m.ChannelID, m.TS)
		if m.Err == nil || m.NotFound {
			// Slack deleted it (or it was already gone): do what its
			// echo does, so the thread panel closes on a deleted parent
			// even when the echo is slow. Repeating it after the echo
			// changes nothing.
			return a.applyMessageDeleted(m.ChannelID, m.TS), true
		}
		if ok && d.echoed {
			// The echo showed the message deleted (the call failed
			// after Slack accepted it): nothing to put back or report.
			return nil, true
		}
		if ok {
			a.restoreTo(m.ChannelID, d.removed)
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
		a.markedUnread(m.ChannelID, m.ThreadTS, m.BoundaryTS)
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
		a.pendingOps.DeleteEchoed(m.ChannelID, m.TS)
		a.pendingOps.DropHeld(m.ChannelID, m.TS)
		return a.applyMessageDeleted(m.ChannelID, m.TS), true
	}
	return nil, false
}

// reduceNewMessage handles NewMessageMsg. Extracted because the
// arm is ~100 lines covering six decision branches (edit echo,
// self-send dedup, holding while a send is pending, the ephemeral
// display-only return, the read-state decision for the channel and
// thread cursors, threads-list dirty bump). The ephemeral branch
// returns after the message is displayed, before any read-state work.
func reduceNewMessage(a *App, m NewMessageMsg) tea.Cmd {
	if m.TeamID == "" {
		// Only the active workspace's WebSocket dispatches messages.
		m.TeamID = a.activeTeamID
	}
	debuglog.Cache("NewMessageMsg: channel=%s ts=%s thread_ts=%s active=%s",
		m.ChannelID, m.Message.TS, m.Message.ThreadTS, a.activeChannelID)
	if m.Message.IsEdited {
		debuglog.Cache("NewMessageMsg: channel=%s ts=%s decision=skipped_edit_echo",
			m.ChannelID, m.Message.TS)
		// Edit echo: update existing message in place rather than
		// appending, in every pane that may show it (shownSinks:
		// every window viewing the channel, and the thread panel's
		// reply or parent when the panel is on that channel).
		// This branch must run BEFORE the isSelfSent dedup below,
		// since edits to messages we recently sent would otherwise
		// be silently dropped (the TS is still in selfSentTSes).
		//
		// The echo is Slack's stored state: its text, its blocks
		// (which the body is drawn from) and the edited mark, as they
		// are. While one of the user's own edits of the message is in
		// flight it is only recorded (EditEcho): it cannot say which
		// edit it carries, and the edit's result decides what shows.
		body := messages.BodyOf(m.Message)
		if a.pendingOps.EditEcho(m.ChannelID, m.Message.TS, body) {
			debuglog.Cache("NewMessageMsg: channel=%s ts=%s decision=recorded_edit_echo_pending",
				m.ChannelID, m.Message.TS)
			return nil
		}
		a.showBody(m.ChannelID, m.Message.TS, body)
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
	// While a send is pending in this conversation, the user's own
	// message is held rather than shown: it may be that send's echo,
	// another pending send's, or a message posted elsewhere, and
	// nothing in it says which. The send's result releases it (see
	// pendingOps). An ephemeral is never an echo of chat.postMessage.
	if !m.Message.Ephemeral && m.Message.UserID != "" && m.Message.UserID == a.currentUserID &&
		a.pendingOps.HoldEcho(m) {
		debuglog.Cache("NewMessageMsg: channel=%s ts=%s decision=held_pending_send",
			m.ChannelID, m.Message.TS)
		return nil
	}
	return showNewMessage(a, m)
}

// showNewMessage shows m, a new message that is neither an edit nor
// the echo of a send this session made, in every pane on its
// conversation and does what a new message does to read state and the
// threads list. reduceNewMessage calls it once the message passes its
// checks, and releaseHeld for a message held while a send was pending.
func showNewMessage(a *App, m NewMessageMsg) tea.Cmd {
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
	// user's own messages in the channel are held until the result
	// either way.
	ins := pendingInsert{
		localTS: localTS, channelID: m.ChannelID, body: messages.TypedBody(m.Text),
		postTeam: a.activeTeamID, teamID: m.TeamID, typed: m.Typed,
	}
	a.pendingOps.BeginInsert(ins)
	for _, s := range a.sinksFor(m.ChannelID).channel {
		s.InsertLocal(a.localRow(localTS, ins.body, "", ""))
	}
	messageSvc := a.messageSvc
	chID, text := ids.ChannelID(m.ChannelID), m.Text
	return stampLocalTS(ins, func() tea.Msg {
		return messageSvc.Send(chID, text)
	})
}

// editedBody is what chat.update stores for an edit to wire: the typed
// body (messages.TypedBody), marked edited.
func editedBody(wire string) messages.Body {
	b := messages.TypedBody(wire)
	b.Edited = true
	return b
}

// reduceEditMessage handles EditMessageMsg: the new body shows at once,
// converted as chat.update converts it (editedBody: the mrkdwn
// and the rich_text block the pane draws) and marked edited, wherever
// the message is (showBody), and the edit is recorded in pendingOps. Its
// chat.update goes now, or, when an edit of the message is in flight,
// once that one ends (reduceMessageEdited), so Slack applies the edits
// in the order they were made.
func reduceEditMessage(a *App, m EditMessageMsg) tea.Cmd {
	if messages.IsLocalTS(m.TS) {
		// The message is still sending, so it cannot be edited yet.
		// The text is given back like a failed edit's, and the
		// refusal is reported the same way, clipboard note included.
		copyText, _, onClipboard := a.giveTextBack(commandOrigin{teamID: m.TeamID, channelID: m.ChannelID, threadTS: m.ThreadTS, typed: m.Typed})
		return tea.Batch(copyText, editFailedToast(errStillSending, onClipboard))
	}
	body := editedBody(m.NewText)
	prior, shown := a.showBody(m.ChannelID, m.TS, body)
	e, send := a.pendingOps.BeginEdit(pendingEdit{
		channelID: m.ChannelID, ts: m.TS, wire: m.NewText, body: body,
		panel: m.Panel, threadTS: m.ThreadTS, teamID: m.TeamID, typed: m.Typed,
	}, prior, shown)
	if !send {
		debuglog.Cache("EditMessageMsg: channel=%s ts=%s decision=queued_behind_edit_in_flight", m.ChannelID, m.TS)
		return nil
	}
	return a.sendEdit(e)
}

// sendEdit is e's chat.update, its result stamped with e's id. An edit
// that waited behind another is refused, as a failure, when the user
// has switched workspace since typing it: the service sends to the
// active workspace.
func (a *App) sendEdit(e pendingEdit) tea.Cmd {
	if e.teamID != "" && e.teamID != a.activeTeamID {
		return func() tea.Msg {
			return MessageEditedMsg{ChannelID: e.channelID, TS: e.ts, EditID: e.id, Err: errEditWorkspaceChanged}
		}
	}
	messageSvc := a.messageSvc
	chID, ts, text := ids.ChannelID(e.channelID), ids.MessageTS(e.ts), e.wire
	return stampEditID(e, func() tea.Msg {
		return messageSvc.Edit(chID, ts, text)
	})
}

var (
	errEditWorkspaceChanged = errors.New("switched workspace before the edit was sent")
	errEditMessageGone      = errors.New("the message was deleted")
)

// showBody shows body on message ts of channelID wherever the message
// is: every pane that may show it (shownSinks), a held echo of it, and
// the rows a pending delete took out, so a failed delete puts back the
// message as it is now. It returns the message as it was before, from a
// pane, else from the pending delete; ok is false when neither has it.
// Every edit of a message's body goes through it: the optimistic edit,
// its settling, and message_changed echoes.
func (a *App) showBody(channelID, ts string, body messages.Body) (prior messages.MessageItem, ok bool) {
	prior, ok = a.shownSinks(channelID).patch(ts, body.Apply)
	a.pendingOps.PatchHeld(channelID, ts, body.Apply)
	if p, removed := a.pendingOps.PatchRemoved(channelID, ts, body.Apply); removed && !ok {
		prior, ok = p, true
	}
	return prior, ok
}

// reduceMessageEdited handles MessageEditedMsg, the chat.update result.
// The message then shows what pendingOps says it should (EndEdit), and
// the edit that waited behind this one, if any, is sent. A failed edit
// whose text Slack does not have, and that no newer edit replaced, is
// reported: reopened where it was typed, or, when the user has moved
// on, its text given back as that conversation's draft. When the
// message is deleted (or its delete is pending) the text goes to the
// clipboard instead, since a draft would post it as a new message. The
// editor is never closed here: submitEdit already closed it, and an
// editor open now is a newer edit.
func reduceMessageEdited(a *App, m MessageEditedMsg) tea.Cmd {
	var gone tea.Cmd
	if m.NotFound {
		// Slack has no such message: it was deleted, so it leaves every
		// pane as its message_deleted echo would take it out.
		gone = a.applyMessageDeleted(m.ChannelID, m.TS)
	}
	s, found := a.pendingOps.EndEdit(m.EditID, m.Err == nil)
	if !found {
		if m.Err == nil && m.Text != "" && !a.pendingOps.EditPending(m.ChannelID, m.TS) {
			// A result with no pending edit (one fired directly): its
			// text alone, so no stale rich_text block hides it.
			a.showBody(m.ChannelID, m.TS, messages.Body{Text: m.Text, Edited: true})
		}
		if m.Err == nil {
			return gone
		}
		return tea.Batch(gone, editFailedToast(m.Err, false))
	}
	if s.showKnown {
		a.showBody(m.ChannelID, m.TS, s.show)
	}
	if m.Err == nil {
		a.recovered(s.ended.typed)
	}
	cmds := []tea.Cmd{gone}
	if s.next != nil {
		cmds = append(cmds, a.sendEdit(*s.next))
	}
	err := m.Err
	if err == nil {
		err = errEditMessageGone
	}
	switch {
	case s.superseded:
		cmds = append(cmds, editFailedToast(err, false))
	case s.failed != nil && (s.gone || a.pendingOps.DeletePending(m.ChannelID, m.TS)):
		var copyText tea.Cmd
		if s.failed.typed != "" {
			copyText = a.keepUnsent(s.failed.typed)
		}
		cmds = append(cmds, copyText, editFailedToast(err, s.failed.typed != ""))
	case s.failed != nil:
		reopen, ok := a.reopenEdit(*s.failed)
		var copyText tea.Cmd
		var onClipboard bool
		if !ok {
			copyText, _, onClipboard = a.giveTextBack(s.failed.origin())
		}
		cmds = append(cmds, editFailedToast(err, onClipboard), reopen, copyText)
	}
	return tea.Batch(cmds...)
}

// errStillSending is why an edit of a placeholder is refused.
var errStillSending = errors.New("the message is still sending")

// editFailedToast reports a failed edit.
func editFailedToast(err error, onClipboard bool) tea.Cmd {
	reason := err.Error()
	return func() tea.Msg {
		return statusbar.EditFailedMsg{Reason: reason, OnClipboard: onClipboard}
	}
}

// applyMessageDeleted shows that Slack deleted message ts of channelID,
// on its message_deleted echo or the user's own successful delete. It is
// idempotent: a row already gone (the optimistic delete took it) is a
// no-op that leaves every selection alone.
func (a *App) applyMessageDeleted(channelID, ts string) tea.Cmd {
	a.pendingOps.EditGone(channelID, ts)
	for _, mm := range a.modelsForChannel(channelID) {
		mm.RemoveMessageByTS(ts)
	}
	if channelID == a.threadPanel.ChannelID() {
		a.threadPanel.RemoveMessageByTS(ts)
	}
	// If the deleted message is the one currently being
	// edited, cancel the edit (the message is gone --
	// submitting would fail).
	if a.editing.Matches(channelID, ts) {
		a.cancelEdit()
	}
	// If the deleted message was the open thread's parent,
	// close the thread panel -- Slack deletes the entire
	// thread when the parent is deleted. Cancel any in-flight
	// edit first so we don't leave the user in insert mode
	// with a hidden compose.
	if a.threadVisible && a.threadPanel.ThreadTS() == ts && channelID == a.threadPanel.ChannelID() {
		a.cancelEdit()
		if a.compose.Uploading() || a.threadCompose.Uploading() {
			a.threadCloseAfterUpload = true
		} else {
			a.CloseThread()
		}
	}
	return a.dropActionPrompt(channelID, ts)
}

// releaseHeld shows echoes a resolved send held (pendingOps.EndInsert)
// as the new messages they are, in order, through showNewMessage, so
// their reply counts, read state and threads-list refresh happen as for
// any message. It returns their commands.
func (a *App) releaseHeld(held []NewMessageMsg) tea.Cmd {
	var cmds []tea.Cmd
	for _, m := range held {
		if m.TeamID != a.activeTeamID {
			// The user switched workspace while it was held: its panes
			// are gone, and a Slack Connect channel with the same ID in
			// this workspace must not show it. That workspace's next
			// fetch of the channel brings it.
			debuglog.Cache("NewMessageMsg: team=%s channel=%s ts=%s decision=dropped_held_other_team",
				m.TeamID, m.ChannelID, m.Message.TS)
			continue
		}
		debuglog.Cache("NewMessageMsg: channel=%s ts=%s decision=released_held_echo",
			m.ChannelID, m.Message.TS)
		cmds = append(cmds, showNewMessage(a, m))
	}
	return tea.Batch(cmds...)
}
