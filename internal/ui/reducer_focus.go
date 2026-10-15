// internal/ui/reducer_focus.go
//
// Terminal-focus reducer for App.Update.
//
// Owns:
//
//	tea.FocusMsg - the terminal running slk gained focus.
//	tea.BlurMsg  - the terminal running slk lost focus.
//	markFlushMsg - the debounced read-cursor flush tick.
//	markShownCheckMsg - is a message whose mark is held for
//	               visibility on screen yet?
//
// The first two are emitted only once App.View sets ReportFocus, and
// only by terminals that support focus reporting. The mark messages
// live here because whether a staged mark may be issued is a question
// of what the user can see: whether slk's terminal has focus, and
// whether the message is on screen in it.
//
// Why the App tracks this at all: a channel being *selected* does not
// mean the user can see it. slk may be sitting in a background
// terminal, an inactive tmux window, or an unfocused tmux pane with
// the same channel still selected. Read-marking that follows selection
// alone would diverge from Slack's own read state and suppress mobile
// notifications for messages the user never saw.
package ui

import (
	tea "charm.land/bubbletea/v2"
)

// markFlushMsg fires after the mark debounce interval; see
// App.scheduleMarkFlush.
type markFlushMsg struct{}

// markShownCheckMsg asks whether a message whose read mark is held for
// visibility (it landed below the fold) is on screen now. It is sent
// as a command's result so that it arrives after the frame of the
// message that sent it: bubbletea draws a frame after every Update, and
// the panes answer from their last frame (App.releaseShownMarks).
// App.update sends one after each key, mouse event, resize and scroll
// flush while a mark is held; the arrival that holds one sends one too.
//
// notify is set by a focused arrival: the message's unread state is
// shown only if this check finds it still off screen, so a message that
// did land in the window does not flash the unread dot.
type markShownCheckMsg struct{ notify bool }

var reduceFocus reducerFunc = func(a *App, msg tea.Msg) (tea.Cmd, bool) {
	switch m := msg.(type) {
	case tea.FocusMsg:
		a.terminalFocused = true
		a.focusEverReported = true
		// Not gated on autoMarkArmed: reaching this arm is itself the
		// proof that arming waits for, and the user is by now looking
		// at the channel. Catch-up — marks staged while blurred, or
		// staged inside tmux before arming, go out now. Without this,
		// a user who alt-tabs back, reads everything, and never
		// switches channels leaves the channel unread on Slack.
		return a.flushPendingMarks(), true

	case tea.BlurMsg:
		a.terminalFocused = false
		// A blur proves focus reporting is wired up just as well as a
		// focus does, and inside tmux it is the likelier first event.
		a.focusEverReported = true
		return nil, true

	case markShownCheckMsg:
		if a.releaseShownMarks() {
			return a.scheduleMarkFlush(), true
		}
		if m.notify && a.marksHeld() {
			a.notifyReadStateChanged()
		}
		return nil, true

	case markFlushMsg:
		a.markFlushScheduled = false
		if !a.terminalFocused {
			// Blurred between scheduling and firing: keep the slots
			// staged rather than dropping them. The FocusMsg arm
			// above issues them when the user comes back.
			return nil, true
		}
		return a.flushPendingMarks(), true
	}
	return nil, false
}
