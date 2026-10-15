package messages

import (
	"strings"
	"testing"

	"github.com/charmbracelet/x/ansi"
	"github.com/gammons/slk/internal/mention"
	"github.com/gammons/slk/internal/ui/styles"
)

func TestSelfMentionDrawnAsPill(t *testing.T) {
	names := map[string]string{"U1": "kira", "U2": "dave"}
	pill := bgANSIFor(styles.MentionPillBg)
	pillParams := bgSGRParams(pill)

	out := RenderSlackMarkdownWith("hey <@U1> and <@U2>", RenderSlackMarkdownOpts{UserNames: names, SelfUserID: "U1"})
	if got := ansi.Strip(out); !strings.Contains(got, "@kira") || !strings.Contains(got, "@dave") {
		t.Fatalf("mentions not rendered: %q", got)
	}
	before, after, ok := strings.Cut(out, "@kira")
	if !ok {
		t.Fatalf("@kira not contiguous in %q", out)
	}
	if last := strings.LastIndex(before, "\x1b["); last < 0 || !strings.Contains(before[last:], pillParams) {
		t.Errorf("@kira not drawn on the pill background %q: %q", pillParams, out)
	}
	if _, rest, _ := strings.Cut(after, "@dave"); strings.Contains(after[:len(after)-len(rest)], pillParams) {
		t.Errorf("@dave drawn on the pill background: %q", out)
	}
}

func TestNoPillWithoutSelfUserID(t *testing.T) {
	out := RenderSlackMarkdownWith("hey <@U1>", RenderSlackMarkdownOpts{UserNames: map[string]string{"U1": "kira"}})
	if strings.Contains(out, bgSGRParams(bgANSIFor(styles.MentionPillBg))) {
		t.Errorf("pill drawn with no self user: %q", out)
	}
}

// mentionRows renders msg in a channel pane whose current user is U1 and
// wraps it for the list.
func mentionRows(t *testing.T, msg MessageItem) (RenderedMessage, WrappedRows) {
	t.Helper()
	m := New([]MessageItem{msg}, "general")
	m.SetCurrentUser("U1")
	m.SetUserNames(map[string]string{"U1": "kira", "U2": "dave"})
	const width = 40
	r := RenderMessage(msg, width, m.renderContext(), RenderOptions{})
	return r, WrapRows(msg, r, width, NewWrapStyles(true))
}

func TestWrapRows_MentionMarks(t *testing.T) {
	bar := strings.TrimSuffix(strings.TrimPrefix(fgANSIFor(styles.MentionBar), "\x1b["), "m")
	tint := bgSGRParams(bgANSIFor(styles.MentionBg))
	plainRows, plain := mentionRows(t, MessageItem{TS: "1.0", UserID: "U2", UserName: "dave", Text: "deploy is green", Timestamp: "3:04 PM"})

	tests := []struct {
		name     string
		msg      MessageItem
		wantKind mention.Kind
		wantBar  bool
		wantTint bool
	}{
		{"direct", MessageItem{TS: "1.0", UserID: "U2", UserName: "dave", Text: "hey <@U1> look", Timestamp: "3:04 PM"}, mention.Direct, true, true},
		{"broadcast", MessageItem{TS: "1.0", UserID: "U2", UserName: "dave", Text: "<!here> deploying", Timestamp: "3:04 PM"}, mention.Broadcast, true, false},
		{"other user", MessageItem{TS: "1.0", UserID: "U2", UserName: "dave", Text: "hey <@U2>", Timestamp: "3:04 PM"}, mention.None, false, false},
		{"own message", MessageItem{TS: "1.0", UserID: "U1", UserName: "kira", Text: "<!here> <@U1> note to self", Timestamp: "3:04 PM"}, mention.None, false, false},
	}
	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			r, w := mentionRows(t, tt.msg)
			if r.Mention != tt.wantKind {
				t.Fatalf("Mention = %v, want %v", r.Mention, tt.wantKind)
			}
			for i, line := range w.Lines {
				border, _, _ := strings.Cut(line, "▌")
				if got := strings.Contains(border, bar); got != tt.wantBar {
					t.Errorf("row %d: mention bar = %v, want %v: %q", i, got, tt.wantBar, line)
				}
				if got := strings.Contains(line, tint); got != tt.wantTint {
					t.Errorf("row %d: mention tint = %v, want %v: %q", i, got, tt.wantTint, line)
				}
			}
			// The plain mirror and the selected variant are not marked.
			if len(w.Plain) != len(w.Lines) {
				t.Errorf("plain rows %d, lines %d", len(w.Plain), len(w.Lines))
			}
			for i, line := range w.Selected {
				if strings.Contains(line, tint) || strings.Contains(strings.SplitN(line, "▌", 2)[0], bar) {
					t.Errorf("selected row %d carries a mention mark: %q", i, line)
				}
			}
		})
	}
	if plainRows.Mention != mention.None || len(plain.Lines) == 0 {
		t.Fatalf("baseline message classified %v", plainRows.Mention)
	}
}

// TestWrapRows_DirectMentionTintReachesEveryCell checks that no theme
// background escape is left inside a directly mentioned row, so the tint
// has no dark patches behind inner spans (author, timestamp, the text
// after the pill).
func TestWrapRows_DirectMentionTintReachesEveryCell(t *testing.T) {
	_, w := mentionRows(t, MessageItem{TS: "1.0", UserID: "U2", UserName: "dave", Text: "hey <@U1> can you look at the migration before lunch", Timestamp: "3:04 PM"})
	themeBg := bgSGRParams(BgANSI())
	for i, line := range w.Lines {
		if strings.Contains(line, themeBg) {
			t.Errorf("row %d keeps the theme background: %q", i, line)
		}
	}
}
