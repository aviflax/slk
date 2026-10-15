package thread

import (
	"fmt"
	"image/color"
	"strings"
	"testing"

	"github.com/charmbracelet/x/ansi"
	"github.com/gammons/slk/internal/ui/messages"
	"github.com/gammons/slk/internal/ui/styles"
)

// sgrParams is c as a 24-bit SGR parameter list, fg (38) or bg (48).
func sgrParams(c color.Color, code string) string {
	r, g, b, _ := c.RGBA()
	return fmt.Sprintf("%s;2;%d;%d;%d", code, r>>8, g>>8, b>>8)
}

// TestParentMentionIsMarked checks the thread parent, which is drawn
// outside WrapRows, gets the mention bar and tint when it is not selected.
func TestParentMentionIsMarked(t *testing.T) {
	m := New()
	m.SetCurrentUser("U9")
	m.SetUserNames(map[string]string{"U9": "kira"})
	parent := messages.MessageItem{TS: "100.0", UserID: "U2", UserName: "dave", Text: "hey <@U9> look", Timestamp: "3:04 PM"}
	reply := messages.MessageItem{TS: "101.0", UserID: "U3", UserName: "ana", Text: "on it", Timestamp: "3:05 PM"}
	m.SetThread(parent, []messages.MessageItem{reply}, "C1", "100.0")
	if m.selected == parentSelected {
		t.Fatal("fixture: the parent is selected; the test needs it unselected")
	}

	bar := sgrParams(styles.MentionBar, "38")
	tint := sgrParams(styles.MentionBg, "48")
	var found bool
	for _, line := range strings.Split(m.View(20, 40), "\n") {
		plain := ansi.Strip(line)
		if !strings.Contains(plain, "@kira") {
			continue
		}
		found = true
		if !strings.Contains(line, bar) || !strings.Contains(line, tint) {
			t.Errorf("parent row lacks the mention bar or tint: %q", line)
		}
	}
	if !found {
		t.Fatal("parent row with @kira not drawn")
	}
}
