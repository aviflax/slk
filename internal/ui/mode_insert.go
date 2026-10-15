// internal/ui/mode_insert.go
//
// Insert-mode key handler (Phase 5l).
//
// Insert mode routes keys to the active compose textarea (main
// compose or thread compose, depending on focusedPanel + thread
// visibility). It also owns:
//
//   - Any key with active upload -> "Upload in progress" toast (no
//     draft mutations or sends while an upload is in flight).
//   - Esc with active edit       -> close any open compose picker
//     first, else cancel the edit.
//   - Esc otherwise              -> close any open compose picker
//     first, else exit insert mode.
//   - Ctrl+V                     -> smartPaste (clipboard image /
//     file path / verbatim text).
//   - Ctrl+U                     -> clear compose (text +
//     attachments + uploading flag).
//   - Ctrl+O (thread compose)    -> toggle "also send to channel" for
//     the next thread reply.
//   - Ctrl+X                     -> edit the draft in $VISUAL/$EDITOR
//     (suspends the TUI; see editor.go).
//   - Ctrl+E / Ctrl+A            -> not handled here: they reach the
//     textarea, which moves to the end / start of the line.
//   - Up / Down on first/last line -> jump to start/end of textarea.
//   - Plain Enter                -> send (or commit edit, or upload-
//     then-send if attachments present).
//   - Shift+Enter / Ctrl+J       -> insert literal newline.
//   - Other keys                 -> forward to compose; throttled
//     typing-indicator emit on every
//     text keystroke.
//
// Compose-overlay pickers (emoji / @mention / #channel) get
// priority on Up/Down/Enter: when a picker is active, those keys
// go to the picker so the user can navigate / select.
package ui

import (
	"time"

	"charm.land/bubbles/v2/key"
	tea "charm.land/bubbletea/v2"
)

// leaveInsertMode is what Esc does once no picker is open: back to
// normal mode with both composers blurred (their drafts stay where they
// are), then a re-check for a queued action prompt.
func (a *App) leaveInsertMode() tea.Cmd {
	a.exitInsertMode()
	return a.tryOpenActionPrompt()
}

// exitInsertMode is leaveInsertMode without the action-prompt re-check,
// for a caller that has more to do before a dialog may open.
func (a *App) exitInsertMode() {
	a.SetMode(ModeNormal)
	a.compose.Blur()
	a.threadCompose.Blur()
}

