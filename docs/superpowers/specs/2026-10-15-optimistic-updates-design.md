# Optimistic updates: one shared mechanism

Status: phases 1 and 2 done, 2026-10-15; phase 3 (optimistic edit and
delete) not started

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
pane that displays that row, settled by the HTTP result or adopted by
the echo". Non-row state (presence, activity read, thread marks) stays
as it is; its shape differs and the activity path is already sound.

### 1. Row operations on `[]MessageItem` (package `messages`)

Pure functions in `internal/ui/messages/rows.go`, each returning a
`RowChange{Op, Index}` (`RowNone`, `RowAdded`, `RowRemoved`,
`RowReplaced`) so the pane can fix its own selection and caches:

```go
func IndexOfTS(rows []MessageItem, ts string) int      // from the back
func FirstIndexOfTS(rows []MessageItem, ts string) int // from the front
func ReplaceTS(rows []MessageItem, ts string, item MessageItem) ([]MessageItem, RowChange)
func RemoveTS(rows []MessageItem, ts string) ([]MessageItem, RowChange)
func RemoveAt(rows []MessageItem, i int) ([]MessageItem, RowChange)
func PatchTS(rows []MessageItem, ts string, f func(*MessageItem)) ([]MessageItem, RowChange)
func AppendIfAbsent(rows []MessageItem, item MessageItem) ([]MessageItem, RowChange)
func Upsert(rows []MessageItem, item MessageItem) ([]MessageItem, RowChange)
// Settle replaces the placeholder localTS with item (RowReplaced at
// its index). When another row already has item.TS (the echo got
// there first), that row takes item and the placeholder is removed
// (RowRemoved at its old index). No placeholder: RowNone.
func Settle(rows []MessageItem, localTS string, item MessageItem) ([]MessageItem, RowChange)
```

Matching is literal: an empty TS matches a row with an empty TS. The
send paths (append, settle, roll back, upsert) and the delete path write
through these; the edit, reaction and reply-count paths only locate their
row with `FirstIndexOfTS`. `PatchTS` has no caller yet; it is for the
optimistic edit.

Property tests are in `rows_prop_test.go`: `Settle` never yields
duplicate TSes, reports its op, and leaves every other row alone;
`RemoveTS` then re-inserting at the reported index restores the slice.

The selection fix-ups are not unified yet. Each pane's
`afterLocalRemoved` (used by `RemoveLocal` and by a `Settle` that
removes) steps back on `i < selected`; `RemoveMessageByTS` steps back on
`i <= selected && selected > 0`. Phase 3's delete has to pick one.

### 2. One row-writing interface for both panes

```go
// internal/ui/rowsink.go
type rowSink interface {
	InsertLocal(item messages.MessageItem)            // append unless shown
	Settle(localTS string, item messages.MessageItem) // messages.Settle, else upsert
	RemoveLocal(localTS string) bool
}
var _ rowSink = (*messages.Model)(nil)
var _ rowSink = (*thread.Model)(nil)

type rowSinks struct {
	thread  rowSink   // the thread panel, when it shows the row's thread
	channel []rowSink // every window showing the row's channel
}
func (s rowSinks) settle(localTS string, item messages.MessageItem) // clones per channel window
func (s rowSinks) removeLocal(localTS string)

func (a *App) sinksFor(channelID string) rowSinks
func (a *App) replySinksFor(channelID, threadTS string, broadcast bool) rowSinks
func (a *App) insertSinks(ins pendingInsert) rowSinks // one of the two, for a pending send
func (a *App) localRow(localTS, text, threadTS, subtype string) messages.MessageItem
func stampLocalTS(localTS string, broadcast bool, call func() tea.Msg) tea.Cmd
```

The thread pane and the channel panes are kept apart in `rowSinks`
because the paths write them differently: a broadcast placeholder is a
`thread_broadcast` row only in the channel feed, and only channel
windows get their own clone of the settled message. `sinksFor` is every
window on the channel; `replySinksFor` adds the thread panel when it is
visible on that thread (matched on the panel's own channel, not
`activeChannelID`), and the channel windows only for a broadcast.

### 3. A ledger of pending operations (package `ui`)

`internal/ui/pending_ops.go`, App field `a.pendingOps`:

```go
type pendingInsert struct {
	localTS   string
	channelID string
	threadTS  string // "" for a top-level send
	broadcast bool
	teamID    string // where the text was typed
	typed     string // the composer text before mention translation
	adopted   bool   // the echo has settled the placeholder
}
func (ins pendingInsert) origin() commandOrigin // for offerTypedBack

type pendingOps struct { /* inserts []*pendingInsert, oldest first; sentTSes; counter */ }
func (p *pendingOps) BeginInsert(ins pendingInsert)
func (p *pendingOps) AdoptEcho(channelID string, msg messages.MessageItem) (pendingInsert, bool)
func (p *pendingOps) EndInsert(localTS string) (pendingInsert, bool)
func (p *pendingOps) RecordSent(ts string)
func (p *pendingOps) IsSelfSent(ts string) bool
func (p *pendingOps) NextLocalTS() string

func isLocalTS(ts string) bool
func (a *App) sendingGuard(ts string) (tea.Cmd, bool)
func (a *App) sendingSelectionGuard() (tea.Cmd, bool)
```

