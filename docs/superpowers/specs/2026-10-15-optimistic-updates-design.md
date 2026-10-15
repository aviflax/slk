# Optimistic updates: one shared mechanism

Status: proposed, 2026-10-15

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

Pure functions that both panes call, each returning what changed so the
pane can fix its own selection and caches:

```go
// rows.go
func IndexOfTS(rows []MessageItem, ts string) int
func ReplaceTS(rows []MessageItem, ts string, item MessageItem) ([]MessageItem, RowChange)
func RemoveTS(rows []MessageItem, ts string) ([]MessageItem, RowChange)
func PatchTS(rows []MessageItem, ts string, f func(*MessageItem)) ([]MessageItem, RowChange)
// Settle replaces the placeholder localTS with item. When a row
// with item.TS is already present (the echo got there first), the
// placeholder is removed instead, so the row never appears twice.
func Settle(rows []MessageItem, localTS string, item MessageItem) ([]MessageItem, RowChange)
```

`RowChange` carries the affected index and whether a row was added,
removed or replaced. Selection adjustment for a removal becomes one
function both panes use, which settles the `i < selected` vs
`i <= selected && selected > 0` disagreement in one place. These get
property tests (`rows_prop_test.go`): `Settle` never yields duplicate
TSes; `RemoveTS` then re-inserting the removed row at its index
restores the slice.

### 2. One row-writing interface for both panes

```go
// internal/ui/rowsink.go
type rowSink interface {
	InsertLocal(item messages.MessageItem)
	Settle(localTS string, item messages.MessageItem) bool
	Remove(ts string) (removed messages.MessageItem, index int, ok bool)
	Restore(item messages.MessageItem, index int)
	Patch(ts string, f func(*messages.MessageItem)) (prior messages.MessageItem, ok bool)
}
var _ rowSink = (*messages.Model)(nil)
var _ rowSink = (*thread.Model)(nil)
```

Each pane implements it as a thin wrapper over the functions in §1 plus
its own cache and selection fix-up. The `*Reply`-suffixed and `*SelfSent`
methods are folded into it.

`(*App).sinksFor(channelID, threadTS string) []rowSink` replaces both
`modelsForChannel` loops in these paths and every inline
thread-visibility check. A top-level row targets the channel panes; a
reply targets the thread pane when it shows that thread, and the
channel panes as well when it is a broadcast; a thread parent targets
both.

### 3. A ledger of pending operations (package `ui`)

`selfSendDedup` becomes `pendingOps`, which records each optimistic
operation until it settles:

```go
type pendingOp struct {
	id        string // "local:N" for inserts; the real TS for edits/deletes
	kind      opKind // opInsert, opEdit, opDelete
	channelID string
	threadTS  string
	prior     messages.MessageItem // for edit/delete rollback
	index     int                  // for delete rollback
	typed     string               // the user's text, offered back on failure
}
```

The App-level flow is three functions:

- `beginOp(op, apply)` records the op, applies it to `sinksFor(...)`
  and returns the `tea.Cmd` wrapper that stamps the op ID on the result
  (replacing the two hand-written type switches).
- `settleOp(id, item)` on success: `Settle` for inserts, `Patch` with
  the authoritative text for edits, nothing for deletes. It records
  the real TS so a late echo is recognised.
- `failOp(id, err)`: removes the placeholder, restores the prior row
  or re-inserts the deleted one, offers `typed` back to the composer it
  came from (`OfferDraft`, as slash commands already do), and shows the
  failure toast.

**Echo adoption replaces the 3s window.** When a self-authored echo
arrives for a channel/thread with a pending insert, it settles the
oldest pending insert there instead of being dropped; the later HTTP
result then finds the real TS present and `Settle` removes nothing
twice. Echoes that match no pending op (uploads, other clients) render
normally. This removes bugs 1 to 3 and the edit silencing. The flicker the
window was added to prevent (echo text normalised differently from the
optimistic text) is the same swap the HTTP result already performs, so
it does not get worse. An echo with files is never adopted by a text
insert.

### 4. Paths moved onto it

| Path | Begin | Success | Failure |
|---|---|---|---|
| Channel send | `InsertLocal` | `Settle` | remove, offer text back |
| Thread reply (+broadcast) | `InsertLocal` on the thread, and the channel when broadcast | `Settle`, reply count | remove, offer text back |
| **Edit** (new) | `Patch` text, mark edited | `Patch` with Slack's converted text | restore prior, reopen edit with the typed text |
| **Delete** (new) | `Remove`, keep row and index | nothing | `Restore` |

Reactions keep their own mechanism for now, with the duplicated closure
merged into one function. Activity read, thread mark-read and presence
are out of scope.

Every action that targets the selected message refuses a `local:` TS
with a toast ("Still sending…").

## Phasing

Per AGENTS.md, a refactor commit must not change behaviour, so:

1. **Move (no behaviour change).** §1 row functions with property
   tests; both panes' existing methods rewritten over them (keeping
   today's selection fix-ups exactly, the disagreement recorded in a
   comment); `rowSink` and `sinksFor`; the placeholder builder and the
   result wrapper. Existing tests pass unchanged.
2. **Fixes, one commit each with its test:** `Settle` drops a duplicate
   (bug 1); echo adoption replaces the window (bugs 2, 3); empty-TS
   cleanup (4); text offered back on failure (5); guard against acting
   on a placeholder (6).
3. **Features:** optimistic edit, then optimistic delete.

## Out of scope

- Reaction toggle serialisation (the activity path's
  `want`/`confirmed`/`inFlight` scheme would fit, but it is a separate
  change), and not rolling back on `already_reacted`/`no_reaction`.
- Presence rollback.
- The service closures in `cmd/slk` that return `nil` without an active
  workspace (no result reaches the UI); worth fixing, but on the
  `cmd/slk` side.
