package sidebar

import (
	"strings"
	"testing"

	"github.com/charmbracelet/x/ansi"
)

// labelColumn returns the display column at which label starts on the
// first rendered line containing it.
func labelColumn(t *testing.T, view, label string) int {
	t.Helper()
	for _, line := range strings.Split(view, "\n") {
		plain := ansi.Strip(line)
		if i := strings.Index(plain, label); i >= 0 {
			return ansi.StringWidth(plain[:i])
		}
	}
	t.Fatalf("%q not rendered:\n%s", label, ansi.Strip(view))
	return -1
}

// While Activity is the displayed view, the channel last viewed must not
// draw the ▌ marker: with the cursor on Activity it would read as a
// second cursor.
func TestActiveChannelMarkerHiddenWhileActivityActive(t *testing.T) {
	m := New([]ChannelItem{{ID: "D1", Name: "alice", Type: "dm"}})
	m.SetActiveChannelID("D1")
	rowMarked := func() bool {
		for _, line := range strings.Split(m.View(20, 30), "\n") {
			plain := ansi.Strip(line)
			if strings.Contains(plain, "alice") && strings.Contains(plain, "▌") {
				return true
			}
		}
		return false
	}
	m.SelectActivityRow()
	if !rowMarked() {
		t.Fatal("setup: the active channel should be marked while its view is shown")
	}
	m.SetActivityActive(true)
	if rowMarked() {
		t.Error("active channel still carries ▌ while Activity is the active view")
	}
}

// "Activity" and "Threads" start in the same column whichever row the
// cursor is on. A two-column glyph (the 🔔 emoji) pushed Activity one
// column right of Threads.
func TestActivityRowAlignsWithThreadsRow(t *testing.T) {
	m := New([]ChannelItem{{ID: "C1", Name: "general", Type: "channel"}})
	for _, where := range []string{"on Threads", "on Activity", "on a section header"} {
		view := m.View(20, 30)
		if th, ac := labelColumn(t, view, "Threads"), labelColumn(t, view, "Activity"); th != ac {
			t.Errorf("cursor %s: Threads starts at column %d, Activity at %d", where, th, ac)
		}
		m.MoveDown()
	}
}
