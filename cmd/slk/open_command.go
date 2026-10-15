// This file implements both ends of `slk open <url>`: the subcommand
// that hands a permalink to a running slk, and the listener the TUI
// starts so there is something to hand it to. The socket itself is
// internal/opensocket; the link reaches the UI as ui.OpenPermalinkMsg.

package main

import (
	"errors"
	"fmt"
	"io"
	"sync/atomic"
	"time"

	tea "charm.land/bubbletea/v2"

	"github.com/gammons/slk/internal/debuglog"
	"github.com/gammons/slk/internal/opensocket"
	"github.com/gammons/slk/internal/ui"
)

// Exit codes for `slk open`. Any non-zero code means the link was not
// shown, so a caller can open it in the browser instead.
const (
	openExitOK         = 0 // slk is showing the link
	openExitRejected   = 1 // slk is running but declined it, or the exchange failed
	openExitUsage      = 2 // wrong arguments
	openExitNotRunning = 3 // no slk is listening
)

// openReplyTimeout is how long the listener waits for the UI to answer.
// It is shorter than opensocket.ExchangeTimeout so the client hears
// "did not respond" instead of a dropped connection.
const openReplyTimeout = 5 * time.Second

const openUsage = `Usage: slk open <permalink>

Show a Slack message permalink in the running slk.

Exit status:
  0  slk is showing the link
  1  slk is running but declined the link (another workspace, unknown
     channel, not a permalink) or the request failed
  2  usage error
  3  slk is not running
`

// openCommand runs `slk open` with the arguments after "open" and
// returns the process exit code.
func openCommand(args []string, socketPath string, stdout, stderr io.Writer) int {
	if len(args) == 1 && (args[0] == "-h" || args[0] == "--help") {
		_, _ = io.WriteString(stdout, openUsage)
		return openExitOK
	}
	if len(args) != 1 || args[0] == "" {
		_, _ = io.WriteString(stderr, openUsage)
		return openExitUsage
	}
	err := opensocket.Send(socketPath, args[0])
	var rej *opensocket.Rejected
	switch {
	case err == nil:
		_, _ = fmt.Fprintln(stdout, "ok")
		return openExitOK
	case errors.Is(err, opensocket.ErrNotRunning):
		_, _ = fmt.Fprintln(stderr, "slk is not running")
		return openExitNotRunning
	case errors.As(err, &rej):
		_, _ = fmt.Fprintf(stderr, "error: %s\n", rej.Reason)
		return openExitRejected
	default:
		_, _ = fmt.Fprintf(stderr, "error: %v\n", err)
		return openExitRejected
	}
}

// permalinkHandler delivers each socket request to the UI through send
// (program.Send) and waits for its reply. send runs on its own
// goroutine because program.Send blocks until the event loop takes the
// message.
//
// The message may still reach the UI after the handler has given up,
// when something blocks the event loop for longer than timeout (an
// $EDITOR session does). The handler and the reducer race for one
// claim, so exactly one of them decides: if the handler wins, it
// answers "did not respond" and the reducer drops the late message; if
// the reducer wins, it navigates and the handler waits for its reply.
func permalinkHandler(send func(tea.Msg), timeout time.Duration) opensocket.Handler {
	const (
		pending int32 = iota
		takenByUI
		abandoned
	)
	return func(url string) error {
		var state atomic.Int32
		reply := make(chan error, 1)
		go send(ui.OpenPermalinkMsg{
			URL:   url,
			Claim: func() bool { return state.CompareAndSwap(pending, takenByUI) },
			Reply: reply,
		})
		timer := time.NewTimer(timeout)
		defer timer.Stop()
		select {
		case err := <-reply:
			return err
		case <-timer.C:
		}
		if state.CompareAndSwap(pending, abandoned) {
			return errors.New("slk did not respond")
		}
		// The UI claimed it just as the timer fired; its reply follows
		// on the same Update call. The second bound only guards
		// against a reducer that never gets there.
		timer.Reset(timeout)
		select {
		case err := <-reply:
			return err
		case <-timer.C:
			return errors.New("slk did not respond")
		}
	}
}

// startOpenSocket starts the `slk open` listener at path for p and
// returns the function that stops it. Failing to listen is logged, not
// fatal: another slk may already own the socket, and the TUI works
// without it. The returned stop is never nil.
func startOpenSocket(path string, p *tea.Program) (stop func()) {
	srv, err := opensocket.Listen(path, permalinkHandler(p.Send, openReplyTimeout))
	if err != nil {
		if errors.Is(err, opensocket.ErrInUse) {
			debuglog.General("open socket: %s is owned by another slk; not listening", path)
		} else {
			debuglog.General("open socket: not listening: %v", err)
		}
		return func() {}
	}
	debuglog.General("open socket: listening on %s", path)
	return func() { _ = srv.Close() }
}
