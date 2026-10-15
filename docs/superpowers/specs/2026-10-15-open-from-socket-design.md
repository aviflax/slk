# Open Slack permalinks in a running slk

**Date:** 2026-10-15
**Status:** Approved
**Related:** 2026-06-09-open-links-keybinding-design.md (the `o` routing this reuses)

## Problem

Other programs on the machine (a URL handler, a browser extension, a
script) have no way to hand a Slack permalink to a running slk. They can
only open it in the browser or the Slack desktop app.

## Goals

1. A running slk listens on a unix socket for permalinks.
2. `slk open <url>` sends one and exits with a status a caller can branch
   on, so it can fall back to the browser when slk is not running or
   cannot show the link.
3. The link goes through the same routing as `o`. There is no second
   navigation path.

## Non-goals

- Raising or focusing the terminal window.
- Switching workspaces. `o` does not switch either: a permalink for a
  workspace other than the active one goes to the browser.
- Jumping further back than `o` can (the existing `FetchAround` is the
  limit).
- Windows. The listener is not started there and `slk open` reports
  that slk is not running.

## Design

### Socket

`internal/opensocket` holds both ends. The path is
`filepath.Join(xdgData(), "slk.sock")`, so `~/.local/share/slk/slk.sock`
by default and `$XDG_DATA_HOME/slk/slk.sock` when that is set.

`Listen(path, handle)`:

- creates the directory with mode 0700 if it is missing (an existing
  directory keeps its mode) and chmods the socket to 0600;
- if a file already sits at the path, dials it first. A successful dial
  means another slk owns it: `Listen` returns `ErrInUse` and the second
  instance logs this and runs without a listener. A failed dial means a
  crashed instance left it behind, and `Listen` removes it, but only when
  it is a socket. Any other file is left alone and reported as an error;
- removes the socket file on `Close`.

Two instances starting at the same moment over a stale socket could
both decide it is stale. That window is small and the cost is one
instance not receiving links, so there is no lock file.

### Protocol

One request per connection. The client writes a URL and a newline; the
server writes one line back and closes:

- `ok`: slk is navigating to the link.
- `error: <reason>`: slk will not show it, for example because it is not
  a Slack permalink, the workspace is not the active one, or the channel
  is unknown.

The server reads at most 4 KiB, applies a 5 second deadline to the whole
exchange, and rejects anything `slackurl.Parse` does not accept before
it reaches the UI.

### Into the UI

`cmd/slk` passes a handler that sends `ui.OpenPermalinkMsg{URL, Reply}`
with `program.Send` and waits up to 5 seconds for the reply. `Reply` is a
buffered channel; the reducer sends to it without blocking.

`reduceLinks` handles the message. `routeLink` is split so its in-app
half, `routeLinkInApp`, returns a reason when it declines instead of
calling the browser opener. `OpenLinkMsg` (the `o` path) still falls back
to the browser; `OpenPermalinkMsg` replies with the reason and leaves the
browser to the caller. `linkOpensInApp` uses the same guard, so the
picker badge, `o` and the socket cannot drift apart.

### `slk open`

`slk open <url>` dials the socket, sends the URL and prints the reply.

| Exit | Meaning |
|---|---|
| 0 | slk is showing the link |
| 1 | slk is running but declined the link, or the exchange failed |
| 2 | usage error |
| 3 | no slk is listening (not running, or Windows) |

## Testing

- `internal/opensocket`: round trip over a socket in a temp dir; stale
  socket replaced; a live socket is not taken (`ErrInUse`); a regular
  file at the path is not deleted; invalid and oversized input rejected
  without reaching the handler; file modes; socket removed on `Close`.
- `cmd/slk`: `slk open` exit codes against a test listener.
- `internal/ui`: `OpenPermalinkMsg` for the active workspace reaches
  `applyLocation` (pending nav set, `ChannelSelectedMsg` dispatched) and
  replies `nil`; a foreign workspace or unknown channel replies with an
  error and does not open the browser.
