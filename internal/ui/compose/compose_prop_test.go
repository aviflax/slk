package compose

import (
	"fmt"
	"slices"
	"strings"
	"testing"

	"github.com/gammons/slk/internal/core"
	"pgregory.net/rapid"
)

// refDraft is the reference model's idea of one conversation's draft.
type refDraft struct {
	text  string
	files []string
}

func (d refDraft) empty() bool { return d.text == "" && len(d.files) == 0 }

func fileNames(as []core.PendingAttachment) []string {
	var out []string
	for _, a := range as {
		out = append(out, a.Filename)
	}
	return out
}

// Drafts follow a per-key reference model through any sequence of typing,
// attaching, switching conversations (including detaching with an empty
// channel and the unknown-team rebind) and OfferDraft: switching away and
// back restores text and attachments exactly, keys never leak into each
// other, a detach saves and clears, and OfferDraft never overwrites a
// non-empty draft and says so.
func TestDraftsMatchReferenceModel(t *testing.T) {
	teams := []string{"", "T1", "T2"}
	channels := []string{"", "C1", "C2"}
	threads := []string{"", "1.1"}
	keyGen := rapid.Custom(func(t *rapid.T) draftKey {
		return draftKey{
			teamID:    rapid.SampledFrom(teams).Draw(t, "team"),
			channelID: rapid.SampledFrom(channels).Draw(t, "channel"),
			threadTS:  rapid.SampledFrom(threads).Draw(t, "thread"),
		}
	})
	textGen := rapid.StringMatching(`[a-z ]{0,12}`)

	rapid.Check(t, func(t *rapid.T) {
		m := New("general")
		stored := map[draftKey]refDraft{}
		var active draftKey
		var visible refDraft
		n := 0

		check := func() {
			if got := m.Value(); got != visible.text {
				t.Fatalf("visible text = %q, want %q", got, visible.text)
			}
			if got := fileNames(m.Attachments()); !slices.Equal(got, visible.files) {
				t.Fatalf("visible attachments = %v, want %v", got, visible.files)
			}
		}

		t.Repeat(map[string]func(*rapid.T){
			"type": func(t *rapid.T) {
				visible.text = textGen.Draw(t, "text")
				m.SetValue(visible.text)
			},
			"attach": func(t *rapid.T) {
				n++
				name := fmt.Sprintf("f%d", n)
				visible.files = append(visible.files, name)
				m.AddAttachment(core.PendingAttachment{Filename: name})
			},
			"switch": func(t *rapid.T) {
				next := keyGen.Draw(t, "key")
				m.SetDraftContext(next.teamID, next.channelID, next.threadTS)
				if next.channelID == "" {
					next = draftKey{}
				}
				if next == active {
					check()
					return
				}
				if next.channelID != "" && next.channelID == active.channelID &&
					next.threadTS == active.threadTS && active.teamID == "" && next.teamID != "" {
					active = next // rebind in place: the draft stays visible
					check()
					return
				}
				if active.channelID != "" {
					if visible.empty() {
						delete(stored, active)
					} else {
						stored[active] = visible
					}
				}
				visible = refDraft{}
				if next.channelID != "" {
					if d, ok := stored[next]; ok {
						visible = d
						delete(stored, next)
					}
				}
				active = next
				check()
			},
			"offer": func(t *rapid.T) {
				k := keyGen.Draw(t, "key")
				text := textGen.Draw(t, "text")
				got := m.OfferDraft(k.teamID, k.channelID, k.threadTS, text)
				want := false
				switch {
				case k.channelID == "" || text == "":
				case k == active:
					if visible.empty() {
						visible = refDraft{text: text}
						want = true
					}
				default:
					if d := stored[k]; d.empty() {
						stored[k] = refDraft{text: text}
						want = true
					}
				}
				if got != want {
					t.Fatalf("OfferDraft(%v, %q) = %v, want %v", k, text, got, want)
				}
				check()
			},
		})
		check()
	})
}

// Attachments() returns a copy: changing it never changes the composer.
func TestAttachmentsReturnsCopy(t *testing.T) {
	m := New("general")
	m.AddAttachment(core.PendingAttachment{Filename: "a"})
	got := m.Attachments()
	got[0].Filename = "mutated"
	if m.Attachments()[0].Filename != "a" {
		t.Fatal("Attachments exposed its backing array")
	}
}

// translateMentions: text with no '@' or '#' is returned unchanged, and the
// result never depends on the order the maps are built in.
func TestTranslateMentionsNoTriggerUnchanged(t *testing.T) {
	rapid.Check(t, func(t *rapid.T) {
		names, channels, groups := genMaps(t)
		text := rapid.StringMatching(`[a-z <>|!^.,]{0,30}`).Draw(t, "text")
		if got := translateMentions(text, names, channels, groups); got != text {
			t.Fatalf("translateMentions(%q) = %q", text, got)
		}
	})
}

