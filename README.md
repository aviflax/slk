# slk

> **A blazingly fast Slack TUI.**
> Keyboard-driven, beautifully themed, and under 20MB. One static binary. No Electron required.
>
> Docs: [wiki/](wiki/Home.md) · Upstream's marketing site: [getslk.sh](https://getslk.sh)

![slk demo: navigating channels, live messages, a split window and switching workspaces](docs/assets/demo/hero.gif)

`slk` is a daily-driver replacement for the official Slack desktop client, built in Go with [bubbletea](https://github.com/charmbracelet/bubbletea) and [lipgloss](https://github.com/charmbracelet/lipgloss).

## About this fork

This is a fork of [gammons/slk](https://github.com/gammons/slk), maintained by Kira Howe. Thanks to Grant Ammons and the upstream contributors for building it. The fork fixes some papercuts and adds a few things, and what differs is listed in [CHANGELOG.md](CHANGELOG.md). Releases start at v0.27.0, picking up after upstream's v0.26.2.

## Why slk?

- **Fast.** Cold start in milliseconds. Render-cached messages. SQLite-backed scrollback. Real-time over WebSocket.
- **Tiny.** ~19 MB on disk. ~60 MB RSS for a live multi-workspace session vs. 500 MB–1.5 GB for the official client. No node_modules, no Chromium, no 1Gb RAM tax.
- **Keyboard-first.** Vim-style modal editing. `j/k`, `h/l`, `i`, `Esc`.
- **Pretty.** 59 built-in themes, lipgloss-styled panels, true-pixel avatars on kitty (half-block fallback elsewhere), emoji shortcodes, day separators, and pill-style reactions.
- **Multi-workspace.** All your workspaces stay connected in parallel. `1`–`9` to instantly jump between them, with live unread badges in the rail.
- **Yours.** TOML config, custom themes, custom channel sections via glob, XDG-compliant paths.

## Highlights

- Real-time messages, edits, deletes, reactions, typing indicators, apps' "working" status
- Inline images (kitty graphics / sixel / half-block fallback) with full-screen preview
- Threads side panel + a workspace-wide threads view
- Messages that mention you are highlighted like in Slack's desktop app: a gutter bar, a tinted row, and your `@name` in a pill
- Activity feed: mentions, thread replies, reactions, and DMs in one list, like Slack's Activity tab
- Smart paste: clipboard images, file paths, or text — multiple attachments + caption in one send
- Slack-native sidebar sections, kept live; or glob-based config sections
- Automatic auth from the Slack desktop app — no tokens to copy, no Slack App required
- Vim-style modal keybindings, fuzzy channel finder, workspace picker
- `slk open <permalink>` lets other apps show a Slack link in the running slk
- 59 themes + drop-in custom themes, live theme switcher
- OS desktop notifications on DMs, mentions, and configurable keywords

Full feature breakdown: **[Features](https://github.com/kirahowe/slk/blob/main/wiki/Features.md)**

## Quick install

**Homebrew** (macOS and Linux):

```bash
brew install --cask kirahowe/tap/slk
```

The cask installs this fork's releases. `go install` is not an option: the Go module path is still `github.com/gammons/slk`, so it would install upstream. To build from source instead:

```bash
go build -ldflags="-s -w" -trimpath -o ~/.local/bin/slk ./cmd/slk
```

Tarballs and `.deb` / `.rpm` / `.apk` packages are attached to each [release](https://github.com/kirahowe/slk/releases).

## Setup

slk reads your session directly from the **Slack desktop app** — no DevTools,
no tokens to copy. Make sure you're signed in to the desktop app, then:

```bash
slk --add-workspace
```

slk lists the workspaces you're signed in to; pick the ones you want and
you're done.

No desktop app (a server, a container, an SSH session)? `slk --add-workspace
--browser` signs in from your browser session instead: in DevTools > Network,
Copy as cURL any request to `/api/` on app.slack.com, and paste it. slk also
offers this when it cannot read the desktop app's session.

Full walkthrough: [Setup wiki page](https://github.com/kirahowe/slk/blob/main/wiki/Setup.md).

## Debugging

Set `SLK_DEBUG=1` to enable a comprehensive debug log written to
`slk-debug.log` in the current working directory. The file is
**truncated each run**, so reproduce the issue, quit slk, then copy
the file before relaunching. Log lines are categorized
(`[cache]`, `[imgfetch]`, `[imgrender]`, `[ws]`, `[general]`) so
`grep '\[cache\]' slk-debug.log` slices to one focus area.

## Documentation

Everything lives in the [**wiki/**](wiki/Home.md) directory:

- [Installation](https://github.com/kirahowe/slk/blob/main/wiki/Installation.md) — prebuilt binaries, Homebrew, build from source
- [Setup](https://github.com/kirahowe/slk/blob/main/wiki/Setup.md) — desktop-app auth, adding workspaces
- [Features](https://github.com/kirahowe/slk/blob/main/wiki/Features.md) — full feature breakdown
- [Keybindings](https://github.com/kirahowe/slk/blob/main/wiki/Keybindings.md) — every key, every mode
- [Configuration](https://github.com/kirahowe/slk/blob/main/wiki/Configuration.md) — `config.toml`, custom themes, XDG paths
- [Terminal Compatibility](https://github.com/kirahowe/slk/blob/main/wiki/Terminal-Compatibility.md) — what each terminal supports, including tmux setup (inline images, unread-title forwarding)
- [Clipboard and OSC 52](https://github.com/kirahowe/slk/blob/main/wiki/Clipboard-and-OSC-52.md) — copy/paste setup notes
- [Tradeoffs and Non-Goals](https://github.com/kirahowe/slk/blob/main/wiki/Tradeoffs-and-Non-Goals.md) — roadmap, caveats, TOS notice
- [Architecture](https://github.com/kirahowe/slk/blob/main/wiki/Architecture.md) — service layout, data layer

## Contributing

Contributions are welcome. A few ground rules:

- **AI-assisted PRs are accepted** — and in fact encouraged — but only if
  they're driven by a **frontier model** (e.g. Claude Opus, GPT-5,
  Gemini Pro) running with **high thinking effort**. Low-effort,
  small-model output that nobody reviewed tends to create more work than
  it saves, and will be closed.
- Ideally, drive the work with the
  [superpowers](https://github.com/obra/superpowers) framework (or an
  equivalent skills/TDD-disciplined workflow). Brainstorm the design
  first, write tests, then implement.
- **For large feature additions, open an issue first.** Before sinking
  time into a big change, file an issue to discuss the idea and approach
  so we can agree on direction. Bug fixes and small improvements can go
  straight to a PR.
- Whether human- or AI-written, **you are responsible for your PR.**
  Understand the diff, make sure it builds and passes `go vet ./...` and
  `go test ./...`, and be ready to explain your choices in review.

## Disclaimer

`slk` is an independent, unofficial project. It is not affiliated with, endorsed by, or sponsored by Slack Technologies, LLC or Salesforce, Inc. "Slack" is a trademark of Slack Technologies, LLC; it is used here only to describe the service this client interoperates with.

slk talks to Slack via the same internal browser protocol the official web client uses. This is unofficial and not sanctioned by Slack — see [Tradeoffs and Non-Goals](https://github.com/kirahowe/slk/blob/main/wiki/Tradeoffs-and-Non-Goals.md#unofficial--tos-caveat) for details.

## License

[MIT](LICENSE) © Grant Ammons
