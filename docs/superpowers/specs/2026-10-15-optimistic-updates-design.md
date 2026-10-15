# Optimistic updates: one shared mechanism

Status: done, 2026-10-15 (phases 1 and 2, phase 3: optimistic edit
and delete, phase 4: review fixes, phase 5: edit and delete fixes,
phase 6: ordering, read cursors and timeouts)

## Problem

Several user actions update the screen before Slack answers: channel
sends, thread replies (with broadcast), reaction toggles, activity read
toggles, thread mark-read and presence. Each one is written by hand, and
the message-row paths are written twice, once for the channel pane and
once for the thread pane:

- The placeholder `MessageItem` literal appears three times
  (`reducer_send.go`, twice in `reducer_threads.go`). The preamble
  (`MarkInFlight`, `NextLocalTS`, `mrkdwn.Convert`), the closure that
  attaches `LocalTS` to the result, swap-or-upsert, the empty-TS guard
  and remove-on-failure each appear twice.
- `messages.Model` and `thread.Model` each carry their own
  `SwapLocalSent`/`SwapLocalSentReply`, `RemoveLocalSent`/
  `RemoveLocalSentReply`, `UpsertSelfSent`/`UpsertSelfSentReply`,
  `UpdateMessageInPlace` and `UpdateReaction`. Each scans `[]MessageItem`
  from the back by TS. They differ only in how they fix up the selection
  and caches afterwards, and `RemoveLocalSent` and `RemoveMessageByTS`
  disagree on that fix-up even within one pane.
- Every thread-side write is guarded by its own copy of
  `a.threadVisible && ThreadTS == … && ChannelID == …` (three copies
  in `reducer_threads.go` alone).
- The reaction closure is duplicated between `app.go` and
  `mode_reaction_picker.go`.

Edit and delete are not optimistic at all. An edit only appears when
Slack's `message_changed` arrives over the WebSocket, which takes a few
seconds.

The hand-written copies have also drifted into bugs:

1. **Duplicate row.** If the WebSocket echo arrives after the 3s
   in-flight window but before the HTTP response, the echo is appended
   under its real TS and `SwapLocalSent` then gives the placeholder the
   same TS. Neither swap checks for an existing row.
2. **Over-broad echo suppression.** `MarkInFlight` drops every
   self-authored echo in the channel for 3s, not just the one for the
   pending send. That includes slk's own file uploads, whose only render
   path is the echo, and messages sent from another Slack client. An
   edit also calls `MarkInFlight`, so editing silences the channel too.
3. **Message posted but never shown.** If the HTTP call fails after Slack
   accepted the post (a timeout), the placeholder is removed and the
   early echo was already dropped.
4. **Stuck placeholder.** A sent result with an empty TS returns without
   removing the placeholder.
5. **Lost text.** Send and reply reset the composer before the result;
   a failed edit exits edit mode and discards the edited text.
6. **Actions on a placeholder.** Nothing stops edit, delete, react or
   mark-unread from targeting a `local:N` TS, so a bogus TS goes to Slack.

## Design

The rule from AGENTS.md applies: extract the substrate, not the widget.
The shared part is "a pending change to a message row, shown in every
pane that displays that row, settled by the HTTP result". Non-row state (presence, activity read, thread marks) stays
as it is; its shape differs and the activity path is already sound.

### 1. Row operations on `[]MessageItem` (package `messages`)

Pure functions in `internal/ui/messages/rows.go`, each returning a
`RowChange{Op, Index, From}` (`RowNone`, `RowAdded`, `RowRemoved`,
`RowReplaced`, `RowMoved`) so the pane can fix its own selection and
caches:

