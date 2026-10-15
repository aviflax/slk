package ui

import (
	"strings"
	"testing"

	tea "charm.land/bubbletea/v2"

	"github.com/gammons/slk/internal/core"
	"github.com/gammons/slk/internal/ids"
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
		a.beginReply(replyRequest{ChannelID: "C1", ThreadTS: "1.000000", Text: "pending"})
		a.focusedPanel = PanelThread
		if r, ok := a.threadPanel.SelectedMessage(); !ok || !messages.IsLocalTS(r.TS) {
			t.Fatalf("selected reply = %+v, want the placeholder", r)
		}
		return a
	}
	a.beginSend(sendRequest{ChannelID: "C1", Text: "pending"})
	a.focusedPanel = PanelMessages
	if m, ok := a.messagepane.SelectedMessage(); !ok || !messages.IsLocalTS(m.TS) {
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
	a := newTestApp(t, withSize(120, 30), withActiveChannel("C1"))
	a.SetCurrentUserID("USELF")
	a.beginSend(sendRequest{ChannelID: "C1", Text: "pending"})
	a.View() // lay out the pane so the click lands on the placeholder's row
	fetched := 0
	a.setThreadFetcherForTest(func(ids.ChannelID, ids.ThreadTS) core.Msg {
		fetched++
		return nil
	})
	// The placeholder's row: below the pane's header, with no date
	// separator above it (the placeholder carries no date).
	x, y := a.layout.sidebarEnd+2, 3
	a.Update(tea.MouseClickMsg{X: x, Y: y, Button: tea.MouseLeft})
	if m, ok := a.messagepane.SelectedMessage(); !ok || !messages.IsLocalTS(m.TS) {
		t.Fatalf("the click selected %+v, want the placeholder", m)
	}
	a.Update(tea.MouseReleaseMsg{X: x, Y: y, Button: tea.MouseLeft})
	if a.threadVisible || fetched != 0 {
		t.Errorf("threadVisible=%v fetches=%d: a thread was opened on the placeholder", a.threadVisible, fetched)
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
