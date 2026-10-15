package sidebar

import (
	"strings"
	"testing"

	"charm.land/lipgloss/v2"
)

// A section emoji that resolves to more than one codepoint (base +
// VS16, ZWJ, ...) draws at a width terminals disagree with lipgloss
// about, shifting everything to its right. The header must fall back to
// the :shortcode: text, as message bodies do (gammons/slk#301).
func TestSectionHeader_MultiCodepointEmojiFallsBackToShortcode(t *testing.T) {
	cases := []struct {
		emoji string
		want  string
	}{
		{"hammer_and_wrench", ":hammer_and_wrench: Tools"},
		{"mountain", ":mountain: Tools"},
		{"hammer", "\U0001F528  Tools"},
	}
	for _, c := range cases {
		provider := &fakeProvider{
			ready:    true,
			sections: []SectionMeta{{ID: "A", Name: "Tools", Type: "standard", Emoji: c.emoji}},
		}
		m := New([]ChannelItem{{ID: "C1", Name: "x", Type: "channel", Section: "A"}})
		m.SetSectionsProvider(provider)
		normal, _ := m.renderSectionHeaderLabel("A", "", lipgloss.NewStyle(), "")
		if !strings.Contains(normal, c.want) {
			t.Errorf("emoji %q: label %q does not contain %q", c.emoji, normal, c.want)
		}
	}
}