```go
func IndexOfTS(rows []MessageItem, ts string) int      // from the back
func FirstIndexOfTS(rows []MessageItem, ts string) int // from the front
func ReplaceTS(rows []MessageItem, ts string, item MessageItem) ([]MessageItem, RowChange)
func RemoveTS(rows []MessageItem, ts string) ([]MessageItem, RowChange)
func RemoveAt(rows []MessageItem, i int) ([]MessageItem, RowChange)
func PatchTS(rows []MessageItem, ts string, f func(*MessageItem)) ([]MessageItem, RowChange) // from the front
func Insert(rows []MessageItem, item MessageItem) ([]MessageItem, RowChange) // in TS order; no-op if shown
func IsNewest(rows []MessageItem, i int) bool // no row Slack knows after i: the pane follows it
func CompareTS(a, b string) int // numeric time order of Slack TSes
func Upsert(rows []MessageItem, item MessageItem) ([]MessageItem, RowChange) // replace, else Insert
// Settle replaces the placeholder localTS with item, which then moves
// to where its TS belongs (RowReplaced at its index, or RowMoved from
// it). When another row already has item.TS (a history load brought it
// in first), that row takes item and the placeholder is removed
// (RowRemoved at its old index). No placeholder: RowNone.
func Settle(rows []MessageItem, localTS string, item MessageItem) ([]MessageItem, RowChange)
func (c RowChange) Selection(selected int) int // the cursor kept on its message

func LocalTS(n uint64) string           // "local:<n>", the placeholder id
func IsLocalTS(ts string) bool          // the one placeholder predicate
func KnownToSlack(msg MessageItem) bool // a real TS, not ephemeral
func LastIndexBefore(rows []MessageItem, i int, keep func(MessageItem) bool) int
```

Matching is literal: an empty TS matches a row with an empty TS. The
send paths (append, settle, roll back, upsert), the edit path and the
delete path write through these; the reaction and reply-count paths only
locate their row with `FirstIndexOfTS`. The send paths scan from the
back (`IndexOfTS`); the edit and delete paths and their echoes scan from
the front (`PatchTS`, `FirstIndexOfTS`), as they always have, so an
edit or delete and its echo touch the same row.

The invariant: in both panes the rows Slack knows are in TS order,
each TS once, and placeholders come after them in the order they were
sent. `Insert` is the only way a row is added (a WebSocket message, a
released held echo, a forward, an upsert, a restored delete), and
`Settle` moves the sent message to its TS. `Insert` puts a row before
the first row Slack knows with a later TS, else after the last such
row, ahead of placeholders; a placeholder is appended. History
prepended or messages appended meanwhile do not decide where it goes,
only their TSes do. It does nothing when the TS is shown already. A
pane follows the newest row (`IsNewest`: it selects it, which scrolls
to it, as appending always did); any other insert or move keeps the
cursor on the message it was on (`RowChange.Selection`). History loads
(`SetMessages`, `PrependMessages`) come from Slack in order.

Property tests are in `rows_prop_test.go`: `Settle` never yields
duplicate TSes, reports its op, and keeps every other row's order;
`RemoveTS` then re-inserting at the reported index restores the slice;
on TS-sorted rows `Insert` undoes `RemoveAt` and is a no-op on a
present TS, and it keeps the rows sorted after any prepends and
appends; `CompareTS` is the time order. `TestRowOpsKeepPaneOrder`
runs random sequences of arrivals, sends, settles (onto new or shown
TSes), removals and duplicates, and checks the invariant and that
`Selection` keeps the cursor on its message after every step.

There are two selection fix-ups. Each pane's `afterLocalRemoved` (used
by `RemoveLocal`) steps back on `i < selected`, keeping the cursor on
the message it was on. A `Settle` that removes the placeholder does the
same, except that a selected placeholder hands the cursor to the row
that took the message (`afterSettleRemoved`). A delete
(`Remove`, behind `RemoveMessageByTS`) steps back on
`i <= selected && selected > 0`, so deleting the selected message lands
on the one before it, as deleting always has.

### 2. One row-writing interface for both panes

