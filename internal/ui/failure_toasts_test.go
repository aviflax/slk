package ui

import (
	"errors"
	"strings"
	"testing"
)

// A join that fails says so; the user pressed Enter on a channel and
// nothing else would tell them why it did not open.
func TestJoinFailureToasts(t *testing.T) {
	a := newTestApp(t)
	_, cmd := a.Update(ChannelJoinFailedMsg{ID: "C9", Name: "design", Err: errors.New("is_archived")})
	if toast := shownToast(t, a, cmd); !strings.Contains(toast, "#design") || !strings.Contains(toast, "is_archived") {
		t.Errorf("toast = %q, want the channel and the reason", toast)
	}
}

// Older history that failed to load clears the spinner and says so; an
// empty page (the start of the channel) says nothing.
func TestOlderHistoryFailureToasts(t *testing.T) {
	a := newTestApp(t, withActiveChannel("C1"))
	_, cmd := a.Update(OlderMessagesLoadedMsg{ChannelID: "C1", Err: errors.New("timeout")})
	if toast := shownToast(t, a, cmd); !strings.Contains(toast, "older messages") {
		t.Errorf("toast = %q, want the failure reported", toast)
	}
	b := newTestApp(t, withActiveChannel("C1"))
	_, cmd = b.Update(OlderMessagesLoadedMsg{ChannelID: "C1"})
	if toast := shownToast(t, b, cmd); strings.Contains(toast, "older messages") {
		t.Errorf("toast = %q for an empty page, want no failure", toast)
	}
}
