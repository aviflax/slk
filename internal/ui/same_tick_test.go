package ui

import (
	"strings"
	"testing"

	tea "charm.land/bubbletea/v2"

	"github.com/gammons/slk/internal/ui/messages"
)

// The user's optimistic actions take effect in the Update of the key
// that starts them, before any command runs: a key pressed straight
// after acts on what the action left on screen, never on the message
// selected before it. These tests press the keys back to back through
// Update and run none of the commands in between, which is what a fast
// typist gets from the runtime.

// sameTickApp is an App in insert mode on C1 whose three messages
// (1.0..3.0) are the current user's, with thread 5.000000 of C1 open on
// one reply of theirs.
func sameTickApp(t *testing.T) *App {
	t.Helper()
	a, _ := slashApp(t, nil)
	a.SetCurrentUserID("U1")
	a.threadPanel.SetThread(messages.MessageItem{TS: "5.000000", Text: "parent", UserID: "U1"},
		[]messages.MessageItem{{TS: "6.000000", ThreadTS: "5.000000", Text: "first reply", UserID: "U1"}},
		"C1", "5.000000")
	a.threadVisible = true
	a.threadCompose.SetDraftContext("T1", "C1", "5.000000")
	return a
}

// assertRefusedAsSending checks that the key pressed after Enter hit
// the placeholder: refused with the still-sending toast, nothing opened.
func assertRefusedAsSending(t *testing.T, a *App) {
	t.Helper()
	if a.mode == ModeConfirm || a.confirmPrompt.IsVisible() {
		t.Errorf("a delete prompt opened on the message selected before the send")
	}
	if a.editing.IsActive() {
		t.Errorf("an edit opened on %s, the message selected before the send", a.editing.TS())
	}
	if a.reactionPicker.IsVisible() {
		t.Errorf("the reaction picker opened on %s, the message selected before the send", a.reactionPicker.MessageTS())
	}
	if got := statusbarText(a); !strings.Contains(got, stillSendingText) {
		t.Errorf("status bar = %q, want the still-sending refusal", got)
	}
}

func TestKeyAfterEnter_ActsOnThePlaceholder(t *testing.T) {
	for _, k := range []struct {
		name string
		key  tea.KeyMsg
	}{{"delete", keyPress('D')}, {"edit", keyPress('E')}, {"react", keyPress('r')}} {
		t.Run("channel send then "+k.name, func(t *testing.T) {
			a := sameTickApp(t)
			a.focusedPanel = PanelMessages
			a.compose.SetValue("hi")
			a.Update(keyCode(tea.KeyEnter))
			a.Update(k.key)
			assertRefusedAsSending(t, a)
		})
		t.Run("thread reply then "+k.name, func(t *testing.T) {
			a := sameTickApp(t)
			a.focusedPanel = PanelThread
			a.threadCompose.SetValue("re")
			a.Update(keyCode(tea.KeyEnter))
			a.Update(k.key)
			assertRefusedAsSending(t, a)
		})
	}
}

// An edit shows at Enter, so editing the message again straight away
// starts from the edited text, not the text before it.
func TestKeyAfterEnter_EditShowsBeforeTheNextKey(t *testing.T) {
	for _, panel := range []Panel{PanelMessages, PanelThread} {
		a := sameTickApp(t)
		a.mode = ModeNormal
		a.focusedPanel = panel
		c := &a.compose
		if panel == PanelThread {
			c = &a.threadCompose
		}
		a.Update(keyPress('E'))
		if !a.editing.IsActive() {
			t.Fatalf("panel %v: E opened no edit", panel)
		}
		c.SetValue("changed")
		a.Update(keyCode(tea.KeyEnter))
		a.Update(keyPress('E'))
		if !a.editing.IsActive() {
			t.Fatalf("panel %v: second E opened no edit", panel)
		}
		if got := c.Value(); got != "changed" {
			t.Errorf("panel %v: second edit starts from %q, want the edit just made", panel, got)
		}
	}
}

// Confirming a delete takes the message off screen at once, so the next
// key cannot act on it again.
func TestKeyAfterConfirm_DeleteIsGone(t *testing.T) {
	a := sameTickApp(t)
	a.mode = ModeNormal
	a.focusedPanel = PanelMessages
	a.Update(keyPress('D'))
	if !a.confirmPrompt.IsVisible() {
		t.Fatal("D opened no prompt")
	}
	a.Update(keyPress('y'))
	wantRows(t, "channel", a.messagepane.Messages(), "1.0=msg-1", "2.0=msg-2")
	if !a.pendingOps.DeletePending("C1", "3.0") {
		t.Error("no delete pending for 3.0 after y")
	}
	a.Update(keyPress('D'))
	if !strings.Contains(a.confirmPrompt.View(), "msg-2") {
		t.Errorf("the next D prompted for %q, want msg-2, the message now selected", a.confirmPrompt.View())
	}
}