```go
// internal/ui/rowsink.go
type rowSink interface {
	InsertLocal(item messages.MessageItem)            // append unless shown
	Settle(localTS string, item messages.MessageItem) // messages.Settle, else upsert
	RemoveLocal(localTS string) bool
	Patch(ts string, f func(*messages.MessageItem)) (prior messages.MessageItem, ok bool)
	Remove(ts string) (messages.RemovedRow, bool) // the row, whether selected, where the cursor was left
	Restore(r messages.RemovedRow)                // in TS order; reselected only if the cursor has not moved; no-op if shown
}
var _ rowSink = (*messages.Model)(nil)
var _ rowSink = (*thread.Model)(nil)

type rowSinks struct {
	thread  rowSink   // the thread panel, when it shows the row's thread
	channel []rowSink // every window showing the row's channel
}
func (s rowSinks) settle(localTS string, item messages.MessageItem) // clones per channel window
func (s rowSinks) removeLocal(localTS string)
func (s rowSinks) patch(ts string, f func(*messages.MessageItem)) (prior messages.MessageItem, ok bool)

func (a *App) sinksFor(channelID string) rowSinks
func (a *App) replySinksFor(channelID, threadTS string, broadcast bool) rowSinks
func (a *App) insertSinks(ins pendingInsert) rowSinks // one of the two, for a pending send
func (a *App) shownSinks(channelID string) rowSinks   // an existing message: edit, delete, echoes
func (a *App) removeFrom(s rowSinks, ts string) []removedRow
func (a *App) restoreTo(channelID string, removed []removedRow)
func (a *App) showBody(channelID, ts string, body messages.Body) (prior messages.MessageItem, ok bool) // reducer_send.go
func (a *App) localRow(localTS, text, threadTS, subtype string) messages.MessageItem

const noWorkspaceReason = "no active workspace"
var errNoWorkspace = errors.New(noWorkspaceReason)
func stampLocalTS(ins pendingInsert, call func() tea.Msg) tea.Cmd
func stampEditID(e pendingEdit, call func() tea.Msg) tea.Cmd
func failNilDelete(channelID, ts string, call func() tea.Msg) tea.Cmd
func failNil(call func() tea.Msg, fail tea.Msg) tea.Cmd
```

The wrappers put on the result what the reducer needs to find the
pending operation (services never set it) and turn a nil result into
the operation's failure: the `cmd/slk` closures return nil when no
workspace is active, which used to leave a pending send open for good.
`failNil` does the same for the other actions the user starts (mark
unread, older history, fetch-around, join, upload), so their in-flight
flags, spinners and upload state clear and a toast says why. The join
and older-history failures toast too (`ChannelJoinFailedMsg`,
`OlderMessagesLoadedMsg.Err`, which `cmd/slk` sets when the fetch
fails, so an empty page at the start of a channel stays silent); until
phase 6 they only logged or cleared the spinner. `Send` and
`SendReply` in `cmd/slk` also time out after 30s (`sendTimeout`): a
send must always end, because echoes are held until it does. The
timeout works because `SlackAPI` declares only slack-go's `*Context`
methods (`TestSlackAPI_EveryMethodTakesContext`); the plain ones use
`context.Background()` and dropped every ctx slk passed. Every other
Web API call the UI waits on is bounded by `requestTimeout`.

The thread pane and the channel panes are kept apart in `rowSinks`
because the paths write them differently: a broadcast placeholder is a
`thread_broadcast` row only in the channel feed, and only channel
windows get their own clone of the settled message. `sinksFor` is every
window on the channel; `replySinksFor` adds the thread panel when it is
visible on that thread (matched on the panel's own channel, not
`activeChannelID`), and the channel windows only for a broadcast.
`shownSinks` is for a message that already exists: every window on the
channel, plus the thread panel whenever it is on that channel (no
visibility check), the fan-out the `message_changed` and
`message_deleted` echoes always used. The thread pane's `Patch` writes a
matching reply and the parent; its `Remove` never takes the parent.
`restoreTo` only puts a row back into a pane that still shows the
conversation it came from (a window on the channel, the thread panel on
that thread).

### 3. A ledger of pending operations (package `ui`)

`internal/ui/pending_ops.go`, App field `a.pendingOps`:

