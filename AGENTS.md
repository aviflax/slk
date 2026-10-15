# AGENTS.md

Orientation for anyone — human or agent — writing code in this repo.

**The single most important rule: search before you write.** The most common
defect in this codebase's history is not bugs, it is the same logic implemented
a fourth time because the author did not know the first three existed. See
[Shared code](#shared-code--check-here-before-writing-a-helper) below.

## Build, test, lint

```
go build ./...
go test ./...                 # ~10s
go test ./... -race           # ~47s; this is what CI runs
make fuzz                     # every Fuzz target, 30s each (FUZZTIME=2m)
go vet ./...
golangci-lint run             # v2.14.0, config in .golangci.yml
gofmt -l .                    # must be empty; enforced in CI
```

Those two timings are wall clock with `-count=1` and a warm build cache on an
8-core Linux box, re-measured at the end of Phase 0 (they were `~21s`/`~44s`
before it, on unrecorded hardware — treat them as a shape, not a target).
The shape is what matters: under `-race`, four packages are ~95% of the run —
`internal/ui` 32s, `cmd/slk` 29s, `internal/cache` 19s, `internal/ui/messages`
16s (they overlap, hence a 47s wall). If you are iterating, run the one package
you are changing; `internal/ui` alone is now longer under `-race` than the whole
suite used to be.

Tests are plain `testing.T`. No testify, no gomock, no golden libraries.
White-box (`package ui`, not `package ui_test`) by convention. The one
test dependency is `pgregory.net/rapid`, for property tests.

### Property tests and fuzzing

A function whose output depends only on its arguments gets a property
test, not only examples. Two tools, picked by input shape:

- **rapid** (`rapid.Check`) for structured inputs and stateful models.
  Draw from the shared generators in `internal/proptest` (styled terminal
  text, Slack wire text, IDs, timestamps) before writing a local one, and
  add a generator there when two packages need it. A failure prints the
  shrunk input; rerun it with the `-rapid.seed` it reports.
- **Native fuzzing** (`func FuzzX(f *testing.F)`) for parsers and string
  transforms that take arbitrary bytes. `go test` replays the seeds and
  every saved crasher under `testdata/fuzz/`; `make fuzz` (or
  `scripts/fuzz.sh 2m ./internal/text`) runs the fuzzer itself. Commit a
  crasher with its fix.

Property tests live in `*_prop_test.go` beside the example tests. State
the property in the test's comment. When a function is impure only
because it reads a theme global, the clock or model state, extract the
pure core and test that; the refactor rules below still apply (move
behaviour, never change it in the same commit). A property that finds a
bug lands with the fix in its own commit.

## Architecture in one screen

```
cmd/slk/                composition root: wiring, workspace connection,
                        WebSocket event handling, message fetch/cache pipeline
internal/core/          the ports (service interfaces) the TUI calls, and the
                        values the TUI and cmd/slk exchange through them
internal/ui/            bubbletea App: reducers, mode key handlers, view regions
internal/ui/<widget>/   self-contained sub-models (messages, thread, sidebar,
                        compose, and 14 modal packages)
internal/bubbles/       self-contained components and the substrate they
                        share (RFC #236), e.g. ansi, scrollbar, selection, and
                        chrome, the substrate every dialog draws through. Never
                        imports internal/ui; the TUI's I/O ban applies here too.
                        Widgets move here from internal/ui/<widget>/ one at
                        a time; confirmprompt is the first
internal/slack/         Slack Web API + browser-protocol WebSocket client
internal/slack/edge/    edgeapi: conditional revalidation, server-side search
internal/bootstrap/     startup fetch orchestration
internal/cache/         SQLite cache (a cache, not a source of truth)
internal/config/        TOML config
internal/demo/          fake workspaces + services behind the hidden
                        `slk --demo` flag; for recording README GIFs only
```

**`wiki/Architecture.md` is stale by roughly 7× and describes a service layer
that no longer exists. Do not trust it.** Current structural documentation:

- `docs/superpowers/plans/2026-09-06-architecture-refactor.md` — the active
  refactor: measured baseline, known problems, phase sequence
- `docs/superpowers/plans/2026-05-23-app-go-solid-refactor.md` — the completed
  `app.go` decomposition; establishes the patterns still in use
- `docs/superpowers/specs/` — one design doc per feature

### Invariants worth knowing

- **`internal/ui` does no I/O of its own.** Slack, SQLite, the filesystem, the
  clipboard, the external editor and launching apps all go through the service
  ports in `internal/core`, which `cmd/slk` wires. No `internal/slack`,
  `slackhttp`, `cache`, `config`, `filedl`, `export`, `editor`, `net/http` or
  `os/exec`; `slack-go` only in `blockkit`, as the data it renders. `internal/ui/boundary_test.go`
  enforces this. The boundary is deliberate; do not breach it. The same
  rules cover `internal/bubbles`, with no `slack-go` exemption, and it may not
  import `internal/ui` or anything under it: components are the app's building
  blocks, not part of it (`TestBubblesReachTheAppOnlyThroughCore`, same file).
- **`App.Update` routes through a reducer chain**, not a switch. Add behavior by
  adding to a `reducer_*.go` file, not by extending `Update`. The only step
  outside the chain is the thin `Update` wrapper that records `stackFront`
  (which content pane — messages or thread — last had focus, for the stacked
  thread layout) after every message; do not add other post-chain logic
  there.
- **Per-mode key handling is a table**, `modeHandlers` in
  `internal/ui/mode_handlers.go`. One `mode_*.go` file per mode.
- **SQLite is a cache.** Slack remains authoritative.
- **A map shared across goroutines is never written.** Every map the UI holds
  is either built for it (a copy, like `userNameStore.SnapshotForUI()` or the
  per-message `ExternalUsers` set) or published once and never mutated
  again (`WorkspaceContext.CustomEmoji` / `UserGroups`, swapped whole via
  `atomic.Pointer`). A copy handed to the UI is the UI's from then on: the
  sender must not keep reading it (the UI writes it). The same goes for maps
  captured by a `tea.Cmd` (`saveThreadToFile` clones them). A plain map
  written while another goroutine reads it is a `fatal error: concurrent map
  read and map write`, which bubbletea cannot recover from; it crashed slk on
  2026-10-02 via the user-name map (see `cmd/slk/usernames.go`). Engine state
  several goroutines write is locked (`userNameStore`) instead.

## Shared code — check here before writing a helper

If you are about to write text wrapping, box drawing, list windowing,
scrollbars, date formatting, case folding, or ID formatting: it already exists.

### Text and rendering

| Need | Use |
|---|---|
| Word wrap to a width | `messages.WordWrap(s, limit)` |
| Plain-text line segmentation (grapheme-correct) | `entrylist.PlainLines`, `entrylist.DisplayWidth`, `entrylist.SliceColumns` (`bubbles/entrylist`; `messages.PlainLines`/`DisplayWidthOfPlain`/`SliceColumns` delegate for existing callers) |
| Display width of a string (emoji-aware) | `emoji.Width(s)` |
| Case/accent-insensitive fold for matching | `text.Fold(s)` |
| Slack mrkdwn → plain text | `messages.FlattenMrkdwn`, `messages.FlattenMrkdwnWithUserGroups` |
| Search-term highlighting (ANSI/OSC-safe) | `messages.HighlightSearchTerms`, `messages.SearchHighlightSGR` |
| Re-assert a background (or background+foreground) after every SGR reset, so nested lipgloss renders don't drop it | `bubbles/ansi.ReapplyAfterResets(text, style)` — the one implementation; `messages.ReapplyBgAfterResets` delegates to it for existing callers. The theme's sequences come from `messages.BgANSI()` / `messages.FgANSI()` |
| Extract links from message text | `messages.ExtractLinks` |
| Every link a message offers to `o` (text links + permalinks of messages it shares, from attachment `from_url`) | `messages.MessageLinks(msg)` |
| Does message text mention the current user? | `mention.InText(text, selfUserID)`; `mention.Classify` when a direct mention and a broadcast (`@here`/`@channel`/`@everyone`) must be told apart |
| Mark a row that mentions the current user (gutter bar, plus tint for a direct mention; the pill on their own `@name` comes from `RenderContext.SelfUserID`) | `RenderMessage` sets `RenderedMessage.Mention`, which `WrapRows` paints; a row drawn outside `WrapRows` (the thread parent) uses `WrapStyles.MentionVariant`. Colours: `styles.MentionBar`, `MentionBg`, `MentionPillBg` |
| Show an edit of a message: the text, the blocks the body is drawn from (a rich_text block wins over `Text`, see `MessageTextSource`) and the edited mark together | `messages.Body` (`BodyOf(msg)`, `Body.Apply(&msg)`); `messages.TypedBody(typed)` is what chat.postMessage and chat.update store for typed text, from `blockkit.FromTyped` (mrkdwn.Convert's text and rich_text block as `blockkit` blocks); an edit marks it edited (`editedBody`, `internal/ui/reducer_send.go`), and a send's placeholder and settled result are drawn from it (`localRow`, `sentItem`, `internal/ui/rowsink.go`). Never patch `Text` alone on an existing message |
| Reaction pill rendering | `messages.ReactionPillText` |
| Render one `MessageItem` (author line, body, Block Kit, attachments, reactions, hits) for a content pane | `messages.RenderMessage(msg, width, rc, opt)` with a `RenderContext` the pane builds (`renderContext()` in each pane) and `RenderOptions` for its differences; `messages.BlockkitContext(msg, rc)` for the Block Kit host context. Both panes; the thread pane's options are `thread.(*Model).renderMessage` |
| Turn a `RenderedMessage` into list rows: fill, invisible/tinted left border, the long-timestamp selected variant, the plain-text mirror (the part of building an `entrylist.Entry` both panes share) | `messages.WrapRows(msg, rendered, width, messages.NewWrapStyles(focused))` returning `WrappedRows` (`Lines`, `Selected`, `Plain`, `ShortSrc`); the panes add ID/Ref/hits and (channel pane) the spacer rows. `WrapStyles.SelectedVariant` builds the short-timestamp variant for `EntryPayload.SelectedLines`. The thread parent row is not wrapped this way: it is re-rendered every frame |
| Apply a reaction add/remove event idempotently; the reaction-pill cursor | `messages.ApplyReaction`, `messages.ReactionNav` |
| Find, replace, remove, patch, insert or upsert a row in a `[]MessageItem` by TS, reporting the index and what changed so the pane can fix its selection and caches; swap a placeholder for the sent message without duplicating an echoed row | `messages.IndexOfTS` (from the back), `FirstIndexOfTS` (from the front, for the edit/delete/reaction paths), `ReplaceTS`, `RemoveTS`, `RemoveAt`, `PatchTS` (the edit paths, through `rowSink.Patch`), `Insert`, `Upsert`, `Settle`, returning `RowChange{Op, Index, From}` (`internal/ui/messages/rows.go`). Both panes' send, edit and delete paths write through these; the reaction and reply-count paths only locate their row with `FirstIndexOfTS` |
| Add a row to a content pane so the rows Slack knows stay in TS order (each once, placeholders after them), and keep the cursor on its message | `messages.Insert(rows, item)` (the only way a row is added; `AppendMessage`, `AddReply`, upserts and restores use it), `messages.Settle` (moves the sent message to its TS, `RowMoved`), `messages.IsNewest(rows, i)` (the pane follows that row) and `RowChange.Selection(selected)` (the cursor after any insert, removal or move) |
| A content pane's per-entry payload next to its `entrylist.Entry` (short-timestamp selected variant, kitty flushes, sixel rows), and the rows to draw for the cursor entry | `messages.EntryPayload`, `(*EntryPayload).SelectedLines`; the emoji-image context both panes take is `messages.EmojiContext` |
| Date label from a Slack ts | `messages.DateFromTS`, `messages.FormatDateSeparator`, `messages.FormatShortDate` (compact form for a message header: "Today", "Jan 2") |
| The date labels against a given clock (pure; the exported ones read the package clock `SetNowFunc` sets) | `messages.formatDateSeparatorAt(dateStr, now)`, `formatShortDateAt(dateStr, now)`, `longTimestampAt(ts, short, now)` |
| A relative-time label ("5m", "2h") against a given clock | `formatRelTimeAt(ts, now)`, one copy each in `activityview` and `threadsview` (see Known duplication) |
| Date-qualified timestamp for the selected message header | `messages.LongTimestamp(ts, short)`, `messages.SelectedHeader(rendered, header, ts, short, maxWidth)`; any other header timestamp swap that must fit the width budget: `messages.ReplaceHeaderTimestamp(rendered, header, short, replacement, maxWidth)` |
| Clock layout `<!date` tokens render `{time}`/`{time_secs}` with | `messages.SetDateTokenTimeLayout(layout)` — seeded once at startup from `appearance.timestamp_format` (`cmd/slk/main.go`); empty keeps the default `"3:04 PM"`. Package-level (like theme state) so every render path agrees |
| mpdm channel name → human name | `slackfmt.FormatMPDMName` |
| Channel-type glyph (`#` / `◆` / `●`) | `messages.ChannelGlyph(chType)` |
| Marker row for a message only the current user can see | `messages.EphemeralLabel()` — drawn above the author line in both panes |
| A message's pressable legacy buttons (selects excluded) | `messages.ButtonActions(msg)`, `messages.HasButtonActions(msg)` |
| The hint under an interactive message ("b to respond" for an ephemeral with buttons, else "open in Slack") | `messages.InteractiveHint(msg)`; both panes |
| Open the action dialog for an ephemeral's buttons | `(*App).openActionPrompt(actionPromptRef)` (`internal/ui/action_prompt.go`); queueing and the interruption rule are `enqueueActionPrompt` / `tryOpenActionPrompt` |
| Replace a pane's rows wholesale (a history fetch, a cache read on entering a channel or opening a thread) | `(*App).paneRows(sink, channelID, threadTS, fetched, shown)`, or `olderRows` for a prepended page, and `threadParent` for a thread's parent row (`internal/ui/pending_view.go`). They put back what Slack's rows cannot know: pending edits' bodies, messages being deleted left out, the user's messages that may be a pending send's held, the ephemerals in `shown` (pass `shown` only for the conversation already on screen) and each pending send's placeholder. `TestPaneRowsGoThroughPaneRows` rejects any other `SetMessages`/`PrependMessages`/`SetThread` rows but nil |
| Does a message_changed echo carry text the user sent (Slack escapes &, < and > in the mrkdwn but keeps the rich_text block)? | `messages.EchoCarries(echo, sent Body)` (`internal/ui/messages/body.go`); never compare `Text` directly |
| Slack permalink parsing | `slackurl.Parse` |
| Slack's edited mark on a fetched or WebSocket message, for the cache's `edited_at` (`IsEdited` is `m.Edited != nil`) | `editedAt(m.Edited)` (`cmd/slk/history.go`) |
| Slack ts → `time.Time` (whole seconds, any timezone) | `export.TimeFromTS`; the other way, `messages.TSAt(t)` |
| A since/until/overlap date range in a timezone, and "is this ts in it?" | `export.NewWindow`, `export.Window.Contains` |
| The first instant of a local calendar date, safe in zones that skip midnight (Sao Paulo once did) | `export.dayStart(date, deltaDays, loc)`; `time.Date(y, m, d, 0, 0, 0, 0, loc)` returns the previous evening on such a day |
| Sleep out a slack-go rate-limit error (ctx-aware) | `slackclient.WaitOutRateLimit` |
| Page through all channel history in a ts range / a thread's replies in a ts range | `(*slackclient.Client).WalkHistory`, `GetRepliesBetween` |
| Press a legacy attachment button (`chat.attachmentAction`, payload byte-identical to the web client's) | `(*slackclient.Client).AttachmentAction(ctx, AttachmentActionRequest)`, with `service_id` = the posting app's bot ID |
| Press a legacy attachment button from the UI | `core.InteractionService.PressAttachmentAction` (App field `a.interactions`); wired in `cmd/slk` via `pressAttachmentAction` to the active workspace's `Client.AttachmentAction` |
| Run a slash command (`chat.command`, request shape copied from the web client; arguments as rich text via `mrkdwn.Convert`) | `(*slackclient.Client).RunCommand(ctx, CommandRequest)`; a refusal (`unknown_command`, …) is `*slackclient.CommandRejectedError` |
| Run a slash command from the UI | `core.CommandService.Run` (App field `a.commandSvc`); wired in `cmd/slk` via `runCommand` to the `Client.RunCommand` of the workspace the command was typed in (`teamID`, looked up with `router.ByID`, not the active one). Slack's refusal arrives as `*core.CommandError{Code}` |
| Turn typed `@name` / `#channel` / `@group` into wire tokens for sending | `compose.translateMentions(text, names, channels, groups)`, a pure function of the three maps; `(*compose.Model).TranslateMentionsForSend` delegates |
| Count the mentions of the user in a slice of cached messages (DMs and apps count every message from someone else) | `countMentions(msgs, chType, selfUserID)` (`cmd/slk/markread.go`); `countMentionsSince` wraps it with the cache read |
| A web-client `client_token` (`web-<unix ms>`) for any request that sends one | `newClientToken()` (`internal/slack`); the only generator |
| Emoji shortcode → glyph | `emoji.Sprint`, `emoji.CodeMap`, `emoji.StripSkinTone` |
| Emoji shortcode → glyph on any row whose width matters (sidebar, overlays, status glyphs, picker) | `emoji.ResolveShortcodesInText(s)` or `emoji.TextGlyph(glyph)` on a `CodeMap` lookup. base+VS16 (`:heart:`, `:mountain:`) is drawn as the bare base, because terminals and lipgloss disagree on the pair's width but agree on one codepoint; ZWJ, flags, skin tones and keycaps are left as `:shortcode:`. Never call `emoji.Sprint` or read `CodeMap` for display without the gate (#301) |
| Does Block Kit already render the message body? | `blockkit.RendersBody(blocks)`, `messages.BlocksCarryBody(msg)` |
| Current DND state from a Slack API result | `slack.DNDStateFromStatus` |
| Persist a full Web API profile's custom status and huddle (including clears) | `cmd/slk/applyProfileStatus(teamID, userID, profile, db, send)` after `UpsertUser`, before any resolved-user notice or row repair; `UpsertUser` alone deliberately preserves status on conflict |
| Drop cached thread replies a complete `conversations.replies` response no longer contains (missed deletes, Slackbot ephemerals) | `(*cache.DB).PruneThreadReplies(channelID, threadTS, returnedTS, fetchStartUnix)`; soft-deletes, never touches the parent or rows cached after the fetch began |
| Peer custom status, DND and huddle rendering | `ui/peerstatus` (`Status`, glyph/expiry/summary methods); `messages.AuthorStatusSuffix` for author headers |
| Workspace app/bot classification, from any goroutine | `WorkspaceContext.IsBotUser` / `MarkBotUser`; private synchronized set, never directly read/write a plain bot-ID map |
| Usergroup map helpers | `usergroups.Copy`, `usergroups.Equal`, `usergroups.Display` |
| A workspace's user ID → display name, from any goroutine in `cmd/slk` | `wctx.UserNames` (`*userNameStore`: `Get`/`Set`, `MentionedNames(text)` for one message; `lookupUserCached` / `resolveUserCached` add the SQLite fallback). Just `Set`: hand the UI the map from `SnapshotForUI()` and pass only its version to `NotifyFrom`, which makes every later new or changed name reach the UI as `UserResolvedMsg`. Never keep or read the map you handed over |
| User IDs mentioned in message text | `slackfmt.MentionedUserIDs(text)` |
| Channel IDs mentioned in message text (`<#C…\|name>`), or each with its label | `slackfmt.MentionedChannelIDs(text)`; `slackfmt.MentionedChannels(text)` returns `ChannelMention{ID, Label}` |
| Wake the serialized WebSocket event owner for local work | Implement `slackclient.PendingEventHandler` (`PendingEvents() <-chan struct{}`, `OnPendingEvents()`). The dispatcher selects the wake-up channel even on an idle socket and serializes the drain with Slack callbacks; nil disables it, and the channel must never close |
| Apply a deferred DM profile to conversation snapshots | `userResolver.queueResolvedPeer(userID)` after caching a resolved profile; `rtmEventHandler.OnPendingEvents()` drains relevant DM peers on the serialized event owner even on an idle socket. `rtmEventHandler.repairDMPeer(userID)` is the shared cache-to-row repair used by that drain and reconnect reconciliation. `rememberConversation` mirrors notifier metadata; `publishConversation` updates only the active UI |
| Context-bounded synchronous peer resolution at reconnect | `userResolver.ResolveNowContext(ctx, ids)` batches first; `resolveOneContext(ctx, userID)` is the fallback. Both share cache/name/workspace-scoped notifications, never mutate workspace slices |
| userBoot IM → conversation metadata | `cmd/slk/bootIMConversation`; callers decide whether an ordinary open IM or a closed starred IM is eligible |
| Starred conversation membership / recover missing sidebar rows | `SectionStore.StarredConversationIDs()` (locked defensive copy); `cmd/slk/missingStarredConversations` (read-only metadata hydration), then the existing `buildChannelItem` / `addConversation` paths |
| Workspace-relative cached external-user flags | `cache.DB.ExternalUsers(workspaceID)` returns a caller-owned map, derived from `User.HomeTeamID`; the legacy boolean is only a fallback within the row's original workspace |
| A WebSocket message's author ID and its UI `MessageItem` (bot-ID fallback, display-name resolution, file/block/attachment conversion) | `(*rtmEventHandler).messageAuthor`, `(*rtmEventHandler).messageItemFromEvent` (`cmd/slk/rtm_handler.go`); shared by `OnMessage` and `OnEphemeralMessage` |
| Legacy attachment action ids slack-go drops | `slackclient.EphemeralMessage.ActionIDs` (decoded from the raw frame), applied with `cmd/slk/applyActionIDs` |
| Slack original-avatar URL → sized CDN URL (e.g. 72px) | `avatar.SizedURL(orig, px)`; returns non-Slack-original URLs unchanged |
| Copy text to the clipboard | `App.clipboardWrite` / `SetClipboardWriter`; `cmd/slk/newClipboardWriter` selects local macOS `pbcopy` or terminal OSC 52 |
| Compare release versions (`v` prefix, prerelease only counts when current is one) | `isNewerVersion(latest, current)` (`cmd/slk/update_check.go`) |
| Tell the user a newer release exists: daily cached GitHub check, install-method-specific hint | `checkForUpdate` (background, from `run()`), `cachedUpdateHint` (no network; `--version`), `detectInstallMethod(exe, installEnv)`, `updateHint`; reaches the UI as `ui.UpdateAvailableMsg{Hint}`. Gated by `Config.EffectiveCheckForUpdates()` (`[general].check_for_updates`) |

### UI chrome

| Need | Use |
|---|---|
| Scrollbar gutter on a rendered pane | `bubbles/scrollbar.Overlay`, `bubbles/scrollbar.Visible` |
| Lay out a wrapping row of labelled controls (buttons, select labels) with positions for hit-testing | `internal/bubbles/buttonrow` (`Layout`, `Rows`, `RowsFunc`); used by Block Kit actions and the confirm prompt |
| Draw a dialog: its box, title / filter / body / footer layout, filter line, list window, selected-row marker, scrollbar gutter, and the composer popups' box and marker | `bubbles/chrome` (`Frame`, `Box`, `FilterInput`, `Window`, `List`, `Marker`, `BodyTop`, `Popup`, `PopupMarker`), drawn with `messages.ChromeStyles()` called per draw. A dialog keeps its own fields, matching and row content; see the package doc |
| A scrollbar thumb's rows, for a gutter you draw yourself | `bubbles/scrollbar.Thumb` (the one formula; `Overlay` and `chrome.List` use it) |
| Centered modal over a dimmed backdrop | `ui/overlay.DimmedOverlay` |
| Prompt modal with N buttons (title, wrapped body of up to 8 lines, buttons; yes/no is `Open`, any buttons via `OpenChoice(confirmprompt.Choice)`, `Button.Then` for a follow-up step) | `bubbles/confirmprompt` (`New`, `Open`, `OpenChoice`, `Update`, `PressHighlighted`, `View`, `SetStyles`, `SetWidth`; options `WithStyles`/`WithKeyMap`/`WithWidth`; `KeyMap` field). In the App, open it with `App.openConfirmPrompt` (`internal/ui/confirm.go`), which also maps the theme onto its styles and composites it |
| Text selection ranges and anchors | `bubbles/selection` (`Range`, `Anchor`, `LessOrEqual`) |
| A scrollable, selectable list of pre-rendered rows: line offsets, scroll offset and snapping, the visible window, text selection and copy, clickable-rect translation, the scrollbar | `bubbles/entrylist` (`New` with options, `SetHeight`, `SetEntries`, `Layout`, `Compose`). It anchors the window across every `SetEntries` / `ReplaceEntries` itself: a window at the bottom stays there, otherwise the first entry on screen keeps its row, and the cursor's snap follows its entry by ID, so an owner changing rows never positions the window (only `ResetSnap` when the content is replaced). The channel and thread panes are its two owners; each divergence between them is an `entrylist` option, documented on the option |
| The theme mapped onto a content pane's entrylist styles (padding, scrollbar, selection highlight); the thread pane calls it; the channel pane uses the unexported `themeListStyles` it wraps | `messages.ThemeListStyles()` (`internal/ui/messages/pane_list.go`) |
| Do something to whichever message pane (channel or thread) is focused or named by a panel, without a messages/thread branch | the `contentPane` interface, `(*App).contentPaneFor(panel)` and `(*App).focusedContentPane()` (returns the pane and its channel ID) in `internal/ui/content_pane.go`; both panes are asserted against it at compile time. Built on it: `handleReactionNav`, `openReactionPicker`, `toggleReactionOnSelected` (`app.go`) and the click hit tests `toggleReactionAt` (reaction pill) and `openImagePreviewAt` (inline image, via `HitTestImage`) (`reducer_mouse.go`), each taking the pane and its channel ID |
| Theme colors and styles | `ui/styles` (`Username`, `SelectionStyle`, `SearchHighlightStyle`, `MentionBadgeStyle`, `UserColor`) |
| Change the theme inside the App | `App.applyTheme(name)` (`internal/ui/theme.go`): applies it, invalidates render caches and pushes fresh styles to components that snapshot them. A component that holds a style snapshot (anything in `internal/bubbles`) gets its push added there, not at each call site. `entrylist` is the exception: its owners (the channel and thread panes) call `SetStyles` at the top of every `View`, so `applyTheme` only invalidates for them. `styles.CurrentTheme()` names the applied one |
| Status-bar toast that clears itself | `toastWithClear(a, text, d)` (`internal/ui/reducer_io.go`); `toastUntilReplaced(a, text)` only for a toast a later one replaces on purpose (upload progress). Set toasts on the Update goroutine, never inside a `tea.Cmd`; `statusbar.ToastSeq()` identifies the toast showing, so a clear tick leaves a newer toast alone. `TestToastsGoThroughToastSetters` fails on a direct `statusbar.SetToast` with text |
| Window tree geometry | `ui/wintree` |
| Sidebar display order, collapsed-section totals, next/previous unread, and page-key geometry, as pure functions | `sidebar.sortFilteredIdx`, `aggregateSection`, `nextUnread`, `pageRows` (`internal/ui/sidebar/model.go`); the `Model` methods are thin wrappers over them |
| Truncate a styled line with an ellipsis | `ansi.Truncate(s, w, "…")` from `charmbracelet/x/ansi`. It parses OSC 8 hyperlinks; `reflow/truncate.StringWithTail` does not and lets a line with a link overflow by a cell |
| Sidebar selection across item/filter reorders | Capture `sidebar.Model.currentCursorKey()` **before** mutation, then `rebuildNavWithCursor`; `rebuildNavPreserveCursor` is only safe while old nav indices still identify the same items |
| Modal geometry / row hit-testing | `chrome.Boxed`, `chrome.Clickable` (list rows; declared in `internal/bubbles/chrome`, asserted by each dialog package) and `pointClickable` (a single glyph, e.g. the profile dialog's 📋; `internal/ui/reducer_modal_click.go`) |
| Channel/DM destination picker for forwarding | `channelfinder.Model.OpenForForwarding()` (joined conversations only); `Open()` restores the normal switcher |
| Refresh a workspace finder entry without losing visit recency | `rtmEventHandler.upsertFinderItem(item)` on the serialized event owner; returns the stored item and a changed flag, preserves existing `LastVisited`, seeds a new row from `LastVisitedByChannel` |
| Render a thread from cache on open, before the fetch lands | `cachedThreadRepliesCmd(threads, chID, threadTS)` (`internal/ui/app.go`); yields a `FromCache` `ThreadRepliesLoadedMsg`, which renders but never marks the thread read — only the fetched result does |
| Bind a composer to the conversation its draft belongs to | `(*compose.Model).SetDraftContext(teamID, channelID, threadTS)` — an empty channel detaches (saves the prior conversation's draft, clears the visible input). `SetActiveChannel` sets only mention context; it does not move draft storage |
| Put text into a conversation's draft without overwriting anything newer (visible composer or stored draft) | `(*compose.Model).OfferDraft(teamID, channelID, threadTS, text)`; false when that conversation already has text or attachments. Used to give back a slash command that did not run (via `offerTypedBack`) and for `/msg`'s pre-fill |
| Name a navigable position (workspace, channel, message, thread reply) | `ui.Location`; `(*App).currentLocation` / `currentPosition` snapshot where the user is |
| Route a permalink in-app, or learn why slk will not (active workspace, known channel) | `(*App).routeLinkInApp(url)` (`internal/ui/reducer_links.go`); `routeLink` adds the browser fallback for `o`, `OpenPermalinkMsg` replies with the error instead. `permalinkLocation(url)` is the parse and workspace check alone, shared with `linkOpensInApp` |
| Hand a permalink to the running slk from another process (`slk open`) | `internal/opensocket`: `Listen(path, handler)` (stale-socket and second-instance handling), `Send(path, url)` (`ErrNotRunning`, `*Rejected`), `Path(dataDir)`. Wired in `cmd/slk/open_command.go`, which delivers to the UI as `ui.OpenPermalinkMsg` |
| Navigate to a `Location` (history walk, mark jump, permalink) | `(*App).applyLocation(loc, fromHistory)` (`internal/ui/reducer_links.go`); goes through `pendingLinkNav`, so it waits for the channel and thread to load, and refuses while an upload is in flight |
| Persistent marks in the cache | `(*cache.DB).UpsertMark` / `ListMarks` / `DeleteMark`, keyed by workspace and letter |

### App state and actions

| Need | Use |
|---|---|
| Selected message or reply in the focused pane (channel, TS, text, author, panel) | `App.selectedMessageContext()` (`internal/ui/app.go`) |
| Show, settle or roll back an optimistic message row (send, thread reply, broadcast) in every pane showing it | `rowSink` (`InsertLocal`, `Settle`, `RemoveLocal`; both panes implement it) and `(*App).sinksFor(channelID)` / `replySinksFor(channelID, threadTS, broadcast)`, whose `rowSinks.settle` / `removeLocal` clone per channel window; the placeholder row is `(*App).localRow(localTS, text, threadTS, subtype)`, and `stampLocalTS(ins, call)` wraps the service call so the result carries `LocalTS` and a nil result (no active workspace) becomes the send's failure (`internal/ui/rowsink.go`) |
| Change an existing message's body wherever it is (an edit, its settling, the `message_changed` echo) | `(*App).showBody(channelID, ts, body)` (`reducer_send.go`): every pane that may show it (`shownSinks` + `rowSinks.patch`, the thread pane patching a matching reply and the parent), a held echo of it (`pendingOps.PatchHeld`) and the rows a pending delete took out (`pendingOps.PatchRemoved`), so a failed delete restores the message as it is now. Returns the row as it was |
| Track an optimistic edit until chat.update answers, one chat.update per message at a time, and know what the message should show | `pendingOps.BeginEdit(pendingEdit{…}, prior, ok)` returns the edit and whether to send it now (else it waits, replacing an older waiting edit); `EditEcho(ch, ts, body)` records a `message_changed` echo while an edit is in flight (true: show nothing); `EndEdit(id, ok)` returns `editSettled` (what to show, the waiting edit to send next, the edit to report as failed; a failure whose text an echo carried is a success); `EditPending`, `EditGone` (`internal/ui/pending_ops.go`). `(*App).sendEdit(e)` is the chat.update; `stampEditID(e, call)` puts the id on the result and turns nil into a failure. `MessageEditedMsg.NotFound` / `MessageDeletedMsg.NotFound` carry Slack's message_not_found (`isMessageNotFound`, `cmd/slk/slack_errors.go`) |
| Take a message out of every pane at once and put it back where it was if the delete fails | `(*App).removeFrom(shownSinks(ch), ts)` returns the `removedRow`s; `(*App).restoreTo(ch, removed)` restores them into panes still on that conversation; per pane `rowSink.Remove` / `Restore` over `messages.RemovedRow` and `messages.Insert(rows, item)` (in TS order, ahead of placeholders, no-op when the TS is shown; the cursor goes back only if it has not moved since `Remove`). Pending deletes: `pendingOps.BeginDelete` / `EndDelete` / `DeleteEchoed` / `DeletePending` / `PatchRemoved`; a result with `NotFound` counts as success; `(*App).applyMessageDeleted(ch, ts)` is what a confirmed delete does (echo or success); `failNilDelete` turns a nil result into a failure (`internal/ui/rowsink.go`, `pending_ops.go`, `reducer_send.go`) |
| Make a service call the user started end even when `cmd/slk` returns nil (no active workspace) | `failNil(call, failureMsg)` (`internal/ui/rowsink.go`); `errNoWorkspace` is the error to put on it. Wraps mark-unread, older history, fetch-around, join and upload; `stampLocalTS`, `stampEditID` and `failNilDelete` do the same for send, edit and delete |
| Open the editor on a message with given text; reopen a failed edit where it was typed | `(*App).enterEdit(channelID, ts, panel, text)` (`beginEditOfSelected` uses it); `(*App).reopenEdit(e)` reports false when the user has moved on (`internal/ui/app.go`) |
| Track a send from Enter until Slack answers, and keep the user's other messages in that conversation from being mistaken for it | `pendingOps` (`internal/ui/pending_ops.go`, App field `a.pendingOps`): `BeginInsert(pendingInsert{…})` at send; `HoldEcho(m)` in `reduceNewMessage` holds any of the user's own messages in a conversation (`conversationOf(channelID, msg)`: a channel's top level or one thread) while a send is pending there; `EndInsert(localTS, sentTS)` on the HTTP result records `sentTS` and returns the held messages to show once none is pending, minus those a result showed, which `(*App).releaseHeld` shows through `showNewMessage` (`reducer_send.go`). A failed send gives `typed` back through `pendingInsert.origin()` and `offerTypedBack`. `RecordSent` / `IsSelfSent` drop an echo after the result; `PatchHeld` / `DropHeld` apply an edit or delete to a held message; `NextLocalTS` mints the placeholder id. `(*App).insertSinks(ins)` gives the panes a pending send writes (`internal/ui/rowsink.go`) |
| Is this TS a placeholder (a message still sending)? Refuse an action on one | `messages.IsLocalTS(ts)` (the one predicate; `messages.LocalTS(n)` mints the id, through `pendingOps.NextLocalTS`). Refusing: `(*App).sendingGuard(ts)` (toast "Still sending…") and `sendingSelectionGuard()` for the focused selection (`internal/ui/pending_ops.go`). Keys: `handleNormalMode` runs it for every `isMessageOpKey` key plus edit and delete. Choke points that guard themselves whatever reaches them: `openReactionPicker`, `toggleReactionOnMessageItem` (keys, reaction nav, pill clicks), the reaction picker's result, `copyPermalinkOfSelected`, `beginForwardOfSelected`, `saveThreadToFile`, `setMark`, `openThreadForSelectedMessage`, and the `EditMessageMsg` / `DeleteMessageMsg` / `MarkUnreadMsg` arms. Read cursors (`recordChannelMark`, `recordThreadMark`) ignore one. Navigation names a placeholder by the sent message before it: `(*App).sentLocation` behind `currentLocation` / `channelPaneLocation`; `selectedLocation` is the raw selection |
| Order two Slack TSes by time (numerically: "9.5" before "10.0") | `messages.CompareTS(a, b)` (`internal/ui/messages/rows.go`); a placeholder TS compares as a string |
| The nearest row before an index that passes a test; may a read cursor or unread boundary sit on this message? | `messages.LastIndexBefore(rows, i, keep)`; `messages.KnownToSlack(msg)` (a real TS, not ephemeral) (`internal/ui/messages/rows.go`). Used by `markUnreadOfSelected` and `newestMarkableReplyTS` |
| Switch the active workspace from the UI | `App.switchWorkspace(teamID)` (`internal/ui/app.go`). Always use it: it saves a theme a cycle left pending, so a switch back to that workspace reads the new theme |
| Save a per-workspace theme | `settings.SaveTheme(teamID, name, scope)` with the workspace on screen (`a.activeTeamID`), never the backend's active one: a switch moves that first |
| Set the user's own presence or DND from the UI (optimistic cache + status bar, then the service) | `(*App).applyPresenceAction(action, snoozeMinutes)` (`internal/ui/presence_action.go`); the presence menu, custom snooze and the `/away` family use it |
| Switch to a channel, joining it first if the user is not a member | `(*App).openChannel(channelfinder.ChannelResult)` (`internal/ui/open_channel.go`); the channel finder and `/open` use it |
| Resolve a command's channel argument (`#name` or a `<#C…\|name>` mention) to a channel, joined or not | `(*App).findChannelByID` (finder rows, by ID) and `matchChannelByName` (case- and accent-insensitive, channels and private channels only) in `internal/ui/slash_builtins.go`; a name the finder does not hold goes to the server through `ChannelService.SearchRemote` and comes back as `channelLookupMsg` (reduced in `reducer_slash_command.go`), because the finder's rows are only the user's own conversations. `/open` and `/join` use it |
| Open (or find) a DM / group DM and switch to it, optionally pre-filling its draft | `(*App).openConversation(userIDs, msgPrefill)` (`internal/ui/open_conversation.go`); the new-message picker and `/msg` use it. Request IDs come from a counter that never resets; a `/msg` pre-fill whose open fails, is cancelled or is superseded is given back by `abandonMsgPrefill`, and `/msg` refuses while `openingConversation()` |
| Is composer text a slash command, and its name/args? | `parseSlashCommand(text)` (`internal/ui/slash_command.go`); `(*App).trySlashCommand(panel, c, channelID, threadTS)` runs it from either Enter branch. The result is `commandRanMsg`, reduced in `reducer_slash_command.go`; `commandFailureText` words the toast |
| Give a slash command's typed text back to the composer it came from (a built-in that refuses, a failed remote run, a failed `/msg` open, and a failed send or reply), with a toast | `commandOrigin{teamID, channelID, threadTS, typed}` (`slash_command.go`), which `trySlashCommand` puts on `commandArgs.Origin`; `(*App).refuseCommand(origin, text)` is the toast plus the give-back, `(*App).offerTypedBack(origin)` the give-back alone (thread composer if the origin has a threadTS, else the channel composer; never over a newer draft). Every built-in refusal goes through `refuseCommand` |
| Give back the text of a send, reply or edit that failed, never losing it | `(*App).giveTextBack(origin)` (`internal/ui/slash_command.go`): `offerTypedBack`, else the session's unsent texts, all copied to the clipboard together (`(*App).keepUnsent(text)`; `(*App).recovered(text)` when one reaches a composer or Slack; `internal/ui/unsent.go`); pass its `onClipboard` as `OnClipboard` on `statusbar.SendFailedMsg` / `EditFailedMsg`, whose toast (`failureToast`, `reducer_io.go`) then says so. Is a composer showing a given conversation's draft: `(*compose.Model).BoundTo(teamID, channelID, threadTS)` |
| Add a command | `:` commands: an entry in `commands` (`internal/ui/command.go`); slash built-ins slk runs itself: an entry in `slashCommands` (`internal/ui/slash_command.go`). Both are `commandFunc(a, commandArgs{Fields, Rest, Origin})`, dispatched by `dispatchCommand` (a slash command by `splitCommandLine` + `runCommand`, so `Origin` can be set); a slash name not in the table runs through `chat.command` |
| Stage or issue an automatic read mark without moving Slack's cursor backward | `(*App).recordChannelMark` / `recordThreadMark` (staging), `flushPendingMarks` (issue); both check `a.readFloors` (`readFloors.behind` / `advance` / `reset`, keyed by `channelFloor` / `threadFloor`, `internal/ui/read_floor.go`). An explicit mark-unread goes through `(*App).markedUnread(channelID, threadTS, boundaryTS)`, which moves the floor back and drops the staged mark |
| An app's "working" status on a thread (Slack's `ai_assistant_status`, from `assistant.threads.setStatus`) | Arrives as `BotStatusMsg`; state on `a.typing` (`internal/ui/typing.go`): `SetBotStatus` (empty clears), `ClearBot` (the app replied), read with `WorkingBots(channel)` for the channel typing row and `ThreadBotStatuses(channel, thread)` for the thread pane. Lapses after `botStatusExpiry` (2 min, Slack's timeout) on the typing tick |

### Test helpers

Everything here is unexported and lives in a `_test.go` file, so it is
reachable only from the package that declares it (all of these are
`package ui` unless noted). They are listed because the failure mode this
file exists to prevent — writing a sixteenth ad-hoc test-app builder — is
exactly what happened before Phase 0 consolidated them. Symbols are
greppable by name; no line numbers, because these files move.

| Need | Use |
|---|---|
| Build an `App` for a test or benchmark | `newTestApp(t, opts...)` (`internal/ui/testapp_test.go`) |
| The same without a `testing.TB` | `buildTestApp(opts...)` — only for the four legacy builders whose signatures must not change: `newPanelAtApp`, `sixelTestApp`, `makeBenchApp`, `makeWideScrollApp` |
| Size the App | `withSize(w, h)` (direct field assign) / `withWindowSize(w, h)` (real `tea.WindowSizeMsg` resize path). **Not interchangeable** — only the latter sets `forceSixelRepaint`. Mutually last-wins |
| Seed panes and data | `withMessages`, `withChannels`, `withWorkspaces`, `withThreadsView`, `withChannelFinderOpen` |
| Seed App-level state | `withMode`, `withView`, `withActiveChannel`, `withActiveTeam`, `withChannelService`, `withWindowSplit`. **`withActiveChannel` assigns only `a.activeChannelID`** — nothing derives a *name* from an ID, so the messages-pane header renders as a bare `#`, the statusbar as `#`, and the compose placeholder as `Message #...`. A test that asserts on any of those must push the name through the production trio itself; `newGoldenApp` does it via `nameGoldenActiveChannel` (`internal/ui/golden_test.go`) |
| Populate `a.layout` bands / pane caches (needed for mouse hit-testing) | `withRender()` |
| N plain message fixtures | `testMessageItems(n)` |
| A golden for a dialog or composer popup over the standard fixture | `dialogGoldens()` / `renderDialogGolden(t, d)` (`internal/ui/golden_dialogs_test.go`); add a scenario there for a new dialog |
| Compare or bless a full-screen frame against `testdata/golden/<name>.ansi` | `compareGolden(t, name, got)` (`internal/ui/golden_test.go`) |
| Prove two ANSI screens look identical even when their bytes differ (re-encoded SGR), or that every golden is cell-identical to a baseline directory | `cellDiff(want, got)` and `SLK_GOLDEN_BASELINE=<dir> go test ./internal/ui -run TestGoldenCellsMatchBaseline` (`internal/ui/goldencells_test.go`) |
| Re-bless goldens | the package-local `-update` flag: `go test ./internal/ui -run TestGolden -update`. It is not defined repo-wide, so `go test ./... -update` fails |
| An `App` with every render nondeterminism pinned (theme, emoji mode, clock) | `newGoldenApp(t, opts...)`, with `goldenMessages()` / `goldenChannels()` as the fixtures |
| Drive a message through the real `Update` chain and render one frame | `updateAndRender(t, a, msg)` (`internal/ui/thread_breadcrumb_test.go`) |
| Run a command the way the runtime does (batch members concurrently, ticks on their real timers) and take the message it delivers | `deliveredMsgs[T](t, cmds...)` (`internal/ui/cmdmsgs_test.go`). Use it, not a hand-built tick message, to show what a real tick carries |
| An App at a given width, resized via `WindowSizeMsg`, channel focused, for thread-layout tests | `stackedApp(t, w, extra...)` (`internal/ui/thread_stacked_test.go`) |
| Assert which of channel / thread the last frame drew | `assertFront(t, a, wantChannel, wantThread)` (same file) |
| Fake one service method on an `App` | `a.setChannelFetcherForTest(fn)` and its siblings, `setUploaderForTest`, `setClipboardReaderForTest`, `setReadStateReaderForTest`, `setDesktopForTest(func(*core.DesktopServiceFuncs))`, `setCommandRunnerForTest(fn)`, `setFilesystemForTest()`, `setEditorForTest()`, `setProfileFetcherForTest(fn)` (`internal/ui/services_helpers_test.go`). Calls for sibling methods of one service compose instead of replacing each other |
| Table-drive a mode handler's keys | `runKeyCases(t, mode, []keyCase{...})` (`internal/ui/modekeys_test.go`). Calls `dispatchModeKey` directly, so it **bypasses** the reducer chain and the `ctrl+c` / bootstrap / scroll-flush gates ahead of it |
| Build a key message for such a table | `keyPress(r)` printable rune, `keyCode(c)` special key, `keyMod(c, mod)` modified key |
| Count the rows a finder-style modal is showing | `modalRows(bs)` (`internal/ui/mode_workspace_finder_test.go`) — `h - 7`; floors at 1, and is **not** valid for `newmessagepicker` |
| Read the highlighted (`▌`) row out of a rendered modal box | `modalHighlightedRow(t, box)` (same file) |
| Compare two `color.Color` without `==` panicking on a non-comparable dynamic type | `colorEqual(a, b)` (`internal/ui/mode_theme_switcher_test.go`; `package styles` has its own twin in `styles/styles_test.go`) |
| Observe a status-bar toast (no getter exists) | `statusbarText(a)` (`internal/ui/mode_presence_snooze_test.go`) |
| Record status-setter invocations | the `statusCall` struct (same file) |
| A fixture with real unread channels either side of the active one | `unreadOpts()` + `seedUnreads(t, a)` (`internal/ui/mode_normal_keys_test.go`) |
| Establish a focused pane with an asserted selection | `focusMessages(t, a)`, `focusThreadPanel(t, a)` (same file) |
| Park the message viewport at an exact `yOffset` | `scrollTo(off)` (same file) |
| Make nav-history entries resolvable | `navLookupOpt()` (same file) |
| An `App` with the main composer mid-upload, a thread open with its own draft, and workspaces T1/T2, counting uploader and workspace-switcher calls | `uploadGuardApp(t)` (`internal/ui/conversation_drafts_safety_test.go`) |
| Observe a compose cursor position or blur state (no getter exists) | `afterKeyValue(c, r)` (`internal/ui/mode_insert_keys_test.go`) |
| An `App` on `C1` showing given messages, whose attachment-button presses are recorded (and answered with an error you choose) | `actionApp(t, err, msgs...)` (`internal/ui/action_prompt_test.go`), returning the `*[]pressCall` log |
| Slackbot's "not in this channel" ephemeral with Add Them / Dismiss / Don't Show Again buttons | `mentionEphemeral(ts)` (`internal/ui/action_prompt_test.go`) |
| Deliver a message to the App through the real `Update` chain and get the messages its commands produced | `arrive(t, a, msg)` (`internal/ui/action_prompt_queue_test.go`) |
| Send a key and feed every resulting command's message back through `Update` | `pressKey(t, a, k)` (`internal/ui/action_prompt_test.go`) |
| The toast text among such messages | `toastText(msgs)` (`internal/ui/action_prompt_test.go`) |
| Is the confirm prompt visible and does its view contain some text? | `dialogShows(a, text)` (`internal/ui/action_prompt_queue_test.go`) |
| Click the cell where some text is drawn inside the open confirm prompt | `clickConfirmText(t, a, text)` (`internal/ui/confirm_click_test.go`) |
| An `App` with a normal message and then a given ephemeral in `C1`, the ephemeral selected in a focused messages pane | `selectedEphemeralApp(t, msg)` (`internal/ui/ephemeral_keys_test.go`) |
| An `App` in T1 for jump tests: every channel resolves, C1–C3 have fresh cached messages | `jumpMarkTestApp(t)` (`internal/ui/jump_mark_test.go`) |
| An `App` in T1 on C1 for set-mark tests, with no cache wired | `setMarkTestApp(t)` (`internal/ui/set_mark_test.go`) |
| Record a mark directly in T1's store | `seedMark(t, app, letter, loc)` (`internal/ui/jump_mark_test.go`) |
| Press a mark chord through `App.Update` | `pressJumpChord(app, letter)`, `pressBackJump(app)` (`internal/ui/jump_mark_test.go`), `pressMarkChord(app, letter)` (`internal/ui/set_mark_test.go`) |
| Feed a jump's `ChannelSelectedMsg` back through `Update` | `driveJump(app, cmd)` (`internal/ui/jump_mark_test.go`) |
| A marks table backed by an in-memory cache, or one whose every call fails | `marksPersistForTest(t)` (`internal/ui/marks_test.go`), `failingMarksPersist()` (`internal/ui/command_marks_test.go`) |
| List a workspace's marks, failing the test on a read error | `mustListMarks(t, store, teamID)` (`internal/ui/marks_test.go`) |
| An 80x24 `App` with the marks list open over marks a (C2) and b (C3), and the box's top-left screen cell | `marksClickApp(t)` (`internal/ui/marks_mode_test.go`) |
| An `App` with given per-channel caches and an optional fetch-around, for history tests | `navFixtureApp(t, caches, fetchAround)` (`internal/ui/navhistory_test.go`) |
| Drive a history walk through the whole pipeline | `walkAndUpdate(t, app, cmd)`; `walkToCompletion(t, app, cmd)` also returns the selection and toasts (same file) |
| A nav stack's entries as channel IDs | `navEntryChannelIDs(entries)` (`internal/ui/app_test.go`) |
| An `App` in insert mode on C1/T1 whose slash-command runs are recorded and answered with an error you choose; press Enter through `Update` | `slashApp(t, err)`, `pressEnter(a)`, the `runCall` record (`internal/ui/slash_command_test.go`) |
| Register a slash built-in for one test (restored on cleanup) | `withSlashBuiltin(t, name, fn)` (`internal/ui/slash_command_test.go`) |
| A slashApp with the status setter recorded and a cached presence for T1 | `presenceSlashApp(t, presence)`, returning the App and the `*[]statusCall` log (`internal/ui/slash_builtins_test.go`) |
| A slashApp whose channel finder holds joined general (C1) and random (C2), whose server channel search answers with unjoined design (C9), and whose joins and searches are recorded | `openSlashApp(t)`, returning the App and the `*[]runCall`, joins and searches logs (`internal/ui/slash_builtins_test.go`) |
| A slashApp whose conversations.open is recorded and answers with DM D9 | `msgSlashApp(t)`, returning the App, the `*[]runCall` log and the `*[][]string` OpenConversation log (`internal/ui/slash_builtins_test.go`); `failOpenConversation(a)` then makes that open answer `NewMessageFailedMsg` |
| The draft of a conversation in T1, read by switching the composer to it (changes the composer's draft context) | `draftIn(a, channelID)` (`internal/ui/slash_builtins_test.go`) |
| Compare a request this repo builds with a scrubbed web-client capture (`{method, form, response}` JSON in `testdata/`) | `capturedForm(t, name)` (the capture's form minus `token` and the `_x_*` envelope) and `assertFormMatchesCapture(t, got, name)` (same fields and values, nothing extra; `client_token` checked for shape) (`internal/slack/command_test.go`, `package slackclient`) |
| Generate styled terminal text, Slack wire text (mentions, links, code, entities), plain text with wide and combining runes, Slack IDs and timestamps, awkward time zones, for a property test | `internal/proptest` (`StyledText`, `StyledTextWith`, `SGR`, `BgSGR`, `OSC8`, `SlackText(users, channels)`, `PlainText`, `Word`, `ID(prefix)`, `TS`, `Zone()` with embedded tzdata); an ordinary package that only `_test.go` files import |
| Run a check under another local time zone | `tztest.Each(t, zones, body)` (`internal/tztest`): body runs in a child process of the test binary per zone, started with `TZ` set, and returns a string to the parent. Never assign `time.Local` in a test: every `time.Now` in every goroutine reads it, and timers left by earlier tests make that a data race under `-race` |
| Keep a width property clear of the base+VS16 disagreement (#301) | `proptest.NoVS16(s)`; the messages pane's narrower `stripASCIIVS16` covers only an ASCII base |
| Run the fuzz targets | `scripts/fuzz.sh [fuzztime] [pattern]`, `make fuzz` |
| Load a config from a TOML string | `loadConfigString(t, body)` (`internal/config/config_test.go`, `package config`) |

### Known duplication — do not add to it

These are tracked in the refactor plan and are being consolidated. Do not copy
them as templates:

- **`messages.Model` and `thread.Model`** share `bubbles/entrylist` (scroll,
  selection, window, scrollbar) and `messages.RenderMessage` (one message
  renderer); their differences are `entrylist` options and `RenderOptions`
  fields, each documented where it is declared. Do not add a pane-specific
  copy of either.
- **`formatRelTimeAt`** is copied in `activityview` and `threadsview`; both
  carry the same property tests, which keep them in step until one shared
  home exists.
- **`convertAndCacheHistory` / `fetchChannelMessages` / `fetchThreadReplies`** in
  `cmd/slk/main.go` have ~85% duplicated bodies.

## Conventions

**Adding a reusable helper?** Add it to the tables above in the same commit. An
unlisted helper gets re-implemented. When this file and the code disagree, the
code is right and this file is a bug — fix it.

**Two implementations that must stay parallel?** Express it as an interface with
a compile-time assertion (`var _ Chrome = (*Model)(nil)`) or a lockstep test.
Not a comment. The comment that used to sit at the top of
`internal/ui/thread/model.go` — "this shape mirrors
`internal/ui/messages.viewEntry` exactly; keeping them in lockstep means…" —
was the counter-example: prose asking humans to maintain a 377-line invariant
by hand. Phase 0 pinned it with a lockstep test, and Phase 3 replaced both with
structure. The `contentPane` interface (`internal/ui/content_pane.go`), which
both panes are asserted against at compile time, covers what the App calls on
either pane. Every behavioural difference between them is an `entrylist` option
(`WithEagerClamp`, `WithCursorFollowsScroll`, …) or a `RenderOptions` field,
documented where it is declared (the `entrylist` options are tested per
setting in `entrylist_test.go`). Do the same: when you find a comment standing
in for a check, replace it with the check.

**Extract the substrate, not the widget.** Share the uniform part; leave the
divergent part alone. Forcing genuinely different behavior into a common shape
is worse than the duplication it removes.

**Refactoring?** Moving functions is free — two phases of the prior refactor
moved ~3,000 lines with zero test changes. Moving *state* is not: `internal/ui`
test files make **2,905 references to 142 distinct unexported `App` fields and
methods** (type-checked count, not a grep; measured at the end of Phase 0, up
from 1,995 / 121 before it — Phase 0's characterization tests added ~910 of
them). Budget roughly **220 mechanical test-line edits per 10 extractions**, and
expect five members to dominate: `messagepane` (243), `compose` (204),
`focusedPanel` (146), `mode` (129) and `activeChannelID` (125) are 847 of the
total between them. Method and per-member breakdown:
`docs/superpowers/plans/2026-09-06-architecture-refactor.md`, Phase 0 "Achieved".

**Found a bug while refactoring?** Record it, annotate it, raise it separately.
A refactor commit that also changes behavior cannot be reviewed.

## Workflow

Per the README: brainstorm the design first, write tests, then implement.
Designs go to `docs/superpowers/specs/YYYY-MM-DD-<topic>-design.md`,
implementation plans to `docs/superpowers/plans/`.

Before opening a PR: `go build ./...`, `go vet ./...`, `go test ./... -race`,
`gofmt -l .` empty.
