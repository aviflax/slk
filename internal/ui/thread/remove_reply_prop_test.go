package thread

import (
	"fmt"
	"testing"

	"github.com/gammons/slk/internal/ui/messages"
	"pgregory.net/rapid"
)

// Rolling back an optimistic reply keeps the cursor on the message it
// was on: the parent stays selected, a reply above the removed one stays
// selected, and a reply below it keeps its identity although its index
// drops by one. Removing the selected reply leaves the cursor on a reply
// that exists, or on the parent when no reply is left.
func TestRemoveLocalSentReplyKeepsSelection(t *testing.T) {
	rapid.Check(t, func(t *rapid.T) {
		n := rapid.IntRange(1, 6).Draw(t, "replies")
		replies := make([]messages.MessageItem, n)
		for i := range replies {
			replies[i] = messages.MessageItem{TS: fmt.Sprintf("101.%06d", i), UserID: "U2", Text: "reply"}
		}
		removed := rapid.IntRange(0, n-1).Draw(t, "removed")
		replies[removed].TS = "local:1"

		m := New()
		m.SetThread(messages.MessageItem{TS: "100.000000", UserID: "U1", Text: "parent"}, replies, "C1", "100.000000")
		sel := rapid.IntRange(-1, n-1).Draw(t, "selected") // -1 is the parent
		m.selected = sel
		var want string
		switch {
		case sel == parentSelected:
			want = "100.000000"
		case sel != removed:
			want = replies[sel].TS
		}

		if !m.RemoveLocalSentReply("local:1") {
			t.Fatal("reply not removed")
		}
		if len(m.replies) == 0 {
			if m.selected != parentSelected {
				t.Fatalf("no replies left but selected = %d, want the parent", m.selected)
			}
			return
		}
		if m.selected != parentSelected && (m.selected < 0 || m.selected >= len(m.replies)) {
			t.Fatalf("selected %d out of range for %d replies", m.selected, len(m.replies))
		}
		if want == "" {
			return
		}
		got := "100.000000"
		if m.selected != parentSelected {
			got = m.replies[m.selected].TS
		}
		if got != want {
			t.Fatalf("selection moved from %s to %s", want, got)
		}
	})
}
