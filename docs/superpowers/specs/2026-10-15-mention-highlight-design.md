# Mention highlight

## Problem

A message that mentions the user looks the same as any other message.
`renderInlineFormattingWith` styles every `<@U…>` in bold `Primary`,
whoever it names, and `@here` / `#channel` get the same style. Scrolling
through a busy channel, a mention of you is easy to miss. The Slack
desktop client marks these rows with a yellow bar and a pale tint, and
draws your own name as a yellow chip.

## Design

Three marks, applied by the content panes (channel pane and thread pane,
including the thread parent):

1. **Gutter bar.** The left border column that every row reserves (drawn
   invisible today, see `WrapStyles.invis`) is painted in the mention
   colour.
2. **Row tint.** The row's background is a faint mix of the mention
   colour into the theme background, filled to the row width, in the same
   way the selection tint is filled.
3. **Name pill.** Inside the text, `@<your name>` gets a stronger tint as
   background, in bold. Other users' mentions keep today's style.

How much of this a message gets depends on what kind of mention it
carries:

| Message | Bar | Tint | Pill |
|---|---|---|---|
| `<@you>` (direct) | yes | yes | yes |
| `<!here>`, `<!channel>`, `<!everyone>` only | yes | no | n/a |
| Neither, or written by you | no | no | no |

Broadcasts get only the bar, so a channel full of `@here` announcements
does not drown out the messages that name you. The user's own messages
are never marked, because Slack doesn't mark them either and because
writing `@here` should not light up your own row.

### Selection

The selected row keeps its current look: selection border and selection
tint. A row that is both selected and a mention shows the selection,
because the cursor is on it and you are already reading it. The pill is
part of the content, so it still shows on the selected row.

### Detection

`mention.Classify(text, selfUserID)` returns `None`, `Broadcast` or
`Direct`, and `mention.InText` becomes `Classify(...) != None`, so the
notifier, the sidebar badge and this feature keep one definition. The
known gaps in `InText` carry over unchanged. Pipe-labelled forms and
usergroup mentions are not detected, so a message can be missed but
never marked wrongly.

Detection reads `MessageItem.Text`. Bot messages that carry the body in
Block Kit still put a fallback in `text`, and the pill is drawn by
`RenderSlackMarkdownWith`, which renders both the body and Block Kit
section text.

### Colours

Three optional theme fields, defaulted in `styles.Apply`:

| Field | Default | Used for |
|---|---|---|
| `mention_bar` | `Warning` | gutter bar |
| `mention_bg` | `Warning` mixed into `Background` at 0.10 | row tint |
| `mention_pill_bg` | `Warning` mixed into `Background` at 0.35 | pill background |

The pill's foreground is the theme text colour. Search hits are already
drawn as solid `Warning`, so a solid-yellow pill would look like a search
hit; the tinted pill stays distinct from them.

### Out of scope

- Jumping to the next or previous mention (a separate feature).
- A config switch to turn this off. The theme fields can tone it down;
  a switch can come later if it is wanted.
- Usergroup mentions, which need the user's own group memberships.

## Implementation notes

- `RenderContext` gains `SelfUserID`; both panes' `renderContext()` fill
  it from the `currentUserID` they already hold, and `SetCurrentUser`
  invalidates their render caches when it changes.
- `RenderSlackMarkdownOpts` gains `SelfUserID`; the user-mention
  replacement draws the pill when the ID matches.
- `WrapRows` picks the unselected border and fill from the message's
  mention kind. The tinted variant swaps the theme background for the
  tint with the existing grammar-aware `substituteBgSGR`, the same way
  `RepaintBgToSelectionTint` does for selection. `Plain` is unaffected,
  so copying text and mapping mouse columns do not change.
- The thread parent is drawn outside `WrapRows` and gets the same
  treatment where it builds its border.
