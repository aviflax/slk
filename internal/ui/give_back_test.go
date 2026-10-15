package ui

import (
	"errors"
	"strings"
	"testing"
	"time"

	tea "charm.land/bubbletea/v2"
)

// clipboardLog records what the App copies, in place of the terminal.
func clipboardLog(a *App) *[]string {
	var copied []string
	a.SetClipboardWriter(func(text string) tea.Cmd {
		copied = append(copied, text)
		return nil
	})
	return &copied
}

// shownToast delivers cmd through Update and returns the toast it
// shows.
func shownToast(t *testing.T, a *App, cmd tea.Cmd) string {
	t.Helper()
	for _, m := range drainMsgs(cmd, 50*time.Millisecond) {
		a.Update(m)
	}
	return statusbarText(a)
}

// When a failed send's text cannot go back to its composer (the user
// has typed something newer there), it goes to the clipboard and the
// toast says so; it is never dropped.
func TestFailedTextGoesToClipboardWhenComposerIsTaken(t *testing.T) {
	t.Run("send", func(t *testing.T) {
		a := typedApp(t)
		copied := clipboardLog(a)
		_, _, local := sendFromComposer(t, a, PanelMessages, "first")
		a.compose.SetValue("second")
		_, cmd := a.Update(MessageSendFailedMsg{ChannelID: "C1", LocalTS: local, Reason: "boom"})
		if toast := shownToast(t, a, cmd); !strings.Contains(toast, "Send failed: boom") || !strings.Contains(toast, "clipboard") {
			t.Errorf("toast = %q, want the failure and that the text is on the clipboard", toast)
		}
		if a.compose.Value() != "second" || len(*copied) != 1 || (*copied)[0] != "first" {
			t.Errorf("composer = %q, clipboard = %q; want the newer draft kept and the failed text copied", a.compose.Value(), *copied)
		}
	})
	t.Run("reply", func(t *testing.T) {
		a := typedApp(t)
		copied := clipboardLog(a)
		_, _, local := sendFromComposer(t, a, PanelThread, "first")
		a.threadCompose.SetValue("second")
		_, cmd := a.Update(ThreadReplySendFailedMsg{ChannelID: "C1", ThreadTS: "5.000000", LocalTS: local, Reason: "boom"})
		if toast := shownToast(t, a, cmd); !strings.Contains(toast, "clipboard") {
			t.Errorf("toast = %q, want it to say the text is on the clipboard", toast)
		}
		if a.threadCompose.Value() != "second" || len(*copied) != 1 || (*copied)[0] != "first" {
			t.Errorf("thread composer = %q, clipboard = %q", a.threadCompose.Value(), *copied)
		}
	})
	t.Run("edit", func(t *testing.T) {
		a := editApp(t, errors.New("boom"))
		copied := clipboardLog(a)
		res := editFrom(t, a, PanelThread, "6.000000", "new reply")
		// The user moved on and typed a new reply in that thread.
		a.focusedPanel = PanelMessages
		a.mode = ModeInsert
		a.threadCompose.SetValue("something newer")
		_, cmd := a.Update(res)
		if toast := shownToast(t, a, cmd); !strings.Contains(toast, "Edit failed: boom") || !strings.Contains(toast, "clipboard") {
			t.Errorf("toast = %q, want the failure and that the text is on the clipboard", toast)
		}
		if a.threadCompose.Value() != "something newer" || len(*copied) != 1 || (*copied)[0] != "new reply" {
			t.Errorf("thread composer = %q, clipboard = %q", a.threadCompose.Value(), *copied)
		}
	})
}

// Text that does go back to its composer is not also copied, and the
// toast does not mention the clipboard.
func TestFailedTextGivenBackSkipsClipboard(t *testing.T) {
	a := typedApp(t)
	copied := clipboardLog(a)
	_, _, local := sendFromComposer(t, a, PanelMessages, "first")
	_, cmd := a.Update(MessageSendFailedMsg{ChannelID: "C1", LocalTS: local, Reason: "boom"})
	if toast := shownToast(t, a, cmd); strings.Contains(toast, "clipboard") {
		t.Errorf("toast = %q, want no clipboard mention", toast)
	}
	if a.compose.Value() != "first" || len(*copied) != 0 {
		t.Errorf("composer = %q, clipboard = %q", a.compose.Value(), *copied)
	}
}

// A failed broadcast reply given back to the thread composer turns its
// "also send to channel" toggle back on, so Enter sends it as before.
func TestFailedBroadcastReplyRestoresToggle(t *testing.T) {
	a := typedApp(t)
	a.threadCompose.SetBroadcast(true)
	_, sent, local := sendFromComposer(t, a, PanelThread, "to everyone")
	if !sent.Broadcast {
		t.Fatal("setup: the reply was not a broadcast")
	}
	a.threadCompose.SetBroadcast(false)
	a.Update(ThreadReplySendFailedMsg{ChannelID: "C1", ThreadTS: "5.000000", LocalTS: local, Reason: "boom"})
	if a.threadCompose.Value() != "to everyone" || !a.threadCompose.Broadcast() {
		t.Errorf("thread composer = %q broadcast=%v, want the text back with broadcast on",
			a.threadCompose.Value(), a.threadCompose.Broadcast())
	}
}