```go
type pendingInsert struct {
	localTS   string
	channelID string
	threadTS  string // "" for a top-level send
	broadcast bool
	postTeam  string // the workspace it posts to (active at Enter)
	teamID    string // where the text was typed
	typed     string // the composer text before mention translation
}
func (ins pendingInsert) origin() commandOrigin // for giveTextBack

// A conversation is a channel's top level (threadTS "") or one thread,
// in one workspace: a Slack Connect channel has the same ID in every
// workspace sharing it. A message's is conversationOf: a thread parent
// (ThreadTS == TS) is top level, a reply or thread_broadcast belongs to
// its parent's thread. NewMessageMsg.TeamID is the workspace whose
// WebSocket delivered it ("" means the active one).
type conversationKey struct{ teamID, channelID, threadTS string }
func conversationOf(teamID, channelID string, msg messages.MessageItem) conversationKey

type pendingOps struct { /* inserts, oldest first; held echoes per conversation; sentTSes; edits and deletes by (channel, ts); counters */ }
func (p *pendingOps) BeginInsert(ins pendingInsert)
func (p *pendingOps) HoldEcho(m NewMessageMsg) bool
func (p *pendingOps) EndInsert(localTS, sentTS string) (ins pendingInsert, release []NewMessageMsg, ok bool)
func (p *pendingOps) PatchHeld(channelID, ts string, f func(*messages.MessageItem))
func (p *pendingOps) DropHeld(channelID, ts string)
func (p *pendingOps) RecordSent(ts string)
func (p *pendingOps) IsSelfSent(ts string) bool
func (p *pendingOps) NextLocalTS() string

type pendingEdit struct {
	id               uint64 // from BeginEdit, increasing
	channelID, ts    string
	wire             string        // the text chat.update sends
	body             messages.Body // what it shows: editedBody(wire)
	panel            Panel         // where it was typed, to reopen it
	threadTS, teamID string
	typed            string // the composer text before mention translation
}
func (p *pendingOps) BeginEdit(e pendingEdit, prior messages.MessageItem, priorOK bool) (_ pendingEdit, send bool)
func (p *pendingOps) EditEcho(channelID, ts string, body messages.Body) bool
func (p *pendingOps) EndEdit(id uint64, ok bool) (editSettled, bool) // show, next, failed, superseded, gone
func (p *pendingOps) EditPending(channelID, ts string) bool
func (p *pendingOps) EditGone(channelID, ts string)

type pendingDelete struct {
	channelID, ts string
	removed       []removedRow // per pane: the row, whether selected, where the cursor was left
	echoed        bool         // message_deleted arrived first
}
func (p *pendingOps) BeginDelete(d pendingDelete)
func (p *pendingOps) EndDelete(channelID, ts string) (pendingDelete, bool)
func (p *pendingOps) DeleteEchoed(channelID, ts string)
func (p *pendingOps) DeletePending(channelID, ts string) bool
func (p *pendingOps) PatchRemoved(channelID, ts string, f func(*messages.MessageItem)) (messages.MessageItem, bool)

func (a *App) sendingGuard(ts string) (tea.Cmd, bool)
func (a *App) sendingSelectionGuard() (tea.Cmd, bool)
func (a *App) releaseHeld(held []NewMessageMsg) tea.Cmd // reducer_send.go
func (a *App) giveTextBack(o commandOrigin) (copyCmd tea.Cmd, inComposer, onClipboard bool)
```

The flow for a send or reply:

- **Begin** (`reduceSendMessage`, `SendThreadReplyMsg`): mint the id,
  `BeginInsert` (always, even when no pane shows the placeholder),
  `InsertLocal` into the sinks, and wrap the service call in
  `stampLocalTS`. `SendMessageMsg` / `SendThreadReplyMsg` carry `TeamID`
  and `Typed`, set by the composer's Enter paths.
- **Echo** (`reduceNewMessage`, after the edit-echo branch and the
  `IsSelfSent` drop): nothing in an echo says which send it belongs to.
  The user may have two sends in flight in one conversation (their
  results and echoes arrive in any order), or post to the same
  conversation from another client, by a forward or by an upload. So no
  echo is ever matched to a send. While any send is pending in a
  conversation, a non-ephemeral message from the current user in that
  conversation is held (`HoldEcho`) and shown nowhere. Debug log
  decision: `held_pending_send`. An edit of a held message patches it
  (`PatchHeld`) and a delete drops it (`DropHeld`).
- **Success**: `EndInsert(localTS, realTS)` records the real TS, then
  the placeholder is settled with Slack's response in the sinks, with
  reply count and threads-dirty for replies. A success with an empty
  TS removes the placeholder.
- **Failure**: `EndInsert(localTS, "")`, remove the placeholder, give
  the text back (`giveTextBack`, below), and show the failure toast.
