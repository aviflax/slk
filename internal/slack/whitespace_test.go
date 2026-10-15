package slackclient

import (
	"context"
	"encoding/json"
	"net/url"
	"testing"
)

// sendWhitespaceCases is typed text whose line breaks, blank lines and
// indentation must reach Slack unchanged on every send path. text is
// the wire `text` field; section is the text of the single
// rich_text_section the blocks carry. The converter's full contract is
// TestConvert_PreservesWhitespace (internal/slack/mrkdwn); these check
// that each request carries it.
var sendWhitespaceCases = []struct {
	name    string
	in      string
	text    string
	section string
}{
	{"line break", "first\nsecond", "first\nsecond", "first\nsecond"},
	{"blank line", "para one\n\npara two", "para one\n\npara two", "para one\n\npara two"},
	{"two blank lines", "a\n\n\nb", "a\n\n\nb", "a\n\n\nb"},
	{"spaces and indentation", "a   b\n  two\n    four", "a   b\n  two\n    four", "a   b\n  two\n    four"},
}

// sectionText decodes form's blocks and returns the text of its one
// rich_text_section, failing unless that is the whole shape.
func sectionText(t *testing.T, form url.Values) string {
	t.Helper()
	var blocks []struct {
		Type     string `json:"type"`
		Elements []struct {
			Type     string `json:"type"`
			Elements []struct {
				Type string `json:"type"`
				Text string `json:"text"`
			} `json:"elements"`
		} `json:"elements"`
	}
	if err := json.Unmarshal([]byte(form.Get("blocks")), &blocks); err != nil {
		t.Fatalf("decoding blocks %q: %v", form.Get("blocks"), err)
	}
	if len(blocks) != 1 || blocks[0].Type != "rich_text" || len(blocks[0].Elements) != 1 {
		t.Fatalf("blocks = %s, want one rich_text with one element", form.Get("blocks"))
	}
	sec := blocks[0].Elements[0]
	if sec.Type != "rich_text_section" || len(sec.Elements) != 1 || sec.Elements[0].Type != "text" {
		t.Fatalf("blocks = %s, want one section holding one text element", form.Get("blocks"))
	}
	return sec.Elements[0].Text
}

func TestSendPathsPreserveWhitespace(t *testing.T) {
	paths := []struct {
		name string
		send func(c *Client, text string) (string, error)
	}{
		{"new message", func(c *Client, text string) (string, error) {
			_, mr, err := c.SendMessage(context.Background(), "C1", text)
			return mr, err
		}},
		{"thread reply", func(c *Client, text string) (string, error) {
			_, mr, err := c.SendReply(context.Background(), "C1", "1700000000.000100", text, false)
			return mr, err
		}},
		{"edit", func(c *Client, text string) (string, error) {
			return c.EditMessage(context.Background(), "C1", "1700000000.000100", text)
		}},
	}
	for _, p := range paths {
		for _, tc := range sendWhitespaceCases {
			t.Run(p.name+"/"+tc.name, func(t *testing.T) {
				api, getForm, closeFn := newTestSlackAPI(t, `{"ok":true,"ts":"1700000000.000100","channel":"C1","text":""}`)
				defer closeFn()
				mr, err := p.send(&Client{api: api}, tc.in)
				if err != nil {
					t.Fatalf("send: %v", err)
				}
				if mr != tc.text {
					t.Errorf("returned mrkdwn = %q, want %q", mr, tc.text)
				}
				form := getForm()
				if got := form.Get("text"); got != tc.text {
					t.Errorf("wire text = %q, want %q", got, tc.text)
				}
				if got := sectionText(t, form); got != tc.section {
					t.Errorf("section text = %q, want %q", got, tc.section)
				}
			})
		}
	}
}

func TestCommandFormPreservesArgWhitespace(t *testing.T) {
	for _, tc := range sendWhitespaceCases {
		t.Run(tc.name, func(t *testing.T) {
			form, err := commandForm(CommandRequest{ChannelID: "C1", Command: "/remind", Args: tc.in}, "web-1")
			if err != nil {
				t.Fatalf("commandForm: %v", err)
			}
			if got := sectionText(t, form); got != tc.section {
				t.Errorf("section text = %q, want %q", got, tc.section)
			}
		})
	}
}
