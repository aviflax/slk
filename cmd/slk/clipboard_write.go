package main

import (
	"context"
	"log"
	"os/exec"
	"strings"
	"sync"
	"sync/atomic"
	"time"

	tea "charm.land/bubbletea/v2"
)

// newClipboardWriter keeps remote copies on the terminal's clipboard, while
// local macOS copies use pbcopy because Terminal.app does not support OSC 52.
// Its writes land in the order the UI made them (newestClipboardWrite).
func newClipboardWriter(goos string, getenv func(string) string, nativeWrite func(string) error) func(string) tea.Cmd {
	if goos != "darwin" || getenv("SSH_CONNECTION") != "" || getenv("SSH_CLIENT") != "" || getenv("SSH_TTY") != "" {
		return newestClipboardWrite(tea.SetClipboard)
	}
	return newestClipboardWrite(func(text string) tea.Cmd {
		return func() tea.Msg {
			if err := nativeWrite(text); err != nil {
				log.Printf("[clipboard] pbcopy failed: %v; falling back to OSC 52", err)
				return tea.SetClipboard(text)()
			}
			return nil
		}
	})
}

// newestClipboardWrite makes write's commands land in the order write
// was called. Each command runs on its own goroutine, so two quick
// writes (a second unsent text joining the bundle, two copies) could
// otherwise finish in either order and leave the older text on the
// clipboard. Each call takes a sequence number on the Update goroutine,
// without blocking it; a command that a newer call has overtaken by the
// time it runs does nothing, and the rest run one at a time.
func newestClipboardWrite(write func(string) tea.Cmd) func(string) tea.Cmd {
	var issued atomic.Uint64
	var running sync.Mutex
	return func(text string) tea.Cmd {
		seq := issued.Add(1)
		inner := write(text)
		return func() tea.Msg {
			running.Lock()
			defer running.Unlock()
			if seq != issued.Load() {
				return nil
			}
			return inner()
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
