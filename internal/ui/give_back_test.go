package ui

import (
	"errors"
	"fmt"
	"strings"
	"testing"

	tea "charm.land/bubbletea/v2"

	"github.com/gammons/slk/internal/ui/messages"
	"github.com/gammons/slk/internal/ui/statusbar"
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
	for _, m := range drainCmd(cmd) {
		// The toast's clear arrives seconds later in the app; this
		// reads the toast while it is showing.
		if _, ok := m.(statusbar.CopiedClearMsg); ok {
			continue
		}
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
		_, local := sendFromComposer(t, a, PanelMessages, "first")
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
		_, local := sendFromComposer(t, a, PanelThread, "first")
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
	_, local := sendFromComposer(t, a, PanelMessages, "first")
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
	sent, local := sendFromComposer(t, a, PanelThread, "to everyone")
	if !sent.broadcast {
		t.Fatal("setup: the reply was not a broadcast")
	}
	a.threadCompose.SetBroadcast(false)
	a.Update(ThreadReplySendFailedMsg{ChannelID: "C1", ThreadTS: "5.000000", LocalTS: local, Reason: "boom"})
	if a.threadCompose.Value() != "to everyone" || !a.threadCompose.Broadcast() {
		t.Errorf("thread composer = %q broadcast=%v, want the text back with broadcast on",
			a.threadCompose.Value(), a.threadCompose.Broadcast())
	}
}

// failSends sends each text from the channel composer, then fails them
// in order, and returns the toast the last failure shows.
func failSends(t *testing.T, a *App, texts ...string) string {
	t.Helper()
	locals := make([]string, len(texts))
	for i, text := range texts {
		_, locals[i] = sendFromComposer(t, a, PanelMessages, text)
	}
	toast := ""
	for _, local := range locals {
		_, cmd := a.Update(MessageSendFailedMsg{ChannelID: "C1", LocalTS: local, Reason: "boom"})
		toast = shownToast(t, a, cmd)
	}
	return toast
}

// Every failed text that could not go back to a composer stays on the
// clipboard until it is recovered: a third failure adds to the second
// rather than replacing it, and the toast counts them.
func TestUnsentTextsAccumulateOnClipboard(t *testing.T) {
	a := typedApp(t)
	copied := clipboardLog(a)
	toast := failSends(t, a, "one", "two", "three")
	if got := a.compose.Value(); got != "one" {
		t.Fatalf("composer = %q, want the first failure back in it", got)
	}
	if n := len(*copied); n == 0 || (*copied)[n-1] != "two\n\nthree" {
		t.Fatalf("clipboard writes = %q, want both unsent texts, oldest first", *copied)
	}
	if !strings.Contains(toast, "Send failed: boom") || !strings.Contains(toast, "2 unsent messages are on the clipboard") {
		t.Errorf("toast = %q, want the failure and the count", toast)
	}
}

// A text sent again successfully, word for word, is recovered: it
// leaves the clipboard list, and the next failure no longer copies it.
func TestUnsentTextLeavesWhenSentAgain(t *testing.T) {
	a := typedApp(t)
	copied := clipboardLog(a)
	failSends(t, a, "one", "two", "three")
	a.compose.SetValue("")
	_, local := sendFromComposer(t, a, PanelMessages, "two")
	a.Update(MessageSentMsg{ChannelID: "C1", LocalTS: local, Message: messages.MessageItem{TS: "20.000000", UserID: "USELF", Text: "two"}})
	_, local = sendFromComposer(t, a, PanelMessages, "four")
	a.compose.SetValue("busy")
	_, cmd := a.Update(MessageSendFailedMsg{ChannelID: "C1", LocalTS: local, Reason: "boom"})
	toast := shownToast(t, a, cmd)
	if n := len(*copied); (*copied)[n-1] != "three\n\nfour" {
		t.Fatalf("clipboard = %q, want three and four only", (*copied)[n-1])
	}
	if !strings.Contains(toast, "2 unsent messages") {
		t.Errorf("toast = %q, want the count of two", toast)
	}
}

// A sent message that contains unsent texts recovers them: the user
// pasted the clipboard (or part of it) into a message. Sending the
// whole bundle empties the list, so the next failure copies only its
// own text.
func TestUnsentTextLeavesWhenASentMessageContainsIt(t *testing.T) {
	a := typedApp(t)
	copied := clipboardLog(a)
	failSends(t, a, "one", "two", "three")
	a.compose.SetValue("")
	bundle := "two\n\nthree"
	_, local := sendFromComposer(t, a, PanelMessages, "fyi: "+bundle)
	a.Update(MessageSentMsg{ChannelID: "C1", LocalTS: local, Message: messages.MessageItem{TS: "20.000000", UserID: "USELF", Text: bundle}})
	_, local = sendFromComposer(t, a, PanelMessages, "four")
	a.compose.SetValue("busy")
	_, cmd := a.Update(MessageSendFailedMsg{ChannelID: "C1", LocalTS: local, Reason: "boom"})
	toast := shownToast(t, a, cmd)
	if n := len(*copied); (*copied)[n-1] != "four" {
		t.Fatalf("clipboard = %q, want four only", (*copied)[n-1])
	}
	if !strings.Contains(toast, "Your text is on the clipboard") {
		t.Errorf("toast = %q, want the single-text note", toast)
	}
}

// The list is capped; the oldest go first, and the toast says some
// were dropped.
func TestUnsentTextsAreCapped(t *testing.T) {
	a := typedApp(t)
	copied := clipboardLog(a)
	texts := make([]string, unsentLimit+3)
	for i := range texts {
		texts[i] = fmt.Sprintf("text %d", i)
	}
	// texts[0] goes back to the composer; the rest to the clipboard.
	toast := failSends(t, a, texts...)
	got := strings.Split((*copied)[len(*copied)-1], "\n\n")
	if len(got) != unsentLimit || got[0] != texts[3] || got[len(got)-1] != texts[len(texts)-1] {
		t.Fatalf("clipboard holds %d texts from %q to %q, want the newest %d", len(got), got[0], got[len(got)-1], unsentLimit)
	}
	if !strings.Contains(toast, fmt.Sprintf("%d unsent messages", unsentLimit)) || !strings.Contains(toast, "2 older") {
		t.Errorf("toast = %q, want the count and that 2 older ones were dropped", toast)
	}
}

// An edit of a message still sending is refused. Its text goes back to
// the composer, or to the clipboard when that is taken, and then the
// toast says so, as for any failed edit.
func TestPlaceholderEditRefusalSaysWhereTheTextWent(t *testing.T) {
	a := typedApp(t)
	copied := clipboardLog(a)
	a.compose.SetValue("newer draft")
	cmd := a.beginEdit(editRequest{ChannelID: "C1", TS: "local:1", NewText: "edited", Typed: "edited", TeamID: "T1"})
	toast := shownToast(t, a, cmd)
	if len(*copied) != 1 || (*copied)[0] != "edited" {
		t.Fatalf("clipboard = %q, want the edit's text", *copied)
	}
	if !strings.Contains(toast, "sending") || !strings.Contains(toast, "clipboard") {
		t.Errorf("toast = %q, want the refusal and that the text is on the clipboard", toast)
	}
}
