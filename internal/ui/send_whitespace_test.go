package ui

import (
	"testing"

	tea "charm.land/bubbletea/v2"

	"github.com/gammons/slk/internal/ids"
)

type whitespaceSentMsg struct{ text string }

// Text typed and pasted into the composer, line breaks, blank lines,
// runs of spaces and indentation included, reaches the message
// service exactly as the composer holds it. (What the service does
// with it is TestSendPathsPreserveWhitespace in internal/slack.)
func TestComposerWhitespaceReachesSendUnchanged(t *testing.T) {
	a := newTestApp(t, append(insertOpts(), withActiveTeam("T1"), withMode(ModeInsert))...)
	a.focusedPanel = PanelMessages
	a.compose.SetDraftContext("T1", "C1", "")
	_ = a.compose.Focus() // an unfocused textarea swallows typing
	wireMessageSend(a, func(_ ids.ChannelID, text string) tea.Msg {
		return whitespaceSentMsg{text}
	})

	typeText := func(s string) {
		for _, r := range s {
			a.Update(keyPress(r))
		}
	}
	typeText("first  line")
	a.Update(keyMod(tea.KeyEnter, tea.ModShift))
	a.Update(keyMod('j', tea.ModCtrl))
	typeText("  after a blank")
	// A CRLF paste keeps one break per line, not two.
	a.Update(tea.PasteMsg{Content: "\r\n    pasted\r\n\r\nend"})

	want := "first  line\n\n  after a blank\n    pasted\n\nend"
	if got := a.compose.Value(); got != want {
		t.Fatalf("compose = %q, want %q", got, want)
	}

	sent := deliveredMsgs[SendMessageMsg](t, pressEnter(a))[0]
	if sent.Text != want {
		t.Errorf("SendMessageMsg.Text = %q, want %q", sent.Text, want)
	}
	_, cmd := a.Update(sent)
	got := deliveredMsgs[whitespaceSentMsg](t, cmd)[0]
	if got.text != want {
		t.Errorf("MessageService.Send text = %q, want %q", got.text, want)
	}
}
