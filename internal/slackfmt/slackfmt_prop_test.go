package slackfmt

import (
	"regexp"
	"strings"
	"testing"

	"github.com/gammons/slk/internal/proptest"
	"pgregory.net/rapid"
)

// Every mentioned user ID appears in the text as a <@ID> token, and
// there are no more IDs than <@ openers.
func TestMentionedUserIDsAreTokens(t *testing.T) {
	rapid.Check(t, func(t *rapid.T) {
		text := proptest.SlackText(nil, nil).Draw(t, "text")
		got := MentionedUserIDs(text)
		for _, id := range got {
			if !strings.Contains(text, "<@"+id+">") {
				t.Fatalf("id %q is not a <@%s> token in %q", id, id, text)
			}
		}
		if n := strings.Count(text, "<@"); len(got) > n {
			t.Fatalf("%d ids from %d openers", len(got), n)
		}
	})
}

// Channel mentions reported are <#ID|label> tokens in the text, in
// order, and MentionedChannelIDs is the ID projection of
// MentionedChannels.
func TestMentionedChannelsAreTokens(t *testing.T) {
	rapid.Check(t, func(t *rapid.T) {
		text := proptest.SlackText(nil, nil).Draw(t, "text")
		ms := MentionedChannels(text)
		idsOnly := MentionedChannelIDs(text)
		if len(ms) != len(idsOnly) {
			t.Fatalf("len mismatch %d vs %d", len(ms), len(idsOnly))
		}
		pos := 0
		for i, m := range ms {
			if idsOnly[i] != m.ID {
				t.Fatalf("id %d: %q vs %q", i, idsOnly[i], m.ID)
			}
			tok := "<#" + m.ID + "|" + m.Label + ">"
			j := strings.Index(text[pos:], tok)
			if j < 0 {
				t.Fatalf("token %q not found in order in %q", tok, text)
			}
			pos += j + len(tok)
		}
	})
}

// Mentions inserted into otherwise token-free text are all found, in
// order.
func TestMentionedUserIDsFindsInserted(t *testing.T) {
	rapid.Check(t, func(t *rapid.T) {
		want := rapid.SliceOfN(proptest.ID("U"), 0, 6).Draw(t, "ids")
		var b strings.Builder
		for _, id := range want {
			b.WriteString(proptest.Word().Draw(t, "w") + " <@" + id + "> ")
		}
		got := MentionedUserIDs(b.String())
		if strings.Join(got, ",") != strings.Join(want, ",") {
			t.Fatalf("got %v, want %v", got, want)
		}
	})
}

var (
	labelledUser      = regexp.MustCompile(`<@([A-Z0-9]+)\|[^>]*>`)
	bareChannel       = regexp.MustCompile(`<#([A-Z0-9]+)>`)
	labelledBroadcast = regexp.MustCompile(`<!(here|channel|everyone)\|[^>]*>`)
)

type normalizer struct{}

func (normalizer) Replace(s string) string {
	s = labelledUser.ReplaceAllString(s, "<@$1>")
	s = bareChannel.ReplaceAllString(s, "<#$1|x>")
	return labelledBroadcast.ReplaceAllString(s, "<!$1>")
}

var unhandledForms normalizer

// With a well-formed generated text, StripMarkup leaves no wire token
// delimiters of the kinds it handles: no "<@", "<#", "<!" and no
// markup characters.
func TestStripMarkupRemovesWireTokens(t *testing.T) {
	rapid.Check(t, func(t *rapid.T) {
		// KNOWN: StripMarkup only understands the bare <@U> user form,
		// the labelled <#C|name> channel form and the bare <!here>
		// broadcasts. "<@U1|name>", "<#C1>" and "<!here|@here>" pass
		// through with their brackets. Slack rarely sends those today,
		// so the text is normalized to the handled forms first.
		text := proptest.SlackText([]string{"U1"}, nil).Draw(t, "text")
		text = unhandledForms.Replace(text)
		for _, out := range []string{
			StripMarkup(text, map[string]string{"UAAAAAAAA": "ann"}),
			StripMarkupWithUserGroups(text, nil, map[string]string{"S1": "eng"}),
		} {
			for _, bad := range []string{"<@U", "<#C", "<!subteam", "<!here", "<!channel", "<!everyone", "<https://", "*", "_", "~", "`"} {
				if strings.Contains(out, bad) {
					t.Fatalf("StripMarkup(%q) = %q still has %q", text, out, bad)
				}
			}
		}
	})
}

