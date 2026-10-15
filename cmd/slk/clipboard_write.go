package main

import (
	"context"
	"log"
	"os/exec"
	"strings"
	"time"

	tea "charm.land/bubbletea/v2"
)

// newClipboardWriter keeps remote copies on the terminal's clipboard, while
// local macOS copies use pbcopy because Terminal.app does not support OSC 52.
func newClipboardWriter(goos string, getenv func(string) string, nativeWrite func(string) error) func(string) tea.Cmd {
	if goos != "darwin" || getenv("SSH_CONNECTION") != "" || getenv("SSH_CLIENT") != "" || getenv("SSH_TTY") != "" {
		return tea.SetClipboard
	}
	return func(text string) tea.Cmd {
		return func() tea.Msg {
			if err := nativeWrite(text); err != nil {
				log.Printf("[clipboard] pbcopy failed: %v; falling back to OSC 52", err)
				return tea.SetClipboard(text)()
			}
			return nil
		}
	}
}

// pbcopyTimeout bounds a pbcopy run, so a hung pbcopy cannot hold the
// copy command forever.
const pbcopyTimeout = 2 * time.Second

func writeMacOSClipboard(text string) error {
	ctx, cancel := context.WithTimeout(context.Background(), pbcopyTimeout)
	defer cancel()
	return pbcopyCommand(ctx, text).Run()
}

// pbcopyCommand is the pbcopy invocation writeMacOSClipboard runs, built
// without starting it.
func pbcopyCommand(ctx context.Context, text string) *exec.Cmd {
	cmd := exec.CommandContext(ctx, "pbcopy")
	cmd.Stdin = strings.NewReader(text)
	return cmd
}