func handleInsertMode(a *App, msg tea.KeyMsg) tea.Cmd {
	if a.compose.Uploading() || a.threadCompose.Uploading() {
		// An upload is in flight: no insert-mode key may mutate the
		// in-flight caption or send through the other composer. A
		// single early return covers every key (Esc included),
		// replacing the former Esc-only guard.
		return toastWithClear(a, "Upload in progress", 2*time.Second)
	}
	if a.editing.IsActive() && key.Matches(msg, a.keys.Escape) {
		// If a picker is active in the relevant compose, close it
		// instead of cancelling the edit.
		if a.editing.Panel() == PanelThread {
			if a.threadCompose.IsEmojiActive() {
				a.threadCompose.CloseEmoji()
				return nil
			}
			if a.threadCompose.IsMentionActive() {
				a.threadCompose.CloseMention()
				return nil
			}
			if a.threadCompose.IsChannelActive() {
				a.threadCompose.CloseChannel()
				return nil
			}
		} else {
			if a.compose.IsEmojiActive() {
				a.compose.CloseEmoji()
				return nil
			}
			if a.compose.IsMentionActive() {
				a.compose.CloseMention()
				return nil
			}
			if a.compose.IsChannelActive() {
				a.compose.CloseChannel()
				return nil
			}
		}
		a.cancelEdit()
		return nil
	}
	if key.Matches(msg, a.keys.Escape) {
		// If a picker is active, close it instead of exiting insert mode.
		if a.focusedPanel == PanelThread && a.threadVisible {
			if a.threadCompose.IsEmojiActive() {
				a.threadCompose.CloseEmoji()
				return nil
			}
			if a.threadCompose.IsMentionActive() {
				a.threadCompose.CloseMention()
				return nil
			}
			if a.threadCompose.IsChannelActive() {
				a.threadCompose.CloseChannel()
				return nil
			}
		} else {
			if a.compose.IsEmojiActive() {
				a.compose.CloseEmoji()
				return nil
			}
			if a.compose.IsMentionActive() {
				a.compose.CloseMention()
				return nil
			}
			if a.compose.IsChannelActive() {
				a.compose.CloseChannel()
				return nil
			}
		}
		return a.leaveInsertMode()
	}

	code := msg.Key().Code
	// Lock-state bits (NumLock/CapsLock) ride along in Mod on terminals
	// implementing the Kitty Keyboard Protocol, regardless of whether
	// they're relevant to the binding — strip them before comparing.
	mod := msg.Key().Mod &^ (tea.ModCapsLock | tea.ModNumLock | tea.ModScrollLock)
	isPaste := code == 'v' && mod == tea.ModCtrl
	if isPaste {
		return a.smartPaste()
	}
	if code == 'x' && mod == tea.ModCtrl {
		return a.openComposeInEditor()
	}

	// Insert-mode shortcuts that operate on the active compose:
	//   Ctrl+U  -> clear compose (text + attachments + uploading flag)
	//   Up      -> if cursor on first line, jump to start of textarea
	//   Down    -> if cursor on last line,  jump to end of textarea
	target := &a.compose
	if a.focusedPanel == PanelThread && a.threadVisible {
		target = &a.threadCompose
	}
	if code == 'u' && mod == tea.ModCtrl {
		target.Reset()
		return nil
	}
	// If a compose-overlay picker (emoji / @mention / #channel)
	// is active, let it own Up/Down so users can navigate the
	// suggestion list. Without this guard, the jump-to-start/end
	// shortcuts below swallow the arrow keys before the picker
	// ever sees them.
	pickerActive := target.IsEmojiActive() || target.IsMentionActive() || target.IsChannelActive()
	// Ctrl+O toggles Slack's "Also send to #channel" for the next
	// thread reply. Thread compose only -- the channel compose has no
	// broadcast concept. Skipped while a picker is active so picker
	// navigation keys keep precedence.
	if target == &a.threadCompose && !pickerActive && code == 'o' && mod == tea.ModCtrl {
		a.threadCompose.ToggleBroadcast()
		return nil
	}
	if !pickerActive {
		if code == tea.KeyUp && mod == 0 && target.CursorAtFirstLine() {
			target.MoveCursorToStart()
			return nil
		}
		if code == tea.KeyDown && mod == 0 && target.CursorAtLastLine() {
			target.MoveCursorToEnd()
			return nil
		}
	}
	// Plain Enter sends; Shift+Enter (and Ctrl+J as a fallback
	// for terminals that don't disambiguate modifiers) inserts a
	// newline. Alt+Enter sends with broadcast in thread compose
	// (one-shot send-with-broadcast without needing the Ctrl+O toggle).
	isAltEnter := code == tea.KeyEnter && mod.Contains(tea.ModAlt)
	isSend := code == tea.KeyEnter && !mod.Contains(tea.ModShift)
	isNewline := (code == tea.KeyEnter && mod.Contains(tea.ModShift)) ||
		(code == 'j' && mod == tea.ModCtrl)

	// Determine which compose box is active based on focused panel.
	if a.focusedPanel == PanelThread && a.threadVisible {
		// If a picker is active, forward all keys to compose
		// (including Enter).
		if a.threadCompose.IsEmojiActive() || a.threadCompose.IsMentionActive() || a.threadCompose.IsChannelActive() {
			var cmd tea.Cmd
			a.threadCompose, cmd = a.threadCompose.Update(msg)
			return cmd
		}

		// Thread reply compose.
		if isNewline {
			var cmd tea.Cmd
			a.threadCompose, cmd = a.threadCompose.Update(tea.KeyPressMsg{Code: tea.KeyEnter})
			return cmd
		}
		if isSend {
			if cmd, ok := a.trySlashCommand(PanelThread, &a.threadCompose, a.threadPanel.ChannelID(), a.threadPanel.ThreadTS()); ok {
				return cmd
			}
			if len(a.threadCompose.Attachments()) > 0 {
				cmd := a.submitWithAttachments(&a.threadCompose)
				if a.threadCompose.Uploading() {
					a.exitInsertAfterSend()
				}
				return cmd
			}
			if a.editing.IsActive() && a.editing.Panel() == PanelThread {
				return a.submitEdit(a.threadCompose.Value(), a.threadCompose.TranslateMentionsForSend(a.threadCompose.Value()))
			}
			typed := a.threadCompose.Value()
			if typed != "" {
				text := a.threadCompose.TranslateMentionsForSend(typed)
				broadcast := a.threadCompose.Broadcast() || isAltEnter
				a.threadCompose.Reset()
				threadTS := a.threadPanel.ThreadTS()
				channelID := a.threadPanel.ChannelID()
				teamID := a.activeTeamID
				a.exitInsertAfterSend()
				// Begun here, not through a message: the placeholder
				// is selected before the next key is read.
				send := a.beginReply(replyRequest{
					ChannelID: channelID,
					ThreadTS:  threadTS,
					Text:      text,
					Broadcast: broadcast,
					TeamID:    teamID,
					Typed:     typed,
				})
				return tea.Batch(send, a.tryOpenActionPrompt())
			}
			return nil
		}
		var cmd tea.Cmd
		a.threadCompose, cmd = a.threadCompose.Update(msg)
		a.typingOut.MaybeSend(a.threadPanel.ChannelID())
		return cmd
	}

	// Channel message compose.
	// If a picker is active, forward all keys to compose
	// (including Enter).
	if a.compose.IsEmojiActive() || a.compose.IsMentionActive() || a.compose.IsChannelActive() {
		var cmd tea.Cmd
		a.compose, cmd = a.compose.Update(msg)
		return cmd
	}

	if isNewline {
		var cmd tea.Cmd
		a.compose, cmd = a.compose.Update(tea.KeyPressMsg{Code: tea.KeyEnter})
		return cmd
	}
	if isSend {
		if cmd, ok := a.trySlashCommand(PanelMessages, &a.compose, a.activeChannelID, ""); ok {
			return cmd
		}
		if len(a.compose.Attachments()) > 0 {
			cmd := a.submitWithAttachments(&a.compose)
			if a.compose.Uploading() {
				a.exitInsertAfterSend()
			}
			return cmd
		}
		if a.editing.IsActive() && a.editing.Panel() == PanelMessages {
			return a.submitEdit(a.compose.Value(), a.compose.TranslateMentionsForSend(a.compose.Value()))
		}
		typed := a.compose.Value()
		if typed != "" {
			text := a.compose.TranslateMentionsForSend(typed)
			channelID, teamID := a.activeChannelID, a.activeTeamID
			a.compose.Reset()
			a.exitInsertAfterSend()
			// Begun here, not through a message: the placeholder is
			// selected before the next key is read, so a key pressed
			// straight after Enter cannot act on the message selected
			// before it.
			send := a.beginSend(sendRequest{
				ChannelID: channelID,
				Text:      text,
				TeamID:    teamID,
				Typed:     typed,
			})
			return tea.Batch(send, a.tryOpenActionPrompt())
		}
		return nil
	}

	var cmd tea.Cmd
	a.compose, cmd = a.compose.Update(msg)
	a.typingOut.MaybeSend(a.activeChannelID)
	return cmd
}
