package ui

import (
	"strings"
	"testing"

	tea "charm.land/bubbletea/v2"

	"github.com/gammons/slk/internal/ui/messages"
)

// placeholderApp is an App on C1 whose newest row is the placeholder of
// a send still waiting for Slack, selected in the focused pane given.
func placeholderApp(t *testing.T, panel Panel) *App {
	t.Helper()
	a := newTestApp(t, withActiveTeam("T1"), withActiveChannel("C1"),
		withMessages(messages.MessageItem{TS: "1.000000", UserID: "USELF", Text: "sent earlier"}))
	a.SetCurrentUserID("USELF")
	if panel == PanelThread {
		openThread(a, "1.000000")
		a.Update(SendThreadReplyMsg{ChannelID: "C1", ThreadTS: "1.000000", Text: "pending"})
		a.focusedPanel = PanelThread
		if r, ok := a.threadPanel.SelectedMessage(); !ok || !isLocalTS(r.TS) {
			t.Fatalf("selected reply = %+v, want the placeholder", r)
		}
		return a
	}
	a.Update(SendMessageMsg{ChannelID: "C1", Text: "pending"})
	a.focusedPanel = PanelMessages
	if m, ok := a.messagepane.SelectedMessage(); !ok || !isLocalTS(m.TS) {
		t.Fatalf("selected message = %+v, want the placeholder", m)
	}
	return a
}

// Every action that would send the selected message's TS to Slack, or
// record it, refuses a placeholder: the TS is slk's own until the post
// returns. The key does nothing but say so.
func TestActionsRefusePlaceholder(t *testing.T) {
	keys := map[string]tea.KeyMsg{
		"open thread":     keyCode(tea.KeyEnter),
		"react":           keyPress('r'),
		"reaction nav":    keyPress('R'),
		"list reactions":  keyPress('L'),
		"edit":            keyPress('E'),
		"delete":          keyPress('D'),
		"copy permalink":  keyPress('Y'),
		"copy permalink2": keyPress('C'),
		"forward":         keyPress('F'),
		"mark unread":     keyPress('U'),
		"save thread":     keyPress('S'),
	}
	for _, panel := range []Panel{PanelMessages, PanelThread} {
		for name, k := range keys {
			if panel == PanelThread && name == "open thread" {
				continue // Enter in the thread pane opens nothing.
			}
			t.Run(panelName(panel)+"/"+name, func(t *testing.T) {
				a := placeholderApp(t, panel)
				a.Update(k)
				if got := statusbarText(a); !strings.Contains(got, stillSendingText) {
					t.Errorf("status bar = %q, want %q", got, stillSendingText)
				}
				if a.mode != ModeNormal || a.editing.IsActive() || a.pendingForward != nil ||
					(panel == PanelMessages && a.threadVisible) {
					t.Errorf("mode=%v editing=%v forward=%v thread=%v: the key must do nothing else",
						a.mode, a.editing.IsActive(), a.pendingForward != nil, a.threadVisible)
				}
			})
		}
	}
}

// Setting a mark on a placeholder is refused rather than recording a TS
// that will never resolve.
func TestSetMarkRefusesPlaceholder(t *testing.T) {
	a := placeholderApp(t, PanelMessages)
	a.Update(keyPress('m'))
	a.Update(keyPress('a'))
	if got := statusbarText(a); !strings.Contains(got, stillSendingText) {
		t.Errorf("status bar = %q, want %q", got, stillSendingText)
	}
	if marks, _ := a.marks.List(a.activeTeamID); len(marks) != 0 {
		t.Errorf("marks = %+v, want none recorded on the placeholder", marks)
	}
}

// Clicking a placeholder to open its thread is refused like Enter.
func TestOpenThreadClickRefusesPlaceholder(t *testing.T) {
	a := placeholderApp(t, PanelMessages)
	a.openThreadForSelectedMessage()
	if a.threadVisible {
		t.Error("a thread was opened on the placeholder")
	}
	if got := statusbarText(a); !strings.Contains(got, stillSendingText) {
		t.Errorf("status bar = %q, want %q", got, stillSendingText)
	}
}

func panelName(p Panel) string {
	if p == PanelThread {
		return "thread"
	}
	return "channel"
}