- **Release**: `EndInsert` drops the held messages whose TS a result
  recorded (`dropped_held_self_send`). Once no send is pending in the
  conversation, it returns the rest in TS order, and the result's arm
  shows each through `showNewMessage` (`releaseHeld`,
  `released_held_echo`; a message held in a workspace the user has
  since left is dropped, `dropped_held_other_team`, and that
  workspace's next fetch brings it), so reply counts, read state and threads-dirty
  happen as for any message. A released reply that a channel refetch
  already counted is not counted again: `IncrementReplyCount` skips a
  reply at or before the parent's `LatestReply` (Slack's latest_reply
  when the parent was loaded), and records nothing when the parent is
  not shown. A send that failed after Slack accepted it
  (a timeout) then shows once, from its echo, and its text is also
  given back: nothing proves which send the echo belongs to.

A rapid test (`TestHoldAndReleaseShowsEachMessageOnce`) drives the
ledger through random interleavings of sends, echoes, results that
succeed or fail, and messages from other clients in two conversations.
It checks that each message shows exactly once (a successful send by
its result, never its echo) and that nothing is held or pending once
every send has ended.

Giving text back (`giveTextBack`): the typed text goes back to the
composer it came from (`offerTypedBack`; `OfferDraft` never overwrites a
newer draft). When it cannot, it joins the session's unsent texts
(`keepUnsent`, `unsent.go`) and the clipboard gets all of them, oldest
first, separated by a blank line, so a later failure never replaces an
earlier one. The failure toast adds "Your text is on the clipboard.",
or "N unsent messages are on the clipboard." (`OnClipboard` on
`statusbar.SendFailedMsg` / `EditFailedMsg`, `unsentTexts.note`). A
text leaves the list when it goes back to a composer or is sent or
edited in again, word for word, successfully. The list keeps the 20
newest (`unsentLimit`); once any were dropped, the toast says how
many. A failed broadcast reply given back to the thread
composer on screen turns its "also send to channel" toggle back on. A
draft stored for a thread not on screen cannot keep it, because opening
a thread clears the toggle.

What an edit shows is a whole body, `messages.Body` (text, blocks and
the edited mark). The pane draws a message's body from its rich_text
block when there is one (`MessageTextSource`), and slk and the web client
attach one to every message typed, so patching `Text` alone shows
nothing. `messages.TypedBody(wire)` is what Slack stores for typed
text, sent by chat.postMessage or chat.update: `blockkit.FromTyped`
runs `mrkdwn.Convert` as the client does and parses its rich_text
block. An edit shows it marked edited (`editedBody`). A send's
placeholder is drawn from it (`pendingInsert.body`, `localRow`), and
the HTTP result, which cmd/slk builds from the mrkdwn alone, settles
with its blocks (`sentItem`), so a sent message looks the same from
Enter through its echo and later loads. An echo's body is Slack's,
copied as it is.
Every body change goes through `showBody`: the panes, a held echo, and
the rows a pending delete holds.

The flow for an edit:

- **Begin** (`reduceEditMessage`): `submitEdit` closes the editor at
  Enter (restoring the stashed draft, as a send leaves insert mode) and
  puts `Typed`, `TeamID`, `Panel` and `ThreadTS` on `EditMessageMsg`.
  The edit's body is shown (`showBody`) and `BeginEdit` records it.
- **One in flight**: per message, at most one chat.update is in flight.
  `BeginEdit` sends the edit at once when none is; otherwise it waits,
  replacing an older waiting edit, which is never sent (the newest text
  wins), and `EndEdit` hands it back to be sent when the one in flight
  ends (`sendEdit`). Slack therefore applies the edits in the order they
  were made. A waiting edit is refused, as a failure, if the user has
  switched workspace by the time it would go.
- **State**: per message, `server`, Slack's state as best known: the
  body before the first edit, then each succeeded edit's body and each
  echo's. `known` is false while no pane has shown the message.