// KNOWN: translateMentions rewrites entry by entry, so a replacement can be
// re-matched by a later, shorter entry. With names {"bob": "U1", "U1": "U2"},
// "@bob @U1" becomes "<<@U2>> <@U2>". It needs a display name equal to
// another user's ID, so these generators keep names lowercase and IDs
// uppercase. A display name containing '>' or '|' ("a>b") is replaced
// correctly, since the wire token is built from the ID, not the name.
func genMaps(t *rapid.T) (names, channels, groups map[string]string) {
	word := rapid.StringMatching(`[a-z]{1,6}`)
	names = rapid.MapOfN(word, rapid.StringMatching(`U[A-Z0-9]{4}`), 0, 6).Draw(t, "names")
	channels = rapid.MapOfN(word, rapid.StringMatching(`C[A-Z0-9]{4}`), 0, 6).Draw(t, "channels")
	groups = rapid.MapOfN(rapid.StringMatching(`S[A-Z0-9]{4}`), word, 0, 4).Draw(t, "groups")
	return
}

// Rebuilding the same maps (Go randomizes iteration order per range)
// gives the same output every time.
func TestTranslateMentionsDeterministic(t *testing.T) {
	rapid.Check(t, func(t *rapid.T) {
		names, channels, groups := genMaps(t)
		var parts []string
		for k := range names {
			parts = append(parts, "@"+k)
		}
		for k := range channels {
			parts = append(parts, "#"+k)
		}
		for _, h := range groups {
			parts = append(parts, "@"+h)
		}
		parts = append(parts, "@here", "@everyone")
		text := strings.Join(rapid.SliceOf(rapid.SampledFrom(parts)).Draw(t, "mentions"), rapid.SampledFrom([]string{" ", ", ", "\n"}).Draw(t, "sep"))
		want := translateMentions(text, names, channels, groups)
		for range 20 {
			clone := func(m map[string]string) map[string]string {
				c := map[string]string{}
				for k, v := range m {
					c[k] = v
				}
				return c
			}
			if got := translateMentions(text, clone(names), clone(channels), clone(groups)); got != want {
				t.Fatalf("nondeterministic: %q vs %q", got, want)
			}
		}
	})
}

// A user or channel mentioned alone, at a word boundary, is replaced by
// its wire token, and a longer name beats a shorter one that is its prefix.
func TestTranslateMentionsLongestWins(t *testing.T) {
	rapid.Check(t, func(t *rapid.T) {
		short := rapid.StringMatching(`[a-z]{1,5}`).Draw(t, "short")
		long := short + rapid.StringMatching(`[a-z]{1,4}`).Draw(t, "ext")
		if long == "here" || long == "channel" || long == "everyone" || short == "here" {
			t.Skip()
		}
		names := map[string]string{short: "USHORT", long: "ULONG"}
		channels := map[string]string{short: "CSHORT", long: "CLONG"}
		for _, sep := range []string{"", " ", ".", "\n"} {
			if got, want := translateMentions("@"+long+sep, names, nil, nil), "<@ULONG>"+sep; got != want {
				t.Fatalf("user: %q, want %q", got, want)
			}
			if got, want := translateMentions("@"+short+sep, names, nil, nil), "<@USHORT>"+sep; got != want {
				t.Fatalf("user short: %q, want %q", got, want)
			}
			if got, want := translateMentions("#"+long+sep, nil, channels, nil), "<#CLONG|"+long+">"+sep; got != want {
				t.Fatalf("channel: %q, want %q", got, want)
			}
		}
	})
}

// A name that is only a prefix of what was typed is never translated:
// "@bob" must not match inside "@bobby".
func TestTranslateMentionsNeedsWordBoundary(t *testing.T) {
	rapid.Check(t, func(t *rapid.T) {
		name := rapid.StringMatching(`[a-z]{1,5}`).Draw(t, "name")
		if name == "here" {
			t.Skip()
		}
		tail := rapid.StringMatching(`[a-z0-9_-]{1,4}`).Draw(t, "tail")
		text := "@" + name + tail
		if got := translateMentions(text, map[string]string{name: "U1"}, nil, nil); got != text {
			t.Fatalf("translateMentions(%q) = %q", text, got)
		}
	})
}

func FuzzTranslateMentions(f *testing.F) {
	for _, s := range []string{"", "@bob", "hi @bob, see #general", "@a>b|c", "#x@y"} {
		f.Add(s)
	}
	f.Fuzz(func(t *testing.T, text string) {
		names := map[string]string{"bob": "U1", "a>b": "U2", "x|y": "U3"}
		channels := map[string]string{"general": "C1", "gen": "C2"}
		groups := map[string]string{"S1": "eng"}
		first := translateMentions(text, names, channels, groups)
		if second := translateMentions(text, names, channels, groups); second != first {
			t.Fatalf("nondeterministic")
		}
		if !strings.ContainsAny(text, "@#") && first != text {
			t.Fatalf("changed text without triggers: %q -> %q", text, first)
		}
	})
}