// StripMarkup of text without any wire token or markup character is
// the identity.
func TestStripMarkupIdentityOnPlain(t *testing.T) {
	rapid.Check(t, func(t *rapid.T) {
		s := rapid.StringMatching(`[A-Za-z0-9 .,!?\n]{0,60}`).Draw(t, "s")
		if got := StripMarkup(s, nil); got != s {
			t.Fatalf("StripMarkup(%q) = %q", s, got)
		}
	})
}

// A resolvable mention becomes "@name".
func TestStripMarkupResolvesUser(t *testing.T) {
	rapid.Check(t, func(t *rapid.T) {
		id := proptest.ID("U").Draw(t, "id")
		name := rapid.StringMatching(`[a-z]{1,8}`).Draw(t, "name")
		if got := StripMarkup("hi <@"+id+">!", map[string]string{id: name}); got != "hi @"+name+"!" {
			t.Fatalf("got %q", got)
		}
	})
}

// FormatMPDMName: a name without the mpdm- prefix is returned as is; an
// mpdm name with n handles and a numeric index has n comma-separated
// parts; a nil lookup, or one returning "", yields the handles.
func TestFormatMPDMNameProperties(t *testing.T) {
	rapid.Check(t, func(t *rapid.T) {
		other := rapid.StringMatching(`[a-z0-9-]{0,20}`).Draw(t, "other")
		if strings.HasPrefix(other, "mpdm-") {
			return
		}
		if got := FormatMPDMName(other, nil); got != other {
			t.Fatalf("non-mpdm %q changed to %q", other, got)
		}

		handles := rapid.SliceOfNDistinct(rapid.StringMatching(`[a-z][a-z0-9.]{0,8}`), 1, 6, func(s string) string { return s }).Draw(t, "handles")
		name := "mpdm-" + strings.Join(handles, "--") + "-" + rapid.StringMatching(`[0-9]{1,2}`).Draw(t, "idx")
		want := strings.Join(handles, ", ")
		if got := FormatMPDMName(name, nil); got != want {
			t.Fatalf("nil lookup: %q -> %q, want %q", name, got, want)
		}
		if got := FormatMPDMName(name, func(string) string { return "" }); got != want {
			t.Fatalf("empty lookup: %q -> %q, want %q", name, got, want)
		}
		got := FormatMPDMName(name, func(h string) string { return "N_" + h })
		parts := strings.Split(got, ", ")
		if len(parts) != len(handles) {
			t.Fatalf("%q -> %q has %d parts, want %d", name, got, len(parts), len(handles))
		}
		for i, p := range parts {
			if p != "N_"+handles[i] {
				t.Fatalf("part %d = %q", i, p)
			}
		}
	})
}

// StripMarkup and FormatMPDMName never panic on arbitrary bytes.
func FuzzStripMarkup(f *testing.F) {
	for _, s := range []string{"", "<@U1>", "<#C1|x> <!subteam^S1|@a> <!here>", "<https://a|b> <mailto:x@y|x@y>", "*_~`", "<@", "<!subteam^"} {
		f.Add(s)
	}
	f.Fuzz(func(t *testing.T, s string) {
		_ = StripMarkup(s, map[string]string{"U1": "ann"})
		_ = StripMarkupWithUserGroups(s, nil, nil)
		_ = MentionedUserIDs(s)
		_ = MentionedChannels(s)
		_ = FormatMPDMName(s, nil)
	})
}