The flow for a send or reply:

- **Begin** (`reduceSendMessage`, `SendThreadReplyMsg`): mint the id,
  `BeginInsert` (always, even when no pane shows the placeholder),
  `InsertLocal` into the sinks, and wrap the service call in
  `stampLocalTS`. `SendMessageMsg` / `SendThreadReplyMsg` carry `TeamID`
  and `Typed`, set by the composer's Enter paths.
- **Echo** (`reduceNewMessage`, after the edit-echo branch and the
  `IsSelfSent` drop): a non-ephemeral message from the current user with
  no file attachments, in the same channel and thread as a pending,
  unadopted insert, is adopted by the oldest such insert. It settles
  that insert's placeholder in `insertSinks(ins)`, is recorded with
  `RecordSent`, and returns without the normal echo side effects.
  Anything else (slk's own uploads, other clients, other threads)
  renders normally. Debug log decision: `adopted_pending_send`.
- **Success**: `EndInsert`, `RecordSent`, settle in the sinks (an
  adopted row is upserted with Slack's response), reply count and
  threads-dirty for replies. A success with an empty TS removes the
  placeholder unless the echo already adopted it.
- **Failure**: `EndInsert`. If adopted, the post went through: keep the
  row, no toast. Otherwise remove the placeholder, offer `typed` back
  through `offerTypedBack(ins.origin())` (`OfferDraft` never overwrites
  a newer draft), and show the failure toast.

An echo matched by mistake is possible: with a compose send pending in a
channel, the echo of a forward the user sends there at the same moment
is adopted by the send. The rows still end right (the send's own result
appends it, the forward's result dedups), but a failure of that send in
the window would not give its text back.

Every action that targets the selected message refuses a placeholder
with "Still sending…": `handleNormalMode` runs `sendingSelectionGuard`
for every `isMessageOpKey` key plus edit and delete, and `setMark` and
`openThreadForSelectedMessage` (Enter and click) call `sendingGuard`.

### 4. Paths moved onto it

| Path | Begin | Success | Failure |
|---|---|---|---|
| Channel send (done) | `InsertLocal` | `Settle` | `RemoveLocal`, offer text back |
| Thread reply, +broadcast (done) | `InsertLocal` on the thread, and the channel when broadcast | `Settle`, reply count | `RemoveLocal`, offer text back |
| **Edit** (phase 3) | patch text, mark edited | patch with Slack's converted text | restore prior, reopen edit with the typed text |
| **Delete** (phase 3) | remove, keep row and index | nothing | restore at index |

Reactions keep their own mechanism for now, with the duplicated closure
merged into one function. Activity read, thread mark-read and presence
are out of scope.

## Phasing

1. **Move (no behaviour change). Done:** 96059ab1, 13b6cef7, a5299540.
2. **Fixes, one commit each with its test. Done:** `Settle` drops a
   duplicate (bug 1); echo adoption replaces the window (bugs 2, 3);
   empty-TS cleanup (4); text offered back on a failed send or reply
   (5, sends only); guard against acting on a placeholder (6).
3. **Features, not started:** optimistic edit, then optimistic delete.
   What they need from the code as it stands:
   - `rowSink` gains `Patch(ts, f) (prior MessageItem, ok bool)`,
     `Remove(ts) (removed MessageItem, index int, ok bool)` and
     `Restore(item, index)`. The panes implement them over `PatchTS`
     and `RemoveTS`/`slices.Insert`. Edits and deletes today locate rows
     with `FirstIndexOfTS` while `PatchTS`/`RemoveTS` scan from the back;
     with `Settle` no longer duplicating, pick one direction. Delete
     also has to settle which selection fix-up wins (see §1).
   - The thread panel's parent row is not in its replies: an edit of
     the open thread's parent also patches the parent
     (`UpdateParentInPlace` today), and a delete of it closes the panel
     (`WSMessageDeletedMsg` today).
   - The sinks for an existing message are every channel window plus
     the thread panel when it shows that channel (the
     `WSMessageDeletedMsg` / edit-echo fan-out), not
     `sinksFor`/`replySinksFor`.
   - `pendingOps` gains entries for edits and deletes, keyed by the real
     TS, holding the prior row (and index for delete) and, for an edit,
     the typed text. `EditMessageMsg` / `MessageEditedMsg` and
     `DeleteMessageMsg` / `MessageDeletedMsg` begin and settle them; a
     failed edit restores the prior row and reopens the edit with the
     typed text (bug 5's edit half, still open: a failed edit exits
     edit mode and discards the text). The edit and delete echoes
     (`message_changed`, `WSMessageDeletedMsg`) arrive for the user's
     own changes and must be idempotent against the optimistic state.

## Out of scope

- Reaction toggle serialisation (the activity path's
  `want`/`confirmed`/`inFlight` scheme would fit, but it is a separate
  change), and not rolling back on `already_reacted`/`no_reaction`.
- Presence rollback.
- The service closures in `cmd/slk` that return `nil` without an active
  workspace (no result reaches the UI); worth fixing, but on the
  `cmd/slk` side.
