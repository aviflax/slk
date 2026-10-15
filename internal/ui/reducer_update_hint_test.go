package ui

import (
	"strings"
	"testing"
	"time"

	"github.com/gammons/slk/internal/ui/statusbar"
)

const testUpdateHint = "slk 0.26.2 is available. Run: brew upgrade --cask slk"

func TestUpdateAvailableMsg_ShowsHintInStatusBar(t *testing.T) {
	a := newTestApp(t)
	_, cmd := a.Update(UpdateAvailableMsg{Hint: testUpdateHint})
	if got := statusbarText(a); !strings.Contains(got, testUpdateHint) {
		t.Errorf("status bar = %q, want it to contain the hint", got)
	}
	if cmd == nil {
		t.Error("want a clear tick so the hint expires")
	}
}

func TestUpdateAvailableMsg_NoteSurvivesToastExpiry(t *testing.T) {
	a := newTestApp(t, withSize(160, 30))
	a.Update(UpdateAvailableMsg{Version: "0.26.2", Hint: testUpdateHint})
	a.Update(statusbar.CopiedClearMsg{Seq: a.statusbar.ToastSeq()})
	got := statusbarText(a)
	if strings.Contains(got, testUpdateHint) {
		t.Errorf("toast should have cleared: %q", got)
	}
	if !strings.Contains(got, "↑ 0.26.2") {
		t.Errorf("status bar = %q, want the persistent note", got)
	}
}

func TestUpdateAvailableMsg_EmptyHintShowsNothing(t *testing.T) {
	a := newTestApp(t)
	_, cmd := a.Update(UpdateAvailableMsg{})
	if cmd != nil {
		t.Error("an empty hint must not schedule a clear")
	}
	if got := statusbarText(a); strings.Contains(got, "available") {
		t.Errorf("status bar = %q", got)
	}
}

func TestUpdateAvailableMsg_OutlivesOrdinaryToast(t *testing.T) {
	if updateHintDuration <= 3*time.Second {
		t.Errorf("updateHintDuration = %v, want longer than an ordinary toast", updateHintDuration)
	}
	// A stale ordinary-toast tick must not clear the hint.
	a := newTestApp(t)
	old := toastWithClear(a, "earlier", 10*time.Millisecond)
	a.Update(UpdateAvailableMsg{Hint: testUpdateHint})
	for _, m := range deliveredMsgs[statusbar.CopiedClearMsg](t, old) {
		a.Update(m)
	}
	if got := statusbarText(a); !strings.Contains(got, testUpdateHint) {
		t.Errorf("status bar = %q, hint was cleared by a stale tick", got)
	}
}
