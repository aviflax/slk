# slk Wiki

A blazingly fast Slack TUI. Keyboard-driven, beautifully themed, under 20MB,
one static binary, no Electron required.

Upstream's marketing site: [getslk.sh](https://getslk.sh) · Source: [github.com/kirahowe/slk](https://github.com/kirahowe/slk)

## Getting started

1. **[Installation](Installation.md)** — prebuilt binaries for Linux, macOS, Windows; Homebrew; build from source.
2. **[Setup](Setup.md)** — signing in through the Slack desktop app (or a browser session), adding a workspace.
3. **[Configuration](Configuration.md)** — `config.toml`, custom themes, XDG paths, per-workspace settings.

## Using slk

- **[Features](Features.md)** — full feature breakdown (messaging, compose, images, threads, reactions, channels, notifications, status, connectivity, customization).
- **[Keybindings](Keybindings.md)** — every key, every mode.
- **[Terminal Compatibility](Terminal-Compatibility.md)** — which terminals do what (kitty graphics, sixel, half-block, OSC 52).
- **[Clipboard and OSC 52](Clipboard-and-OSC-52.md)** — getting copy/paste working under tmux, screen, and stricter terminals.

## Reference

- **[Tradeoffs and Non-Goals](Tradeoffs-and-Non-Goals.md)** — what's on the roadmap, what's out of scope, known caveats.
- **[Architecture](Architecture.md)** — service-oriented layout, data layer, render pipeline.

## Project

- License: [MIT](https://github.com/kirahowe/slk/blob/main/LICENSE)
- `slk` is an independent, unofficial project. Not affiliated with Slack Technologies, LLC. See the [Tradeoffs and Non-Goals](Tradeoffs-and-Non-Goals.md) page for the TOS caveat.