- **Echo**: a `message_changed` reaches the App as a `NewMessageMsg`
  with `Changed` set; its `IsEdited` comes from Slack's `edited` field
  (decoded in `internal/slack/events.go`), so a change that is not an
  edit (an unfurl, a parent's reply count, an app updating its message)
  is not marked "(edited)". While an edit is in flight, it is recorded
  (`EditEcho`: `server` becomes the echo; debug log
  `recorded_edit_echo_pending`), not shown, so an older state cannot
  flicker over a newer edit. An echo that carries the in-flight edit's
  text in Slack's form (`messages.EchoCarries`) proves Slack stored that
  edit (`echoed`), and the message then settles on the echo's body,
  Slack's own form. With nothing in flight the echo is shown as it is.
- **Result** (`reduceMessageEdited`): `EndEdit` settles the edit in
  flight. If it was echoed, `server` (the echo, or a newer one) stands,
  and a failure is not a failure: the HTTP call failed after Slack
  stored it, so there is no toast and no reopen. Otherwise success sets
  `server` to the edit's body. When an edit is waiting, it is sent and
  shown; a failure of the one that ended only toasts. When none is
  waiting, the message shows `server` (nothing when unknown) and the
  ledger forgets it. A real failure is reopened by `reopenEdit` (same
  workspace, normal mode, no other edit open, the same panel focused and
  still showing the message) with the typed text in its composer;
  otherwise the text goes back through `giveTextBack`. When the message
  is deleted (its echo, a successful delete, or Slack's
  message_not_found, `MessageEditedMsg.NotFound`) or its delete is
  pending, the text goes to the clipboard instead: a draft would post it
  as a new message. message_not_found also takes the message out of
  every pane (`applyMessageDeleted`), as its echo would. A waiting edit
  whose message is gone is never sent and its text is the one given
  back. The result never closes the
  editor: any editor open by then is a newer edit.

The proof rule is "an echo carrying the edit's text", not "any echo
after the edit was sent": another client's change could arrive in that
window and would then swallow a real failure, losing the user's text.
The text is compared in the form Slack stores it (`EchoCarries`):
Slack escapes &, < and > in the mrkdwn as entities (so every URL with
a query string differs from what was sent) but keeps the rich_text
block as sent, so the echo's mrkdwn is compared with those escapes
undone once, or the two blocks' text when both have one. The rule can
only err the other way (Slack stored a form of the text neither
comparison recognises), which reopens an edit Slack already has. One case
stays ambiguous: an edit succeeds, its echo is lost (a reconnect), and
a stale echo of an older change by another client arrives after the
result. The message then shows that echo until the next change.

The flow for a delete:

- **Begin** (`DeleteMessageMsg`, after the confirm prompt): `removeFrom`
  takes the row out of `shownSinks`, and `BeginDelete` keeps what each
  pane removed, with where it left the cursor.
- **Meanwhile**: `showBody` writes edits and echoes of the message into
  the kept rows (`PatchRemoved`), so a restore brings back what the
  message is now.
- **Echo**: `WSMessageDeletedMsg` marks the pending delete echoed and
  runs `applyMessageDeleted` (remove by TS, mark an edit in flight
  gone, cancel an open edit of it, close the thread panel of a deleted
  parent, drop its action prompt). A row already gone is a no-op with
  no selection change.
- **Success**: `applyMessageDeleted`, so a deleted parent's panel
  closes even when the echo is late. Slack's message_not_found
  (`MessageDeletedMsg.NotFound`, from `isMessageNotFound` in `cmd/slk`)
  is a success: the message is gone, so a second delete of a message
  already deleted reports nothing.
- **Failure**: unless echoed (the delete went through), `restoreTo` puts
  the rows back in TS order (`Insert`), reselected only if the
  cursor is still where the delete left it, and the toast shows. After
  the echo nothing is restored or reported.

A rapid stateful test (`TestEditsAndDeletesEndOnSlacksState`,
`pending_edit_prop_test.go`) models Slack as the source of truth,
applying each chat.update when it receives it, and interleaves edits,
their results (success, failure, failure after Slack stored it), echoes
(some lost, for edits Slack accepted), other clients' edits, deletes
and their results (including message_not_found) and older history.
It checks that one chat.update per message is in flight and Slack gets
them in the order typed, that rows stay in TS order, that a failed
edit's text Slack lacks is given back, and that once everything has
settled every pane shows exactly Slack's body, or nothing for a
deleted message.

Kept pessimistic: the thread panel of a deleted parent closes only once
Slack confirms, since a closed panel cannot be put back as it was; and
parent reply counts do not change on a reply's delete, neither
optimistically nor on the echo, as before.

### Reloads (`pending_view.go`)

A pane's rows are replaced wholesale by a history fetch, a cache read
on entering a channel or opening a thread, and a fetch around a
message; older history is prepended. Those rows come from Slack or its
cache and know nothing of the operations still pending, so every such
replacement goes through `paneRows` (`olderRows` for a prepended page,
`threadParent` for a thread's parent row), which puts them back on top
(`pendingOps.view`):

- a message with an edit in flight or waiting shows its newest edit's
  body; the fetched body becomes `server` if nothing was known;
- a message with a delete pending stays out, and the pending delete
  keeps the fetched row for that pane (`keepDeleted`), so a failed
  delete restores Slack's state as the fetch had it, in that pane too;
- a message held already stays hidden, and so does one of the user's
  own messages in a conversation with a send pending, unless a result
  recorded it, when it is stamped no earlier than a minute
  (`sendHoldWindow`, for clock skew) before the oldest send pending
  there (`holdFetched`). Slack may post a send before answering it, so
  a fetch can bring the send's own message while its placeholder shows;
  like an echo it cannot say which send it is. It is held as an echo
  is, so the result settles the placeholder onto it and drops it, or
  the release shows it: once either way. Showing it at once would show
  the message twice until the result. Older messages of the user's
  cannot be a pending send's and show at once. `HoldEcho` keeps one
  entry per TS, so a fetched message whose echo follows is held once;
- the ephemerals the pane showed for the same conversation stay
  (`keepEphemerals`);
- each pending send's placeholder (`pendingInsert.row`, the row
  `localRow` built at Enter) comes after every row Slack knows, in the
  order sent; a broadcast reply's shows in the channel feed as a
  `thread_broadcast`.

`TestPaneRowsGoThroughPaneRows` fails on any `SetMessages`,
`PrependMessages` or `SetThread` in package `ui` whose rows are neither
nil (a clear) nor from `paneRows`/`olderRows`; `splitWindow` copies a
pane's rows and is exempt. Both rapid tests reload the panes at random
points: `TestEditsAndDeletesEndOnSlacksState` also checks after every
step that a message being deleted is not shown and one with an edit in
flight shows the newest edit, and `TestHoldAndReleaseShowsEachMessageOnce`
checks that a reload shows every pending placeholder, no message twice,
no held message and no pending send's own message beside its
placeholder.

A placeholder's TS (`messages.IsLocalTS`) is slk's own until the post
returns. It must never reach Slack, the cache, a mark, the navigation
history or a link. The guards:

- Keys: `handleNormalMode` runs `sendingSelectionGuard` ("Still
  sending…") for every `isMessageOpKey` key plus edit and delete.
- Choke points that refuse whatever reaches them: `openReactionPicker`,
  `toggleReactionOnMessageItem` (keys, reaction navigation, pill
  clicks), the reaction picker's result, `copyPermalinkOfSelected`,
  `beginForwardOfSelected`, `saveThreadToFile` (while any reply is
  sending), `setMark`, `openThreadForSelectedMessage` (Enter and
  click), and the `EditMessageMsg`, `DeleteMessageMsg` and
  `MarkUnreadMsg` reducer arms. A pane's append ends reaction
  navigation, which runs before the key guard. The `EditMessageMsg`
  refusal gives the text back (`giveTextBack`) and reports it as a
  failed edit, "the message is still sending", with the clipboard note
  when the text went there.
- Read cursors: `recordChannelMark` and `recordThreadMark` ignore a
  placeholder, and never stage a mark at or behind the cursor Slack
  already holds (`readFloors`, `read_floor.go`): a held message
  released after newer ones were marked would otherwise move Slack's
  cursor back. The floor rises with every mark slk issues (flush,
  channel entry, thread open, the fetcher's `MarkedTS`), the loaded
  `LastReadTS` and slk's own mark echoes; a mark from another client
  sets it either way; the user's mark-unread (`markedUnread`) moves it
  back to the boundary and drops a staged mark of that conversation. The thread mark (`newestMarkableReplyTS`) and the
  mark-unread boundary take the nearest message Slack knows
  (`KnownToSlack`: no placeholder, no ephemeral).
- Navigation: `currentLocation` and `channelPaneLocation` name a
  placeholder or an ephemeral by the message before it in the same pane
  that Slack knows (`KnownToSlack`), else the thread parent or the
  channel alone (`sentLocation`). `setMark` reads
  the raw selection (`selectedLocation`) so it refuses instead.
- `OldestTS`, the anchor for older history, skips placeholders.

No other path reads a pane's TS into a request. Thread fetch, reply and
mark use the thread panel's parent, which only opens on a real message;
uploads and typing name the conversation, not a message.

### 4. Paths moved onto it

| Path | Begin | Success | Failure |
|---|---|---|---|
| Channel send (done) | `InsertLocal` | `Settle` | `RemoveLocal`, offer text back |
| Thread reply, +broadcast (done) | `InsertLocal` on the thread, and the channel when broadcast | `Settle`, reply count | `RemoveLocal`, offer text back |
| Edit (done) | show the edited body; one chat.update in flight per message | show Slack's state (an echo, else the edit) | show Slack's state, reopen edit with the typed text, else offer it back (clipboard if the message is gone) |
| Delete (done) | remove, keep rows (kept current by later edits) | apply as the echo does | restore in TS order, reselect if the cursor has not moved |

Reactions keep their own mechanism for now, with the duplicated closure
merged into one function. Activity read, thread mark-read and presence
are out of scope.

## Phasing

1. **Move (no behaviour change). Done:** 96059ab1, 13b6cef7, a5299540.
2. **Fixes, one commit each with its test. Done:** `Settle` drops a
   duplicate (bug 1); echo adoption replaces the window (bugs 2, 3;
   phase 4 replaced adoption);
   empty-TS cleanup (4); text offered back on a failed send or reply
   (5, sends only); guard against acting on a placeholder (6).
3. **Features. Done:** a nil service result fails the operation;
   optimistic edit; optimistic delete. The rowSink, pendingOps and flow
   sections above describe them.
4. **Review fixes. Done:** adoption, which matched an echo to the
   oldest pending send and could pick the wrong one, replaced by
   holding the user's own echoes until the conversation's sends end;
   placeholder TSes kept out of navigation, read cursors, mark-unread
   and every request; failed text copied to the clipboard when its
   composer is taken, and a broadcast's toggle restored; a settled
   placeholder hands the cursor to its message; nil results of the
   other user actions become failures.

5. **Edit and delete fixes. Done:** an edit replaces the whole body,
   so it shows on messages drawn from a rich_text block (it never did
   on real messages); one chat.update per message in flight, so Slack
   applies edits in order; echoes recorded during an edit decide the
   final state, and one carrying the edit's text turns a timeout into a
   success; a pending delete's rows follow edits and echoes, and a
   failed delete restores in TS order without taking a moved cursor;
   message_not_found ends a delete, and a failed edit of a deleted
   message goes to the clipboard, never a draft.

6. **Ordering, read cursors and timeouts. Done:** every ctx reaches
   slack-go (`SlackAPI` declares only `*Context` methods), so
   `sendTimeout` ends a hung send and `requestTimeout` bounds the other
   calls the UI waits on; rows Slack knows are TS-ordered in both panes
   (`messages.Insert`, `Settle` moves the sent message); automatic read
   marks never move Slack's cursor backward (`readFloors`); every
   unsent text stays on the clipboard (`unsentTexts`); a reply already
   in a refetched `reply_count` is not counted again (`LatestReply`);
   join and older-history failures toast; held echoes are keyed by
   workspace; the placeholder edit refusal reports the clipboard;
   ephemerals stay out of navigation; a sent message is drawn from its
   typed body throughout.

## Out of scope

- Reaction toggle serialisation (the activity path's
  `want`/`confirmed`/`inFlight` scheme would fit, but it is a separate
  change), and not rolling back on `already_reacted`/`no_reaction`.
- Presence rollback.
- A deleted reply does not lower its parent's reply count, optimistically
  or on the echo.

