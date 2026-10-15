# Changelog

All notable changes to this fork are listed here. The format follows
[Keep a Changelog](https://keepachangelog.com/en/1.1.0/), and the project uses
[Semantic Versioning](https://semver.org/).

Earlier history, up to v0.26.2, is in [upstream's releases](https://github.com/gammons/slk/releases).

## [Unreleased]

### Fixed

- The update hint now shows up within a few hours of a release. slk asked
  GitHub at most once a day and only at startup, so a release published
  after that check went unnoticed until the next day, or until a restart.

## [0.28.0] - 2026-10-15

### Added

- Messages that mention you stand out, the way they do in the Slack desktop
  app. A message that names you gets a yellow bar in the left gutter, a faint
  warm tint across the row, and a pill behind your `@name`. An `@here`,
  `@channel` or `@everyone` message gets only the bar. Your own messages are
  never marked. Custom themes can set `mention_bar`, `mention_bg` and
  `mention_pill_bg`; they default to shades of the theme's `warning` colour.

### Changed

- The help overlay (`?`) credits this fork, made by Kira Howe, with a fainter
  line under it crediting Grant Ammons, who wrote slk up to v0.26.
- Channels in each sidebar section are listed alphabetically, ignoring case and
  accents. Channels pinned with a `:N` suffix in a config section still come
  first, and the Starred section still lists channels before people. slk can't
  read the sort order chosen in the official Slack client.
- When a channel matches more than one config section with the same `order`,
  it goes in the one whose name sorts first. It used to land in a different
  one from run to run.

### Fixed

- Sending a message that starts with a number and a full stop no longer
  renumbers it: "2024. was a year" was sent as "1. was a year", and a list
  typed from 5 was sent counting from 1.
- Text typed on the same line as an opening ```` ``` ```` is no longer dropped
  when the message is sent.
- A block quote that wraps onto more than one line keeps its bar and muted
  colour on every line, not just the first.
- Bold, italics, links and mentions inside a block quote are formatted
  instead of showing their raw markup.
- A line typed right after a list, without indenting it, is sent as its own
  line instead of being joined onto the last list item.
- Loading older history into an empty channel could crash slk.
- Older history could show a message twice after one of your own messages was
  sent.
- When a message or thread reply failed to send, the cursor jumped to the next
  message. It now stays where it was.
- Removing a reaction in a channel could show the wrong reactions on the open
  thread's parent message.
- A config section named like a built-in one (for example `Channels`) was drawn
  twice in the sidebar.
- Page Down in the sidebar could move the cursor up after a fast scroll.
- The channel finder ranked names with accents, CJK or emoji below equally close
  plain-ASCII names.
- Activity and Threads cards with a link in them could run one column past the
  edge of the pane.
- Copying a selection that starts with a combining accent no longer drops the
  accent.
- A wide character (CJK or emoji) where the scrollbar is drawn no longer shifts
  the scrollbar a column to the left on that row.
- A blank entry in `on_keyword` sent a desktop notification for every message.
  Blank entries are now ignored.
- `slk export` in a time zone whose daylight saving time once began at midnight
  (such as America/Sao_Paulo) started those days an hour early.
- Saving a theme whose name contains the DEL character wrote a config file slk
  could not load.
- On Linux, `slk open` with a request slk rejects for length could report
  "connection reset by peer" instead of slk's reason.
- Installing or upgrading the Homebrew cask no longer prints a warning that
  `postflight` is deprecated.

## [0.27.0] - 2026-10-15

First release of the kirahowe/slk fork. It starts from upstream's main after
v0.26.2.

### Added

- `slk open <permalink>` shows a Slack message link in the slk that is already
  running, the way `o` does, so a script or URL handler can try slk before the
  browser. It exits `0` when slk showed the link, `1` when slk declined it, `2`
  on a usage error and `3` when slk is not running. slk declines with `slk is
  busy: <what>` while a dialog, a `:` command, a search, the image preview, a
  message edit or an upload is in the way. If you are composing, it leaves
  insert mode and keeps your draft. The socket is `run/slk.sock` in the data
  directory (`~/.local/share/slk/run/slk.sock`, or under `$XDG_DATA_HOME`).
  Not available on Windows.
- slk tells you when a newer release exists. Once a day it checks GitHub for
  the latest stable release of kirahowe/slk, shows a 12-second status-bar toast
  with the upgrade command for your install, and then leaves a muted
  `↑ <version>` on the right of the status bar. `slk --version` prints the same
  hint. Set `check_for_updates = false` under `[general]` to turn it off. Dev
  builds skip the check.
- `:help` opens the keybinding overlay, the same as `?`.
- `:quit` and `:qa` quit with the same confirmation as `Q`.

### Changed

- **`Ctrl+X` now opens the draft in `$EDITOR`.** `Ctrl+E` used to do this. It
  now moves to the end of the line, and `Ctrl+A` to the start, in both the
  channel and thread composers.
- **`:q` on the last window now quits**, with the `:quit` confirmation, like
  vim. With more than one window it still closes the focused one. `Ctrl+w q`
  keeps toasting "Cannot close last window".
- **The sidebar hides itself on narrow terminals.** It stays off screen while
  the channel pane beside it would be under 60 columns, which is below 100
  columns with the default widths. `Ctrl+b` still toggles it at any width.
  Showing a hidden sidebar pins it until you hide it again.
- Threads open beside the channel from 120 columns, down from 162. The sidebar
  gives way first: 120 to 151 columns show the channel and thread, and 152 and
  up show all three. Below 120 they stack as before.
- **Emoji written as a base character plus VS16, such as the heart, now draw as
  the bare glyph** instead of falling back to `:shortcode:` text, in messages,
  reaction pills, the reaction picker and viewer, sidebar section headers and
  peer status. Some terminals may draw the glyph in text style rather than
  colour. ZWJ sequences, flags, skin tones and keycaps still show the
  shortcode.
- Releases, the Homebrew cask (`brew install --cask kirahowe/tap/slk`) and the
  update check now use kirahowe/slk. `go install` still installs upstream,
  because the Go module path is unchanged, so the update hint no longer
  suggests it.
- The `?` overlay groups its entries under Navigation, Search, Messages,
  Threads, Composer, Windows, Views, Commands and App. It lists `Ctrl+U`
  (clear message input), drops the "Press / to search" line, and shows the
  filter line only while you are searching.
- The pane scrollbar track is a dotted `┊` line, so it no longer looks like a
  second border next to the pane's own.
- The thread breadcrumb is inset one column to line up with the channel header,
  and the rule under it uses box-drawing characters.
- The presence menu's filter placeholder reads `Type to filter...` like the
  other filter boxes.

### Fixed

- Line breaks, blank lines and repeated spaces typed in the composer are now
  preserved in sent messages.
- Sidebar section emoji such as `:hammer_and_wrench:` and `:mountain:` no longer
  push the sidebar edge, message pane and overlay borders one cell off (#301).
- The reaction picker no longer shows a literal `:heart: heart` row among the
  glyphs.
- The sidebar no longer shows two cursor bars while Activity is open. The
  last-viewed channel drops its `▌` marker while Threads or Activity is the
  view on screen.

[0.28.0]: https://github.com/kirahowe/slk/releases/tag/v0.28.0
[0.27.0]: https://github.com/kirahowe/slk/releases/tag/v0.27.0
