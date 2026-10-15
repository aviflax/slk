package slackfmt

import (
	"strings"
	"testing"
	"unicode/utf8"
)

func TestMentionedChannelIDs(t *testing.T) {
	got := MentionedChannelIDs("see <#C0EXAMPLE01|general> and <#G0EXAMPLE02|secret>, not <#bad> or <@U1>")
	want := []string{"C0EXAMPLE01", "G0EXAMPLE02"}
	if strings.Join(got, ",") != strings.Join(want, ",") {
		t.Errorf("MentionedChannelIDs = %v, want %v", got, want)
	}
	if got := MentionedChannelIDs("no mentions"); len(got) != 0 {
		t.Errorf("MentionedChannelIDs(no mentions) = %v, want empty", got)
	}
}

func TestMentionedChannels(t *testing.T) {
	got := MentionedChannels("see <#C0EXAMPLE01|general> and <#G0EXAMPLE02|secret>, not <#bad> or <@U1>")
	want := []ChannelMention{{ID: "C0EXAMPLE01", Label: "general"}, {ID: "G0EXAMPLE02", Label: "secret"}}
	if len(got) != len(want) || got[0] != want[0] || got[1] != want[1] {
		t.Errorf("MentionedChannels = %v, want %v", got, want)
	}
	if got := MentionedChannels("no mentions"); len(got) != 0 {
		t.Errorf("MentionedChannels(no mentions) = %v, want empty", got)
	}
}

func TestStripMarkup_ResolvesAndStrips(t *testing.T) {
	names := map[string]string{"U1": "alice"}
	got := StripMarkup("<@U1> hi <#C1|general> *bold* <!here> <https://x.io|link>", names)
	for _, want := range []string{"@alice", "#general", "bold", "@here", "link"} {
		if !strings.Contains(got, want) {
			t.Fatalf("want %q in %q", want, got)
		}
	}
	if strings.ContainsAny(got, "*`~") {
		t.Fatalf("emphasis punctuation should be stripped: %q", got)
	}
}

// StripMarkup applies NO length cap (the card/notification clip
// themselves) and must never slice a multibyte body mid-rune.
func TestStripMarkup_NoTruncationAndUTF8Safe(t *testing.T) {
	long := strings.Repeat("あ", 500) // 1500 bytes, 500 runes
	got := StripMarkup(long, nil)
	if got != long {
		t.Fatalf("StripMarkup must not truncate; len(got)=%d want %d", len([]rune(got)), 500)
	}
	if !utf8.ValidString(got) {
		t.Fatalf("StripMarkup produced invalid UTF-8")
	}
}

func TestMentionedUserIDs(t *testing.T) {
	got := MentionedUserIDs("hi <@U1>, <@W2X> and <@U1> again; not <#C1|c> or <!here>")
	want := []string{"U1", "W2X", "U1"}
	if len(got) != len(want) {
		t.Fatalf("MentionedUserIDs = %v; want %v", got, want)
	}
	for i := range want {
		if got[i] != want[i] {
			t.Fatalf("MentionedUserIDs = %v; want %v", got, want)
		}
	}
	if got := MentionedUserIDs("none"); len(got) != 0 {
		t.Errorf("MentionedUserIDs(none) = %v; want empty", got)
	}
}

// An empty label shows the URL alone, as Slack draws it.
func TestStripMarkup_EmptyLabelLink(t *testing.T) {
	if got := StripMarkup("<https://x.io/|>", nil); got != "https://x.io/" {
		t.Fatalf("StripMarkup = %q, want the URL alone", got)
	}
}
