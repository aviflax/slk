package compose

import (
	"testing"

	tea "charm.land/bubbletea/v2"

	"github.com/gammons/slk/internal/ui/mentionpicker"
)

// A picker with no match draws nothing, so it must not take the keys
// the user aims at the composer: it reports inactive, and Enter, Tab
// or Esc go where they would with no picker. Typing on, or deleting
// back to a match, still filters it, so it shows again.
func TestHiddenPickerTakesNoKeys(t *testing.T) {
	pickers := []struct {
		name   string
		setup  func(m *Model)
		typed  string // shows the picker with a match
		miss   string // typed after: no match
		active func(m Model) bool
	}{
		{"mention", func(m *Model) {
			m.SetUsers([]mentionpicker.User{{ID: "U1", DisplayName: "Maya", Username: "maya"}})
		}, "x @Maya", " y", Model.IsMentionActive},
		{"channel", func(m *Model) { m.SetChannels(chans()) }, "x #gen", "zz", Model.IsChannelActive},
		{"emoji", func(m *Model) { m.SetEmojiEntries(sampleEmojiEntries()) }, "x :ro", "qqq", Model.IsEmojiActive},
	}
	for _, p := range pickers {
		m := New("general")
		m.SetWidth(80)
		p.setup(&m)
		m.Focus()
		m = typeText(m, p.typed)
		if !p.active(m) {
			t.Fatalf("%s: setup: no picker showing after %q", p.name, p.typed)
		}
		m = typeText(m, p.miss)
		if p.active(m) {
			t.Errorf("%s: picker reports active with no match after %q", p.name, p.typed+p.miss)
		}
		for range len(p.miss) {
			m, _ = m.Update(tea.KeyPressMsg{Code: tea.KeyBackspace})
		}
		if !p.active(m) {
			t.Errorf("%s: deleting back to %q did not show the picker again", p.name, p.typed)
		}
		if p.name == "emoji" {
			continue // the emoji picker reopens on any query character by design
		}
		m = typeText(m, p.miss)
		m, _ = m.Update(tea.KeyPressMsg{Code: tea.KeyEscape})
		m = typeText(m, p.miss)
		if p.active(m) {
			t.Errorf("%s: picker reopened after Esc closed it", p.name)
		}
	}
}
