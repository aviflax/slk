package messages

import (
	"strings"
	"testing"

	"github.com/charmbracelet/x/ansi"
)

// A received message's line breaks, blank lines and indentation are
// drawn as sent: the body rows match the text's lines one for one.
func TestRenderMessage_KeepsLineBreaksAndIndentation(t *testing.T) {
	text := "para one\n\npara two\n  two spaces\n    four spaces  then gap"
	item := MessageItem{TS: "1.0", UserID: "U1", UserName: "priya", Text: text, Timestamp: "3:40 PM"}
	m := New([]MessageItem{item}, "general")
	got := ansi.Strip(RenderMessage(item, 80, m.renderContext(), RenderOptions{}).Content)

	lines := strings.Split(got, "\n")
	start := -1
	for i, l := range lines {
		if strings.Contains(l, "para one") {
			start = i
			break
		}
	}
	if start < 0 {
		t.Fatalf("no \"para one\" row in:\n%s", got)
	}
	// The body's left edge is wherever "para one" starts.
	indent := strings.Index(lines[start], "para one")
	want := strings.Split(text, "\n")
	if len(lines) < start+len(want) {
		t.Fatalf("rendered %d rows from the body, want %d:\n%s", len(lines)-start, len(want), got)
	}
	for i, w := range want {
		row := lines[start+i]
		var body string
		if len(row) > indent {
			body = strings.TrimRight(row[indent:], " ")
		}
		if body != w {
			t.Errorf("body row %d = %q, want %q", i, body, w)
		}
	}
}
