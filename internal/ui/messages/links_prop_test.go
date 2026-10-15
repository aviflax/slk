package messages

import (
	"strings"
	"testing"

	"github.com/gammons/slk/internal/core/blocks"
	"github.com/gammons/slk/internal/proptest"
	"pgregory.net/rapid"
)

// linkText is Slack text with links drawn from a small pool, so the same
// URL recurs, bare and labelled.
func linkText() *rapid.Generator[string] {
	return rapid.Custom(func(t *rapid.T) string {
		pool := []string{"https://a.example/x", "https://a.example/xy", "http://b.example", "mailto:c@example.com"}
		var b strings.Builder
		for range rapid.IntRange(0, 8).Draw(t, "pieces") {
			url := rapid.SampledFrom(pool).Draw(t, "url")
			switch rapid.IntRange(0, 3).Draw(t, "kind") {
			case 0:
				b.WriteString("<" + url + ">")
			case 1:
				b.WriteString("<" + url + "|" + rapid.StringMatching(`[a-z ]{1,6}`).Draw(t, "label") + ">")
			default:
				b.WriteString(proptest.SlackText(nil, nil).Draw(t, "filler"))
			}
			b.WriteString(" ")
		}
		return b.String()
	})
}

// firstLinkAt is where url's first link token starts in text.
func firstLinkAt(text, url string) int {
	best := -1
	for _, tok := range []string{"<" + url + ">", "<" + url + "|"} {
		if i := strings.Index(text, tok); i >= 0 && (best < 0 || i < best) {
			best = i
		}
	}
	return best
}

// ExtractLinks returns each URL once, every URL a link token in the
// text, in the order their first tokens appear.
func TestExtractLinksUniqueAndOrdered(t *testing.T) {
	rapid.Check(t, func(t *rapid.T) {
		text := linkText().Draw(t, "text")
		links := ExtractLinks(text)
		seen := map[string]bool{}
		last := -1
		for _, l := range links {
			if seen[l.URL] {
				t.Fatalf("URL %q twice in %+v", l.URL, links)
			}
			seen[l.URL] = true
			at := firstLinkAt(text, l.URL)
			if at < 0 {
				t.Fatalf("URL %q is not a link token in %q", l.URL, text)
			}
			if at <= last {
				t.Fatalf("links out of order: %+v in %q", links, text)
			}
			last = at
		}
	})
}

// MessageLinks starts with exactly the text's links and adds only shared
// message permalinks not already among them.
func TestMessageLinksExtendsTextLinks(t *testing.T) {
	rapid.Check(t, func(t *rapid.T) {
		text := linkText().Draw(t, "text")
		permalink := "https://acme.slack.com/archives/C0123ABCD/p1700000000000100"
		if rapid.Bool().Draw(t, "inText") {
			text += " <" + permalink + ">"
		}
		var atts []blocks.LegacyAttachment
		for range rapid.IntRange(0, 3).Draw(t, "atts") {
			atts = append(atts, blocks.LegacyAttachment{
				FromURL:    rapid.SampledFrom([]string{"", permalink, "https://a.example/x", "https://news.example/story"}).Draw(t, "from"),
				AuthorName: rapid.SampledFrom([]string{"", "Ann"}).Draw(t, "author"),
			})
		}
		textLinks := ExtractLinks(text)
		all := MessageLinks(MessageItem{Text: text, LegacyAttachments: atts})
		if len(all) < len(textLinks) {
			t.Fatalf("MessageLinks dropped text links: %+v vs %+v", all, textLinks)
		}
		for i := range textLinks {
			if all[i] != textLinks[i] {
				t.Fatalf("link %d = %+v, want %+v", i, all[i], textLinks[i])
			}
		}
		seen := map[string]bool{}
		for _, l := range all {
			if seen[l.URL] {
				t.Fatalf("URL %q twice in %+v", l.URL, all)
			}
			seen[l.URL] = true
		}
		for _, l := range all[len(textLinks):] {
			if l.URL != permalink {
				t.Fatalf("added non-permalink %+v", l)
			}
		}
	})
}

// FuzzExtractLinks feeds arbitrary bytes, unbalanced '<' and '|'
// included: no panic, no repeated URL, and every URL is in the text.
func FuzzExtractLinks(f *testing.F) {
	for _, s := range []string{"", "<https://x>", "<https://x|y>", "<https://x|", "<<https://x>>", "<mailto:a@b|c|d>", "<https://x||>"} {
		f.Add(s)
	}
	f.Fuzz(func(t *testing.T, s string) {
		seen := map[string]bool{}
		for _, l := range ExtractLinks(s) {
			if seen[l.URL] || !strings.Contains(s, l.URL) || !strings.Contains(s, l.Label) {
				t.Fatalf("bad link %+v from %q", l, s)
			}
			seen[l.URL] = true
		}
	})
}
