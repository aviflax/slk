//go:build !windows

package main

import (
	"bytes"
	"errors"
	"os"
	"path/filepath"
	"strings"
	"testing"
	"time"

	tea "charm.land/bubbletea/v2"

	"github.com/gammons/slk/internal/opensocket"
	"github.com/gammons/slk/internal/ui"
)

const testPermalink = "https://myteam.slack.com/archives/C054JFCBN69/p1779284733270139"

// openSockPath is short enough for sun_path on macOS, where t.TempDir()
// can be too long.
func openSockPath(t *testing.T) string {
	t.Helper()
	dir, err := os.MkdirTemp("", "slkopen")
	if err != nil {
		t.Fatal(err)
	}
	t.Cleanup(func() { _ = os.RemoveAll(dir) })
	return filepath.Join(dir, opensocket.FileName)
}

func serveOpen(t *testing.T, path string, h opensocket.Handler) {
	t.Helper()
	srv, err := opensocket.Listen(path, h)
	if err != nil {
		t.Fatal(err)
	}
	t.Cleanup(func() { _ = srv.Close() })
}

func TestOpenCommandExitCodes(t *testing.T) {
	path := openSockPath(t)

	run := func(args ...string) (int, string, string) {
		var out, errOut bytes.Buffer
		code := openCommand(args, path, &out, &errOut)
		return code, out.String(), errOut.String()
	}

	if code, _, errOut := run(testPermalink); code != openExitNotRunning || !strings.Contains(errOut, "not running") {
		t.Errorf("nothing listening: code %d, stderr %q", code, errOut)
	}
	if code, _, _ := run(); code != openExitUsage {
		t.Errorf("no args: code %d, want %d", code, openExitUsage)
	}
	if code, _, _ := run("a", "b"); code != openExitUsage {
		t.Errorf("two args: code %d, want %d", code, openExitUsage)
	}
	if code, out, _ := run("--help"); code != openExitOK || !strings.Contains(out, "Exit status") {
		t.Errorf("--help: code %d, stdout %q", code, out)
	}

	serveOpen(t, path, func(url string) error {
		if strings.Contains(url, "otherteam") {
			return errors.New("the link is for another workspace")
		}
		return nil
	})
	if code, out, _ := run(testPermalink); code != openExitOK || out != "ok\n" {
		t.Errorf("accepted: code %d, stdout %q", code, out)
	}
	if code, _, errOut := run(strings.Replace(testPermalink, "myteam", "otherteam", 1)); code != openExitRejected || !strings.Contains(errOut, "another workspace") {
		t.Errorf("declined: code %d, stderr %q", code, errOut)
	}
	if code, _, errOut := run("https://example.com/"); code != openExitRejected || !strings.Contains(errOut, "not a Slack message permalink") {
		t.Errorf("not a permalink: code %d, stderr %q", code, errOut)
	}
}

func TestPermalinkHandlerRelaysUIReply(t *testing.T) {
	var got ui.OpenPermalinkMsg
	h := permalinkHandler(func(m tea.Msg) {
		got = m.(ui.OpenPermalinkMsg)
		if !got.Claim() {
			t.Error("Claim refused before the timeout")
		}
		got.Reply <- errors.New("channel C1 is not one slk knows")
	}, time.Second)
	if err := h(testPermalink); err == nil || err.Error() != "channel C1 is not one slk knows" {
		t.Errorf("handler = %v", err)
	}
	if got.URL != testPermalink {
		t.Errorf("UI got %q", got.URL)
	}
}

// The UI takes the message only after the handler gave up (bubbletea's
// loop was blocked, e.g. by $EDITOR). Its Claim must then refuse, so
// the reducer drops it instead of navigating long after the client was
// told "did not respond".
func TestPermalinkHandlerTimesOutAndAbandons(t *testing.T) {
	delivered := make(chan ui.OpenPermalinkMsg, 1)
	h := permalinkHandler(func(m tea.Msg) { delivered <- m.(ui.OpenPermalinkMsg) }, 20*time.Millisecond)
	if err := h(testPermalink); err == nil || !strings.Contains(err.Error(), "did not respond") {
		t.Errorf("handler = %v, want a timeout", err)
	}
	if late := <-delivered; late.Claim() {
		t.Error("Claim accepted a message the handler had abandoned")
	}
}

// A UI that claimed the message before the timeout fired is answered
// with its own reply, even if the reply lands after the timeout.
func TestPermalinkHandlerWaitsForClaimedReply(t *testing.T) {
	h := permalinkHandler(func(m tea.Msg) {
		msg := m.(ui.OpenPermalinkMsg)
		if !msg.Claim() {
			t.Error("Claim refused before the timeout")
		}
		time.Sleep(150 * time.Millisecond)
		msg.Reply <- nil
	}, 100*time.Millisecond)
	if err := h(testPermalink); err != nil {
		t.Errorf("handler = %v, want the UI's ok", err)
	}
}
